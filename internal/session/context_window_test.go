package session

import "testing"

// TestSetContextWindowMovesTheCompactionThreshold is the other half of the
// /model contract: a surface that switches models mid-session tells the agent
// what the new one holds, and compaction has to fire against THAT window. The
// status line moving while the threshold stayed on the old model's window is
// the same class of bug model_wire_test.go pins for the wire model.
func TestSetContextWindowMovesTheCompactionThreshold(t *testing.T) {
	agent := &Agent{config: Config{ContextWindow: 128_000}}
	configured := agent.compactThreshold()

	agent.SetContextWindow(1_000_000)
	if got := agent.window(); got != 1_000_000 {
		t.Fatalf("window = %d, want the window that was set", got)
	}
	if after := agent.compactThreshold(); after <= configured {
		t.Fatalf("threshold stayed at %d after a switch to a 1M window (now %d)", configured, after)
	}

	// Zero is "nobody knows", not "no context": it must not clear a window that
	// was learned, and it must not override a configured one.
	agent.SetContextWindow(0)
	if got := agent.window(); got != 1_000_000 {
		t.Fatalf("window = %d after an unknown figure, want 1000000", got)
	}
	fresh := &Agent{config: Config{ContextWindow: 200_000}}
	fresh.SetContextWindow(-1)
	if got := fresh.window(); got != 200_000 {
		t.Fatalf("window = %d, want the configured 200000", got)
	}

	// Nothing configured and nothing learned is still the conservative default.
	if got := (&Agent{}).window(); got != defaultContextWindow {
		t.Fatalf("window = %d, want the default %d", got, defaultContextWindow)
	}
}
