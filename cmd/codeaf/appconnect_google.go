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
	if flags.NArg() < 1 || flags.NArg() > 2 || flags.Arg(0) != "google" || flags.NArg() == 1 && *write {
		return errors.New("usage: codeaf app connect google <gmail|calendar|drive> [--write] [--no-browser]")
	}
	manager := v3Connect(profile)
	if manager == nil {
		return errors.New("Google Workspace: Connections could not be read.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	sources := []string{flags.Arg(1)}
	if flags.NArg() == 1 {
		sources = []string{"gmail", "calendar"}
	}
	for _, source := range sources {
		flow, err := manager.BeginWorkspaceAuth(ctx, source, *write)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, flow.URL())
		if !*noBrowser && connectOpen(flow.URL()) != nil {
			fmt.Fprintln(out, opener.BrowserFailureWord)
		}
		_, err = flow.Wait(ctx)
		flow.Cancel()
		if err != nil {
			return err
		}
		access := "read"
		if *write {
			access = "write"
		}
		fmt.Fprintf(out, "Google %s %s access is connected.\n", source, access)
	}
	return nil
}
