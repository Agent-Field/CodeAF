package session

// A PIECE FOLDED INTO ITS PARENT'S REPORT IS SAID ONCE, ACROSS LIVES.
//
// The fold is a delivery like any other and owes what every delivery owes: a
// record the recipient keeps, and an acknowledgement written down only when that
// record holds it (task_latefold.go, [durableDelivery]). These tests take the
// checkpoint the running graph really writes ([TaskGraph.documentLocked]), carry
// it through JSON the way the disk does, and read it back through the same
// [TaskGraph.rehydrate] and [restoreNode] a restarted session uses.

import (
	"encoding/json"
	"strings"
	"testing"
)

// checkpointNow is the document this graph would write, round-tripped through
// JSON so that a field the record forgot to carry is lost here exactly as it
// would be lost on disk.
func checkpointNow(t *testing.T, graph *TaskGraph) taskDocument {
	t.Helper()
	graph.mu.Lock()
	document := graph.documentLocked()
	graph.mu.Unlock()
	wire, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("the checkpoint does not marshal: %v", err)
	}
	var back taskDocument
	if err := json.Unmarshal(wire, &back); err != nil {
		t.Fatalf("the checkpoint does not read back: %v", err)
	}
	return back
}

// foldedNest is a parent whose worker has stopped reading and whose landing is
// written, with one piece that has just come home into its report.
func foldedNest(t *testing.T) (*nest, *TaskNode) {
	t.Helper()
	nest := newNest(t, nil, nil)
	nest.handOut(t, "read the law")
	kid := nest.graph.children(nest.parent.id)[0]
	nest.parent.openRoom().speaking(nil)
	nest.parent.finish("the whole job is done", nil, "", "")
	kid.finish("the law is in section four", nil, "", "")
	nest.graph.complete(kid, TaskDone)
	waitFor(t, "the piece to be folded into its parent's report", func() bool {
		return strings.Contains(nest.parent.notice().Report, "the law is in section four")
	})
	return nest, kid
}

// THE FOLD IS THE ACKNOWLEDGEMENT. A folded piece is written down as announced
// on the checkpoint, so the next life of the session restores it as history and
// does not tell its landing again — to the person, once the parent has settled,
// which is the very delivery the fold exists to stop.
func TestAFoldedPieceIsNotToldAgainAfterARestart(t *testing.T) {
	nest, kid := foldedNest(t)

	document := checkpointNow(t, nest.graph)
	if !recordOf(t, document, kid.id).Noted {
		t.Fatal("the folded piece is not written down as announced, so a restart would tell it again")
	}

	fresh := newTaskGraph()
	fresh.rehydrate(document, t.TempDir(), TaskSettleAsk)
	restored := fresh.node(kid.id)
	if restored == nil {
		t.Fatal("the folded piece was not restored")
	}
	if !restored.reported() {
		t.Fatal("the restored piece is still owed a telling, so its landing would be delivered a second time")
	}
}

// A FOLD AFTER A RESTORE ADDS ITS PIECE AND NOTHING ELSE. The record carries the
// report's two halves ([taskRecord.Late], [taskRecord.Landed]), so a restored node
// composes from what its landing wrote and what was folded into it — not from the
// composed report taken as though it were the landing, which would carry the
// first fold twice.
func TestTwoFoldsAcrossARestoreKeepOneOfEach(t *testing.T) {
	nest, _ := foldedNest(t)
	first := nest.parent.notice().Report

	fresh := newTaskGraph()
	parent := restoreNode(fresh, recordOf(t, checkpointNow(t, nest.graph), nest.parent.id))
	fresh.nodes[parent.id] = parent
	fresh.order = append(fresh.order, parent.id)
	if got := parent.notice().Report; got != first {
		t.Fatalf("the restored report is %q, want the one that was written down %q", got, first)
	}
	// Open again, as a continued node is (task_continue.go): the fold only takes
	// news for a node that has not settled.
	fresh.mu.Lock()
	parent.state = TaskRunning
	fresh.mu.Unlock()

	if !parent.foldLatePart("task 3 done: count the clauses\nthere are nine") {
		t.Fatal("an open restored parent refused a piece")
	}
	report := parent.notice().Report
	if strings.Count(report, "the law is in section four") != 1 {
		t.Fatalf("the first piece is in the report %d times after a second fold:\n%s",
			strings.Count(report, "the law is in section four"), report)
	}
	if strings.Count(report, "the whole job is done") != 1 || !strings.Contains(report, "there are nine") {
		t.Fatalf("the report after a second fold is not the landing plus both pieces:\n%s", report)
	}

	// AND A LANDING WRITTEN AFTER BOTH DOES NOT ERASE EITHER.
	parent.finish("the whole job is done, again", nil, "", "")
	report = parent.notice().Report
	for _, want := range []string{"the whole job is done, again", "the law is in section four", "there are nine"} {
		if strings.Count(report, want) != 1 {
			t.Fatalf("the report after a second landing holds %q %d times:\n%s", want, strings.Count(report, want), report)
		}
	}
}

// THE FOLD KEEPS WHAT THE SENDER SAID A RECORD SHOULD KEEP, and writes nothing of
// its own. A landing note opens by telling a model which word to say back; the
// report a person reads keeps the landing's head line and report instead
// ([delivery.record]). A message with no record of its own is its own record.
func TestTheFoldKeepsTheSendersRecordAndNothingElse(t *testing.T) {
	nest := newNest(t, nil, nil)
	nest.parent.finish("the whole job is done", nil, "", "")
	fold := landingFold{at: conversationOf(nest.parent), node: nest.parent}

	got := fold.accept(delivery{origin: fromRuntime, kind: msgResult,
		note: userText("say that word back and no other"), record: "task 9 done: the index"})
	if !got.accepted() || got.reader != nil {
		t.Fatalf("the fold answered %+v, want an acceptance with no reader to wake", got)
	}
	fold.accept(delivery{origin: fromRuntime, kind: msgResult,
		note: userText("task 7 has finished, and task 9 is now waiting on you rather than on it.")})

	want := withReport("the whole job is done",
		"task 9 done: the index\n\ntask 7 has finished, and task 9 is now waiting on you rather than on it.")
	if report := nest.parent.notice().Report; report != want {
		t.Fatalf("the report is\n%s\nwant\n%s", report, want)
	}
}
