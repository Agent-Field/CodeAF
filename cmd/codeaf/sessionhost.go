package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/sessionhost"
)

// Helpers must bypass normal startup because it adopts and writes to the
// configured home. The launch helper is still root when it enters here.
func init() {
	if len(os.Args) < 3 || os.Args[1] != "sessionhost" {
		return
	}
	if code, handled := runSessionhostHelper(os.Args[2:]); handled {
		os.Exit(code)
	}
}

func runSessionhostHelper(args []string) (int, bool) {
	if len(args) > 0 {
		switch args[0] {
		case "launch", "reap", "whoami":
			return sessionhost.RunHelper(args), true
		}
	}
	return 0, false
}

func runSessionhost(args []string) error {
	if code, handled := runSessionhostHelper(args); handled {
		if code != 0 {
			return exitStatus(code)
		}
		return nil
	}
	if len(args) != 1 {
		return errors.New("Choose serve, probe or health.")
	}
	cfg, err := sessionhost.ConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	switch args[0] {
	case "serve":
		// THE SUPERVISOR NARROWS ITSELF BEFORE IT MEASURES ANYTHING, so the
		// probe it publishes describes the process that will actually run: five
		// capabilities, whatever the platform handed root. A failure is said
		// once and is not fatal, because the identity switch refuses on its own
		// when the privilege it needs is missing.
		if err := sessionhost.RestrictSupervisor(); err != nil {
			fmt.Fprintln(os.Stderr, "sessionhost: "+err.Error())
		}
		report := sessionhost.Probe(cfg)
		if report.OK {
			fmt.Fprintln(os.Stderr, "sessionhost: mode "+report.Mode)
		} else {
			fmt.Fprintln(os.Stderr, "refused: "+report.Reason)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if report.OK {
			// The port fence cannot stop a chat reaching a cloud metadata address,
			// so the operator is told when this machine answers on one.
			sessionhost.WarnIfMetadataReachable(ctx, os.Stderr)
		}
		return sessionhost.ListenAndServe(ctx, cfg, report)
	case "probe":
		report := sessionhost.Probe(cfg)
		out := json.NewEncoder(os.Stdout)
		out.SetIndent("", "  ")
		if err := out.Encode(report); err != nil {
			return err
		}
		if !report.OK {
			return exitStatus(1)
		}
		return nil
	case "health":
		if !sessionhostHealthy(cfg) {
			return exitStatus(1)
		}
		return nil
	default:
		return errors.New("Choose serve, probe or health.")
	}
}

func sessionhostHealthy(cfg sessionhost.Config) bool {
	_, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, "http://"+net.JoinHostPort("127.0.0.1", port)+"/v1/health", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Secret)
	client := &http.Client{
		Timeout: 5 * time.Second,
		// A health check uses the loopback server directly.
		Transport:     &http.Transport{},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	res, err := client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	var health struct {
		OK bool `json:"ok"`
	}
	return res.StatusCode == http.StatusOK && json.NewDecoder(res.Body).Decode(&health) == nil && health.OK
}
