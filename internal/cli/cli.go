package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"codex-gateway/internal/apikey"
	"codex-gateway/internal/buildinfo"
	"codex-gateway/internal/config"
	"codex-gateway/internal/oauth"
	"codex-gateway/internal/store"
	"codex-gateway/internal/token"
)

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	jsonOut, args := takeJSON(args)
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(stdout, helpText)
		return 0
	}
	if len(args) > 0 && (args[0] == "version" || args[0] == "--version") {
		fmt.Fprintln(stdout, buildinfo.Version)
		return 0
	}
	if len(args) > 0 && args[0] == "update" {
		if err := runUpdate(args[1:], stdout, jsonOut); err != nil {
			fmt.Fprintf(stderr, "error: %s\n", err.Error())
			return 1
		}
		return 0
	}
	if len(args) == 0 && (!terminalReady(stdin, stdout) || jsonOut) {
		fmt.Fprint(stdout, helpText)
		return 0
	}
	app, err := openApp(stdin, stdout, stderr, jsonOut)
	if err != nil {
		fmt.Fprintf(stderr, "error: %s\n", err.Error())
		return 1
	}
	defer app.store.Close()
	if len(args) == 0 {
		err = app.mainMenu()
	} else {
		err = app.dispatch(args)
	}
	if err != nil && !errors.Is(err, errMenuBack) {
		if app.jsonOut {
			_ = json.NewEncoder(stdout).Encode(map[string]string{"error": err.Error()})
		} else {
			fmt.Fprintf(stderr, "error: %s\n", err.Error())
		}
		return 1
	}
	return 0
}

type app struct {
	cfg     *config.Config
	store   *store.Store
	tokens  *token.Manager
	keys    *apikey.Service
	in      io.Reader
	out     io.Writer
	err     io.Writer
	termIn  *os.File
	termOut *os.File
	termOK  bool
	jsonOut bool
}

func openApp(stdin io.Reader, stdout, stderr io.Writer, jsonOut bool) (*app, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if cfg.GeneratedKey {
		fmt.Fprintf(stderr, "Generated master key file:\n%s\n\nKeep this file private. It cannot be recovered.\n\n", filepath.Join(cfg.DataDir, config.MasterKeyFile))
	}
	st, err := store.Open(cfg.DataDir, cfg.MasterKey)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Listen) == "" {
		stored, err := st.Meta(context.Background(), config.MetaListen)
		if err != nil {
			_ = st.Close()
			return nil, err
		}
		cfg.Listen = config.ResolveListen("", stored)
	}
	if err := config.ValidateListen(cfg.Listen); err != nil {
		_ = st.Close()
		return nil, err
	}
	termIn, termOut, termOK := terminalFiles(stdin, stdout)
	return &app{
		cfg:     cfg,
		store:   st,
		tokens:  token.NewManager(st, oauth.NewClient("")),
		keys:    apikey.NewService(st, cfg.MasterKey, nil),
		in:      stdin,
		out:     stdout,
		err:     stderr,
		termIn:  termIn,
		termOut: termOut,
		termOK:  termOK && !jsonOut,
		jsonOut: jsonOut,
	}, nil
}

