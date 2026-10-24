package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pair"
)

// C1 requires explicit a approval through the real terminal pairing door.
func TestC1TerminalLinkApprovalRequiresA(t *testing.T) {
	rig := newPairRig(t)
	screen, done := rig.asking(t)
	token := screen.waitFor(t, `codeaf pair approve (\S+)`)[1]
	out, err := rig.approveWith(t, []string{"approve", token}, "a\n")
	joined := <-done
	if err != nil || joined != nil || !strings.Contains(out.String(), "is now paired") {
		t.Fatalf("C1 a was not approval: err=%v join=%v output=%s", err, joined, out.String())
	}
}

// C1 declines every other terminal key, including the older code-pairing yes.
func TestC1TerminalLinkApprovalDeclinesEveryOtherKey(t *testing.T) {
	for _, answer := range []string{"d\n", "\x1b\n", "\n", "y\n", "yes\n", "A\n", "anything\n"} {
		t.Run(fmt.Sprintf("%q", answer), func(t *testing.T) {
			rig := newPairRig(t)
			screen, done := rig.asking(t)
			token := screen.waitFor(t, `codeaf pair approve (\S+)`)[1]
			out, err := rig.approveWith(t, []string{"approve", token}, answer)
			if joined := <-done; err != nil || !errors.Is(joined, pair.ErrLinkDeclined) || !strings.Contains(out.String(), pair.DeclinedLine) {
				t.Fatalf("%q approved or failed to decline: err=%v join=%v output=%s", answer, err, joined, out.String())
			}
		})
	}
}

// C2 keeps the existing terminal code-pairing yes/no reader unchanged.
func TestC2CodePairingKeepsYesNo(t *testing.T) {
	for answer, want := range map[string]bool{"y\n": true, "yes\n": true, "a\n": false, "d\n": false, "\n": false} {
		out := &pairScreenText{}
		if got := newTerminal(strings.NewReader(answer), out).confirm(context.Background(), "Compare the three words"); got != want {
			t.Errorf("code pairing answer %q: got %t, want %t", answer, got, want)
		}
	}
}
