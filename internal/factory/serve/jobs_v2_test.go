package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

type v2Jobs struct {
	st      *store.Store
	rec     *record.Record
	h       *serve.Handler
	seam    factory.Seam
	cfg     serve.Config
	clock   atomic.Int64
	person  string
	folders *fakeFolders
}

// fakeFolders stands in for this machine's job folders: the work a drop
// would read there, a delete that may fail, and the jobs it deleted.
type fakeFolders struct {
	mu        sync.Mutex
	work      map[int]run.Work
	unread    error
	refuse    error
	discarded []factory.Item
}

func (f *fakeFolders) Work(it factory.Item) (run.Work, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.work[it.ID], f.unread
}

func (f *fakeFolders) Discard(it factory.Item) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse != nil {
		return f.refuse
	}
	f.discarded = append(f.discarded, it)
	delete(f.work, it.ID)
	return nil
}

func (f *fakeFolders) deleted() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []int
	for _, it := range f.discarded {
		ids = append(ids, it.ID)
	}
	return ids
}

func newV2Jobs(t *testing.T) *v2Jobs {
	t.Helper()
	f := &v2Jobs{person: "sam", folders: &fakeFolders{work: map[int]run.Work{}}}
	f.clock.Store(now.UnixNano())
	var err error
	f.st, err = store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }
	f.rec, err = record.Open(f.st, record.Options{Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	desk, err := lease.Open(f.st, lease.Options{Now: clock, Record: f.rec})
	if err != nil {
		t.Fatal(err)
	}
	doors := lease.NewDoors(desk)
	f.seam = factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := f.st.List()
			return factory.Snapshot{Items: items}, err
		},
		GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: f.person}, nil },
		Launch: doors.Launch, Stop: doors.Stop, Answer: doors.Answer, SignOff: doors.SignOff,
	}
	f.cfg = serve.Config{Mode: api.FactoryTeam, Address: config.Address, Version: config.Version, Store: f.st, Record: f.rec, Now: clock, LeaseOptions: lease.Options{Now: clock, Record: f.rec},
		Pipeline: func(context.Context, string, string) (pipeline.File, error) {
			p := pipeline.Defaults()
			p.DefaultBudget = 4
			return p, nil
		},
		Folders: f.folders,
	}
	f.h = serve.New(f.seam, f.cfg)
	t.Cleanup(func() { f.h.Close() })
	return f
}

func (f *v2Jobs) create(t *testing.T, it factory.Item) factory.Item {
	t.Helper()
	if it.Repo == "" {
		it.Repo = "acme/api"
	}
	if it.Title == "" {
		it.Title = "test job"
	}
	got, err := f.st.Create(it)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func (f *v2Jobs) post(t *testing.T, it factory.Item, verb, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	w := verbRequest(f.h, "POST", fmt.Sprintf("/v1/jobs/%d/%s", it.ID, verb), body)
	checkJSON(t, w, status)
	return w
}
func (f *v2Jobs) read(t *testing.T, it factory.Item) api.Job {
	t.Helper()
	w := verbRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "")
	checkJSON(t, w, 200)
	var j api.Job
	if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestV2RunStopUndoAndStartNow(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam"})
	f.post(t, it, "run", "", 200)
	if j := f.read(t, it); j.State != "queued" || j.Budget != 4 {
		t.Fatalf("run %+v", j)
	}
	// A QUEUED JOB HAD NO STEP TO CUT: its stop takes it out of the queue and
	// back to Backlog, with no stop mark, where Start picks it up.
	f.post(t, it, "stop", "", 200)
	if j := f.read(t, it); j.State != "backlog" || j.Failure != nil || j.NeedsYou != nil || j.StatePresentation.Title != "Backlog" || !j.Can.Run || !j.Can.Undo || j.Can.Stop {
		t.Fatalf("stop %+v", j)
	}
	f.post(t, it, "undo", "", 200)
	if j := f.read(t, it); j.State != "queued" {
		t.Fatal(j.State)
	}
	f.post(t, it, "stop", "", 200)
	f.clock.Add(int64(factory.ApprovalUndo))
	f.post(t, it, "undo", "", 409)
	f.post(t, it, "start_now", "", 200)
	cur, _ := f.st.Get(it.ID)
	if !cur.Marked || cur.State != factory.StateQueued || cur.Failure != nil {
		t.Fatalf("start now %+v", cur)
	}
}

func TestV2DismissIsPersonalAndGitHubClearsIt(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 1498, Origin: factory.OriginForge, State: factory.StateRunning, Assignee: "mei"})
	f.post(t, it, "dismiss", "", 200)
	if j := f.read(t, it); j.State != "running" || !j.Dismissed || j.Ref != "api#1498" {
		t.Fatalf("dismiss %+v", j)
	}
	f.person = "mei"
	if f.read(t, it).Dismissed {
		t.Fatal("dismiss crossed people")
	}
	fresh := it
	fresh.Labels = []string{"bug"}
	if err := github.Merge(f.st, "github", []factory.Item{fresh}); err != nil {
		t.Fatal(err)
	}
	f.person = "sam"
	if f.read(t, it).Dismissed {
		t.Fatal("GitHub activity did not clear dismissal")
	}
}

