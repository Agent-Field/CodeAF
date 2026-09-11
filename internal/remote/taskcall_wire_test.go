package remote

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE REQUEST A NODE'S PHASE IS WAITING ON CROSSES THE LINK WITH THE PHASE.
//
// A hosted window reads a node's life off the standing task lane (tasklane.go),
// which carries every [session.Event] as JSON — so the live request a sizing
// reading is waiting on ([session.TaskPhaseNotice.Call]) reaches a `--host`
// rail and room by this wire and no other. A field that did not survive JSON
// would drop the whole frame (pumpTasks skips an event it cannot encode), and
// the far window would lose the phase along with the request. That is why the
// contract carries a request with no error on it, and this pins it.
func TestARequestAPhaseIsWaitingOnCrossesTheWire(t *testing.T) {
	started := time.Date(2026, 9, 11, 9, 45, 0, 0, time.UTC)
	sent := session.Event{Kind: session.EventTaskPhase, TaskPhase: &session.TaskPhaseNotice{
		ID: 5, Phase: session.TaskPhaseSizing, Text: "asking deepseek/deepseek-v4.1-flash",
		Call: &session.TaskCall{
			Model: "deepseek/deepseek-v4.1-flash", Served: "DeepInfra",
			Started: started, FirstToken: started.Add(11500 * time.Millisecond),
			Reasoning: 4465, Tokens: 12, Phase: provider.CallThinking,
		},
	}}
	payload, err := json.Marshal(WireEvent(sent))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire EventWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := wire.Unwire().TaskPhase
	if got == nil || got.Call == nil {
		t.Fatalf("the phase arrived without its request: %+v", got)
	}
	if *got.Call != *sent.TaskPhase.Call {
		t.Fatalf("the request changed on the way:\nsent %+v\ngot  %+v", *sent.TaskPhase.Call, *got.Call)
	}
}
