package serve_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// withSendBack gives the fixture's factory the runner's SendBack door, as the
// lease doors answer it: the landed job gets one more prove step and queues.
func withSendBack(t *testing.T, f *v2Jobs) {
	t.Helper()
	f.h.Close()
	f.seam.SendBack = func(id int, words string) error {
		return f.st.Update(id, func(it *factory.Item) error { return run.SendBackItem(it, words, time.Unix(0, f.clock.Load()).UTC()) })
	}
	f.h = serve.New(f.seam, f.cfg)
}

// landedAutoFinding is an Auto job that ran and landed with a failing
// repository policy finding, before any pull request.
func landedAutoFinding() factory.Item {
	in := autoProof()
	finding := factory.Claim{Text: "FINDING: the changelog entry carries the issue number", Evidence: "pr: 1758", Medium: "policy"}
	in.Policy = []factory.Claim{finding}
	in.Stream.Phases = []factory.Phase{
		{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone},
		{Name: "review", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Findings: 1, Claims: []factory.Claim{finding}}},
		{Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting}}
	return in
}

// A POLICY FINDING IN AUTO STARTS A REPAIR INSTEAD OF A PAUSE: the job is
// queued again with a prove step whose ask is the finding, nothing is
// published, and no pause notice is shown.
func TestAPolicyFindingInAutoStartsARepairInsteadOfAPause(t *testing.T) {
	f := newV2Jobs(t)
	withSendBack(t, f)
	it := f.create(t, landedAutoFinding())

	j := f.read(t, it)
	if j.AutonomyWhy != "" || j.State != "queued" {
		t.Fatalf("Auto paused instead of repairing: state=%s why=%q", j.State, j.AutonomyWhy)
	}
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Publishing || saved.Approval != nil && saved.Approval.State == "approved" {
		t.Fatalf("a job with a failing finding was published: %+v", saved.Approval)
	}
	if saved.Stream.AutoRepairs != 1 {
		t.Fatalf("repairs counted %d", saved.Stream.AutoRepairs)
	}
	cur := saved.Stream.Phases[saved.Stream.Cur]
	if !strings.HasPrefix(cur.Name, "prove") || cur.State != factory.PhasePending || !strings.Contains(cur.Note, "FINDING: the changelog entry carries the issue number") {
		t.Fatalf("the repair step reads %+v", cur)
	}
}

// MANUAL IS UNCHANGED: the landed job waits for the person's approval with its
// finding on the sheet, and nothing is sent back.
func TestAPolicyFindingInManualStillWaitsForThePerson(t *testing.T) {
	f := newV2Jobs(t)
	withSendBack(t, f)
	in := landedAutoFinding()
	in.Autonomy = factory.AutonomyManual
	it := f.create(t, in)

	j := f.read(t, it)
	if j.State != "needs_you" || !j.Ready {
		t.Fatalf("Manual moved: state=%s ready=%v", j.State, j.Ready)
	}
	saved, _ := f.st.Get(it.ID)
	if saved.State != factory.StateLanded || saved.Stream.AutoRepairs != 0 || len(saved.Stream.Phases) != 3 {
		t.Fatalf("Manual was repaired: state=%s repairs=%d phases=%d", saved.State, saved.Stream.AutoRepairs, len(saved.Stream.Phases))
	}
}

// WHEN THE REPAIRS RUN OUT the job comes to the person exactly as before: the
// pause on the finding, and nothing sent back.
func TestAutoRepairsUsedUpFallBackToTheFindingsPause(t *testing.T) {
	f := newV2Jobs(t)
	withSendBack(t, f)
	in := landedAutoFinding()
	in.Stream.AutoRepairs = factory.AutoRepairLimit
	it := f.create(t, in)

	j := f.read(t, it)
	if j.AutonomyWhy != "Repository policy needs attention: FINDING: the changelog entry carries the issue number. Request changes or take over to continue." {
		t.Fatalf("the person sees %q", j.AutonomyWhy)
	}
	saved, _ := f.st.Get(it.ID)
	if saved.State != factory.StateLanded || saved.Stream.AutoRepairs != factory.AutoRepairLimit || len(saved.Stream.Phases) != 3 {
		t.Fatalf("a repair started past the limit: state=%s repairs=%d", saved.State, saved.Stream.AutoRepairs)
	}
}

// THE MANAGER'S OFFER STAMPS merge_offered_at, on the job JSON the desktop
// reads, only when the job has an open pull request the gate passes.
func TestOfferMergeStampsMergeOfferedAtOnlyWithAnOpenPullRequest(t *testing.T) {
	f := newV2Jobs(t)
	head := strings.Repeat("a", 40)
	open := factory.Item{State: factory.StateShipped, Assignee: f.person, Publish: "pr", HeadCommit: head,
		Proof:       []factory.Claim{{Text: "tests pass", OK: true, Evidence: "go test"}},
		PullRequest: &factory.PullRequest{Number: 7, State: factory.PROpen, HeadSHA: head, Mergeable: factory.MergeableYes, ChecksRead: true, Checks: factory.PRChecks{Passed: 2}, ReviewsRead: true}}
	it := f.create(t, open)
	if j := f.read(t, it); j.MergeOfferedAt != nil {
		t.Fatalf("a job never re-offered carries merge_offered_at %v", j.MergeOfferedAt)
	}

	f.post(t, it, serve.OfferMergeVerb, `{}`, 200)
	w := verbRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "")
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if got, _ := raw["merge_offered_at"].(string); got != now.UTC().Truncate(time.Second).Format(time.RFC3339) {
		t.Fatalf("merge_offered_at = %v, want the offer's time in RFC 3339", raw["merge_offered_at"])
	}

	none := f.create(t, factory.Item{State: factory.StateLanded, Assignee: f.person})
	f.post(t, none, serve.OfferMergeVerb, `{}`, 409)
	if j := f.read(t, none); j.MergeOfferedAt != nil {
		t.Fatalf("a job with no pull request was offered: %v", j.MergeOfferedAt)
	}
}

// TWO SETTLES AT ONCE COUNT ONE REPAIR: settles run on every request, and a
// second one that reads the job between the first's count and its send-back
// must not count a second attempt for the same send-back (review 4).
func TestTwoSettlesAtOnceCountOneAutoRepair(t *testing.T) {
	f := newV2Jobs(t)
	withSendBack(t, f)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	sendBack := f.seam.SendBack
	f.h.Close()
	f.seam.SendBack = func(id int, words string) error {
		once.Do(func() { close(entered); <-release })
		return sendBack(id, words)
	}
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, landedAutoFinding())

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); verbRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "") }()
	<-entered
	go func() { defer wg.Done(); verbRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "") }()
	time.Sleep(100 * time.Millisecond) // the second request's settle reads the job while the first holds its send-back
	close(release)
	wg.Wait()

	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Stream.AutoRepairs != 1 {
		t.Fatalf("one send-back counted %d repairs", saved.Stream.AutoRepairs)
	}
}
