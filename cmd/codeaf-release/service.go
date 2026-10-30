package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// serviceProbeWithin bounds the live check, so a silent service fails the
// release instead of holding the workflow.
const serviceProbeWithin = 60 * time.Second

// probeService is the live check; tests replace it.
var probeService = syncsetup.ProbeService

// runService fails when the sync service a build will talk to does not promise
// what the build needs. A client never ships ahead of its service (#2072).
func runService(args []string, stdout io.Writer) error {
	url := syncsetup.HostedRelayURL
	switch {
	case len(args) == 2 && args[0] == "--url":
		url = args[1]
	case len(args) != 0:
		return usageErr("service takes only --url <sync service>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), serviceProbeWithin)
	defer cancel()
	missing, err := probeService(ctx, url)
	if err != nil {
		return fmt.Errorf("could not check the sync service at %s: %w", url, err)
	}
	if len(missing) > 0 {
		return fmt.Errorf("the sync service at %s must be updated before this build ships; it does not promise %s", url, describe(missing))
	}
	fmt.Fprintf(stdout, "the sync service at %s promises everything this build needs\n", url)
	return nil
}

func describe(missing []directory.Capability) string {
	parts := make([]string, len(missing))
	for i, c := range missing {
		parts[i] = fmt.Sprintf("%s (needed for %s)", c.Header, c.Feature)
	}
	return strings.Join(parts, ", ")
}

// runRelayURL prints the hosted sync service address for one release channel so
// CI scripts read internal/syncsetup/relayurl.go instead of spelling URLs twice.
func runRelayURL(args []string, stdout io.Writer) error {
	if len(args) != 2 || args[0] != "--channel" {
		return usageErr("relay-url takes --channel <stable|rc|staging|dev>")
	}
	switch args[1] {
	case "stable", "rc":
		fmt.Fprintln(stdout, syncsetup.ProductionRelayURL)
	case "staging":
		fmt.Fprintln(stdout, syncsetup.StagingRelayURL)
	case "dev":
		fmt.Fprintln(stdout, syncsetup.DevRelayURL)
	default:
		return usageErr("unknown channel %q (expected stable, rc, staging, or dev)", args[1])
	}
	return nil
}
