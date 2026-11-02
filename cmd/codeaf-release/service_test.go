package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// TestServiceGateFailsAheadOfTheService pins #2072's release gate: a build
// whose service lacks a promise does not ship, and the refusal names it.
func TestServiceGateFailsAheadOfTheService(t *testing.T) {
	defer func(was func(context.Context, string) ([]directory.Capability, error)) { probeService = was }(probeService)
	probeService = func(context.Context, string) ([]directory.Capability, error) {
		return directory.Capabilities, nil
	}
	var out, errs bytes.Buffer
	if code := run([]string{"service", "--url", "https://sync.example"}, nil, &out, &errs); code != 1 {
		t.Fatalf("exit %d, stderr %q", code, errs.String())
	}
	if !strings.Contains(errs.String(), directory.ConditionalTakeHeader) {
		t.Fatalf("the refusal does not name the promise: %q", errs.String())
	}
	probeService = func(context.Context, string) ([]directory.Capability, error) { return nil, nil }
	if code := run([]string{"service"}, nil, &out, &errs); code != 0 {
		t.Fatalf("a complete service failed the gate: %q", errs.String())
	}
}

func TestRelayURLPrintsEachChannelConstant(t *testing.T) {
	cases := map[string]string{
		"stable":  syncsetup.ProductionRelayURL,
		"rc":      syncsetup.ProductionRelayURL,
		"staging": syncsetup.StagingRelayURL,
		"dev":     syncsetup.DevRelayURL,
	}
	for ch, want := range cases {
		var out, errs bytes.Buffer
		if code := run([]string{"relay-url", "--channel", ch}, nil, &out, &errs); code != 0 {
			t.Fatalf("relay-url --channel %s exited %d: %s", ch, code, errs.String())
		}
		if got := strings.TrimSpace(out.String()); got != want {
			t.Fatalf("relay-url --channel %s = %q, want %q", ch, got, want)
		}
	}
	var out, errs bytes.Buffer
	if code := run([]string{"relay-url", "--channel", "unknown"}, nil, &out, &errs); code != 2 {
		t.Fatalf("unknown channel exited %d, want 2", code)
	}
}
