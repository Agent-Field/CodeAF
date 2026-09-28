package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestCompactCommandReportsReductionAndExplainsProtectedHistory(t *testing.T) {
	for _, test := range []struct {
		message compactedMsg
		want    string
	}{
		{compactedMsg{before: 60000, after: 5000}, "compacted · about 60000 to 5000 tokens"},
		{compactedMsg{err: session.ErrNothingToCompact}, "nothing to compact — your messages and recent work are kept"},
		{compactedMsg{err: errors.New(session.ErrNothingToCompact.Error())}, "nothing to compact — your messages and recent work are kept"},
		// THE NO-OP SAYS WHY, from this engine and from a remote one whose
		// error arrives as its words alone.
		{compactedMsg{err: &session.NothingToCompact{Why: "only ~400 tokens since the last summary — too little to summarize"}},
			"nothing to compact — only ~400 tokens since the last summary — too little to summarize"},
		{compactedMsg{err: errors.New("session: nothing to compact: the summary was interrupted")},
			"nothing to compact — the summary was interrupted"},
		// A PASS THAT OUTLIVED THE WAIT IS STILL RUNNING, never "failed".
		{compactedMsg{err: remote.ErrLate}, "still compacting — it is taking longer than usual and finishes on its own"},
		{compactedMsg{err: errors.New("the model refused")}, "compact failed: the model refused"},
	} {
		a := newTestApp(&fakeAgent{model: "m"})
		drive(t, a, test.message)
		if got := lastNote(t, a); !strings.Contains(got, test.want) {
			t.Fatalf("got %q, want %q", got, test.want)
		}
	}
}

// A COMPACTION IN THE MIDDLE OF A TURN OUTLIVES THE FOLD. The work on either
// side of it goes behind the chip as it always did; the one quiet line saying
// the model's copy of the conversation changed stays, above the answer.
func TestACompactionMidTurnStaysVisibleWhenTheWorkFolds(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("bash", "go build ./..."),
		{Kind: session.EventToolEnd, Tool: "bash"},
		{Kind: session.EventCompacting, Hint: "compacting ~31k tokens"},
		{Kind: session.EventCompacted, Hint: "compacted · summarized 4 messages · ~31k → ~13k tokens"},
		toolBegin("bash", "go test ./..."),
		{Kind: session.EventToolEnd, Tool: "bash"},
		text(session.EventTextDelta, "all green"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "build and test it")

	page := plain(frame(a))
	if !strings.Contains(page, "· ⚭ compacted · summarized 4 messages") {
		t.Fatalf("the compaction left no trace once the turn folded:\n%s", page)
	}
	if !strings.Contains(page, "all green") {
		t.Fatalf("the answer is not standing:\n%s", page)
	}
	if strings.Contains(page, "go test ./...") || strings.Contains(page, "go build ./...") {
		t.Fatalf("the work around the compaction did not fold:\n%s", page)
	}
}

// AN END-OF-TURN COMPACTION STAYS TOO. The check after the answer is the
// ordinary place a pass runs, and the fold that takes a turn's trailing
// bookkeeping into its disclosure must leave the pass's line standing under
// the answer, with the answer itself still in view.
func TestAnEndOfTurnCompactionStaysVisibleUnderTheAnswer(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = []entry{
		{kind: entryUser, text: "q", turn: 1},
		{kind: entryTool, tool: "read", status: toolOK, turn: 1, settled: true},
		{kind: entryAssistant, text: "The answer.", turn: 1, settled: true},
		{kind: entryCompact, text: "compacted · folded 3 messages", turn: 1, began: time.Unix(90, 0), ended: time.Unix(100, 0)},
	}
	a.workMode = config.WorkFold
	a.touch()
	text := strings.Join(plainRows(a), "\n")
	answer, mark := strings.Index(text, "The answer."), strings.Index(text, "⚭ compacted · folded 3 messages")
	if answer < 0 || mark < 0 || mark < answer {
		t.Fatalf("want the answer and then the compaction line under it:\n%s", text)
	}
}
