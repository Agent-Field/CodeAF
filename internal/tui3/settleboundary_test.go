package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE TURN BOUNDARY, PINNED (#178): what a turn's end owes every block it
// covers, and what a line the SURFACE wrote at that boundary may not do to the
// answer above it.
//
// Two laws are asserted here and they are separate:
//
//  1. SETTLING IS A PROPERTY OF THE BOUNDARY. Both of a turn's endings — the
//     session's own EventTurnDone and the stream closing behind it — settle
//     every assistant block of the turn, idempotently ([app.settleTurn]).
//  2. A NOTE IS NOT WORK. The two lines a turn ends with (what it changed, what
//     it cost) land under the reply they are about, and a classifier that read
//     them as more work demoted the answer itself into narration — which is
//     drawn plain, so a whole answer came back as the markdown characters it was
//     typed as (workfold.go's [workEntry], hierarchy.go).
//
// THE SECOND LAW ONLY EVER BIT WHERE THE WALK IS THE CLASSIFIER — a turn that
// derives a workfold is answered by the fold instead and was never at risk. The
// shape reproduced against a real model is [TestATurnWhoseFoldIsBlockedKeepsItsAnswerRendered]:
// a task proposal in the turn blocks the fold, and the turn's own cached-tokens
// note is then the entry the walk finds. [TestATurnThatDerivesAFoldWasNeverAtRisk]
// pins the other side, so nobody re-derives the wrong story from this file.
//
// Every want is asked of the STRUCTURE and of the RENDERED ROWS, never of a
// guess about what markdown looks like: the defect this file closes was a
// surface deciding formatting from position, and a test that sniffed the text
// would be agreeing with it.

// boundaryAnswer is one reply with the three shapes the report named — a
// heading, a bold lead and a table — so a block that missed the markdown pipe
// says so in three ways at once.
const boundaryAnswer = "## First: is the problem big enough?\n\n" +
	"**1. Problem existence.** The wedge has to bite.\n\n" +
	"| Wedge | TAM |\n| --- | --- |\n| indie | 4m |\n"

// rawMarkdown reports whether a rendered row is the source text rather than the
// rendering of it — the exact thing a person saw in the report.
func rawMarkdown(row string) bool {
	return strings.Contains(row, "## First") ||
		strings.Contains(row, "**1. Problem existence.**") ||
		strings.Contains(row, "| Wedge | TAM |")
}

// answerBlocks is every assistant entry, in order.
func answerBlocks(a *app) []*entry {
	var out []*entry
	for i := range a.entries {
		if a.entries[i].kind == entryAssistant {
			out = append(out, &a.entries[i])
		}
	}
	return out
}

// cachedTurn is the ordinary second turn of any real session: the provider read
// the conversation back out of its cache, so the turn ends with the ⟲ note
// under its reply (app.go's [app.cacheNote]).
func cachedTurn(answer string) []session.Event {
	return []session.Event{
		text(session.EventTextDelta, answer),
		{Kind: session.EventTurnDone, Usage: session.Usage{Input: 9000, Output: 400, CacheRead: 12000}},
	}
}

// THE REPORT'S OWN CONVERSATION: two consecutive answers, the second of them on
// a turn that read from the cache. Both settle, both are the answer of their
// turn, and both come back through the markdown pipe.
func TestTwoConsecutiveMarkdownAnswersBothRenderAsMarkdown(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{
		{text(session.EventTextDelta, boundaryAnswer), {Kind: session.EventTurnDone}},
		cachedTurn(boundaryAnswer),
	}}
	a := newTestApp(agent)
	a.width, a.height = 80, 40
	runTurn(t, a, agent, "is the problem big enough?")
	runTurn(t, a, agent, "and the wedge?")

	blocks := answerBlocks(a)
	if len(blocks) != 2 {
		t.Fatalf("the two turns drew %d assistant blocks, want 2", len(blocks))
	}
	for i, e := range blocks {
		if !e.settled {
			t.Fatalf("answer %d (turn %d) never settled", i+1, e.turn)
		}
		if e.demoted {
			t.Fatalf("answer %d (turn %d) was classified as narration", i+1, e.turn)
		}
		// THE ROWS ARE THE MARKDOWN PIPE'S OWN, byte for byte. Asking for
		// [app.settledMarkdown] rather than spelling a rendering out is
		// hierarchy_test.go's rule: a want that reimplements its subject is a
		// want that agrees with the bug.
		got := a.assistantRows(0, e, a.width)
		want := a.settledMarkdown(0, e, a.width)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("answer %d did not go through the markdown pipe:\n got %q\nwant %q", i+1, got, want)
		}
	}
	for _, row := range plainRows(a) {
		if rawMarkdown(row) {
			t.Fatalf("a settled answer drew its source text:\n%s", strings.Join(plainRows(a), "\n"))
		}
	}
}

