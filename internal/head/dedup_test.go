package head

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// One dedup probe, three seams. The codebase fought hardest to protect exactly
// this budget and then guarded the one pair least likely to collide: the board's
// result clause and the deep slice's full quote always collided, the board's
// result clause and the thread's own announcement of that summary always
// collided, and the notebook was deduped only against itself while the head
// manufactured the thread-versus-notebook collision on the previous turn.

// failJob settles one job the way a failure settles it: claimed, started, and
// ended with what it has to say for itself. A failure is still a finding, and
// nodeResult reads it as one.
func failJob(t *testing.T, graph *store.Store, id, title, intent, finding string) {
	t.Helper()
	spliceSurgeryJob(t, graph, id, title, intent)
	claim, ok, err := graph.Claim(id, "tester")
	if err != nil || !ok {
		t.Fatalf("claim %s: ok=%t err=%v", id, ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	if err := graph.Fail(claim, finding); err != nil {
		t.Fatalf("fail %s: %v", id, err)
	}
}

// seedDeliveredBoard is the shape the board dedup is about: work that has a
// finding AND is still on the live board.
//
// The board enumerates what is moving now, so a job that finished cleanly leaves
// it and can no longer collide with anything. What stays is work that has said
// something and is not yet closed out — a failure most of all, which is both the
// most important row on a board and the one whose account of itself gets quoted
// twice.
func seedDeliveredBoard(t *testing.T, graph *store.Store) {
	t.Helper()
	failJob(t, graph, "finance-close", "Finance close", "close the finance books for Q3",
		financeFinding+"\n"+financeDetail)
	failJob(t, graph, "podcast-edit", "Podcast edit", "edit the podcast episode",
		podcastFinding+"\n"+podcastDetail)
	spliceSurgeryJob(t, graph, "line-scans", "Line scans", "scan the lines")
}

// TestBoardDropsTheClauseTheDeepSliceIsAboutToQuote is #37. For every job the
// slice opens, the identifying line appears twice by design and the summary's
// first line appeared twice by accident — which costs budget and reads to a
// model as two independent statements of one finding.
func TestBoardDropsTheClauseTheDeepSliceIsAboutToQuote(t *testing.T) {
	graph := openHeadStore(t)
	seedDeliveredBoard(t, graph)
	head := New(nil, graph)

	// With nothing opened the board says what it has always said.
	whole, err := head.renderTurnBoard("dedup", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(whole, "result: "+financeFinding) {
		t.Fatalf("the board lost its result clause entirely:\n%s", whole)
	}

	deep, opened := head.renderDeep("what happened with the finance thing", "")
	if !opened["finance-close"] || !strings.Contains(deep, financeFinding) {
		t.Fatalf("the fixture never opened the finance job:\n%s", deep)
	}
	deduped, err := head.renderTurnBoard("dedup", "", opened)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(deduped, "result: "+financeFinding) {
		t.Fatalf("the board still states the finding the slice quotes in full:\n%s", deduped)
	}
	// The row itself is not the casualty. The board is the floor: every job keeps
	// its identifying line, opened or not.
	for _, id := range []string{"finance-close", "podcast-edit", "line-scans"} {
		if !strings.Contains(deduped, "- "+id+" | ") {
			t.Fatalf("dedup evicted %s from the board:\n%s", id, deduped)
		}
	}
	// And a job the slice did not open keeps its clause.
	if !strings.Contains(deduped, "result: "+podcastFinding) {
		t.Fatalf("an unopened job lost its result clause:\n%s", deduped)
	}
}

// TestBoardDropsTheSummaryTheThreadAlreadyPosted is #39. announceNode posts a
// settled job's summary into the thread as a system message; the board then
// rendered the same first line independently. Every settled job spent its
// summary twice in the prompt, and the pair that always collides was the one
// pair nothing deduped.
func TestBoardDropsTheSummaryTheThreadAlreadyPosted(t *testing.T) {
	graph := openHeadStore(t)
	seedDeliveredBoard(t, graph)
	thread := "system: " + financeFinding
	board, err := New(nil, graph).renderTurnBoard("dedup", thread, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(board, "result: "+financeFinding) {
		t.Fatalf("the board repeated a summary the thread already carries:\n%s", board)
	}
	if !strings.Contains(board, "- finance-close | ") {
		t.Fatalf("dedup took the row with the clause:\n%s", board)
	}
	if !strings.Contains(board, "result: "+podcastFinding) {
		t.Fatalf("a job the thread never mentioned lost its clause:\n%s", board)
	}
}

// TestNotebookDropsWhatTheThreadAlreadyShows is #38, and the collision is one
// the head creates itself: a preference captured mid-turn from the user's own
// sentence comes back on the next message as a numbered notebook line beside
// that same sentence, so the model is shown its own capture as independent
// standing evidence for the thing it captured.
func TestNotebookDropsWhatTheThreadAlreadyShows(t *testing.T) {
	graph := openHeadStore(t)
	const preference = "always answer from the result rather than saying where the answer is"
	if _, err := graph.RecordFactFrom(store.FactWriterHead, store.RootID, "user",
		store.FactPreference, preference); err != nil {
		t.Fatal(err)
	}
	const other = "the deploy script needs sudo on this machine to reach the daemon"
	if _, err := graph.RecordFactFrom(store.FactWriterHead, store.RootID, "user",
		store.FactPreference, other); err != nil {
		t.Fatal(err)
	}

	alone := renderNotebook(graph, preference, "")
	if !strings.Contains(alone, preference) {
		t.Fatalf("the notebook lost the belief with no thread at all:\n%s", alone)
	}
	beside := renderNotebook(graph, preference, "user: "+preference)
	if strings.Contains(beside, preference) {
		t.Fatalf("the notebook restated the sentence sitting above it:\n%s", beside)
	}
	if !strings.Contains(beside, other) {
		t.Fatalf("dedup took a belief the thread never mentioned:\n%s", beside)
	}
	// The floor rides along, so a two-word belief cannot match by accident and
	// vanish from the memory the model reads.
	short := strings.Repeat("c", deepDedupFloorBytes-1)
	if _, err := graph.RecordFactFrom(store.FactWriterHead, store.RootID, "user",
		store.FactPreference, short); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(renderNotebook(graph, short, "user: "+short), short) {
		t.Error("a short belief deduped itself away on too little evidence")
	}
}

// The three seams together, in one real prompt, with the budgets intact.
func TestDedupNeverCostsTheBoardItsFloor(t *testing.T) {
	graph := openHeadStore(t)
	seedDeliveredBoard(t, graph)
	prompt := routerPrompt(t, graph, "dedup", "what happened with the finance thing")
	if strings.Count(prompt, financeFinding) != 1 {
		t.Fatalf("the finance finding appears %d times in one prompt:\n%s",
			strings.Count(prompt, financeFinding), prompt)
	}
	for _, id := range []string{"finance-close", "podcast-edit", "line-scans"} {
		if !strings.Contains(prompt, "- "+id+" | ") {
			t.Fatalf("%s fell off the board:\n%s", id, prompt)
		}
	}
}
