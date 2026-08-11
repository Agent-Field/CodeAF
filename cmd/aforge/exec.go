package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// execEnvelope is the machine contract for one-shot harness callers. It
// reports the linear executor's outcome rather than a resident graph summary.
type execEnvelope struct {
	Text      string          `json:"text"`
	Stop      exec.StopReason `json:"stop"`
	Usage     exec.Usage      `json:"usage"`
	Artifacts []string        `json:"artifacts"`
	Turns     int             `json:"turns"`
	ElapsedMS int64           `json:"elapsed_ms"`
}

func runExec(args []string) error {
	flags := flag.NewFlagSet("exec", flag.ContinueOnError)
	workspace := flags.String("w", ".", "workspace directory")
	system := flags.String("system", "", "working method for the agent")
	maxTurns := flags.Int("turns", 200, "runaway backstop on agent iterations")
	maxTokens := flags.Int("budget", 150000, "token budget for the agent")
	asJSON := flags.Bool("json", false, "print a machine-readable result")
	output := flags.String("o", "", "write the machine-readable result to this file")
	if err := flags.Parse(reorder(args, map[string]bool{
		"w": true, "system": true, "turns": true, "budget": true, "o": true,
	})); err != nil {
		return err
	}
	prompt, err := readText(flags.Args())
	if err != nil {
		return err
	}

	settings, err := config.Load()
	if err != nil {
		return err
	}
	client, err := settings.Client()
	if err != nil {
		return err
	}
	ctx := settings.Context(context.Background(), prompt)
	execCtx := settings.ExecContext(ctx)

	space, err := exec.NewWorkspace(*workspace)
	if err != nil {
		return err
	}
	web := exec.NewWeb()
	if web == nil {
		fmt.Fprintln(os.Stderr, "note: EXA_API_KEY unset — the web tool will be unavailable")
	}

	deadline := 15 * time.Minute
	if scaled := time.Duration(*maxTokens/50_000) * time.Minute; scaled > deadline {
		deadline = scaled
	}
	linear := exec.NewLinear(client, space, web, *maxTurns, *maxTokens, deadline)
	outcome, runErr := linear.Run(execCtx, execTask(prompt, *system, space.Root()))
	if outcome == nil {
		outcome = &exec.Outcome{Stop: exec.StopError, Artifacts: []string{}}
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "error:", runErr)
	}

	envelope := buildExecEnvelope(outcome)
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	var outputErr error
	if *output != "" {
		if err := os.WriteFile(*output, encoded, 0o644); err != nil {
			outputErr = err
		}
	}
	if *asJSON {
		if _, err := os.Stdout.Write(encoded); err != nil {
			outputErr = err
		}
	} else {
		if _, err := fmt.Fprintln(os.Stdout, outcome.Text); err != nil {
			outputErr = err
		}
	}
	if outputErr != nil {
		if runErr != nil {
			fmt.Fprintln(os.Stderr, "error:", outputErr)
			return exitStatus(5)
		}
		return outputErr
	}

	if runErr != nil {
		return exitStatus(5)
	}
	if code := execExitCode(outcome.Stop, outcome.Text); code != 0 {
		return exitStatus(code)
	}
	return nil
}

func execExitCode(stop exec.StopReason, text string) int {
	switch stop {
	case exec.StopDone:
		if strings.TrimSpace(text) == "" {
			return 6
		}
		return 0
	case exec.StopBudget:
		return 2
	case exec.StopTurnCap:
		return 3
	case exec.StopDeadline:
		return 4
	case exec.StopError:
		return 5
	default:
		return 5
	}
}

func buildExecEnvelope(outcome *exec.Outcome) execEnvelope {
	if outcome == nil {
		outcome = &exec.Outcome{Stop: exec.StopError}
	}
	artifacts := make([]string, len(outcome.Artifacts))
	copy(artifacts, outcome.Artifacts)
	return execEnvelope{
		Text:      outcome.Text,
		Stop:      outcome.Stop,
		Usage:     outcome.Usage,
		Artifacts: artifacts,
		Turns:     outcome.Turns,
		ElapsedMS: outcome.Elapsed.Milliseconds(),
	}
}

func execTask(prompt, system, root string) exec.Task {
	title, _, _ := strings.Cut(prompt, "\n")
	return exec.Task{
		NodeID:   1,
		Title:    strings.TrimSpace(title),
		Brief:    prompt + "\n\nWorkspace root (your working directory): " + root,
		Contract: system,
	}
}
