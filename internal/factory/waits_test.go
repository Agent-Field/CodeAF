package factory

import (
	"reflect"
	"strings"
	"testing"
)

// W1, W3, W4: parents satisfy in order, and a repaired parent waits again.
func TestWaitsRules(t *testing.T) {
	child := Item{ID: 3, Repo: "owner/api", State: StateNew, WaitsOn: []int{1, 2}}
	parents := map[int]Item{1: {ID: 1, Repo: "owner/api", Num: 1107}, 2: {ID: 2, Repo: "owner/api"}}
	if got := EvaluateWaits(child, parents); !reflect.DeepEqual(got.WaitingOn, []int{1, 2}) || got.Released || got.Blocked != nil {
		t.Fatalf("waiting = %+v", got)
	}
	parents[1] = Item{ID: 1, PullRequest: &PullRequest{State: PRMerged}}
	if got := EvaluateWaits(child, parents); !reflect.DeepEqual(got.WaitingOn, []int{2}) {
		t.Fatalf("one merged = %+v", got)
	}
	parents[2] = Item{ID: 2, Repo: "owner/api", State: StateShipped, CommentURL: "https://github.com/owner/api/issues/2#issuecomment-1"}
	if got := EvaluateWaits(child, parents); !got.Released || len(got.WaitingOn) != 0 || got.Blocked != nil {
		t.Fatalf("released = %+v", got)
	}
	for _, tc := range []struct {
		name   string
		parent Item
		why    string
	}{
		{"step", Item{Failure: &Failure{Step: "test"}, Stages: []Stage{{Name: "test", Label: "Test"}}}, "failed at Test"},
		{"phase", Item{Stream: &Stream{Phases: []Phase{{Name: "test", State: PhaseFailed, Failure: &Failure{}}}}, Stages: []Stage{{Name: "test", Label: "Test"}}}, "failed at Test"},
		{"publish", Item{PublishError: "refused"}, "failed to publish"},
		{"publishing", Item{PublishStatus: &Publishing{State: "failed"}}, "failed to publish"},
		{"merge", Item{Merging: &MergeRun{State: MergeFailed}}, "failed to merge"},
		{"closed PR", Item{PullRequest: &PullRequest{State: PRClosed}}, "closed without merging"},
		{"dropped", Item{DoneHow: "dropped"}, "was dropped"},
		{"local close", Item{DoneHow: "closed_locally"}, "was closed locally"},
		{"dismissed", Item{State: StateDismissed}, "was dismissed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := tc.parent
			parent.ID, parent.Repo, parent.Num = 1, "owner/api", 1107
			parents[1] = parent
			got := EvaluateWaits(child, parents)
			if got.Blocked == nil || got.Blocked.By != BlockWaitsOn || got.Blocked.Job != 1 || !strings.Contains(got.Blocked.Reason, "api#1107") || !strings.Contains(got.Blocked.Reason, tc.why) {
				t.Fatalf("blocked = %+v", got.Blocked)
			}
			parents[1] = Item{ID: 1, Repo: "owner/api"}
			if fixed := EvaluateWaits(child, parents); fixed.Blocked != nil || fixed.Released || !reflect.DeepEqual(fixed.WaitingOn, []int{1}) {
				t.Fatalf("fixed = %+v", fixed)
			}
		})
	}
}

// W3, W4: a person's Stop is waiting, and a merged PR satisfies even an old failure.
func TestWaitsStopAndMerge(t *testing.T) {
	parent := Item{ID: 1, Failure: &Failure{Code: CodeStopped}}
	child := Item{WaitsOn: []int{1}}
	if got := EvaluateWaits(child, map[int]Item{1: parent}); got.Blocked != nil || got.Released {
		t.Fatalf("stopped parent = %+v", got)
	}
	parent.PullRequest = &PullRequest{State: PRMerged}
	if got := EvaluateWaits(child, map[int]Item{1: parent}); !got.Released {
		t.Fatalf("merged parent = %+v", got)
	}
	if got := EvaluateWaits(Item{WaitsOn: []int{12}}, map[int]Item{12: {ID: 12, DoneHow: "closed_locally"}}); got.Blocked == nil || !strings.Contains(got.Blocked.Reason, "job 12") {
		t.Fatalf("local reference = %+v", got)
	}
}

// W5, W6: waits are inert after any start, Stop or release.
func TestWaitsInert(t *testing.T) {
	for _, it := range []Item{
		{State: StateQueued}, {State: StateRunning}, {Stream: &Stream{}}, {WaitsReleased: true}, {Failure: &Failure{Code: CodeStopped}},
	} {
		it.WaitsOn = []int{1}
		if WaitsPending(it) || !EvaluateWaits(it, nil).Released {
			t.Fatalf("waits remain active: %+v", it)
		}
	}
}

// W2, W6, W10: shape bounds are shared by the service and engine.
func TestWaitsIDs(t *testing.T) {
	for _, ids := range [][]int{{0}, {-1}, {1, 1}, make([]int, WaitsOnMost+1)} {
		if err := CheckWaitsIDs(ids); err == nil {
			t.Fatalf("accepted %v", ids)
		}
	}
	for _, ids := range [][]int{nil, {}, {1, 2}} {
		if err := CheckWaitsIDs(ids); err != nil {
			t.Fatal(err)
		}
	}
}