// A DROP ENDS THE JOB UNTIL A PERSON BRINGS IT BACK: a running job is
// stopped, its local branch and folder are deleted at once when they hold
// nothing to lose, a start is refused with the way back named, never as
// final, and GitHub does not revive it.
func TestV2DropEndsTheJobAndDeletesItsLocalWork(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 7, Origin: factory.OriginForge, State: factory.StateRunning, Assignee: "sam"})
	f.post(t, it, "drop", "", 200)
	if j := f.read(t, it); j.State != "done" || j.DoneHow != "dropped" {
		t.Fatalf("drop %+v", j)
	}
	if got := f.folders.deleted(); len(got) != 1 || got[0] != it.ID {
		t.Fatalf("deleted %v, want the dropped job's local work", got)
	}
	w := f.post(t, it, "run", "", 409)
	var refusal struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &refusal)
	if !strings.HasSuffix(refusal.Error, "#7 was dropped · Bring back returns it to Backlog") {
		t.Fatalf("a start of a dropped job is refused with %q", refusal.Error)
	}
	fresh := it
	fresh.Labels = []string{"bug"}
	if err := github.Merge(f.st, "github", []factory.Item{fresh}); err != nil {
		t.Fatal(err)
	}
	if f.read(t, it).DoneHow != "dropped" {
		t.Fatal("GitHub revived Drop")
	}
	events, _ := f.rec.Since(context.Background(), time.Time{})
	if len(events) != 1 || events[0].Kind != record.Dropped || events[0].Detail != "" {
		t.Fatalf("events %+v", events)
	}
	// Drop takes no words any more: a body that sends them is refused whole.
	other := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam"})
	f.post(t, other, "drop", `{"words":"not this one"}`, 422)
}

