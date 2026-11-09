package main

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

var readinessLookPath = exec.LookPath

// Doctor inherits the person's environment, including gh's normal tokens.
// Model shells keep their separate credential filtering.
var readinessRun = func(ctx context.Context, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.WaitDelay = 100 * time.Millisecond
	raw, err := command.Output()
	return strings.TrimSpace(string(raw)), err
}

func machineReadiness() []appwire.ReadinessCheck {
	var checks []appwire.ReadinessCheck
	add := func(name, state, message, fix string) {
		checks = append(checks, appwire.ReadinessCheck{Name: name, State: state, Message: message, Fix: fix})
	}
	run := func(name string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), readinessTimeout)
		defer cancel()
		return readinessRun(ctx, name, args...)
	}
	if git, err := readinessLookPath("git"); err != nil {
		add("git", "error", "git is not installed", "install Git")
	} else {
		add("git", "ok", "git found", "install Git if it becomes unavailable")
		for _, field := range []string{"user.name", "user.email"} {
			state, message := "ok", "set"
			if value, err := run(git, "config", field); err != nil || value == "" {
				state, message = "error", "not set or could not be read"
			}
			example := `"Your Name"`
			if field == "user.email" {
				example = `"you@example.com"`
			}
			add("git "+field, state, message, "git config --global "+field+" "+example)
		}
	}
	if gh, err := readinessLookPath("gh"); err != nil {
		add("gh", "info", "gh is not installed", "install GitHub CLI, then run gh auth login --web")
	} else if _, err := run(gh, "auth", "status"); err != nil {
		add("gh", "error", "gh is not signed in or could not be checked", "gh auth login --web")
	} else {
		add("gh", "ok", "gh is signed in", "gh auth login --web to sign in again")
	}
	checks = append(checks, platformReadiness()...)
	return checks
}