func (a *app) dispatch(args []string) error {
	switch args[0] {
	case "auth":
		return a.auth(args[1:])
	case "key":
		return a.key(args[1:])
	case "server":
		return a.server(args[1:])
	case "config":
		return a.configCmd(args[1:])
	case "admin":
		return a.adminCmd(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (a *app) auth(args []string) error {
	if len(args) == 0 {
		if a.termOK {
			return a.authMenu()
		}
		return fmt.Errorf("usage: codex-gateway auth login|status|refresh|logout")
	}
	switch args[0] {
	case "login":
		return a.login(args[1:])
	case "status":
		return a.authStatus()
	case "refresh":
		return a.authRefresh()
	case "logout":
		return a.logout()
	default:
		return fmt.Errorf("unknown auth command %q", args[0])
	}
}

func (a *app) login(args []string) error {
	manual := false
	for _, arg := range args {
		switch arg {
		case "--manual":
			manual = true
		default:
			return fmt.Errorf("unknown flag %q", arg)
		}
	}
	if len(args) == 0 && a.termOK {
		picked, err := a.choose("로그인 방법", []string{"브라우저 콜백 기다리기", "콜백 주소 붙여넣기", "뒤로"})
		if err != nil || picked < 0 || picked == 2 {
			return err
		}
		manual = picked == 1
	}
	return a.loginWith(manual)
}

func (a *app) loginWith(manual bool) error {
	flow, authURL, err := oauth.StartFlow(a.tokensClient())
	if err != nil {
		return err
	}
	info := a.human()
	fmt.Fprintf(info, "Open this URL in your browser:\n\n%s\n\n", authURL)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var tok *oauth.Token
	if manual {
		fmt.Fprintln(info, "Paste the final callback URL and press Enter:")
		line, err := bufio.NewReader(a.in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		tok, err = flow.ExchangeCallback(ctx, line)
		if err != nil {
			return err
		}
	} else {
		fmt.Fprintf(info, "Waiting for OAuth callback on:\n%s\n\nIf this server is remote, run:\n\nssh -L 1455:127.0.0.1:1455 user@server\n\n", oauth.RedirectURI)
		tok, err = flow.WaitCallback(ctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("timed out waiting for the OAuth callback")
			}
			return err
		}
	}
	acc, err := a.tokens.SaveLogin(ctx, tok)
	if err != nil {
		return err
	}
	if acc.AccountID == "" {
		fmt.Fprintln(a.err, "Logged in, but the ID token did not include a ChatGPT account id. Codex requests need that id.")
	}
	return a.printAccount(acc, "Logged in.")
}

func (a *app) tokensClient() *oauth.Client {
	return oauth.NewClient("")
}

func (a *app) authStatus() error {
	acc, err := a.tokens.Account(context.Background())
	if err != nil {
		return err
	}
	if acc == nil {
		return a.printAccount(nil, "Not logged in.")
	}
	return a.printAccount(acc, "Signed in.")
}

func (a *app) authRefresh() error {
	acc, err := a.tokens.ForceRefresh(context.Background())
	if err != nil {
		return err
	}
	return a.printAccount(acc, "Token refreshed.")
}

func (a *app) logout() error {
	if err := a.tokens.Logout(context.Background()); err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(map[string]any{"logged_out": true})
	}
	fmt.Fprintln(a.out, "Logged out.")
	return nil
}

func (a *app) printAccount(acc *store.OAuthAccount, headline string) error {
	if acc == nil {
		if a.jsonOut {
			return a.printJSON(map[string]any{"logged_in": false})
		}
		fmt.Fprintln(a.out, headline)
		return nil
	}
	payload := map[string]any{
		"logged_in":       true,
		"status":          acc.Status,
		"email":           acc.Email,
		"account_id":      acc.AccountID,
		"user_id":         acc.UserID,
		"chatgpt_user_id": acc.ChatGPTUserID,
		"plan_type":       acc.PlanType,
		"organization_id": acc.OrganizationID,
		"expires_at":      acc.ExpiresAt.UTC().Format(time.RFC3339),
	}
	if a.jsonOut {
		return a.printJSON(payload)
	}
	fmt.Fprintln(a.out, headline)
	fmt.Fprintf(a.out, "Email: %s\nAccount: %s\nUser: %s\nPlan: %s\nStatus: %s\nAccess token expires at: %s\n",
		emptyDash(acc.Email), emptyDash(acc.AccountID), emptyDash(first(acc.UserID, acc.ChatGPTUserID)), emptyDash(acc.PlanType), acc.Status, acc.ExpiresAt.Local().Format(time.RFC3339))
	return nil
}

func (a *app) key(args []string) error {
	if len(args) == 0 {
		if a.termOK {
			return a.keyMenu()
		}
		return fmt.Errorf("usage: codex-gateway key create|list|revoke|rotate")
	}
	switch args[0] {
	case "create":
		name := "default"
		named := false
		for i := 1; i < len(args); i++ {
			if args[i] == "--name" && i+1 < len(args) {
				name = args[i+1]
				named = true
				i++
				continue
			}
			return fmt.Errorf("unknown flag %q", args[i])
		}
		if !named && a.termOK {
			entered, err := a.prompt("이름", "default")
			if err != nil {
				return err
			}
			name = entered
		}
		created, err := a.keys.Create(context.Background(), name)
		if err != nil {
			return err
		}
		if a.jsonOut {
			return a.printJSON(map[string]string{
				"id":     created.ID,
				"name":   created.Name,
				"prefix": created.Prefix,
				"key":    created.Key,
			})
		}
		fmt.Fprintf(a.out, "API key created.\n\nName: %s\nKey: %s\n\nThis key will only be displayed once.\n", created.Name, created.Key)
		return nil
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: codex-gateway key list")
		}
		keys, err := a.keys.List(context.Background())
		if err != nil {
			return err
		}
		if a.jsonOut {
			out := make([]map[string]any, 0, len(keys))
			for _, key := range keys {
				out = append(out, keyJSON(key))
			}
			return a.printJSON(out)
		}
		if len(keys) == 0 {
			fmt.Fprintln(a.out, "No API keys.")
			return nil
		}
		tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tPREFIX\tSTATUS\tCREATED\tLAST USED")
		for _, key := range keys {
			last := "-"
			if key.LastUsedAt != nil {
				last = key.LastUsedAt.Local().Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", key.ID, key.Name, key.Prefix, key.Status, key.CreatedAt.Local().Format(time.RFC3339), last)
		}
		return tw.Flush()
	case "revoke":
		if len(args) == 1 {
			if a.termOK {
				return a.revokePicked()
			}
			return fmt.Errorf("usage: codex-gateway key revoke <id>")
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: codex-gateway key revoke <id>")
		}
		if err := a.keys.Revoke(context.Background(), args[1]); err != nil {
			return err
		}
		if a.jsonOut {
			return a.printJSON(map[string]any{"id": args[1], "revoked": true})
		}
		fmt.Fprintf(a.out, "API key revoked: %s\n", args[1])
		return nil
	case "rotate":
		if len(args) == 1 {
			if a.termOK {
				return a.rotatePicked()
			}
			return fmt.Errorf("usage: codex-gateway key rotate <id>")
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: codex-gateway key rotate <id>")
		}
		created, err := a.keys.Rotate(context.Background(), args[1])
		if err != nil {
			return err
		}
		if a.jsonOut {
			return a.printJSON(map[string]string{
				"id":     created.ID,
				"name":   created.Name,
				"prefix": created.Prefix,
				"key":    created.Key,
			})
		}
		fmt.Fprintf(a.out, "API key rotated.\n\nName: %s\nKey: %s\n\nThis key will only be displayed once.\n", created.Name, created.Key)
		return nil
	default:
		return fmt.Errorf("unknown key command %q", args[0])
	}
}

func (a *app) server(args []string) error {
	if len(args) == 0 {
		if a.termOK {
			return a.serverMenu()
		}
		return fmt.Errorf("usage: codex-gateway server start|status")
	}
	switch args[0] {
	case "start":
		if len(args) != 1 {
			return fmt.Errorf("usage: codex-gateway server start")
		}
		return a.serve()
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("usage: codex-gateway server status")
		}
		return a.serverStatus()
	default:
		return fmt.Errorf("unknown server command %q", args[0])
	}
}

func (a *app) serverStatus() error {
	acc, err := a.tokens.Account(context.Background())
	if err != nil {
		return err
	}
	keys, err := a.keys.List(context.Background())
	if err != nil {
		return err
	}
	healthURL, err := config.HealthURL(a.cfg.Listen)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	health := "down"
	if resp, err := client.Get(healthURL); err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			health = "up"
		} else {
			health = resp.Status
		}
	}
	oauthReady := acc != nil && acc.Status == store.OAuthActive
	oauthState := "not logged in"
	if acc != nil {
		oauthState = acc.Status
	}
	payload := map[string]any{
		"listen":       a.cfg.Listen,
		"data_dir":     a.cfg.DataDir,
		"health":       health,
		"oauth":        oauthReady,
		"oauth_status": oauthState,
		"api_keys":     len(keys),
	}
	if a.jsonOut {
		return a.printJSON(payload)
	}
	fmt.Fprintf(a.out, "Listen: %s\nData: %s\nHealth: %s\nOAuth: %s (%s)\nAPI keys: %d\n",
		a.cfg.Listen, a.cfg.DataDir, health, yesNo(oauthReady), oauthState, len(keys))
	return nil
}