// A DROP NEVER LOSES WORK UNASKED. Commits on the job's branch or changes in
// its folder refuse the drop with has_work and a sentence that names them,
// and nothing is stopped or deleted; the same drop with discard is the yes,
// and its receipt says what was thrown away. Work that could not be read is
// treated as work.
func TestV2DropWithWorkAsksFirst(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 1844, Origin: factory.OriginForge, State: factory.StateRunning, Assignee: "sam"})
	f.folders.work[it.ID] = run.Work{Commits: 3, Uncommitted: true}
	w := f.post(t, it, "drop", `{}`, 409)
	var refusal struct {
		Error    string `json:"error"`
		Reason   string `json:"reason"`
		NowState string `json:"now_state"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &refusal); err != nil {
		t.Fatal(err)
	}
	if refusal.Reason != api.ReasonHasWork || refusal.Error != "api#1844's branch has 3 commits and uncommitted changes." || refusal.NowState != "running" {
		t.Fatalf("refusal %+v", refusal)
	}
	if j := f.read(t, it); j.State != "running" || j.DoneHow != "" || len(f.folders.deleted()) != 0 {
		t.Fatalf("a refused drop changed the job: %+v deleted %v", j, f.folders.deleted())
	}
	f.post(t, it, "drop", `{"discard":true}`, 200)
	if j := f.read(t, it); j.State != "done" || j.DoneHow != "dropped" || len(f.folders.deleted()) != 1 {
		t.Fatalf("discard %+v deleted %v", j, f.folders.deleted())
	}
	events, _ := f.rec.Since(context.Background(), time.Time{})
	if len(events) != 1 || events[0].Kind != record.Dropped || events[0].Detail != "discarded: api#1844's branch has 3 commits and uncommitted changes." {
		t.Fatalf("events %+v", events)
	}

	unread := f.create(t, factory.Item{Num: 12, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam"})
	f.folders.unread = fmt.Errorf("git: not a repository")
	w = f.post(t, unread, "drop", "", 409)
	_ = json.Unmarshal(w.Body.Bytes(), &refusal)
	if refusal.Reason != api.ReasonHasWork || refusal.Error != "api#12's branch could not be read, so what it holds is unknown." {
		t.Fatalf("unread refusal %+v", refusal)
	}
	f.post(t, unread, "drop", `{"discard":true}`, 200)
	if got := f.folders.deleted(); len(got) != 2 || got[1] != unread.ID {
		t.Fatalf("deleted %v", got)
	}
}

// A DELETE THAT FAILS IS NOT A FAILED DROP: the job is dropped all the same,
// and its activity says the local branch and folder were kept and why.
func TestV2DropWhoseDeleteFailsStillDrops(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 30, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam"})
	f.folders.refuse = fmt.Errorf("could not delete the folder of #30: permission denied")
	f.post(t, it, "drop", "", 200)
	cur, err := f.st.Get(it.ID)
	if err != nil || cur.DoneHow != "dropped" {
		t.Fatalf("not dropped: %+v %v", cur, err)
	}
	if n := len(cur.Activity); n == 0 || cur.Activity[n-1].What != "dropped · the local branch and folder were kept: could not delete the folder of #30: permission denied" {
		t.Fatalf("activity %+v", cur.Activity)
	}
}

// BRING BACK IS THE ONE WAY OUT OF A DROP: the job is in Backlog again with
// no drop left on it, a start works, and the poll treats it as any other item.
func TestV2RestoreBringsADroppedJobBackToBacklog(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 8, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam"})
	if f.read(t, it).Can.Restore {
		t.Fatal("a job that was never dropped offers Bring back")
	}
	f.post(t, it, "restore", "", 409)
	f.post(t, it, "drop", "", 200)
	if j := f.read(t, it); !j.Can.Restore {
		t.Fatalf("a dropped job does not offer Bring back: %+v", j.Can)
	}
	f.person = "mei"
	if f.read(t, it).Can.Restore {
		t.Fatal("someone else's dropped job offers them Bring back")
	}
	f.post(t, it, "restore", "", 403)
	f.person = "sam"
	f.post(t, it, "restore", "", 200)
	j := f.read(t, it)
	if j.State != "backlog" || j.DoneHow != "" || !j.DoneAt.IsZero() || j.Can.Restore {
		t.Fatalf("restored %+v", j)
	}
	f.post(t, it, "restore", "", 409)
	fresh := it
	fresh.Title = "changed on GitHub"
	if err := github.Merge(f.st, "github", []factory.Item{fresh}); err != nil {
		t.Fatal(err)
	}
	if j := f.read(t, it); j.State != "backlog" || j.Title != "changed on GitHub" {
		t.Fatalf("the poll does not treat a restored job normally: %+v", j)
	}
	f.post(t, it, "run", "", 200)
	if f.read(t, it).State != "queued" {
		t.Fatal("a restored job does not start")
	}
	events, _ := f.rec.Since(context.Background(), time.Time{})
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, " ") != "dropped restored started" {
		t.Fatalf("events %v", kinds)
	}
}

// A JOB BROUGHT BACK IS A FRESH JOB, AS IF IT HAD NEVER RUN: no steps run, $0
// spent, no failure, change, commits, runner or pull request. What the job is
// stays: its issue, assignee, priority, budget, notes and activity. The
// earlier pull request is forgotten, never closed, and the receipt names it.
func TestV2RestoreGivesAFreshBacklogJob(t *testing.T) {
	f := newV2Jobs(t)
	stages := []factory.Stage{
		{Name: "plan", Kind: factory.StageChat, Ask: "plan it", On: true},
		{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true},
		{Name: "proof", Kind: factory.StageChat, Ask: "prove it", On: true},
	}
	pr := "https://github.com/acme/api/pull/90"
	it := f.create(t, factory.Item{
		Num: 41, Origin: factory.OriginForge, Title: "Total double-counts", Body: "the issue's words",
		State: factory.StateDismissed, DoneHow: "dropped", DoneAt: now, Assignee: "sam", Cap: 4,
		Priority: &factory.JobPriority{Value: "high", SetBy: "sam"}, Notes: []string{"use the ledger"},
		Stages: stages, Stream: recordedRun(),
		Failure:         &factory.Failure{Code: factory.CodeStopped, Step: "write"},
		Change:          &factory.Change{Branch: "factory/41-total-double-counts"},
		ChangeBase:      "0123456789abcdef0123456789abcdef01234567",
		HeadCommit:      "89abcdef0123456789abcdef0123456789abcdef",
		Proof:           []factory.Claim{{Text: "totals once", OK: true}},
		PullRequest:     &factory.PullRequest{Number: 90, URL: pr, State: factory.PROpen},
		Activity:        []factory.Event{{At: now, What: factory.EventArrived}},
		WorktreeMachine: "mac-1",
	})
	f.post(t, it, "restore", "", 200)
	j := f.read(t, it)
	if j.State != "backlog" || j.Cost != 0 || j.Failure != nil || j.Change != nil || j.HeadCommit != "" || j.Runner != "" || j.PR != nil || len(j.Claims) != 0 || j.DoneHow != "" {
		t.Fatalf("not fresh: state %s cost %v failure %+v change %+v head %q runner %q pr %+v claims %+v", j.State, j.Cost, j.Failure, j.Change, j.HeadCommit, j.Runner, j.PR, j.Claims)
	}
	if len(j.Steps) != 3 {
		t.Fatalf("steps %+v", j.Steps)
	}
	for _, s := range j.Steps {
		if s.State != api.StepPending {
			t.Fatalf("a step of the old run survived: %+v", s)
		}
	}
	if j.Assignee != "sam" || j.Budget != 4 || j.Priority == nil || j.Priority.Value != "high" || j.Title != "Total double-counts" {
		t.Fatalf("the job's own facts were lost: %+v", j)
	}
	cur, err := f.st.Get(it.ID)
	if err != nil || cur.Stream != nil || cur.ChangeBase != "" || cur.Body != "the issue's words" || len(cur.Notes) != 1 || len(cur.Activity) == 0 || cur.Activity[0].What != factory.EventArrived {
		t.Fatalf("stored %+v %v", cur, err)
	}
	events, _ := f.rec.Since(context.Background(), time.Time{})
	if len(events) != 1 || events[0].Kind != record.Restored || events[0].Detail != "earlier pr · "+pr {
		t.Fatalf("events %+v", events)
	}
}

// A PERSON'S START READS THE ONE ITEM AGAIN FIRST. An issue closed on GitHub
// is finished instead of started and the person is told in one sentence; an
// open one starts; a GitHub that cannot be reached never stops a start; and an
// item that is not on GitHub is not read at all.
func TestV2StartRechecksTheItemOnGitHub(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	standing, reads := map[int]string{}, map[int]int{}
	f.cfg.Recheck = func(_ context.Context, it factory.Item) (string, error) {
		reads[it.Num]++
		if it.Num == 404 {
			return "", fmt.Errorf("dial tcp: no route")
		}
		return standing[it.Num], nil
	}
	f.h = serve.New(f.seam, f.cfg)

	closed := f.create(t, factory.Item{Num: 12, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam"})
	standing[12] = github.StandingClosed
	w := f.post(t, closed, "start_now", "", 409)
	var refusal struct {
		Error    string `json:"error"`
		NowState string `json:"now_state"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &refusal)
	if refusal.Error != "#12 was closed on GitHub, so it wasn't started." || refusal.NowState != "done" {
		t.Fatalf("refusal %+v", refusal)
	}
	if j := f.read(t, closed); j.State != "done" || j.DoneHow != "closed" || j.DoneAt.IsZero() {
		t.Fatalf("closed job %+v", j)
	}
	if cur, _ := f.st.Get(closed.ID); cur.Marked {
		t.Fatal("a job that was not started is marked to start")
	}

	merged := f.create(t, factory.Item{Num: 13, Kind: factory.KindPR, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam"})
	standing[13] = github.StandingMerged
	f.post(t, merged, "run", "", 409)
	if j := f.read(t, merged); j.DoneHow != "merged" {
		t.Fatalf("merged job %+v", j)
	}

	open := f.create(t, factory.Item{Num: 14, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam"})
	f.post(t, open, "run", "", 200)
	if f.read(t, open).State != "queued" {
		t.Fatal("an open issue did not start")
	}

	away := f.create(t, factory.Item{Num: 404, Origin: factory.OriginForge, State: factory.StateNew, Assignee: "sam"})
	f.post(t, away, "run", "", 200)
	if f.read(t, away).State != "queued" {
		t.Fatal("a GitHub that cannot be reached stopped a start")
	}

	typed := f.create(t, factory.Item{Origin: factory.OriginTerminal, State: factory.StateNew, Assignee: "sam"})
	f.post(t, typed, "run", "", 200)
	if reads[0] != 0 || reads[12] != 1 || reads[13] != 1 || reads[14] != 1 || reads[404] != 1 {
		t.Fatalf("reads %v", reads)
	}
	events, _ := f.rec.Since(context.Background(), time.Time{})
	var got []string
	for _, e := range events {
		if e.Kind == record.SourceClosed {
			got = append(got, fmt.Sprintf("%d %s %s", e.Job, e.Verb, e.Actor.Kind))
		}
	}
	if want := []string{fmt.Sprintf("%d closed factory", closed.ID), fmt.Sprintf("%d merged factory", merged.ID)}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("recorded %v, want %v", got, want)
	}
}

// CHECK GITHUB NOW WAKES THE POLL AND DOES NOT WAIT FOR IT: the answer is the
// factory alone, and the store's poll-now mark, which a waiting poll in any
// process on this machine looks at, has moved.
func TestV2GitHubPollNudgesThePollAndReturns(t *testing.T) {
	f := newV2Jobs(t)
	if !f.st.PollNudged().IsZero() {
		t.Fatal("a new store was already nudged")
	}
	w := verbRequest(f.h, "POST", "/v1/github/poll", "")
	checkJSON(t, w, 200)
	if f.st.PollNudged().IsZero() {
		t.Fatal("Check GitHub now did not nudge the poll")
	}
	checkJSON(t, verbRequest(f.h, "GET", "/v1/github/poll", ""), 405)
}

func TestV2PriorityBudgetAndAssignmentTransitions(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNew, Labels: []string{"bug", "size:XL"}})
	f.post(t, it, "assign_to_me", "", 200)
	f.post(t, it, "priority", `{"priority":"urgent"}`, 200)
	f.post(t, it, "raise_budget", `{"budget":10}`, 200)
	if j := f.read(t, it); j.Priority.Value != "urgent" || j.Priority.SetBy != "sam" || j.Size != "XL" || j.Budget != 10 || j.State != "backlog" {
		t.Fatalf("metadata %+v", j)
	}
	f.post(t, it, "run", "", 200)
	f.post(t, it, "unassign", "", 200)
	if j := f.read(t, it); j.State != "backlog" || j.AssignedTo != "" {
		t.Fatalf("unassign %+v", j)
	}
	f.post(t, it, "change_assignee", `{"assignee":"mei"}`, 403)
	f.post(t, it, "assign_to_me", "", 200)
	f.post(t, it, "change_assignee", `{"assignee":"mei"}`, 200)
	f.post(t, it, "nudge", `{"words":"please look"}`, 200)
	events, _ := f.rec.Since(context.Background(), time.Time{})
	last := events[len(events)-1]
	if last.Kind != record.Nudged || last.Detail != "@mei please look" {
		t.Fatalf("nudge %+v", last)
	}
	f.post(t, it, "raise_budget", `{"budget":20}`, 403)
}

