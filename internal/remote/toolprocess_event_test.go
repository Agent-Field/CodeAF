package remote

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// E2 and E5: appOpenInProcess crosses this codec too. The job's registry
// start survives intact, while older and ordinary no-job events omit the fact.
func TestToolProcessE2SurvivesTheHostWireAndE5AbsenceIsUnchanged(t *testing.T) {
	for _, process := range []*session.ToolProcess{nil, {Job: 3, Started: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}, {Service: "s3"}} {
		original := session.Event{Kind: session.EventToolEnd, CallID: "process-call", ToolProcess: process}
		raw, err := json.Marshal(WireEvent(original))
		if err != nil {
			t.Fatal(err)
		}
		var wire EventWire
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		got := wire.Unwire()
		if !reflect.DeepEqual(got.ToolProcess, process) || got.CallID != original.CallID {
			t.Fatalf("tool process changed in transport: %+v, want %+v", got, original)
		}
		if process == nil && strings.Contains(string(raw), `"ToolProcess"`) {
			t.Fatalf("an absent fact changed the old wire: %s", raw)
		}
	}
}