func (a *app) configCmd(args []string) error {
	if len(args) == 0 {
		if a.termOK {
			return a.configMenu()
		}
		return fmt.Errorf("usage: codex-gateway config show|set")
	}
	switch args[0] {
	case "show":
		stored, err := a.store.Meta(context.Background(), config.MetaListen)
		if err != nil {
			return err
		}
		payload := map[string]any{
			"listen":            a.cfg.Listen,
			"stored_listen":     stored,
			"data_dir":          a.cfg.DataDir,
			"master_key_source": a.cfg.MasterKeySource,
		}
		if a.jsonOut {
			return a.printJSON(payload)
		}
		fmt.Fprintf(a.out, "listen: %s\ndata_dir: %s\nmaster_key_source: %s\n", a.cfg.Listen, a.cfg.DataDir, a.cfg.MasterKeySource)
		return nil
	case "set":
		if len(args) == 2 && args[1] == "listen" && a.termOK {
			entered, err := a.prompt("수신 주소", a.cfg.Listen)
			if err != nil {
				return err
			}
			args = []string{"set", "listen", entered}
		}
		if len(args) != 3 {
			return fmt.Errorf("usage: codex-gateway config set listen HOST:PORT")
		}
		if args[1] != "listen" {
			return fmt.Errorf("unknown setting %q", args[1])
		}
		if err := config.ValidateListen(args[2]); err != nil {
			return err
		}
		if err := a.store.SetMeta(context.Background(), config.MetaListen, args[2]); err != nil {
			return err
		}
		if a.jsonOut {
			return a.printJSON(map[string]string{"listen": args[2]})
		}
		fmt.Fprintf(a.out, "listen: %s\nRestart the server to apply it.\n", args[2])
		return nil
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func (a *app) human() io.Writer {
	if a.jsonOut {
		return a.err
	}
	return a.out
}

func (a *app) printJSON(v any) error {
	enc := json.NewEncoder(a.out)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func keyJSON(key store.APIKey) map[string]any {
	item := map[string]any{
		"id":         key.ID,
		"name":       key.Name,
		"prefix":     key.Prefix,
		"status":     key.Status,
		"created_at": key.CreatedAt.UTC().Format(time.RFC3339),
	}
	if key.LastUsedAt != nil {
		item["last_used_at"] = key.LastUsedAt.UTC().Format(time.RFC3339)
	}
	if key.RevokedAt != nil {
		item["revoked_at"] = key.RevokedAt.UTC().Format(time.RFC3339)
	}
	return item
}

func takeJSON(args []string) (bool, []string) {
	jsonOut := false
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--json" {
			jsonOut = true
			continue
		}
		out = append(out, arg)
	}
	return jsonOut, out
}

func emptyDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

const helpText = `codex-gateway is a local Codex OAuth gateway.

Run codex-gateway with no arguments in a terminal for the arrow-key menu.
Type only a key name, a callback URL, or a listen address. Everything else is a selection.

Usage:
  codex-gateway auth login [--manual] [--json]
  codex-gateway auth status [--json]
  codex-gateway auth refresh [--json]
  codex-gateway auth logout [--json]
  codex-gateway key create [--name name] [--json]
  codex-gateway key list [--json]
  codex-gateway key revoke <id> [--json]
  codex-gateway key rotate <id> [--json]
  codex-gateway server start
  codex-gateway server status [--json]
  codex-gateway config show [--json]
  codex-gateway config set listen HOST:PORT
  codex-gateway admin reset-password [--json]
  codex-gateway update [--check] [--restart UNIT.service] [--json]
  codex-gateway version

Environment:
  CODEX_GATEWAY_LISTEN      default 127.0.0.1:8080
  CODEX_GATEWAY_DATA_DIR    default ./data
  CODEX_GATEWAY_MASTER_KEY  32-byte hex or base64 key
  CODEX_GATEWAY_ADMIN_LISTEN  default 127.0.0.1:8081, loopback only, "off" disables
  CODEX_GATEWAY_UPDATER     "systemd" hands web updates to codex-gateway-update.path
`