func TestV2ApprovalsWaitUndoAndRecoverOnRestart(t *testing.T) {
	for _, kind := range []string{"plan", "proof", "start"} {
		t.Run(kind, func(t *testing.T) {
			f := newV2Jobs(t)
			it := factory.Item{State: factory.StateNeedsYou, Assignee: "sam", Question: "approve?", QKind: factory.QKindApprove}
			if kind == "start" {
				it.QKind = factory.QKindAskFirst
				it.AskedFirstReason = "touches auth/"
			}
			if kind == "proof" {
				it.State = factory.StateLanded
				it.Question = ""
				it.QKind = ""
				it.HeadCommit = approvedHead
				it.Proof = []factory.Claim{{Text: "works", OK: true}}
			}
			it = f.create(t, it)
			body := "{}"
			if kind == "proof" {
				body = `{"commit":"` + approvedHead + `"}`
			}
			f.post(t, it, "approve", body, 200)
			j := f.read(t, it)
			if j.Approval.Kind != kind || j.Approval.State != "approving" || j.Approval.UndoUntil.Sub(j.Approval.At) != 5*time.Second {
				t.Fatalf("approval %+v", j.Approval)
			}
			if events, _ := f.rec.Since(context.Background(), time.Time{}); len(events) != 0 {
				t.Fatal("approval ran before Undo")
			}
			f.post(t, it, "undo", "", 200)
			f.clock.Add(int64(factory.ApprovalUndo))
			f.read(t, it)
			if events, _ := f.rec.Since(context.Background(), time.Time{}); len(events) != 1 || events[0].Kind != record.Undone {
				t.Fatal("Undo let approval through")
			}
			f.post(t, it, "approve", body, 200)
			f.h.Close()
			f.clock.Add(int64(factory.ApprovalUndo))
			f.h = serve.New(f.seam, f.cfg)
			j = f.read(t, it)
			if j.Approval == nil || j.Approval.State != "approved" {
				t.Fatalf("restart approval %+v", j.Approval)
			}
			events, _ := f.rec.Since(context.Background(), time.Time{})
			if len(events) != 2 || events[1].Kind != record.Approved || events[1].Handle != "sam" {
				t.Fatalf("receipts %+v", events)
			}
			f.read(t, it)
			events, _ = f.rec.Since(context.Background(), time.Time{})
			if len(events) != 2 {
				t.Fatal("approval repeated")
			}
		})
	}
}

