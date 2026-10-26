package executor

import (
	"errors"
	"testing"
)

// C1/C6: user admission reads the existing viewer fact without the tool's
// capture or relay work, and the setup form keeps the same refusal.
func TestC1C6WorkAdmissionReusesTheViewerGate(t *testing.T) {
	refusal := &WorkRefusal{Line: "laptop continued this chat", HolderID: "device", HolderName: "laptop"}
	viewer, preflights := false, 0
	seat := Gated(Host, func() error { preflights++; return refusal }, func() error {
		if viewer {
			return refusal
		}
		return nil
	})
	if err := CheckWork(seat); err != nil {
		t.Fatal(err)
	}
	viewer = true
	for _, form := range []Seat{seat, ForSetup(seat)} {
		if err := CheckWork(form); !errors.Is(err, refusal) {
			t.Fatalf("ownership refusal = %v", err)
		}
	}
	if preflights != 0 {
		t.Fatalf("user admission ran %d tool preflights", preflights)
	}
	if err := CheckWork(Host); err != nil {
		t.Fatalf("ungated seat changed: %v", err)
	}
}
