package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
)

// The failure row the reconciler now composes, drawn where a person reads it.
//
// The producer separated its one composed sentence from the transport that
// caused it with a blank line — the boundary §5 already treats as one — and the
// card honours it: the sentence is on the card, the transport is behind the
// card's own door. dressWork has set tailFold since it was written and nothing
// ever marked a run folded, so the door it asked for never appeared and a card
// grew to whatever its producer wrote.
func TestAWorkCardKeepsItsDetailBehindItsOwnDoor(t *testing.T) {
	raw := `node craft-3799~launch: after 3 node call attempts: API error (404): ` +
		`{"error":{"message":"No endpoints found that support tool use."}}`
	message := store.Message{
		Seq: 12, SessionID: testSession, Role: store.RoleSystem, NodeID: "craft-3799",
		Body: `I couldn't finish "put together a deep dive on the Q3 numbers" — ` +
			"No endpoints found that support tool use. your learned way didn't survive " +
			"its first try here — planning it fresh.\n\n" + raw,
	}
	board := fakeJobs{facts: map[string]jobFacts{
		"craft-3799": {Name: "Q3 deep dive", Life: rail.LifeFailed},
	}}

	block, shut := deliveryRows(t, message, board, 100)
	if !strings.Contains(shut, "I couldn't finish") || !strings.Contains(shut, "No endpoints found") {
		t.Fatalf("the composed sentence is not on the card:\n%s", shut)
	}
	for _, machinery := range []string{"craft-3799~launch", "API error", "{", "node call attempts"} {
		if strings.Contains(shut, machinery) {
			t.Fatalf("the shut card leaked %q:\n%s", machinery, shut)
		}
	}
	if !block.collapsible {
		t.Fatal("the card offers no door onto the detail it is holding")
	}

	block.SetExpanded(true)
	var open strings.Builder
	for _, row := range block.Rows(100) {
		open.WriteString(row)
		open.WriteByte('\n')
	}
	// Wrapping breaks the raw line across rows, so the evidence that it is
	// reachable is its distinctive pieces rather than the whole string.
	for _, piece := range []string{"craft-3799~launch", "API error", "404"} {
		if !strings.Contains(open.String(), piece) {
			t.Fatalf("opening the card does not reach %q:\n%s", piece, open.String())
		}
	}
}

// A work row with nothing held opens no door. 5.20 rule 3: never a door onto an
// empty room.
func TestAWorkCardWithNoDetailOffersNoDoor(t *testing.T) {
	message := store.Message{
		Seq: 13, SessionID: testSession, Role: store.RoleSystem, NodeID: "task-16",
		Body: "reading the issue",
	}
	block, rows := deliveryRows(t, message, threeHaikuBoard(), 100)
	if block.collapsible {
		t.Fatalf("a one-line status opened a door onto nothing:\n%s", rows)
	}
}

// splitWorkBody reads the producer's own boundary first and falls back to the
// card's row cap when there is none.
func TestSplitWorkBodyHonoursTheBlankLineThenTheCap(t *testing.T) {
	shown, rest := splitWorkBody("the status\n\nthe detail\nmore detail")
	if shown != "the status" || rest != "the detail\nmore detail" {
		t.Fatalf("shown=%q rest=%q", shown, rest)
	}
	// No blank line: the row cap decides, exactly as every other card's does.
	long := strings.Repeat("a line\n", cardBodyRows+3)
	shown, rest = splitWorkBody(long)
	if rest == "" {
		t.Fatalf("a body past the card cap was not folded: shown=%q", shown)
	}
	if got := strings.Count(shown, "\n") + 1; got > cardBodyRows {
		t.Fatalf("the card grew to %d rows, past its cap", got)
	}
	// Nothing to split.
	if shown, rest = splitWorkBody("  \n\n "); shown != "" || rest != "" {
		t.Fatalf("an empty body produced shown=%q rest=%q", shown, rest)
	}
	if shown, rest = splitWorkBody("just the one line"); shown != "just the one line" || rest != "" {
		t.Fatalf("shown=%q rest=%q", shown, rest)
	}
}