func TestV2PlainQuestionFirstAnswerWinsExactCopy(t *testing.T) {
	f := newV2Jobs(t)
	f.seam.Answer = func(id int, yes bool, words string) error {
		return f.st.Update(id, func(it *factory.Item) error {
			it.State = factory.StateQueued
			it.Question = ""
			it.QKind = ""
			return nil
		})
	}
	f.h.Close()
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Assignee: "raj", Question: "which API?", QKind: "scope"})
	if j := f.read(t, it); j.WaitingFor != "@raj" {
		t.Fatalf("waiting %+v", j)
	}
	f.person = "jm"
	f.post(t, it, "answer", `{"words":"v2"}`, 200)
	f.clock.Add(int64(10 * time.Second))
	f.person = "sam"
	w := f.post(t, it, "answer", `{"words":"v1"}`, 409)
	var got struct{ Error string }
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Error != "@jm answered this 10s ago. Your answer wasn't sent." {
		t.Fatal(got.Error)
	}
}

func TestV2ClosedPRRunAgainAndMergedRefusal(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateShipped, Assignee: "sam", PullRequest: &factory.PullRequest{State: factory.PRClosed}})
	if j := f.read(t, it); j.DoneHow != "closed" || j.State != "done" {
		t.Fatalf("closed %+v", j)
	}
	f.post(t, it, "run_again", "", 200)
	if f.read(t, it).State != "queued" {
		t.Fatal("Run again did not queue")
	}
	merged := f.create(t, factory.Item{State: factory.StateShipped, Assignee: "sam", PullRequest: &factory.PullRequest{State: factory.PRMerged}})
	f.post(t, merged, "run_again", "", 409)
}

