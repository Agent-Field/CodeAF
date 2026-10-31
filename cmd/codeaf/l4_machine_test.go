package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pair"
)

// L4-M3a, L4-M3b: the approver sees the extra permission, then the check and choice.
func TestL4MachineApprovalForPairedDevice(t *testing.T) {
	question := machineApprovalQuestion("laptop-a", "red green blue", true)
	if question != "laptop-a has the same name as one of your devices; let it run work here too? Same three words on that screen: red green blue?" {
		t.Fatal(question)
	}
	out := &pairScreenText{}
	person := newTerminal(strings.NewReader("y\n"), out)
	if !confirmMachine(person, context.Background(), "laptop-a", "red green blue", true) {
		t.Fatal("yes declined")
	}
	if strings.Index(out.String(), pair.PairingWeight) > strings.Index(out.String(), "y / n") {
		t.Fatal(out.String())
	}
}
