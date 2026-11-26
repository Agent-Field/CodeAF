package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Agent-Field/codeaf/internal/opener"
)

func appConnectCommand(profile string, args []string, out io.Writer) error {
	flags := commandFlags("app connect google")
	write := flags.Bool("write", false, "authorize approved writes for this source")
	noBrowser := flags.Bool("no-browser", false, "print the sign-in address")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 2 || flags.Arg(0) != "google" {
		return errors.New("usage: codeaf app connect google <gmail|calendar|drive> [--write] [--no-browser]")
	}
	manager := v3Connect(profile)
	if manager == nil {
		return errors.New("Google Workspace: Connections could not be read.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	source := flags.Arg(1)
	flow, err := manager.BeginWorkspaceAuth(ctx, source, *write)
	if err != nil {
		return err
	}
	defer flow.Cancel()
	fmt.Fprintln(out, flow.URL())
	if !*noBrowser && connectOpen(flow.URL()) != nil {
		fmt.Fprintln(out, opener.BrowserFailureWord)
	}
	if _, err := flow.Wait(ctx); err != nil {
		return err
	}
	if *write {
		fmt.Fprintf(out, "Google %s write access is connected.\n", source)
	} else {
		fmt.Fprintf(out, "Google %s read access is connected.\n", source)
	}
	return nil
}
