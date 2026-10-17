package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// ── a real factory with a store, a record and a lease desk, and no model ───

const actionHead = "0123456789abcdef0123456789abcdef01234567"

// Credential fixtures must not redact ordinary words in later package tests.
const actionTestCredential = "factory-actions-fixture-bearer-credential"

// actionFactory is the real /v1 server over a real store. Nothing here can
// start a model or write to GitHub: Launch only queues, and the seam has no
// door that opens an issue or a pull request.
type actionFactory struct {
	URL      string
	clock    *factoryClock
	github   *fakeGitHub
	st       *store.Store
	rec      *record.Record
	launched atomic.Int64
	sentBack atomic.Value // the words SendBack was handed
	mu       sync.Mutex
	posts    []string // every POST the factory received, "path"
}

func startActionFactory(t *testing.T) *actionFactory {
	t.Helper()
	f := &actionFactory{clock: &factoryClock{at: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}, github: newFakeGitHub()}
	var err error
	if f.st, err = store.Open(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if f.rec, err = record.Open(f.st, record.Options{Now: f.clock.now}); err != nil {
		t.Fatal(err)
	}
	desk, err := lease.Open(f.st, lease.Options{Now: f.clock.now, Record: f.rec})
	if err != nil {
		t.Fatal(err)
	}
	doors := lease.NewDoors(desk)
	seam := factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := f.st.List()
			return factory.Snapshot{Items: items}, err
		},
		Launch: func(id int) error { f.launched.Add(1); return doors.Launch(id) },
		Stop:   doors.Stop, Answer: doors.Answer, SignOff: doors.SignOff,
		SendBack: func(id int, words string) error { f.sentBack.Store(words); return nil },
		New: func(repo, words string) (int, error) {
			title, _, _ := strings.Cut(words, "\n")
			it, err := f.st.Create(factory.Item{Repo: repo, Title: title, Body: words, State: factory.StateNew})
			return it.ID, err
		},
	}
	h := serve.New(seam, serve.Config{
		Version: "test", Name: "Acme factory", ServedBy: "factory", AccessToken: testHostToken, Mode: api.FactoryTeam,
		GitHubClientID: "client", GitHubHTTPClient: f.github.client(), Now: f.clock.now, Store: f.st, Record: f.rec, Root: f.st.Root(),
		LeaseOptions: lease.Options{Now: f.clock.now, Record: f.rec},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			f.mu.Lock()
			f.posts = append(f.posts, r.URL.Path)
			f.mu.Unlock()
		}
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { srv.Close(); h.Close() })
	f.URL = srv.URL
	return f
}

// count is how many events of one kind the record holds.
func (f *actionFactory) count(kind string) int {
	events, _ := f.rec.Since(context.Background(), time.Time{})
	n := 0
	for _, e := range events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func (f *actionFactory) postCount() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.posts) }

