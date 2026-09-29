package remote

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A PASS'S SUMMARY COUNT CROSSES THE LINK. A surface over --host decides
// whether a compaction's line stands by [session.Event.Summarized], so a wire
// that dropped it would fold a summary away there while the same pass stood on
// the machine that ran it. And a pass with no summary sends no field at all,
// which is the bytes an older engine sends for every pass.
func TestACompactionsSummaryCountCrossesTheWire(t *testing.T) {
	payload, err := json.Marshal(WireEvent(session.Event{
		Kind: session.EventCompacted, Hint: "compacted · summarized 4 messages", Summarized: 4,
	}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire EventWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := wire.Unwire().Summarized; got != 4 {
		t.Fatalf("the summary count did not survive the link: got %d, want 4", got)
	}

	free, err := json.Marshal(WireEvent(session.Event{Kind: session.EventCompacted, Hint: "compacted · folded 3 messages"}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(free), "Summarized") {
		t.Fatalf("a free pass put the field on the wire: %s", free)
	}
}
