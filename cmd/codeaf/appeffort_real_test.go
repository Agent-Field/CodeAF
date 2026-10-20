package main

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// The real wire reaches the terminal's persisted dial through remote.Loopback.
func TestAppEffortRungsRoundTripRealEngine(t *testing.T) {
	h := newRealActionHarness(t)
	read := func(method string, params any) appwire.Agent {
		t.Helper()
		raw, err := h.w.call(method, params)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Agent appwire.Agent `json:"agent"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		return result.Agent
	}
	for _, word := range []string{"low", "medium", "high", "xhigh", "max", "auto"} {
		t.Run(word, func(t *testing.T) {
			want := word
			if word == "auto" {
				want = ""
			}
			got := read("agents.set_effort", map[string]string{"id": h.agent, "effort": word})
			if got.Effort != want {
				t.Fatalf("set %q = %q", word, got.Effort)
			}
			_, live, err := h.b.heldLive(h.agent)
			if err != nil {
				t.Fatal(err)
			}
			dial := live.conn.Conv.(interface{ ConversationEffort() string })
			if got := dial.ConversationEffort(); got != want {
				t.Fatalf("terminal dial = %q, want %q", got, want)
			}
			h.restart()
			got = read("agents.open", map[string]string{"id": h.agent})
			if got.Effort != want {
				t.Fatalf("reopened %q = %q", word, got.Effort)
			}
		})
	}
	if _, err := h.w.call("agents.set_effort", map[string]string{"id": h.agent, "effort": "sideways"}); err == nil || err.Code != appwire.CodeBadParams || err.Message != "effort is low, medium or high" {
		t.Fatalf("unknown effort = %v", err)
	}
	if got := read("agents.open", map[string]string{"id": h.agent}); got.Effort != "" {
		t.Fatalf("unknown effort changed auto: %+v", got)
	}
}
