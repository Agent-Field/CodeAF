package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
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
		{compactedMsg{err: &session.SummarySkipped{Why: "provider unavailable"}, before: 60000, after: 5000},
			"compacted · about 60000 to 5000 tokens · summary skipped: provider unavailable"},
		{compactedMsg{err: errors.New("session: compacted: summary skipped: provider unavailable"), before: 60000, after: 60000},
			"compacted · summary skipped: provider unavailable"},
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

func TestCompactReplyFromAnotherConversationChangesNeitherPageNorMeter(t *testing.T) {
	first := &fakeAgent{model: "first", weight: 8615}
	a := newTestApp(first)
	cmd := a.slash("/compact")
	if cmd == nil {
		t.Fatal("/compact did not start")
	}
	second := &fakeAgent{model: "second", weight: 9000}
	a.takeUp(Conversation{Agent: second}, false)
	a.entries = nil
	a.ctxTokens = 1234
	delete(a.notices.seen, eventCompacted)
	drive(t, a, cmd())
	if len(a.entries) != 0 || a.ctxTokens != 1234 || a.notices.seen[eventCompacted] {
		t.Fatalf("late reply changed the new conversation: entries=%v, meter=%d, compact notice=%v", a.entries, a.ctxTokens, a.notices.seen[eventCompacted])
	}
}

func TestCompactReplyOnItsOriginalConversationStillReportsSuccess(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "first", weight: 8615})
	cmd := a.slash("/compact")
	if cmd == nil {
		t.Fatal("/compact did not start")
	}
	drive(t, a, cmd())
	if got := lastNote(t, a); !strings.Contains(got, "compacted") {
		t.Fatalf("the pass was not reported in its original conversation: %q", got)
	}
	if a.ctxTokens != 8615 || !a.notices.seen[eventCompacted] {
		t.Fatalf("the pass did not update its original conversation: meter=%d, notice=%v", a.ctxTokens, a.notices.seen[eventCompacted])
	}
}

func TestCompactReplyBehindHomeStaysWithOriginalConversation(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "first", weight: 8615})
	cmd := a.slash("/compact")
	if cmd == nil {
		t.Fatal("/compact did not start")
	}
	a.openHome()
	if !a.at(pageHome) {
		t.Fatal("Home did not open")
	}
	a.entries = nil
	a.ctxTokens = 1234
	delete(a.notices.seen, eventCompacted)
	drive(t, a, cmd())
	if len(a.entries) != 1 || a.ctxTokens != 8615 || !a.notices.seen[eventCompacted] {
		t.Fatalf("original conversation lost reply behind Home: entries=%v, meter=%d, compact notice=%v", a.entries, a.ctxTokens, a.notices.seen[eventCompacted])
	}
	a.closeHome()
	if got := lastNote(t, a); !strings.Contains(got, "compacted") {
		t.Fatalf("reply missing on return: %q", got)
	}
}

func TestCompactReplyReportsSizeOnlyWhenTheContextShrinks(t *testing.T) {
	for _, size := range []int{8615, 9000} {
		a := newTestApp(&fakeAgent{model: "m"})
		drive(t, a, compactedMsg{before: 8615, after: size})
		if got := lastNote(t, a); strings.Contains(got, "about") {
			t.Fatalf("size %d was described as a reduction: %q", size, got)
		}
	}
}

func TestCompactLinesUseOnlyASCIIOnTheLinearTier(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.linear = true
	line := a.divider("compacted · summarized 4 messages · ~31k → ~13k tokens · full record in the session journal", 120)
	drive(t, a, compactedMsg{before: 60000, after: 5000})
	line += lastNote(t, a)
	line += a.divider("compacted · summary skipped: the model refused … "+strings.Repeat("a", 150), 120)
	for _, r := range line {
		if r > 127 {
			t.Fatalf("linear compaction line contains non-ASCII %q: %q", r, line)
		}
	}
	b := newTestApp(&fakeAgent{model: "m"})
	b.pal.ascii = true
	drive(t, b, compactedMsg{err: &session.SummarySkipped{Why: "the model refused …"}, before: 60000, after: 5000})
	line = b.divider("compacted · summary skipped: the model refused …", 120) + lastNote(t, b)
	for _, r := range line {
		if r > 127 {
			t.Fatalf("ASCII compaction line contains non-ASCII %q: %q", r, line)
		}
	}
}

func TestCompactLinesUseTheSelectedGlyphTier(t *testing.T) {
	for _, set := range []tokens.GlyphSet{tokens.Plain, tokens.NerdFont} {
		a := newTestApp(&fakeAgent{model: "m"})
		a.pal.icons = set
		mark := set.Glyph(tokens.GCompacted)
		if got := a.divider("compacted", 40); !strings.Contains(got, mark+" compacted") {
			t.Errorf("%s divider = %q, missing selected mark %q", set, got, mark)
		}
		drive(t, a, compactedMsg{before: 60000, after: 5000})
		if got := lastNote(t, a); !strings.HasPrefix(got, mark+" compacted") {
			t.Errorf("%s command note = %q, missing selected mark %q", set, got, mark)
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

// THE METER FOLLOWS A /compact. The status line read the last request's weight
// until the next message, so a conversation just taken from 568k to 15k still
// showed 585.1k (sandbox #3, 2026-09-28).
func TestTheMeterDropsAsSoonAsACompactLands(t *testing.T) {
	fake := &fakeAgent{model: "m", weight: 585_100}
	a := newTestApp(fake)
	a.measureContext()
	if a.ctxTokens != 585_100 {
		t.Fatalf("fixture meter = %d", a.ctxTokens)
	}
	fake.weight = 15_110
	drive(t, a, compactedMsg{before: 567_975, after: 15_110})
	if a.ctxTokens != 15_110 {
		t.Fatalf("meter after /compact = %d, want the new weight 15110", a.ctxTokens)
	}
}
