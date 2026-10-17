package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

var runMergeHead = strings.Repeat("1", 40)

// fakeForge answers follows from a script and records every merge it is
// asked for, so a test can prove a merge was sent once or not at all.
type fakeForge struct {
	follows  []factory.PullRequest
	followed int
	writeErr error
	wrote    []factory.Action
}

func (f *fakeForge) Name() string { return "fake" }
func (f *fakeForge) Read(context.Context, string) ([]factory.Item, string, error) {
	return nil, "", nil
}
func (f *fakeForge) Write(_ context.Context, a factory.Action) (factory.Receipt, error) {
	f.wrote = append(f.wrote, a)
	if f.writeErr != nil {
		return factory.Receipt{}, f.writeErr
	}
	return factory.Receipt{ID: "m3rg3", URL: a.Item.PullRequest.URL, Number: a.Item.PullRequest.Number}, nil
}
func (f *fakeForge) Follow(context.Context, factory.Item) (factory.PullRequest, error) {
	i := min(f.followed, len(f.follows)-1)
	f.followed++
	return f.follows[i], nil
}

func cleanPR() factory.PullRequest {
	return factory.PullRequest{
		Number: 42, URL: "https://github.com/acme/api/pull/42", State: factory.PROpen,
		HeadSHA: runMergeHead, Mergeable: factory.MergeableYes,
		ChecksRead: true, Checks: factory.PRChecks{Passed: 2}, ReviewsRead: true,
	}
}

func mergeRig(t *testing.T, f *fakeForge, m factory.MergeRun) (*Runner, *store.Store, int) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pr := cleanPR()
	it, err := st.Create(factory.Item{
		Repo: "acme/api", Num: 12, Kind: factory.KindIssue, State: factory.StateShipped,
		Publish: "pr", HeadCommit: runMergeHead, PullRequest: &pr,
		Proof:   []factory.Claim{{Text: "works", OK: true}},
		Merging: &m,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := New(Options{Store: st, Source: func(string) factory.Source { return f }})
	return r, st, it.ID
}

func settleMerge(t *testing.T, r *Runner, st *store.Store, id int) factory.Item {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); r.Running(id); time.Sleep(2 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the merge never finished")
		}
	}
	it, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func TestRunnerMergesExactlyTheCommitOnAFreshRead(t *testing.T) {
	f := &fakeForge{follows: []factory.PullRequest{cleanPR()}}
	r, st, id := mergeRig(t, f, factory.MergeRun{State: factory.MergeQueued, By: "owner", Commit: runMergeHead, Method: factory.MergeMethodSquash})
	if err := r.Merge(id, runMergeHead); err != nil {
		t.Fatal(err)
	}
	it := settleMerge(t, r, st, id)
	if len(f.wrote) != 1 || f.wrote[0].Verb != "merge" || f.wrote[0].Commit != runMergeHead || f.wrote[0].Method != factory.MergeMethodSquash {
		t.Fatalf("forge asked %+v", f.wrote)
	}
	if it.Merging == nil || it.Merging.State != factory.MergeMerged || it.PullRequest.State != factory.PRMerged || it.PullRequest.MergedBy != "owner" {
		t.Fatalf("merged item = merging %+v pr %+v", it.Merging, it.PullRequest)
	}
	rep, _ := Diff(factory.Item{Merging: &factory.MergeRun{State: factory.MergeMerging}}, it)
	if rep.Merge == nil || rep.Merge.State != factory.MergeMerged || rep.Merge.SHA != "m3rg3" || rep.Merge.PR == nil || rep.Merge.PR.State != factory.PRMerged {
		t.Fatalf("report = %+v", rep.Merge)
	}
}

// THE SAME GATE, ON THE READ JUST MADE: a push after the factory's check, a
// check gone red, or a new conflict refuses the merge, and the forge is
// never asked.
func TestRunnerRefusesWhenTheFreshReadFailsTheGate(t *testing.T) {
	for name, change := range map[string]func(*factory.PullRequest){
		"head moved":   func(pr *factory.PullRequest) { pr.HeadSHA = strings.Repeat("2", 40) },
		"check failed": func(pr *factory.PullRequest) { pr.Checks.Failed = 1 },
		"conflict":     func(pr *factory.PullRequest) { pr.Mergeable = factory.MergeableNo },
		"closed":       func(pr *factory.PullRequest) { pr.State = factory.PRClosed },
	} {
		t.Run(name, func(t *testing.T) {
			pr := cleanPR()
			change(&pr)
			f := &fakeForge{follows: []factory.PullRequest{pr}}
			r, st, id := mergeRig(t, f, factory.MergeRun{State: factory.MergeQueued, By: "owner", Commit: runMergeHead})
			if err := r.Merge(id, runMergeHead); err != nil {
				t.Fatal(err)
			}
			it := settleMerge(t, r, st, id)
			if len(f.wrote) != 0 {
				t.Fatalf("the forge was asked to merge: %+v", f.wrote)
			}
			if it.Merging == nil || it.Merging.State != factory.MergeFailed || it.Merging.Why == "" {
				t.Fatalf("refused merge = %+v", it.Merging)
			}
		})
	}
}

