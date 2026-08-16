package head

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The night this is about. task-3171 ran out of budget mid-thought, completed
// with half a sentence — "the file conflicted, let me clean up and run the test
// properly" — and the split stamp saying its remainder had been re-planned. The
// continuation landed a minute later saying the opposite: everything verified,
// the browser builds and launches. A hundred seconds after that the user asked
// how it was going and the head narrated the parent's half-sentence in the
// present tense, as though the conflict were still open and nothing had followed
// it. Nothing was running; the person watching could see that.
func seedSplitJob(t *testing.T, graph *store.Store, pieceSummary string, pieceDone bool) store.Node {
	t.Helper()
	spliceSurgeryJob(t, graph, "task-3171", "UI browser", "build a UI browser and launch it")
	completeNodeWith(t, graph, "task-3171",
		"The navtest_main.go conflicted. Let me clean up and run just the test properly.\n\n["+
			resident.OverrunContinuationMessage(1)+"]")
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "task-3171-x1", Title: "Finish the remainder", Brief: "finish what the previous agent started", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginSelf, SessionID: "surgery", Intent: "finish the remainder"}); err != nil {
		t.Fatal(err)
	}
	if pieceDone {
		completeNodeWith(t, graph, "task-3171-x1", pieceSummary)
	}
	node, found, err := graph.Node("task-3171")
	if err != nil || !found {
		t.Fatalf("read task-3171: found=%t err=%v", found, err)
	}
	return node
}

func TestSplitParentPresentsItsContinuationRatherThanItsOwnStaleSummary(t *testing.T) {
	graph := openHeadStore(t)
	parent := seedSplitJob(t, graph, "Everything is verified. The browser builds cleanly and launches a window.", true)

	result := New(nil, graph).jobResult(parent)
	if !strings.Contains(result, "task-3171-x1 (done)") {
		t.Fatalf("result does not name the piece that carries on: %q", result)
	}
	if !strings.Contains(result, "Everything is verified") {
		t.Fatalf("result does not carry the continuation's own account: %q", result)
	}
	if strings.Contains(result, "Let me clean up") {
		t.Fatalf("result still narrates the parent's half-sentence: %q", result)
	}
}

// The board is the read a status question is answered from, so the substitution
// has to survive the board's own budgeting and truncation.
//
// The read is an AIMED one here, and that is the shape of the question rather
// than a weakening of it: a job that ran out of budget and was continued is over,
// and the moving board is what is moving. "How is the browser build going" names
// the work, and a read aimed by the person's own words or by an id reaches
// settled work — which is the only reason result and read are usable at all.
func TestBoardRowForASplitJobShowsTheLandedPiece(t *testing.T) {
	graph := openHeadStore(t)
	seedSplitJob(t, graph, "Everything is verified. The browser builds cleanly and launches a window.", true)

	head := New(nil, graph)
	rows, err := head.boardRowsAt("surgery", "", "", "task-3171", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	board := renderBoard(rows)
	row := ""
	for _, line := range strings.Split(board, "\n") {
		if strings.HasPrefix(line, "- task-3171 |") {
			row = line
		}
	}
	if !strings.Contains(row, "continued as task-3171-x1 (done)") ||
		!strings.Contains(row, "Everything is verified") {
		t.Fatalf("board row = %q\nboard:\n%s", row, board)
	}
}

// A continuation still running has no result of its own yet, and inventing one
// out of the parent's stale summary is the same lie in a quieter voice. The
// chain is still named, because "who is carrying this now" is the answer.
func TestSplitParentWithAnUnfinishedPieceSaysSoRatherThanReverting(t *testing.T) {
	graph := openHeadStore(t)
	parent := seedSplitJob(t, graph, "", false)

	result := New(nil, graph).jobResult(parent)
	if !strings.Contains(result, "task-3171-x1 (pending)") ||
		!strings.Contains(result, "nothing recorded there yet") {
		t.Fatalf("unfinished continuation result = %q", result)
	}
}

// A node with no split stamp is read exactly as before: this is a correction to
// one shape of summary, not a new layer over every read in the package.
func TestOrdinaryJobResultIsUntouched(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "plain", "Plain job", "do the plain thing")
	completeNodeWith(t, graph, "plain", "It is done and the notes are in /tmp/plain.md")
	node, _, err := graph.Node("plain")
	if err != nil {
		t.Fatal(err)
	}
	if got := New(nil, graph).jobResult(node); got != "It is done and the notes are in /tmp/plain.md" {
		t.Fatalf("plain result = %q", got)
	}
}
