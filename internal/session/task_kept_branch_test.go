package session

import (
	"strings"
	"testing"
	"time"
)

// THE RECORD LINE AND THE `tasks` TEXT NAME A NON-VERIFIED NODE'S KEPT BRANCH.
//
// A `/task` node that ends failed or unverified keeps its deliverable on its own
// `task/<slug>` branch and does NOT merge it home ([keptWork], #1178). The
// headless `do` envelope names that branch and its verdict (#1182); the chat side
// must say the same two facts where a person and a model read them: the record
// line a resumed session opens with ([taskRecovery.note]) and the plain text the
// `tasks` tool answers with ([taskRowText]).
//
// The defect this pins: today only INTERRUPTED nodes name a kept branch
// ([taskRecovery.branches], filled in [taskRecovery.reconcile]), and a
// failed/unverified node is counted by [taskRecovery.countSettled] with its
// branch dropped — so a person told "1 incomplete" has no way to find the work
// from the record. And the index row a `tasks` answer is built from carries no
// branch field at all ([TaskIndexEntry]), so the tool names neither the branch
// nor the record's own verdict word.
func TestRecoveryNoteNamesAFailedNodesKeptBranch(t *testing.T) {
	graph := &TaskGraph{}
	recovery := graph.rehydrate(taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 1,
		Nodes: []taskRecord{{
			ID: 1, Title: "Fix the nil-map crash", Brief: "b", Acceptance: "a",
			State: TaskFailed, Merge: mergeAborted, Branch: "task/fix-the-nil-map-crash-9c1a2f",
			Changed: []string{"state.go"},
		}},
	}, t.TempDir(), TaskSettleAsk)
	if recovery.failed != 1 {
		t.Fatalf("recovery counted %+v, want the one failed node", recovery)
	}
	note := recovery.note()
	if !strings.Contains(note, "(branch task/fix-the-nil-map-crash-9c1a2f kept)") {
		t.Fatalf("the record line does not name the failed node's kept branch: %q", note)
	}
}

// The same law over the other non-verified word. An unverified node keeps its
// branch too — a check nobody could pass is not a reason to throw the work away.
func TestRecoveryNoteNamesAnUnverifiedNodesKeptBranch(t *testing.T) {
	graph := &TaskGraph{}
	recovery := graph.rehydrate(taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 1,
		Nodes: []taskRecord{{
			ID: 1, Title: "Hidden rental digs", Brief: "find them", Acceptance: "a list",
			State: TaskUnverified, Merge: mergeConflicted, Branch: "task/hidden-rental-digs-4be2c1",
			Changed: []string{"notes.md"},
		}},
	}, t.TempDir(), TaskSettleAsk)
	if recovery.unverified != 1 {
		t.Fatalf("recovery counted %+v, want the one unverified node", recovery)
	}
	note := recovery.note()
	if !strings.Contains(note, "(branch task/hidden-rental-digs-4be2c1 kept)") {
		t.Fatalf("the record line does not name the unverified node's kept branch: %q", note)
	}
}

// A NODE WITH NOTHING KEPT SAYS NOTHING ABOUT A BRANCH. A failed node that never
// reached a repository has no branch to name, and a clause about one would send
// a person looking for work that was never there.
func TestRecoveryNoteSaysNoBranchForANodeThatKeptNone(t *testing.T) {
	graph := &TaskGraph{}
	recovery := graph.rehydrate(taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 1,
		Nodes: []taskRecord{{
			ID: 1, Title: "Read the journals", Brief: "b", Acceptance: "a",
			State: TaskFailed, Merge: mergeInPlace,
		}},
	}, t.TempDir(), TaskSettleAsk)
	if note := recovery.note(); strings.Contains(recoverySummaryLine(note), "branch") {
		t.Fatalf("the record line invented a branch for an in-place failure: %q", note)
	}
}

// recoverySummaryLine is the one-line graph summary at the head of a recovery
// note, before any delivered completion note is attached under it.
func recoverySummaryLine(note string) string {
	if at := strings.Index(note, "\n"); at >= 0 {
		return note[:at]
	}
	return note
}

// A FAILED ROW IN THE `tasks` TEXT NAMES ITS KEPT BRANCH AND ITS VERDICT, in the
// record's own words — the branch string verbatim, and `failed` from
// [TaskFailed] — so a reader comparing the answer against #1182's envelope reads
// one vocabulary, not two.
func TestTasksTextNamesAFailedRowsKeptBranchAndVerdict(t *testing.T) {
	now := time.Now().Add(-time.Minute)
	rows := []TaskIndexEntry{{
		ID: "7", Title: "Fix the nil-map crash", Name: "fix-the-nil-map-crash",
		Status: string(TaskFailed), SessionID: "s", EndedAt: now,
		Branch:      "task/fix-the-nil-map-crash-9c1a2f",
		ArtifactURI: "git:task/fix-the-nil-map-crash-9c1a2f",
	}}
	text := taskRowsText(rows, "")
	if !strings.Contains(text, "kept branch task/fix-the-nil-map-crash-9c1a2f") {
		t.Fatalf("the tasks text does not name the kept branch:\n%s", text)
	}
	if !strings.Contains(text, "verdict failed") {
		t.Fatalf("the tasks text does not name the verdict:\n%s", text)
	}
	// The artifact no longer repeats the branch it was kept on: the reader gets
	// the branch once, from the clause that names it plainly.
	if strings.Contains(text, "artifact git:") {
		t.Fatalf("the tasks text repeats the kept branch as an artifact:\n%s", text)
	}
}

// The same law over `unverified`.
func TestTasksTextNamesAnUnverifiedRowsKeptBranchAndVerdict(t *testing.T) {
	now := time.Now().Add(-time.Minute)
	rows := []TaskIndexEntry{{
		ID: "9", Title: "Hidden rental digs", Name: "hidden-rental-digs",
		Status: string(TaskUnverified), SessionID: "s", EndedAt: now,
		Branch: "task/hidden-rental-digs-4be2c1",
	}}
	text := taskRowsText(rows, "")
	if !strings.Contains(text, "kept branch task/hidden-rental-digs-4be2c1") {
		t.Fatalf("the tasks text does not name the kept branch:\n%s", text)
	}
	if !strings.Contains(text, "verdict unverified") {
		t.Fatalf("the tasks text does not name the verdict:\n%s", text)
	}
}

// THE BRANCH REACHES THE ROW FROM THE NODE, not only from a hand-built entry. A
// settled node that kept its work hands its branch to the index row it writes.
func TestTheTaskIndexCarriesAFailedNodesKeptBranch(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{
		graph:  graph,
		id:     4,
		state:  TaskFailed,
		spec:   taskSpec{title: "fix the nil-map crash"},
		branch: "task/fix-the-nil-map-crash-9c1a2f",
		merge:  mergeAborted,
	}
	graph.mu.Lock()
	entry := node.indexEntryLocked("aaaa1111aaaa1111")
	graph.mu.Unlock()
	if entry.Branch != "task/fix-the-nil-map-crash-9c1a2f" {
		t.Fatalf("the row's branch = %q, want the node's kept branch", entry.Branch)
	}
}