// A NOTE UNDER THE ANSWER IS THE SURFACE TALKING, and the classifier steps over
// it — while a call under the answer is work, and still demotes what came
// before it. Both halves are asked of [workEntry] directly, because the
// question is about the LIST and nothing else.
func TestAnEndOfTurnNoteIsNotWorkTheAnswerWaitedFor(t *testing.T) {
	withNote := []entry{
		{kind: entryUser, text: "is the problem big enough?", turn: 1},
		{kind: entryAssistant, text: boundaryAnswer, turn: 1, settled: true},
		{kind: entryNote, text: "⟲ 12k cached · saved $0.02", turn: 1},
	}
	if workEntry(withNote, nil, 1) {
		t.Fatal("the turn's own cost line demoted the answer it was written about")
	}
	withWork := []entry{
		{kind: entryUser, text: "is the problem big enough?", turn: 1},
		{kind: entryAssistant, text: "Let me check the numbers.", turn: 1, settled: true},
		{kind: entryNote, text: "stuck? nudged · read", turn: 1},
		{kind: entryTool, tool: "read", text: "tam.md", turn: 1, status: toolOK},
		{kind: entryAssistant, text: boundaryAnswer, turn: 1, settled: true},
	}
	if !workEntry(withWork, nil, 1) {
		t.Fatal("prose that a call opened under is not narration")
	}
	if workEntry(withWork, nil, 4) {
		t.Fatal("the block the turn ended on is not work")
	}
}

// THE SHAPE MEASURED AGAINST A REAL MODEL (#178): the turn carries a task
// proposal, which blocks the fold (workfold.go's `blocked`), so the forward walk
// is the only classifier — and the turn's own `⟲ … cached` line is then the
// entry it finds after the answer. On this branch's parent that answer was drawn
// plain and indented, headings, bold and table pipes and all.
func TestATurnWhoseFoldIsBlockedKeepsItsAnswerRendered(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 80, 40
	a.entries = []entry{
		{kind: entryUser, text: "propose a task, then answer in markdown", turn: 1},
		{kind: entryThinking, text: "which task", turn: 1, settled: true},
		// The card is what blocks the fold, and blocking it is correct — a
		// decision may never disappear into a chip (workfold.go).
		{kind: entryTask, text: "Summarise this chat", turn: 1},
		{kind: entryTool, tool: "propose_task", text: "Summarise this chat", turn: 1, status: toolOK},
		{kind: entryAssistant, text: boundaryAnswer, turn: 1, settled: true},
		{kind: entryNote, text: "⟲ 37.9k cached · saved $0.0025", turn: 1},
	}
	a.touch()
	rows(a)

	if len(a.deckFolds(a.conversation())) != 0 {
		t.Fatal("the proposal did not block the fold, so this is no longer the measured shape")
	}
	if a.entries[4].demoted {
		t.Fatal("the answer of a turn that cannot fold was demoted by its own cost line")
	}
	for _, row := range plainRows(a) {
		if rawMarkdown(row) {
			t.Fatalf("the answer drew its source text:\n%s", strings.Join(plainRows(a), "\n"))
		}
	}
}

// AND THE OTHER SIDE OF IT, so the story stays honest: an ordinary turn derives
// a workfold, the fold answers the question instead of the walk, and a trailing
// note could never have reached the answer. This is why a rig driving plain
// question-and-answer turns against a thinking model sees nothing wrong.
func TestATurnThatDerivesAFoldWasNeverAtRisk(t *testing.T) {
	es := []entry{
		{kind: entryUser, text: "reply in markdown", turn: 1},
		{kind: entryThinking, text: "composing", turn: 1, settled: true},
		{kind: entryAssistant, text: boundaryAnswer, turn: 1, settled: true},
		{kind: entryNote, text: "⟲ 13.3k cached · saved $0.0009", turn: 1},
	}
	folds := deriveWorkfolds(es, 0)
	if len(folds) != 1 {
		t.Fatalf("an ordinary turn derived %d folds, want 1", len(folds))
	}
	if workEntry(es, folds, 2) {
		t.Fatal("the fold did not protect the answer it names")
	}
}