func TestSoloUnassignedPlanApprovalKeepsUndoAndNoAssignment(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.cfg.Mode = api.FactorySolo
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Question: "plan ready?", QKind: factory.QKindApprove})
	j := f.read(t, it)
	w := verbRequest(f.h, "GET", "/v1/queue", "")
	checkJSON(t, w, 200)
	var q api.Queue
	if err := json.Unmarshal(w.Body.Bytes(), &q); err != nil || q.Mode != api.FactorySolo || len(q.Jobs) != 1 || !q.Jobs[0].Can.Approve || q.Jobs[0].AssignedTo != "" {
		t.Fatalf("solo queue approval: %+v %v", q, err)
	}
	if !j.Can.Approve || j.AssignedTo != "" {
		t.Fatalf("solo plan cannot be approved privately: %+v", j.Can)
	}
	f.post(t, it, "approve", "{}", 200)
	j = f.read(t, it)
	if !j.Can.Undo || j.Can.Approve || j.Approval.State != "approving" {
		t.Fatalf("missing Undo: %+v %+v", j.Can, j.Approval)
	}
	f.post(t, it, "undo", "", 200)
	if !f.read(t, it).Can.Approve {
		t.Fatal("Undo did not restore approval")
	}
	f.post(t, it, "approve", "{}", 200)
	f.clock.Add(int64(factory.ApprovalUndo))
	if j = f.read(t, it); j.Approval == nil || j.Approval.State != "approved" {
		t.Fatalf("approval did not settle: %+v", j.Approval)
	}
	stored, err := f.st.Get(it.ID)
	if err != nil || stored.Person() != "" {
		t.Fatalf("solo approval invented an owner: %+v %v", stored, err)
	}
}