// An auto-merge carried out on a runner still needs the pinned policy: the
// runner's gate refuses one whose policy is off.
func TestRunnerNeverAutoMergesWhenThePolicyIsOff(t *testing.T) {
	f := &fakeForge{follows: []factory.PullRequest{cleanPR()}}
	r, st, id := mergeRig(t, f, factory.MergeRun{State: factory.MergeQueued, Auto: true, Commit: runMergeHead})
	if err := r.Merge(id, runMergeHead); err != nil {
		t.Fatal(err)
	}
	it := settleMerge(t, r, st, id)
	if len(f.wrote) != 0 || it.Merging.State != factory.MergeFailed || it.Merging.Why != "auto-merge is off in this job's pipeline" {
		t.Fatalf("wrote %+v merging %+v", f.wrote, it.Merging)
	}
}

func TestRunnerSaysWhatTheForgeRefused(t *testing.T) {
	f := &fakeForge{follows: []factory.PullRequest{cleanPR()}, writeErr: fmt.Errorf("%w: Head branch was modified", forge.ErrHeadMoved)}
	r, st, id := mergeRig(t, f, factory.MergeRun{State: factory.MergeQueued, By: "owner", Commit: runMergeHead})
	if err := r.Merge(id, runMergeHead); err != nil {
		t.Fatal(err)
	}
	it := settleMerge(t, r, st, id)
	if len(f.wrote) != 1 || it.Merging.State != factory.MergeFailed || it.Merging.Why != "the pull request has commits you did not approve" {
		t.Fatalf("wrote %d merging %+v", len(f.wrote), it.Merging)
	}
}

// A LOST ANSWER MAY HIDE A MERGE. When the forge's answer does not arrive,
// the runner reads the pull request again: merged is merged, and the merge
// is never sent a second time.
func TestRunnerReadsAgainWhenTheForgeAnswerIsLost(t *testing.T) {
	merged := cleanPR()
	merged.State = factory.PRMerged
	f := &fakeForge{follows: []factory.PullRequest{cleanPR(), merged}, writeErr: errors.New("connection reset by peer")}
	r, st, id := mergeRig(t, f, factory.MergeRun{State: factory.MergeQueued, By: "owner", Commit: runMergeHead})
	if err := r.Merge(id, runMergeHead); err != nil {
		t.Fatal(err)
	}
	it := settleMerge(t, r, st, id)
	if len(f.wrote) != 1 || it.Merging.State != factory.MergeMerged || it.PullRequest.State != factory.PRMerged {
		t.Fatalf("wrote %d merging %+v pr %+v", len(f.wrote), it.Merging, it.PullRequest)
	}
}

func TestRunnerMergeRefusesWhatWasNotAskedFor(t *testing.T) {
	f := &fakeForge{follows: []factory.PullRequest{cleanPR()}}
	r, _, id := mergeRig(t, f, factory.MergeRun{State: factory.MergeFailed})
	if err := r.Merge(id, runMergeHead); err == nil {
		t.Fatal("merged a job no merge was queued for")
	}
	if err := r.Merge(id, ""); err == nil {
		t.Fatal("merged with no commit")
	}
	if len(f.wrote) != 0 {
		t.Fatalf("wrote %+v", f.wrote)
	}
}

// The factory's half: a report can finish only the merge it queued, and a
// merged report must carry the job's own pull request, merged.
func TestApplyMergeTakesOnlyTheMergeItQueued(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	base := func() factory.Item {
		pr := cleanPR()
		return factory.Item{PullRequest: &pr, Merging: &factory.MergeRun{State: factory.MergeQueued, By: "owner", Commit: runMergeHead}}
	}
	merged := cleanPR()
	merged.State = factory.PRMerged

	it := base()
	if err := applyMerge(&it, &factory.MergeReport{State: factory.MergeMerged, PR: &merged}, now); err != nil || it.Merging != nil || it.PullRequest.State != factory.PRMerged || !it.DoneAt.Equal(now) {
		t.Fatalf("merged apply = %+v %v", it, err)
	}
	it = base()
	other := merged
	other.Number = 99
	if err := applyMerge(&it, &factory.MergeReport{State: factory.MergeMerged, PR: &other}, now); err == nil {
		t.Fatal("a report marked another pull request merged")
	}
	it = base()
	it.Merging = nil
	if err := applyMerge(&it, &factory.MergeReport{State: factory.MergeMerged, PR: &merged}, now); err == nil {
		t.Fatal("a report finished a merge nobody asked for")
	}
	it = base()
	if err := applyMerge(&it, &factory.MergeReport{State: factory.MergeFailed, Why: "a check went red"}, now); err != nil || it.Merging.State != factory.MergeFailed || it.Merging.Why != "a check went red" {
		t.Fatalf("failed apply = %+v %v", it.Merging, err)
	}
}