// NARRATION KEEPS ITS OWN VOICE. The step over notes must not promote the prose
// a turn narrated with: a block real work opened under is still drawn plain, in
// the work column, at the narration tier (hierarchy.go).
func TestNarrationKeepsItsPlainVoiceUnderTheFix(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 80, 40
	a.entries = []entry{
		{kind: entryUser, text: "why is the parser slow?", turn: 1},
		{kind: entryAssistant, text: "## Checking\n\nthe **config** first.", turn: 1, settled: true},
		{kind: entryNote, text: "stuck? nudged · read", turn: 1},
		{kind: entryTool, tool: "read", text: "parser.go", turn: 1, status: toolOK},
		{kind: entryAssistant, text: boundaryAnswer, turn: 1, settled: true},
	}
	a.touch()
	// The tier is stamped by the layout pass and read off the blocks afterwards
	// (hierarchy.go's [stampHierarchy]), so the list is laid out before it is
	// asked anything.
	rows(a)

	if !a.entries[1].demoted {
		t.Fatal("prose a call opened under lost its narration tier")
	}
	if a.entries[4].demoted {
		t.Fatal("the answer the turn ended on was demoted")
	}
	got := a.assistantRows(1, &a.entries[1], a.width)
	want := a.workingProse(a.entries[1].text, a.width)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("narration is not drawn as narration:\n got %q\nwant %q", got, want)
	}
}

// A CARRIED-ON TURN'S FINAL BLOCK SETTLES. The session answers, ends the turn,
// and then carries on into a second block on the SAME stream — so the block the
// person is left reading arrives after the ending that would have settled it,
// and only the stream's own close is left to cover it.
func TestACarriedOnTurnsFinalBlockSettles(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, "Stopping here for now.\n"),
		{Kind: session.EventTurnDone},
		text(session.EventTextDelta, boundaryAnswer),
	}}}
	a := newTestApp(agent)
	a.width, a.height = 80, 40
	runTurn(t, a, agent, "carry on")

	blocks := answerBlocks(a)
	if len(blocks) != 2 {
		t.Fatalf("the carried-on turn drew %d assistant blocks, want 2", len(blocks))
	}
	last := blocks[len(blocks)-1]
	if !last.settled {
		t.Fatal("the block the carry-on ended on never settled")
	}
	for _, row := range plainRows(a) {
		if rawMarkdown(row) {
			t.Fatalf("the carried-on answer drew its source text:\n%s", strings.Join(plainRows(a), "\n"))
		}
	}
}

// A BLOCK STREAMED AND THEN ABANDONED BY A CUT STREAM STILL SETTLES. There is
// no EventTurnDone in this turn at all: the provider went away, and the close is
// the only ending the surface gets.
func TestABlockAbandonedByACutStreamStillSettles(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, boundaryAnswer),
	}}}
	a := newTestApp(agent)
	a.width, a.height = 80, 40
	runTurn(t, a, agent, "is the problem big enough?")

	blocks := answerBlocks(a)
	if len(blocks) != 1 {
		t.Fatalf("the cut turn drew %d assistant blocks, want 1", len(blocks))
	}
	if !blocks[0].settled {
		t.Fatal("a block the stream abandoned never settled")
	}
	if a.live >= 0 {
		t.Fatalf("the surface is still writing into block %d after the close", a.live)
	}
}

// THE TWO ENDINGS ARE IDEMPOTENT. A turn ends twice on this surface, so the
// second pass has to find the work done and write nothing — the settle is
// counted by asking the blocks, which is the only place it leaves a mark.
func TestTheTwoEndingsOfATurnSettleIdempotently(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{cachedTurn(boundaryAnswer)}}
	a := newTestApp(agent)
	a.width, a.height = 80, 40
	runTurn(t, a, agent, "is the problem big enough?")

	before := append([]entry(nil), a.entries...)
	// The stale flags are cleared so a second settle that touched anything
	// would have to say so.
	for i := range a.entries {
		a.entries[i].stale = false
	}
	a.settleTurn()
	for i := range a.entries {
		if a.entries[i].stale {
			t.Fatalf("a second settle re-did block %d", i)
		}
		if a.entries[i].settled != before[i].settled {
			t.Fatalf("a second settle changed block %d", i)
		}
	}
	if len(a.entries) != len(before) {
		t.Fatalf("a second settle changed the transcript: %d blocks, want %d", len(a.entries), len(before))
	}
}
