package remote

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestActualRunIdentitySurvivesTheHostEventWire(t *testing.T) {
	for _, run := range []string{"", "actual-run"} {
		original := session.Event{Kind: session.EventToolBegin, CallID: "same-call", RunID: run}
		raw, err := json.Marshal(WireEvent(original))
		if err != nil {
			t.Fatal(err)
		}
		var wire EventWire
		if err = json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		got := wire.Unwire()
		if got.RunID != run || got.CallID != original.CallID {
			t.Fatalf("run replaced by host: %+v", got)
		}
	}
}
