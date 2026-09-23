package main

// `codeaf <name> …` for a program this build carries (internal/delegate): the
// verb every one of them answers, from a person's shell and from the chat's
// own run alike.
//
// TWO CALLERS, ONE LINE. The chat's run starts `codeaf senior-dev run --json
// --dir … -- <brief>` as its child, with the model API's address and token in
// the child's environment; a person types the same verb at a shell with
// neither. The environment is how the two are told apart: a child of a host
// runs the program's body here and writes its records on stdout; a shell run
// becomes the host itself — it serves the model API and starts the same child.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// runCarried runs one line of a carried program's verb and leaves on the exit
// ladder (envelope.go).
func runCarried(program delegate.Delegate, args []string) error {
	inv, err := delegate.Parse(program, args, os.Stdout)
	if errors.Is(err, delegate.ErrHelp) {
		return exitDone
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return exitCannotRun
	}
	// SIGTERM IS THE HOST'S STOP (internal/delegate's launch): the body's
	// context ends, and the program writes its terminal on the way out.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if _, child := delegate.ModelAPIFromEnv(); child {
		return carriedExit(delegate.RunChild(ctx, inv, os.Stdout))
	}
	return runCarriedHost(ctx, inv)
}

// runCarriedHost is a person's shell run: this process serves the model API
// with the person's key and starts the program as its own child.
func runCarriedHost(ctx context.Context, inv *delegate.Invocation) error {
	fmt.Fprintf(os.Stderr, "error: codeaf %s runs from a shell once its model API is in this build; the chat's /%s is the road until then\n", inv.Program.Name, inv.Program.Name)
	return exitCannotRun
}

// carriedExit is an ending on the exit ladder: the work stands, a limit you
// set stopped it, or it ran and did not finish.
func carriedExit(status string) error {
	switch status {
	case delegate.StatusPass:
		return exitDone
	case delegate.StatusBudget:
		return exitLimit
	default:
		return exitIncomplete
	}
}