func TestTeamUnassignedPlanCannotBeApproved(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Question: "plan ready?", QKind: factory.QKindApprove})
	if f.read(t, it).Can.Approve {
		t.Fatal("team unassigned job gained approval")
	}
	f.post(t, it, "approve", "{}", 422)
	stored, _ := f.st.Get(it.ID)
	if stored.Approval != nil {
		t.Fatal("refused approval left a decision")
	}
}

func TestSoloCallerKeepsPersonalDismissAndStopUndo(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.cfg.Mode = api.FactorySolo
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNew})
	if !f.read(t, it).Can.Dismiss {
		t.Fatal("signed-in solo caller cannot dismiss")
	}
	f.post(t, it, "dismiss", "", 200)
	if !f.read(t, it).Dismissed {
		t.Fatal("personal dismissal was lost")
	}
	active := f.create(t, factory.Item{State: factory.StateQueued})
	f.post(t, active, "stop", "", 200)
	if !f.read(t, active).Can.Undo {
		t.Fatal("solo caller cannot undo their stop")
	}
	f.post(t, active, "undo", "", 200)
	if f.read(t, active).State != api.StateQueued {
		t.Fatal("stop Undo failed")
	}
}

func TestSoloApprovalRequiresGitHubIdentityWithoutInventingAnAssignee(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.cfg.Mode = api.FactorySolo
	f.person = ""
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Question: "plan ready?", QKind: factory.QKindApprove})
	if f.read(t, it).Can.Approve {
		t.Fatal("unknown caller gained approval")
	}
	w := f.post(t, it, "approve", "{}", 422)
	if !strings.Contains(w.Body.String(), "Sign in to GitHub") {
		t.Fatalf("not actionable: %s", w.Body.String())
	}
}

// A JOB'S MODEL: its person chooses it, the job reads it back, a model id with
// spaces is refused, going back to the default clears it, another person may
// not, and a worker holding the job is told.
func TestV2AJobsModelIsItsPersonsToChoose(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam"})
	f.post(t, it, "model", `{"model":"  vendor/model-a  "}`, 200)
	if j := f.read(t, it); j.Model != "vendor/model-a" {
		t.Fatalf("model = %q", j.Model)
	}
	f.post(t, it, "model", `{"model":"two words"}`, 422)
	f.post(t, it, "model", `{"model":""}`, 200)
	if j := f.read(t, it); j.Model != "" {
		t.Fatalf("default = %q", j.Model)
	}
	f.person = "mei"
	f.post(t, it, "model", `{"model":"vendor/model-b"}`, 403)
	f.person = "sam"

	desk, err := lease.Open(f.st, f.cfg.LeaseOptions)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.State = factory.StateQueued; return nil }); err != nil {
		t.Fatal(err)
	}
	worker := lease.Worker{ID: "worker", Machine: "laptop", Handle: "sam", Team: true, Slots: 1}
	held, err := desk.Pull(worker)
	if err != nil {
		t.Fatal(err)
	}
	f.post(t, it, "model", `{"model":"vendor/model-c"}`, 200)
	signals, err := desk.DrainLive(held.ID, held.Worker)
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 1 || signals[0].Verb != lease.SignalModel || signals[0].Words != "vendor/model-c" {
		t.Fatalf("signals = %+v", signals)
	}
}