func (f *actionFactory) create(t *testing.T, it factory.Item) factory.Item {
	t.Helper()
	if it.Repo == "" {
		it.Repo = "acme/api"
	}
	if it.Title == "" {
		it.Title = "a job"
	}
	got, err := f.st.Create(it)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// person is one human at one engine: their own hub, their own wire, signed in
// to the factory as the login the fake GitHub hands them.
type person struct {
	w      *factoryWire
	conn   appwire.FactoryConnection
	handle string
}

// signedIn attaches a new engine to f and signs in. The fake GitHub hands out
// alice, bob, carol in the order the sign-ins start.
func signedIn(t *testing.T, f *actionFactory, n int, handle string) *person {
	t.Helper()
	hub := testFactoryHub(t)
	hub.now = f.clock.now
	w := newFactoryWire(t, hub)
	p := &person{w: w, handle: handle}
	w.must("factory.attach", map[string]any{"url": f.URL, "token": testHostToken}, &p.conn)
	var flow appwire.FactorySignIn
	w.must("factory.sign_in_start", map[string]any{"factory": p.conn.ID, "caller": "w"}, &flow)
	f.github.grant("github-device-secret-" + string(rune('0'+n)))
	f.clock.advance(6 * time.Second)
	var state appwire.FactorySignInState
	w.must("factory.sign_in_poll", map[string]any{"flow": flow.Flow, "caller": "w"}, &state)
	if state.State != appwire.SignInSignedIn || state.Me == nil || state.Me.Handle != handle {
		t.Fatalf("sign in as %s: %+v", handle, state)
	}
	return p
}

// job reads a job and its version, the way the app does before it acts.
func (p *person) job(t *testing.T, n int) api.Job {
	t.Helper()
	var j api.Job
	p.w.must("factory.job", map[string]any{"factory": p.conn.ID, "job": n}, &j)
	return j
}

func (p *person) guard(t *testing.T, n int) map[string]any {
	t.Helper()
	return map[string]any{"version": p.job(t, n).Version}
}

// act calls a factory.job_* with the version just read.
func (p *person) act(t *testing.T, method string, n int, extra map[string]any) appwire.FactoryJobActed {
	t.Helper()
	var out appwire.FactoryJobActed
	p.w.must(method, p.params(t, n, extra), &out)
	return out
}

func (p *person) params(t *testing.T, n int, extra map[string]any) map[string]any {
	t.Helper()
	m := map[string]any{"factory": p.conn.ID, "job": n, "expect": p.guard(t, n)}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// refused calls a method that must be refused and returns the typed detail.
func (p *person) refused(t *testing.T, method string, params map[string]any, code string) (string, *appwire.FactoryRefusal) {
	t.Helper()
	result, got, msg := p.w.call(method, params)
	if got != code {
		t.Fatalf("%s: %q (%s), want %q; result %s", method, got, msg, code, result)
	}
	p.w.mu.Lock()
	defer p.w.mu.Unlock()
	for i := len(p.w.lines) - 1; i >= 0; i-- {
		var fr struct {
			Error *struct {
				Code   string                  `json:"code"`
				Detail *appwire.FactoryRefusal `json:"detail"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(p.w.lines[i]), &fr) == nil && fr.Error != nil && fr.Error.Code == code {
			return msg, fr.Error.Detail
		}
	}
	return msg, nil
}

func (f *actionFactory) secrets(ps ...*person) []string {
	out := []string{testHostToken, "github-device-secret-1", "github-device-secret-2", "github-token-secret-1", "github-token-secret-2", "device_code", "access_token"}
	for _, p := range ps {
		p.w.backend.factoryHub().mu.Lock()
		for _, s := range p.w.backend.factoryHub().sessions {
			out = append(out, s.token)
		}
		p.w.backend.factoryHub().mu.Unlock()
	}
	return out
}

// ── tests ──────────────────────────────────────────────────────────────────

// THE WHOLE JOB DESK, END TO END, AGAINST THE REAL SERVER AND TWO HUMANS.
func TestAppFactoryJobActionsTwoPeopleAgainstTheRealServer(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	bob := signedIn(t, f, 2, "bob")

	// The feature is announced, and the writes capability is NOT claimed.
	alice.w.mu.Lock()
	hello := alice.w.lines[0]
	alice.w.mu.Unlock()
	if !strings.Contains(hello, appwire.FeatureFactoryJobActions) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryJobActions, hello)
	}
	var caps appwire.FactoryCapabilities
	alice.w.must("factory.capabilities", struct{}{}, &caps)
	if caps.Writes {
		t.Fatalf("capabilities claim writes: %+v", caps)
	}

	t.Run("a new job is a backlog job and starts nothing", func(t *testing.T) {
		made := alice.act2(t, "factory.job_create", map[string]any{"factory": alice.conn.ID, "repo": "acme/api", "title": "Fix the login", "text": "It fails on Safari."})
		if made.Action != appwire.JobActCreate || made.Job < 1 || made.Current == nil {
			t.Fatalf("created: %+v", made)
		}
		if made.Current.State != api.StateBacklog || f.launched.Load() != 0 {
			t.Fatalf("a new job started or is not backlog: state %q launched %d", made.Current.State, f.launched.Load())
		}
		it, _ := f.st.Get(made.Job)
		if it.Ask == nil || it.Ask.Source != factory.AskApp || it.Ask.By != "alice" || it.Ask.Text != "It fails on Safari." {
			t.Fatalf("ask = %+v", it.Ask)
		}
		// Starting says who it is for, and a team job cannot be started with
		// no one to do it. Neither refusal leaves a job or starts anything.
		before, _ := f.st.List()
		posts := f.postCount()
		alice.w.mustFail("factory.job_create", map[string]any{"factory": alice.conn.ID, "repo": "acme/api", "title": "Go now", "start": "now"}, appwire.CodeBadParams)
		if f.postCount() != posts {
			t.Fatal("a create with no assignee reached the factory")
		}
		alice.w.mustFail("factory.job_create", map[string]any{"factory": alice.conn.ID, "repo": "acme/api", "title": "Go now", "start": "now", "assignee": ""}, appwire.CodeBadParams)
		after, _ := f.st.List()
		if len(after) != len(before) || f.launched.Load() != 0 {
			t.Fatalf("a refused create left %d jobs (was %d), launched %d", len(after), len(before), f.launched.Load())
		}
	})

	t.Run("start now is the one way a create starts, and it queues without a model", func(t *testing.T) {
		made := alice.act2(t, "factory.job_create", map[string]any{"factory": alice.conn.ID, "repo": "acme/api", "title": "Run me", "assignee": "alice", "start": "now"})
		if f.launched.Load() != 1 || made.Current == nil || made.Current.State != api.StateQueued {
			t.Fatalf("start now: launched %d current %+v", f.launched.Load(), made.Current)
		}
	})

	f.launched.Store(0)
	job := f.create(t, factory.Item{State: factory.StateNew, Assignee: "alice"})
	n := job.ID

	t.Run("the assignee alone changes the assignee, and a stale version is refused once", func(t *testing.T) {
		posts := f.postCount()
		_, d := bob.refused(t, "factory.job_change_assignee", bob.params(t, n, map[string]any{"assignee": "bob"}), appwire.CodeForbidden)
		if d == nil || d.Reason != "not_job_person" || d.JobPerson != "@alice" {
			t.Fatalf("forbidden detail = %+v", d)
		}
		bob.refused(t, "factory.job_unassign", bob.params(t, n, nil), appwire.CodeForbidden)
		bob.refused(t, "factory.job_start", bob.params(t, n, nil), appwire.CodeForbidden)
		if it, _ := f.st.Get(n); it.Assignee != "alice" || f.launched.Load() != 0 {
			t.Fatalf("a forbidden write changed the job: %+v", it)
		}
		// bob cannot take a job that has a person; the refusal names who has it.
		_, d = bob.refused(t, "factory.job_assign_to_me", bob.params(t, n, nil), appwire.CodeConflict)
		if d == nil || d.Taker != "@alice" {
			t.Fatalf("taken detail = %+v", d)
		}
		if got := f.postCount() - posts; got != 4 {
			t.Fatalf("4 refused writes made %d POSTs (a refusal was retried or skipped)", got)
		}

		// STALE: alice read the job, then bob changes its priority.
		old := alice.guard(t, n)
		bob.act(t, "factory.job_set_priority", n, map[string]any{"priority": "urgent"})
		posts = f.postCount()
		msg, d := alice.refused(t, "factory.job_set_priority", map[string]any{"factory": alice.conn.ID, "job": n, "priority": "low", "expect": old}, appwire.CodeStale)
		if d == nil || d.Reason != "stale" || d.NowState == "" || msg == "" {
			t.Fatalf("stale detail = %+v (%s)", d, msg)
		}
		if f.postCount()-posts != 1 {
			t.Fatalf("a stale refusal was sent %d times", f.postCount()-posts)
		}
		if it, _ := f.st.Get(n); it.Priority == nil || it.Priority.Value != "urgent" {
			t.Fatalf("a stale write landed: %+v", it.Priority)
		}
	})

	t.Run("hand the job over, then only the new assignee may act", func(t *testing.T) {
		done := alice.act(t, "factory.job_change_assignee", n, map[string]any{"assignee": "@bob"})
		if done.Current == nil || done.Current.Assignee != "bob" {
			t.Fatalf("handed over: %+v", done.Current)
		}
		alice.refused(t, "factory.job_start", alice.params(t, n, nil), appwire.CodeForbidden)
		alice.refused(t, "factory.job_change_assignee", alice.params(t, n, map[string]any{"assignee": "alice"}), appwire.CodeForbidden)
		if f.launched.Load() != 0 {
			t.Fatal("a forbidden start launched")
		}
		bob.act(t, "factory.job_unassign", n, nil)
		if it, _ := f.st.Get(n); it.Assignee != "" {
			t.Fatalf("unassigned: %+v", it)
		}
		alice.act(t, "factory.job_assign_to_me", n, nil)
		if it, _ := f.st.Get(n); it.Assignee != "alice" {
			t.Fatalf("assigned to alice: %+v", it)
		}
	})

	t.Run("budget and priority carry the factory's own limits", func(t *testing.T) {
		posts := f.postCount()
		alice.w.mustFail("factory.job_set_budget", alice.params(t, n, map[string]any{"budget": 7}), appwire.CodeBadParams)
		if f.postCount() != posts {
			t.Fatal("an off-list budget reached the factory")
		}
		got := alice.act(t, "factory.job_set_budget", n, map[string]any{"budget": 10})
		if got.Current == nil || got.Current.Budget != 10 {
			t.Fatalf("budget: %+v", got.Current)
		}
		// The factory never lowers a limit; its sentence comes back as bad_params.
		alice.w.mustFail("factory.job_set_budget", alice.params(t, n, map[string]any{"budget": 5}), appwire.CodeBadParams)
		bob.refused(t, "factory.job_set_budget", bob.params(t, n, map[string]any{"budget": 20}), appwire.CodeForbidden)
	})

	t.Run("dismiss is personal", func(t *testing.T) {
		got := bob.act(t, "factory.job_dismiss", n, nil)
		if got.Current == nil || !got.Current.Dismissed {
			t.Fatalf("bob's dismiss: %+v", got.Current)
		}
		if alice.job(t, n).Dismissed {
			t.Fatal("bob's dismiss hid the job from alice")
		}
	})

	t.Run("start, stop and Undo", func(t *testing.T) {
		started := alice.act(t, "factory.job_start", n, nil)
		if f.launched.Load() != 1 || started.Current == nil || started.Current.State != api.StateQueued {
			t.Fatalf("start: launched %d, %+v", f.launched.Load(), started.Current)
		}
		alice.act(t, "factory.job_stop", n, nil)
		if alice.job(t, n).State != api.StateBacklog {
			t.Fatal("stop did not return the job to the backlog")
		}
		// Undo is the stopper's alone.
		bob.w.mustFail("factory.job_undo", bob.params(t, n, nil), appwire.CodeConflict)
		undone := alice.act(t, "factory.job_undo", n, nil)
		if undone.Current == nil || undone.Current.State != api.StateQueued {
			t.Fatalf("undo: %+v", undone.Current)
		}
		// ...and only inside the five seconds.
		alice.act(t, "factory.job_stop", n, nil)
		f.clock.advance(factory.ApprovalUndo)
		alice.w.mustFail("factory.job_undo", alice.params(t, n, nil), appwire.CodeConflict)
	})

	t.Run("approval waits its Undo window and then acts once", func(t *testing.T) {
		ask := f.create(t, factory.Item{State: factory.StateNeedsYou, Assignee: "alice", Question: "start?", QKind: factory.QKindAskFirst, AskedFirstReason: "touches auth/"})
		undoneBefore := f.count(record.Undone)
		bob.refused(t, "factory.job_approve", bob.params(t, ask.ID, nil), appwire.CodeForbidden)
		if f.count(record.Approved) != 0 {
			t.Fatal("a forbidden approval left a receipt")
		}
		got := alice.act(t, "factory.job_approve", ask.ID, nil)
		if got.Approval == nil || got.Approval.State != "approving" || got.Approval.DecidedBy != "alice" || got.UndoUntil == nil ||
			got.UndoUntil.Sub(got.Approval.At) != factory.ApprovalUndo {
			t.Fatalf("approve: %+v until %v", got.Approval, got.UndoUntil)
		}
		if f.count(record.Approved) != 0 {
			t.Fatal("approval ran before Undo")
		}
		// Only the approver can withdraw it, and it withdraws the whole thing.
		bob.w.mustFail("factory.job_undo", bob.params(t, ask.ID, nil), appwire.CodeConflict)
		alice.act(t, "factory.job_undo", ask.ID, nil)
		f.clock.advance(factory.ApprovalUndo)
		alice.job(t, ask.ID)
		if f.count(record.Approved) != 0 || f.count(record.Undone) != undoneBefore+1 {
			t.Fatalf("Undo let the approval through: approved %d undone %d", f.count(record.Approved), f.count(record.Undone))
		}
		// Approve again and let the window pass: it acts once, and Undo is over.
		alice.act(t, "factory.job_approve", ask.ID, nil)
		f.clock.advance(factory.ApprovalUndo)
		settled := alice.job(t, ask.ID)
		if settled.Approval == nil || settled.Approval.State != "approved" || f.count(record.Approved) != 1 {
			t.Fatalf("settled: %+v approved %d", settled.Approval, f.count(record.Approved))
		}
		alice.w.mustFail("factory.job_undo", alice.params(t, ask.ID, nil), appwire.CodeConflict)
		_, d := alice.refused(t, "factory.job_approve", alice.params(t, ask.ID, nil), appwire.CodeConflict)
		// The start was approved and the job moved on: the factory says it is
		// no longer waiting, in the state it is in now.
		if d == nil || d.Reason != "conflict" || d.NowState != api.StateQueued {
			t.Fatalf("a second approval: %+v", d)
		}
		if f.count(record.Approved) != 1 {
			t.Fatal("a second approval wrote a second receipt")
		}
	})

	t.Run("a proof is approved at the commit the person saw", func(t *testing.T) {
		proof := f.create(t, factory.Item{State: factory.StateLanded, Assignee: "alice", HeadCommit: actionHead, Proof: []factory.Claim{{Text: "works", OK: true}}})
		other := strings.Repeat("a", 40)
		_, d := alice.refused(t, "factory.job_approve", alice.params(t, proof.ID, map[string]any{"commit": other}), appwire.CodeStale)
		if d == nil || d.Reason != "changed_since_opened" || d.HeadCommit != actionHead {
			t.Fatalf("stale commit detail: %+v", d)
		}
		// No commit at all, on a job that has a head, is refused by the factory.
		alice.w.mustFail("factory.job_approve", alice.params(t, proof.ID, nil), appwire.CodeBadParams)
		bob.refused(t, "factory.job_approve", bob.params(t, proof.ID, map[string]any{"commit": actionHead}), appwire.CodeForbidden)
		got := alice.act(t, "factory.job_approve", proof.ID, map[string]any{"commit": actionHead, "words": "ship it"})
		if got.Approval == nil || got.Approval.Kind != "proof" || got.Approval.Commit != actionHead || got.Approval.State != "approving" {
			t.Fatalf("proof approval: %+v", got.Approval)
		}
		alice.act(t, "factory.job_undo", proof.ID, nil)
	})

	t.Run("request changes and retry publishing", func(t *testing.T) {
		landed := f.create(t, factory.Item{State: factory.StateLanded, Assignee: "alice", HeadCommit: actionHead})
		alice.act(t, "factory.job_request_changes", landed.ID, map[string]any{"words": "prove restart survival"})
		if got, _ := f.sentBack.Load().(string); got != "prove restart survival" {
			t.Fatalf("send back words = %q", got)
		}
		// Retry publishing is the assignee's, and only for a publish that failed.
		failed := f.create(t, factory.Item{State: factory.StateLanded, Assignee: "alice", HeadCommit: actionHead, PublishStatus: &factory.Publishing{State: "failed"}})
		alice.refused(t, "factory.job_retry_publish", alice.params(t, landed.ID, nil), appwire.CodeConflict)
		bob.refused(t, "factory.job_retry_publish", bob.params(t, failed.ID, nil), appwire.CodeForbidden)
		// No approval trailer exists for this head, so the factory refuses to
		// publish an unapproved commit; nothing was published.
		_, d := alice.refused(t, "factory.job_retry_publish", alice.params(t, failed.ID, nil), appwire.CodeStale)
		if d == nil || d.HeadCommit != actionHead {
			t.Fatalf("retry detail: %+v", d)
		}
	})

	t.Run("step controls name a step the job has", func(t *testing.T) {
		running := f.create(t, factory.Item{State: factory.StateRunning, Assignee: "alice", Stream: &factory.Stream{Phases: []factory.Phase{{Name: "build", State: factory.PhaseRunning}}}})
		alice.w.mustFail("factory.job_step_stop", alice.params(t, running.ID, map[string]any{"step": "nope"}), appwire.CodeNotFound)
		bob.refused(t, "factory.job_step_stop", bob.params(t, running.ID, map[string]any{"step": "build"}), appwire.CodeForbidden)
		got := alice.act(t, "factory.job_step_stop", running.ID, map[string]any{"step": "build"})
		if got.Action != appwire.JobActStepStop || got.Job != running.ID {
			t.Fatalf("step stop: %+v", got)
		}
		rerun := alice.act(t, "factory.job_step_rerun", running.ID, map[string]any{"step": "build"})
		if rerun.Action != appwire.JobActStepRerun {
			t.Fatalf("step rerun: %+v", rerun)
		}
	})

	// NO FRAME CARRIES A CREDENTIAL, a session, a device code or a GitHub token.
	alice.w.neverSaid(f.secrets(alice, bob)...)
	bob.w.neverSaid(f.secrets(alice, bob)...)
	// A MODEL WAS NEVER ASKED and nothing was started that the test did not say.
	if f.launched.Load() > 1 {
		t.Fatalf("launched %d times", f.launched.Load())
	}
}

// act2 is a call whose params are given whole (create has no job to read).
func (p *person) act2(t *testing.T, method string, params map[string]any) appwire.FactoryJobActed {
	t.Helper()
	var out appwire.FactoryJobActed
	p.w.must(method, params, &out)
	return out
}

// ── what a write does when the factory misbehaves ──────────────────────────

// A WRITE IS SENT ONCE. Whatever the factory does, the engine sends the one
// request and never a second, and says plainly whether the action may have
// happened.
func TestAppFactoryJobActionsAreSentOnceAndSayWhenTheyAreUncertain(t *testing.T) {
	var hits atomic.Int64
	status := atomic.Int64{}
	mode := atomic.Value{}
	mode.Store("status")
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"no read"}`, 404)
			return
		}
		hits.Add(1)
		switch mode.Load().(string) {
		case "hangup":
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
		case "forbidden":
			w.WriteHeader(403)
			w.Write([]byte(`{"error":"only @carol can do this","reason":"not_job_person","job_person":"@carol","now_state":"queued","request_id":"r1"}`))
		default:
			w.WriteHeader(int(status.Load()))
			w.Write([]byte(`{"error":"boom Bearer secret-in-error"}`))
		}
	})
	redact.Register("secret-in-error")
	b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
	one := appwire.FactoryJobActionParams{Factory: conn.ID, Job: 3, Expect: &api.Expect{Version: ptr(2)}}
	ctx := context.Background()
	check := func(what, mode_ string, code int, want string) {
		t.Helper()
		mode.Store(mode_)
		status.Store(int64(code))
		before := hits.Load()
		_, err := b.FactoryJobStop(ctx, one)
		if got := wireCodeOf(t, err); got != want {
			t.Fatalf("%s: code %q (%v), want %q", what, got, err, want)
		}
		if hits.Load()-before != 1 {
			t.Fatalf("%s: the write was sent %d times", what, hits.Load()-before)
		}
		if strings.Contains(err.Error(), "secret-in-error") {
			t.Fatalf("%s: a secret came back in %v", what, err)
		}
	}
	check("a dropped connection", "hangup", 0, appwire.CodeUncertain)
	check("a 500", "status", 500, appwire.CodeUncertain)
	check("a 502", "status", 502, appwire.CodeUncertain)
	check("a 501", "status", 501, appwire.CodeEngine)
	check("a 409 conflict", "status", 409, appwire.CodeConflict)
	check("a 404", "status", 404, appwire.CodeNotFound)
	check("a 401", "status", 401, appwire.CodeSignedOut)
	check("a 422", "status", 422, appwire.CodeBadParams)
	check("a 429", "status", 429, appwire.CodeBusy)
	check("a refusal", "forbidden", 0, appwire.CodeForbidden)
	var wire *appwire.Error
	mode.Store("forbidden")
	_, err := b.FactoryJobStop(ctx, one)
	if !errors.As(err, &wire) {
		t.Fatal(err)
	}
	if d, ok := wire.Detail.(*appwire.FactoryRefusal); !ok || d.JobPerson != "@carol" || d.Reason != "not_job_person" || d.NowState != "queued" {
		t.Fatalf("detail = %#v", wire.Detail)
	}
}

// A FACTORY THAT IS NOT THERE IS "unreachable", NOT "uncertain": nothing was sent.
func TestAppFactoryJobActionOnAClosedFactoryIsUnreachable(t *testing.T) {
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {})
	b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
	stub.srv.Close()
	// Force a fresh dial: an idle connection closed during shutdown can instead
	// return EOF, which is correctly uncertain after a write might have left.
	b.factoryHub().client.CloseIdleConnections()
	_, err := b.FactoryJobStart(context.Background(), appwire.FactoryJobActionParams{Factory: conn.ID, Job: 1, Expect: &api.Expect{Version: ptr(1)}})
	if got := wireCodeOf(t, err); got != appwire.CodeUnreachable {
		t.Fatalf("code %q (%v)", got, err)
	}
	if _, err := b.FactoryJobStart(context.Background(), appwire.FactoryJobActionParams{Factory: "factory_nope", Job: 1, Expect: &api.Expect{Version: ptr(1)}}); wireCodeOf(t, err) != appwire.CodeNotFound {
		t.Fatalf("an unknown factory: %v", err)
	}
}

