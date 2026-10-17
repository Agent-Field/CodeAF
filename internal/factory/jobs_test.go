package factory

import (
	"testing"
	"time"
)

func TestJobCanvasStatesAndReasons(t *testing.T) {
	for _, tc := range []struct {
		item                Item
		state, reason, done string
	}{
		{Item{State: StateNew}, "backlog", "", ""},
		{Item{State: StateQueued}, "queued", "", ""},
		{Item{State: StateRunning}, "running", "step", ""},
		{Item{State: StateNeedsYou, QKind: QKindAskFirst, AskedFirstReason: "touches auth/"}, "needs_you", "approval_start", ""},
		{Item{State: StateNeedsYou, QKind: QKindApprove}, "needs_you", "approval_plan", ""},
		{Item{State: StateLanded}, "needs_you", "approval_proof", ""},
		{Item{State: StateNeedsYou, QKind: "cap"}, "needs_you", "budget", ""},
		{Item{State: StateShipped, PullRequest: &PullRequest{State: PROpen}}, "running", "github", ""},
		{Item{State: StateShipped, PullRequest: &PullRequest{State: PROpen, ReviewDecision: "changes_requested"}}, "needs_you", "changes_requested", ""},
		{Item{State: StateShipped, PullRequest: &PullRequest{State: PRMerged}}, "done", "", "merged"},
		{Item{State: StateShipped, PullRequest: &PullRequest{State: PRClosed}}, "done", "", "closed"},
		{Item{State: StateShipped, Publish: "comment"}, "done", "", "answered"},
		{Item{State: StateDismissed, DoneHow: "dropped"}, "done", "", "dropped"},
		{Item{State: StateNew, Failure: &Failure{Code: CodeMachineLost}}, "needs_you", "runner_offline", ""},
		{Item{State: StateLanded, Publishing: true, PublishStatus: &Publishing{State: "failed"}}, "needs_you", "publish_failed", ""},
	} {
		if state, reason, done := JobState(tc.item), JobReason(tc.item), JobDoneHow(tc.item); state != tc.state || reason != tc.reason || done != tc.done {
			t.Errorf("%+v => %s %s %s, want %s %s %s", tc.item, state, reason, done, tc.state, tc.reason, tc.done)
		}
	}
}

func TestPriorityAndSizeAreLabelDefaultsWithPersonOverride(t *testing.T) {
	for _, tc := range []struct {
		labels []string
		want   string
	}{
		{[]string{"P0"}, "urgent"}, {[]string{"priority:high"}, "high"}, {[]string{"bug"}, ""}, {[]string{"priority:medium"}, "medium"}, {[]string{"p2"}, "medium"}, {[]string{"p3"}, "low"},
		{[]string{"bug", "priority:low"}, "low"}, {[]string{"P3", "P1"}, "high"}, {[]string{"other"}, ""},
	} {
		it := Item{Labels: tc.labels, Triage: Triage{Priority: 1, Size: "L"}}
		got := ""
		if p := PriorityFor(it); p != nil {
			got = p.Value
		}
		if got != tc.want {
			t.Errorf("%v => %s", tc.labels, got)
		}
		if JobSize(it) != "" {
			t.Fatal("AI size reached the job field")
		}
	}
	for _, size := range []string{"XS", "S", "M", "L", "XL"} {
		if got := JobSize(Item{Labels: []string{"size:" + size}}); got != size {
			t.Fatalf("size %s => %s", size, got)
		}
	}
	it := Item{Labels: []string{"P0"}, Priority: &JobPriority{Value: "low", SetBy: "sam"}}
	if p := PriorityFor(it); p.Value != "low" || p.SetBy != "sam" {
		t.Fatalf("override %+v", p)
	}
	// A bug label alone never invents a mark, and a persisted human mark survives it.
	if p := PriorityFor(Item{Labels: []string{"bug"}, Triage: Triage{Type: "bug", Priority: 1}}); p != nil {
		t.Fatalf("bug label invented %+v", p)
	}
	it = Item{Labels: []string{"bug"}, Priority: &JobPriority{Value: "medium", SetBy: "sam"}}
	if p := PriorityFor(it); p == nil || p.Value != "medium" || p.SetBy != "sam" {
		t.Fatalf("persisted mark lost %+v", p)
	}
	if ref := JobRef(Item{Repo: "acme/api", Num: 1498}); ref != "api#1498" {
		t.Fatal(ref)
	}
}

func TestDismissalAndOverlapAreDerived(t *testing.T) {
	a := Item{ID: 1, Repo: "acme/api", State: StateRunning, DismissedBy: []string{"sam"}, Change: &Change{Files: []ChangedFile{{Path: "auth.go"}}}}
	b := Item{ID: 2, Repo: a.Repo, State: StateRunning, Change: &Change{Files: []ChangedFile{{Path: "auth.go"}}}}
	if !IsDismissed(a, "@Sam") || IsDismissed(a, "mei") || JobState(a) != "running" || !Overlaps(a, b) {
		t.Fatal("derived dismissal or overlap lost")
	}
	b.State = StateQueued
	if Overlaps(a, b) {
		t.Fatal("queued job overlaps")
	}
	b.State = StateRunning
	b.Repo = "other/api"
	if Overlaps(a, b) {
		t.Fatal("different repo overlaps")
	}
	b.Repo = a.Repo
	b.Change = nil
	b.PlannedFiles = []string{"auth.go"}
	if Overlaps(a, b) {
		t.Fatal("planned paths are not changed paths")
	}
	if !Match("is:dismissed", "sam")(a) || Match("is:dismissed", "mei")(a) {
		t.Fatal("dismissed query crossed people")
	}
	if ApprovalUndo != 5*time.Second || DroppedBranchRetention != 7*24*time.Hour {
		t.Fatal("canvas timing changed")
	}
}

func TestJobSizeAcceptsBothSpellingsAndTakesTheLargest(t *testing.T) {
	for _, tc := range []struct {
		labels []string
		want   string
	}{
		{[]string{"size/m"}, "M"}, {[]string{"Size: XS"}, "XS"}, {[]string{"size:s", "size/L"}, "L"},
		{[]string{"size:xl", "size:m"}, "XL"}, {[]string{"size:huge", "size:s"}, "S"}, {[]string{"size"}, ""},
	} {
		if got := JobSize(Item{Labels: tc.labels}); got != tc.want {
			t.Errorf("%v => %q, want %q", tc.labels, got, tc.want)
		}
	}
}
