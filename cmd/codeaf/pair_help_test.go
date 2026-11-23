package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
)

// TestPairHelpWithoutAnEngine proves C27.3 and C15: help remains available
// when the build cannot offer pairing, and the action explains the real limit.
func TestPairHelpWithoutAnEngine(t *testing.T) {
	t.Cleanup(cell.UseAvailabilityInTests(false))
	for _, ask := range []string{"--help", "-h", "-help"} {
		out, _ := captureUsage(t)
		err := newPairDoor(strings.NewReader(""), io.Discard).run(context.Background(), []string{ask})
		if code := exitCodeOf(err); code != 0 {
			t.Fatalf("codeaf pair %s exited %d: %v", ask, code, err)
		}
		if !strings.Contains(out.String(), "codeaf pair") || !strings.Contains(out.String(), cell.UnavailableReason()) {
			t.Fatalf("codeaf pair %s printed no usage:\n%s", ask, out.String())
		}
	}
	err := newPairDoor(strings.NewReader(""), io.Discard).run(context.Background(), nil)
	if err == nil || err.Error() != cell.UnavailableReason() {
		t.Fatalf("pairing with no engine: %v, want %q", err, cell.UnavailableReason())
	}
}

func TestQANumericCodeNamesTheJoinCommand(t *testing.T) {
	celltest.Available(t)
	_, errs := captureUsage(t)
	err := newPairDoor(strings.NewReader(""), io.Discard).run(t.Context(), []string{"approve", "42-715-302"})
	if !strings.Contains(errs.String(), "use: codeaf pair 42-715-302") || exitCodeOf(err) == 0 {
		t.Fatalf("numeric approval did not name the right command and fail: %v", err)
	}
}

func TestQAPairHelpSeparatesLinkApprovalAndNumericCodes(t *testing.T) {
	t.Cleanup(cell.UseAvailabilityInTests(false))
	out, _ := captureUsage(t)
	_ = newPairDoor(strings.NewReader(""), io.Discard).run(t.Context(), []string{"--help"})
	for _, line := range []string{"codeaf pair approve <link-or-token>", "codeaf pair <code> [--replace]", "codeaf pair --code"} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("pair help does not explain %q", line)
		}
	}
}