// THE REQUEST IS EXACTLY THE ROUTE AND BODY THE FACTORY DEFINES, and carries
// the person's session, never anything else.
func TestAppFactoryJobActionsSpeakOnlyTheFactorysRoutes(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		b, _ := json.Marshal(body)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.EscapedPath()+" "+string(b)+" "+r.Header.Get("Authorization"))
		mu.Unlock()
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"job_id":"j","version":8}`))
			return
		}
		w.Write([]byte(`{"factory":{"name":"Stub"},"id":9}`))
	})
	b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
	ctx := context.Background()
	v := &api.Expect{Version: ptr(7)}
	f := conn.ID
	b.FactoryJobStart(ctx, appwire.FactoryJobActionParams{Factory: f, Job: 4, Expect: v})
	b.FactoryJobDrop(ctx, appwire.FactoryJobDropParams{Factory: f, Job: 4, Words: "dup", Expect: v})
	b.FactoryJobChangeAssignee(ctx, appwire.FactoryJobAssigneeParams{Factory: f, Job: 4, Assignee: "bob", Expect: v})
	b.FactoryJobSetPriority(ctx, appwire.FactoryJobPriorityParams{Factory: f, Job: 4, Priority: "low", Expect: v})
	b.FactoryJobSetBudget(ctx, appwire.FactoryJobBudgetParams{Factory: f, Job: 4, Budget: 20, Expect: v})
	b.FactoryJobApprove(ctx, appwire.FactoryJobApproveParams{Factory: f, Job: 4, Commit: "abc1234", Words: "ok", Edited: true, Expect: v})
	b.FactoryJobRequestChanges(ctx, appwire.FactoryJobChangesParams{Factory: f, Job: 4, Words: "redo", Expect: v})
	b.FactoryJobStepRerun(ctx, appwire.FactoryJobStepParams{Factory: f, Job: 4, Step: "re view", Expect: v})
	made, err := b.FactoryJobCreate(ctx, appwire.FactoryJobCreateParams{Factory: f, Repo: "o/r", Title: "T", Text: "B"})
	if err != nil || made.Job != 9 {
		t.Fatalf("create: %+v %v", made, err)
	}
	var posts []string
	mu.Lock()
	for _, s := range seen {
		if strings.HasPrefix(s, "POST") {
			posts = append(posts, s)
		}
	}
	mu.Unlock()
	want := []string{
		`POST /v1/jobs/4/start_now {"expect":{"version":7}} Bearer ` + actionTestCredential,
		`POST /v1/jobs/4/drop {"expect":{"version":7},"words":"dup"} Bearer ` + actionTestCredential,
		`POST /v1/jobs/4/change_assignee {"assignee":"bob","expect":{"version":7}} Bearer ` + actionTestCredential,
		`POST /v1/jobs/4/priority {"expect":{"version":7},"priority":"low"} Bearer ` + actionTestCredential,
		`POST /v1/jobs/4/raise_budget {"budget":20,"expect":{"version":7}} Bearer ` + actionTestCredential,
		`POST /v1/jobs/4/approve {"commit":"abc1234","edited":true,"expect":{"version":7},"words":"ok"} Bearer ` + actionTestCredential,
		`POST /v1/jobs/4/request_changes {"expect":{"version":7},"words":"redo"} Bearer ` + actionTestCredential,
		`POST /v1/jobs/4/steps/re%20view/rerun {"expect":{"version":7}} Bearer ` + actionTestCredential,
		`POST /v1/jobs {"repo":"o/r","source":"app","text":"B","title":"T"} Bearer ` + actionTestCredential,
	}
	if strings.Join(posts, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(posts, "\n"), strings.Join(want, "\n"))
	}
}

func ptr[T any](v T) *T { return &v }
