package head

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// spliceOriginJob seeds one job root under a chosen origin and session. The
// three visibility rules disagreed about exactly this: which origin a job
// carries, and which conversation started it.
func spliceOriginJob(t *testing.T, graph *store.Store, origin store.Origin, session, id, title, brief string) {
	t.Helper()
	provenance := store.Provenance{Origin: origin, SessionID: session, Intent: brief}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: id, Title: title, Brief: brief, Stage: 1},
	}}, provenance); err != nil {
		t.Fatalf("splice %s: %v", id, err)
	}
}

// TestCharterFiredJobIsListableAndCancellable is the transcript, pinned.
//
// A charter-fired job carries OriginTrigger. It passed renderGraph, which
// filtered nothing; it passed beltAddressable, which excludes only OriginSelf;
// and it failed boardRows, which additionally demanded OriginUser. So the head
// showed the user the job and answered questions about it, the user said "stop
// that job", and the belt replied that there is no work of the user's with that
// id. One conversation, one table, three rules, and the head denying a job it
// had just described.
//
// There is one table now — the prompt's board and the belt's board are the same
// query through the same renderer — so the three rules can no longer disagree by
// construction. The test still walks all three surfaces, because "they cannot
// disagree" is a claim about the code and this is the evidence for it.
func TestCharterFiredJobIsListableAndCancellable(t *testing.T) {
	graph := openHeadStore(t)
	spliceOriginJob(t, graph, store.OriginTrigger, "membrane",
		"firing-charter-1-1", "Nightly security sweep", "sweep the new pull requests for vulnerabilities")

	// One: the prompt the head speaks from describes it.
	head := New(nil, graph)
	board, err := head.renderTurnBoard("membrane", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(board, "firing-charter-1-1") {
		t.Fatalf("the board the head speaks from omits the charter job:\n%s", board)
	}

	// Two: the belt lists it, which is the read the loop chooses a target from.
	rows, err := head.boardRows("membrane", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, row := range rows {
		listed = listed || row.node.ID == "firing-charter-1-1"
	}
	if !listed {
		t.Fatalf("the board denies the job the snapshot just described: %s", renderBoard(rows))
	}

	// Three: and the verb reaches it, which is the sentence that failed.
	run := &beltRun{head: head, user: store.Message{SessionID: "membrane", Body: "stop that job"}}
	result, failed := run.control(map[string]any{"verb": "cancel", "ids": []any{"firing-charter-1-1"}})
	if failed {
		t.Fatalf("cancelling a charter-fired job was refused: %s", result)
	}
	if targets := pendingTargets(t, graph, store.CommandCancel); !containsTarget(targets, "firing-charter-1-1") {
		t.Fatalf("no cancel was journalled for the charter job: %v", targets)
	}
}

// The membrane widened in one direction only. The resident's own work stays off
// every one of the three surfaces, which is the law beltAddressable was written
// for and the reason it was the right rule to standardise on.
func TestTheOneMembraneStillHidesTheResidentsOwnWork(t *testing.T) {
	graph := openHeadStore(t)
	spliceOriginJob(t, graph, store.OriginSelf, "", "self-upkeep", "Practice", "practice the weak spot")
	spliceOriginJob(t, graph, store.OriginUser, "membrane", "user-job", "Finance close", "close the books")

	head := New(nil, graph)
	board, err := head.renderTurnBoard("membrane", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(board, "self-upkeep") {
		t.Fatalf("the resident's own work reached the board:\n%s", board)
	}
	if !strings.Contains(board, "user-job") {
		t.Fatalf("the user's own work fell off the board:\n%s", board)
	}

	rows, err := head.boardRows("membrane", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.node.ID == "self-upkeep" {
			t.Fatal("the resident's own work reached the board")
		}
	}
	run := &beltRun{head: head, user: store.Message{SessionID: "membrane", Body: "cancel that"}}
	if answer, failed := run.control(map[string]any{"verb": "cancel", "ids": []any{"self-upkeep"}}); !failed ||
		!strings.Contains(answer, "not the user's work") {
		t.Fatalf("the belt let a verb reach the resident's own work: failed=%t %s", failed, answer)
	}
}

// TestCrossSessionWorkIsMarkedRatherThanHidden pins #41's decision. The board is
// global and the thread is single-session, so with a terminal and a browser open
// one conversation can amend the other's work while neither can say where the
// job came from. The snapshot IS the workforce, so the row stays; what it gains
// is provenance, on the line that was already being sent.
func TestCrossSessionWorkIsMarkedRatherThanHidden(t *testing.T) {
	graph := openHeadStore(t)
	spliceOriginJob(t, graph, store.OriginUser, "terminal", "local-job", "Ledger", "reconcile the ledger")
	spliceOriginJob(t, graph, store.OriginUser, "browser", "remote-job", "Podcast", "edit the podcast")

	head := New(nil, graph)
	board, err := head.renderTurnBoard("terminal", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(board, "\n") {
		switch {
		case strings.Contains(line, "local-job") && strings.Contains(line, crossSessionMark):
			t.Fatalf("this window's own work was marked as somebody else's: %q", line)
		case strings.Contains(line, "remote-job") && !strings.Contains(line, crossSessionMark):
			t.Fatalf("the other window's work arrived unmarked: %q", line)
		}
	}

	rows, err := head.boardRows("terminal", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	rendered := renderBoard(rows)
	if !strings.Contains(rendered, "remote-job") {
		t.Fatalf("the board filtered the other window's work away instead of marking it:\n%s", rendered)
	}
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "remote-job") != strings.Contains(line, crossSessionMark) {
			t.Fatalf("the belt's read disagrees with the prompt's about provenance: %q", line)
		}
	}
	// The two boards that used to disagree are now one function called twice, so
	// the marker cannot mean one thing in the prompt and another in a tool
	// result. That equality is the assertion: same rows, same marks.
	if strings.TrimSpace(board) != strings.TrimSpace(rendered) {
		t.Fatalf("the prompt's board and the belt's read have drifted apart:\nprompt:\n%s\nbelt:\n%s",
			board, rendered)
	}
	// Work with no session at all — the resident's, a charter's — is nobody's
	// window and must never be marked as another person's.
	if crossSession(store.Node{}, "terminal") {
		t.Error("sessionless work was attributed to another window")
	}
}
