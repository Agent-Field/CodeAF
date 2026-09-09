package tui3

// ── WHERE THE WORK ENDED UP IS A FACT AND NEVER A STATE ─────────────────────
//
// The head used to carry `delivery needs attention` over a conflicted merge and
// `stopped — branch kept` over an aborted one: two phrases that fused a state
// and a source-control fact into one, on the one row that is supposed to say the
// state once. What it carries now is `merged`, `branch kept`, or nothing — and
// the state is one cell to the left, in the tier word
// (docs/design/task-states/DESIGN.md).

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestTheHeadNamesTheMergeAsAFactAndNotAsAState(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	for _, tc := range []struct {
		merge, want string
		state       session.TaskState
	}{
		{merge: mergeWordMerged, want: "merged", state: session.TaskDone},
		{merge: mergeWordKept, want: taskBranchKept, state: session.TaskDone},
		{merge: mergeWordAborted, want: taskBranchKept, state: session.TaskUnverified},
		{merge: mergeWordConflicted, want: taskBranchKept, state: session.TaskUnverified},
	} {
		card := &taskDone{
			title:  "Write the guide",
			status: doneStatus(session.TaskFacts{State: tc.state, Merge: tc.merge, Branch: "task/guide"}),
		}
		line := plain(a.doneHead(card, 160, false))
		if !strings.Contains(line, " · "+tc.want) {
			t.Fatalf("a %q landing reads\n\t%q\nand should carry the fact %q", tc.merge, line, tc.want)
		}
		for _, gone := range []string{"delivery needs attention", "stopped — branch kept"} {
			if strings.Contains(line, gone) {
				t.Fatalf("a %q landing still says %q:\n\t%q", tc.merge, gone, line)
			}
		}
	}
}

// A FOLDER FAMILY REFUSES WITH NO BRANCH ANYWHERE, and `branch kept` about no
// branch is a pointer to nothing.
func TestAConflictWithNoBranchNamesNoBranch(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	card := &taskDone{
		title:  "Write the guide",
		status: doneStatus(session.TaskFacts{State: session.TaskUnverified, Merge: mergeWordConflicted}),
	}
	if line := plain(a.doneHead(card, 160, false)); strings.Contains(line, taskBranchKept) {
		t.Fatalf("a conflict with no branch claimed one was kept:\n\t%q", line)
	}
}

func TestExpandedCardDoesNotRepeatBranchInItsHeading(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	card := &taskDone{
		title: "Write the guide", branch: "task/guide", merge: mergeWordKept, open: true,
		status: doneStatus(session.TaskFacts{
			State: session.TaskDone, Merge: mergeWordKept, Branch: "task/guide"}),
	}
	if strings.Contains(plain(a.doneHead(card, 160, false)), card.branch) {
		t.Fatal("expanded heading repeats the branch")
	}
	rows := a.doneAccountRows(card, "Saved on the requested branch.", 78)
	if !strings.Contains(plainOf(rows), glyphHalted) {
		t.Fatal("work left on a branch drew no mark over the sentence saying so")
	}
}

// A BATCH ROW SAYS WHICH OF ITS CHILDREN IS NOT PLAIN NEWS, in the same mark the
// card would wear.
func TestBatchRowMarksTheChildThatIsStillAQuestion(t *testing.T) {
	a, _, _ := taskApp(t)
	card := &taskDone{
		title:  "Write the guide",
		status: doneStatus(session.TaskFacts{State: session.TaskUnverified, Merge: mergeWordAborted}),
	}
	if line := plain(a.rollupRow(card, 80, false)); !strings.Contains(line, glyphAsk) {
		t.Fatalf("batch row hides a question: %q", line)
	}
	done := &taskDone{
		title:  "Write the guide",
		status: doneStatus(session.TaskFacts{State: session.TaskDone, Merge: mergeWordMerged}),
	}
	if line := plain(a.rollupRow(done, 80, false)); strings.Contains(line, glyphAsk) {
		t.Fatalf("batch row marked a plain landing: %q", line)
	}
}
