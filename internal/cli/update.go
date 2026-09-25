package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"time"

	"codex-gateway/internal/buildinfo"
	"codex-gateway/internal/update"
)

var unitPattern = regexp.MustCompile(`^[A-Za-z0-9@._-]+\.service$`)

func runUpdate(args []string, stdout io.Writer, jsonOut bool) error {
	check := false
	restart := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--check":
			check = true
		case args[i] == "--restart" && i+1 < len(args):
			restart = args[i+1]
			i++
		default:
			return fmt.Errorf("usage: codex-gateway update [--check] [--restart UNIT.service] [--json]")
		}
	}
	if restart != "" && !unitPattern.MatchString(restart) {
		return fmt.Errorf("invalid unit name %q", restart)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	client := update.NewClient()
	rel, err := client.Latest(ctx)
	if err != nil {
		return err
	}
	current := buildinfo.Version
	available := update.Newer(rel.Tag, current)
	result := map[string]any{"current": current, "latest": rel.Tag, "available": available, "updated": false}
	if check || !available {
		if jsonOut {
			return json.NewEncoder(stdout).Encode(result)
		}
		switch {
		case !update.ValidTag(current):
			fmt.Fprintf(stdout, "Current: %s (development build)\nLatest: %s\nDevelopment builds are not updated.\n", current, rel.Tag)
		case available:
			fmt.Fprintf(stdout, "Current: %s\nLatest: %s\nRun codex-gateway update to install it.\n", current, rel.Tag)
		default:
			fmt.Fprintf(stdout, "Already up to date (%s).\n", current)
		}
		return nil
	}
	exe, err := update.Executable()
	if err != nil {
		return err
	}
	if err := client.Install(ctx, rel.Tag, exe); err != nil {
		return err
	}
	result["updated"] = true
	if !jsonOut {
		fmt.Fprintf(stdout, "Updated %s from %s to %s.\n", exe, current, rel.Tag)
	}
	if restart != "" {
		out, err := exec.CommandContext(ctx, "systemctl", "restart", restart).CombinedOutput()
		if err != nil {
			return fmt.Errorf("restart %s: %v: %s", restart, err, out)
		}
		if !jsonOut {
			fmt.Fprintf(stdout, "Restarted %s.\n", restart)
		}
	}
	if jsonOut {
		return json.NewEncoder(stdout).Encode(result)
	}
	return nil
}
