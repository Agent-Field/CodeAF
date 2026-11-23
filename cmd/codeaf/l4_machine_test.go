package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pair"
)

// L4-M3a, L4-M3b, L4-FIX5-b: a label cannot prove a paired device; explain the permission before asking.
func TestL4MachineApprovalForPairedDevice(t *testing.T) {
	for _, tc := range []struct {
		label, answer string
		yes           bool
	}{
		{label: "laptop-a", answer: "y\n", yes: true},
		{label: "new-device", answer: "n\n"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			out := &pairScreenText{}
			person := newTerminal(strings.NewReader(tc.answer), out)
			if got := confirmMachine(person, context.Background(), tc.label, "red green blue"); got != tc.yes {
				t.Fatalf("approval = %v; want %v", got, tc.yes)
			}
			text := out.String()
			weight := strings.Index(text, pair.PairingWeight)
			question := strings.Index(text, pair.AskMachineLine(tc.label, "red green blue"))
			choice := strings.Index(text, pair.AskChoice)
			if weight < 0 || question <= weight || choice <= question {
				t.Fatalf("expected permission, ordinary question and choice in that order:\n%s", text)
			}
			if strings.Contains(text, "one of your devices") {
				t.Fatalf("the question infers a paired device from an unverified label:\n%s", text)
			}
		})
	}
}
