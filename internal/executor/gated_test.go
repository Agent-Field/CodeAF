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
	// A conversation's seat carries its agent DID in front of the gate (#2027).
	carried := WithEnvironment(seat, []string{"CODEAF_AGENT_DID=did:key:z6Mk"})
	for _, form := range []Seat{seat, ForSetup(seat), carried, ForSetup(carried)} {
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

func TestIdleRelease_C2_C4_PreparedWorkBypassesCachedAdmission(t *testing.T) {
	refusal := &WorkRefusal{Line: "laptop continued this chat"}
	seat := Gated(Host, func() error { return refusal }, func() error { return nil }, func() error { return refusal })
	carried := WithEnvironment(seat, []string{"CODEAF_AGENT_DID=fixture"})
	for _, form := range []Seat{seat, ForSetup(seat), carried, ForSetup(carried), Watching(carried, new(sightings))} {
		if err := CheckWork(form); err != nil {
			t.Fatalf("cached admission changed: %v", err)
		}
		if err := CheckPreparedWork(form); !errors.Is(err, refusal) {
			t.Fatalf("prepared ownership refusal=%v", err)
		}
	}
}

func TestIdleRelease_R5_Audit_LegacySeatsKeepTheirExistingAdmissionCalls(t *testing.T) {
	calls := 0
	seat := Gated(Host, func() error { return nil }, func() error { calls++; return nil })
	for _, form := range []Seat{seat, ForSetup(seat), WithEnvironment(seat, nil), Watching(seat, new(sightings))} {
		if err := CheckPreparedWork(form); err != nil {
			t.Fatal(err)
		}
		if err := CheckSettingsWork(form); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 0 {
		t.Fatalf("optional idle doors added %d ordinary admission calls", calls)
	}
	if err := CheckWork(seat); err != nil || calls != 1 {
		t.Fatalf("ordinary admission changed: calls=%d err=%v", calls, err)
	}
}
