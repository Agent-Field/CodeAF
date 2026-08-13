package head

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The notebook budget was enforced with a return where a continue was meant, so
// one oversized belief arriving mid-pass ended the whole pass — every search hit
// behind it AND the entire recency layer that runs after it. Which beliefs the
// model saw was therefore a function of which long fact happened to match this
// message's wording, and the shortest, newest, most obviously relevant line
// could be silenced by a paragraph it had nothing to do with.
func TestNotebookPacksPastAnOversizedBelief(t *testing.T) {
	graph := openHeadStore(t)
	const short = "the user files invoices under Q-close"
	if _, err := graph.RecordFact("", "user", store.FactPreference, short); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("a long standing note about deployment ritual, ", 8)
	for index := 0; index < 6; index++ {
		if _, err := graph.RecordFact("", "user", store.FactPreference,
			long+string(rune('a'+index))); err != nil {
			t.Fatal(err)
		}
	}

	// A message that matches nothing, so only the recency layer is in play and
	// the ordering under test is the pass's own.
	rendered := renderNotebook(graph, "zzzz", "", notebookContextBytes)
	if len(rendered) > notebookContextBytes+len(short)+64 {
		t.Fatalf("the notebook overran its budget at %d bytes", len(rendered))
	}
	if !strings.Contains(rendered, short) {
		t.Fatalf("the oldest short belief was silenced by the long ones ahead of it:\n%s", rendered)
	}
}
