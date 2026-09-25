package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/term"

	"codex-gateway/internal/admin"
	"codex-gateway/internal/config"
	"codex-gateway/internal/gateway"
	"codex-gateway/internal/oauth"
)

func (a *app) serve() error {
	gw := &gateway.Gateway{Tokens: a.tokens, Keys: a.keys}
	runner := &gateway.Runner{Handler: gw.Handler()}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = runner.Shutdown(shutCtx)
	}()

	if a.cfg.AdminListen == "" {
		if err := runner.Start(a.cfg.Listen); err != nil {
			return err
		}
		fmt.Fprintf(a.err, "Listening on %s\nAdmin page is off.\n", a.cfg.Listen)
		<-ctx.Done()
		return nil
	}

	if err := config.ValidateAdminListen(a.cfg.AdminListen); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", a.cfg.AdminListen)
	if err != nil {
		return fmt.Errorf("admin page: %w", err)
	}
	if err := runner.Start(a.cfg.Listen); err != nil {
		fmt.Fprintf(a.err, "Gateway could not listen on %s: %v\nChange the listen address on the admin page.\n", a.cfg.Listen, err)
	} else {
		fmt.Fprintf(a.err, "Listening on %s\n", a.cfg.Listen)
	}
	fmt.Fprintf(a.err, "Admin page on http://%s\n", a.cfg.AdminListen)
	token, err := admin.EnsureSetupToken(ctx, a.store, a.cfg.DataDir)
	if err != nil {
		_ = ln.Close()
		return err
	}
	if token != "" {
		a.printSetupHint(token)
	}
	srv := &admin.Server{
		Store:        a.store,
		Tokens:       a.tokens,
		Keys:         a.keys,
		Gateway:      runner,
		OAuth:        oauth.NewClient(""),
		DataDir:      a.cfg.DataDir,
		Listen:       a.cfg.Listen,
		ListenLocked: a.cfg.ListenLocked,
		AdminListen:  a.cfg.AdminListen,
	}
	return srv.Serve(ctx, ln)
}

func (a *app) printSetupHint(token string) {
	if f, ok := a.err.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprintf(a.err, "\nSet the admin password here:\n%s\n\n", admin.SetupURL(a.cfg.AdminListen, token))
		return
	}
	fmt.Fprintf(a.err, "Admin password is not set. The setup link token is in %s\n", filepath.Join(a.cfg.DataDir, config.SetupTokenFile))
}

func (a *app) adminCmd(args []string) error {
	if len(args) != 1 || args[0] != "reset-password" {
		return fmt.Errorf("usage: codex-gateway admin reset-password")
	}
	token, err := admin.ResetPassword(context.Background(), a.store, a.cfg.DataDir)
	if err != nil {
		return err
	}
	listen := a.cfg.AdminListen
	if listen == "" {
		listen = config.DefaultAdminListen
	}
	link := admin.SetupURL(listen, token)
	if a.jsonOut {
		return a.printJSON(map[string]string{"setup_url": link})
	}
	fmt.Fprintf(a.out, "Admin password cleared. Every open admin session is signed out.\n\nOpen this link to set a new one:\n%s\n", link)
	if a.cfg.AdminListen == "" {
		fmt.Fprintf(a.out, "\nThe admin page is off. Unset %s=off and restart the service first.\n", config.EnvAdminListen)
	}
	return nil
}
