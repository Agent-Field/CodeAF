package tui3

import (
	"strings"
	"testing"
)

func TestAcceptedWorkWithFailedDeliveryDoesNotWearAnUnqualifiedDone(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	for _, merge := range []string{mergeWordConflicted, mergeWordAborted} {
		card := &taskDone{title: "Write the guide", merge: merge, branch: "task/guide"}
		line := plain(a.doneHead(card, 160, false))
		if !strings.Contains(line, doneDeliveryWord) || strings.Contains(line, " · done") {
			t.Fatalf("accepted work hid failed delivery: %s", line)
		}
	}
}

func TestIntentionallyKeptBranchIsNotALabelledDeliveryFailure(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	card := &taskDone{title: "Write the guide", merge: mergeWordKept, branch: "task/guide"}
	line := plain(a.doneHead(card, 160, false))
	if strings.Contains(line, doneDeliveryWord) || !strings.Contains(line, taskBranchKept) {
		t.Fatalf("kept branch was mislabeled: %s", line)
	}
}

func TestExpandedCardDoesNotRepeatBranchInItsHeading(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	card := &taskDone{title: "Write the guide", merge: mergeWordKept, branch: "task/guide", open: true}
	if strings.Contains(plain(a.doneHead(card, 160, false)), card.branch) {
		t.Fatal("expanded heading repeats the branch")
	}
	rows := a.doneAccountRows(card, "Saved on the requested branch.", 78)
	if strings.Contains(plainOf(rows), glyphHalted) {
		t.Fatal("intentionally kept branch drew a failure mark")
	}
}

func TestBatchRowMarksFailedDeliveryAfterWorkWasAccepted(t *testing.T) {
	a, _, _ := taskApp(t)
	card := &taskDone{title: "Write the guide", merge: mergeWordAborted}
	line := plain(a.rollupRow(card, 80, false))
	if !strings.Contains(line, glyphHalted) {
		t.Fatalf("batch row hides failed delivery: %q", line)
	}
}
