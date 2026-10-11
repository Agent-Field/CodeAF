package lease_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

type fixture struct {
	store *store.Store
	desk  *lease.Desk
	now   time.Time
}

func setup(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{store: st, now: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)}
	f.desk = f.open(t)
	return f
}

func (f *fixture) open(t *testing.T) *lease.Desk {
	t.Helper()
	desk, err := lease.Open(f.store, lease.Options{Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	return desk
}

func (f *fixture) add(t *testing.T, it factory.Item) factory.Item {
	t.Helper()
	it.Created = f.now
	if it.State == "" {
		it.State = factory.StateQueued
	}
	if it.Repo == "" {
		it.Repo = "acme/api"
	}
	it, err := f.store.Create(it)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func (f *fixture) state(t *testing.T, id int, want factory.State) {
	t.Helper()
	it, err := f.store.Get(id)
	if err != nil || it.State != want {
		t.Fatalf("job %d: state=%s err=%v; want %s", id, it.State, err, want)
	}
}

func worker(id, handle string) lease.Worker { return lease.Worker{ID: id, Handle: handle} }

func TestOneLeasePerJobAcrossDesks(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Assignee: "sam"})
	const count = 16
	desks := make([]*lease.Desk, count)
	for i := range desks {
		desks[i] = f.open(t)
	}
	start := make(chan struct{})
	results := make(chan error, count)
	var group sync.WaitGroup
	for i, desk := range desks {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			held, err := desk.Pull(worker(string(rune('a'+i)), "sam"))
			if err == nil && held.Job != it.ID {
				err = errors.New("pulled a different job")
			}
			results <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	taken := 0
	for err := range results {
		if err == nil {
			taken++
		} else if !errors.Is(err, lease.ErrNoJob) {
			t.Fatal(err)
		}
	}
	if taken != 1 {
		t.Fatalf("%d workers took one job", taken)
	}
	f.state(t, it.ID, factory.StateRunning)
	data, err := os.ReadFile(f.desk.Path())
	if err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		Leases []lease.Lease `json:"leases"`
	}
	if err := json.Unmarshal(data, &persisted); err != nil || len(persisted.Leases) != 1 {
		t.Fatalf("persisted leases: %s, %v", data, err)
	}
}

func TestOneJobPerWorkerSurvivesReopen(t *testing.T) {
	f := setup(t)
	f.add(t, factory.Item{Starter: "sam"})
	f.add(t, factory.Item{Starter: "sam"})
	w := worker("laptop", "sam")
	if _, err := f.desk.Pull(w); err != nil {
		t.Fatal(err)
	}
	if _, err := f.open(t).Pull(w); !errors.Is(err, lease.ErrWorkerBusy) {
		t.Fatalf("second pull: %v", err)
	}
}

func TestALiveLeaseProtectsAJobPutBackInTheQueue(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Assignee: "sam"})
	if _, err := f.desk.Pull(worker("laptop", "sam")); err != nil {
		t.Fatal(err)
	}
	// A concurrent queue writer cannot revoke the claims desk's ownership by
	// changing only the item state; the persisted lease remains authoritative.
	if err := f.store.Update(it.ID, func(it *factory.Item) error {
		it.State = factory.StateQueued
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.open(t).Pull(worker("desktop", "sam")); !errors.Is(err, lease.ErrNoJob) {
		t.Fatalf("second worker took a live lease: %v", err)
	}
}

func TestPersonRule(t *testing.T) {
	cases := []struct {
		name   string
		item   factory.Item
		person string
	}{
		{"assignee", factory.Item{Assignee: "@Sam", Author: "reporter"}, "sam"},
		{"starter", factory.Item{Starter: "sam", Author: "reporter"}, "sam"},
		{"taker", factory.Item{Taker: "sam"}, "sam"},
		{"assignee before taker", factory.Item{Assignee: "sam", Taker: "other", Starter: "other"}, "sam"},
		{"taker before starter", factory.Item{Taker: "sam", Starter: "other"}, "sam"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			it := f.add(t, tc.item)
			stranger := worker("stranger", "other")
			stranger.TakesUnassigned = map[string]bool{it.Repo: true}
			if _, err := f.desk.Pull(stranger); !errors.Is(err, lease.ErrNoJob) {
				t.Fatalf("other person with opt-in: %v", err)
			}
			f.state(t, it.ID, factory.StateQueued)
			held, err := f.desk.Pull(worker("owner", tc.person))
			if err != nil || held.Job != it.ID || held.Handle != tc.person {
				t.Fatalf("person's pull: %+v, %v", held, err)
			}
		})
	}
}

func TestTakeUnassignedRequiresConsentForThatRepo(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Author: "sam"})
	w := worker("laptop", "sam")
	for _, optIn := range []map[string]bool{nil, {it.Repo: false}, {"acme/other": true}} {
		w.TakesUnassigned = optIn
		if _, err := f.desk.Pull(w); !errors.Is(err, lease.ErrNoJob) {
			t.Fatalf("opt-in %v: %v", optIn, err)
		}
	}
	w.TakesUnassigned = map[string]bool{it.Repo: true}
	held, err := f.desk.Pull(w)
	if err != nil || held.Job != it.ID {
		t.Fatalf("opted-in pull: %+v, %v", held, err)
	}
}

func TestAssignPersistsTheTakerAndDoesNotStartWork(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{})
	if err := f.desk.Assign(it.ID, " @Sam "); err != nil {
		t.Fatal(err)
	}
	f.state(t, it.ID, factory.StateQueued)
	read, err := f.store.Get(it.ID)
	if err != nil || read.Taker != "sam" {
		t.Fatalf("taker: %+v, %v", read, err)
	}
	if err := f.open(t).Assign(it.ID, "other"); !errors.Is(err, lease.ErrPerson) {
		t.Fatalf("take someone else's job: %v", err)
	}
	held, err := f.open(t).Pull(worker("laptop", "sam"))
	if err != nil || held.Job != it.ID {
		t.Fatalf("taker's pull: %+v, %v", held, err)
	}
	if err := f.desk.Assign(it.ID, "sam"); !errors.Is(err, lease.ErrLeased) {
		t.Fatalf("take while leased: %v", err)
	}
}

func TestAssignCannotReplaceAnAssigneeOrStarter(t *testing.T) {
	for _, it := range []factory.Item{{Assignee: "sam"}, {Starter: "sam"}} {
		f := setup(t)
		it = f.add(t, it)
		if err := f.desk.Assign(it.ID, "other"); !errors.Is(err, lease.ErrPerson) {
			t.Fatalf("assign %+v: %v", it, err)
		}
		if err := f.desk.Assign(it.ID, ""); err == nil {
			t.Fatal("accepted an empty handle")
		}
	}
}

func TestHeartbeatRenewsOnlyTheHolder(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Assignee: "sam"})
	held, err := f.desk.Pull(worker("laptop", "sam"))
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(lease.DefaultTTL / 2)
	if _, err := f.desk.Heartbeat(it.ID, "desktop"); !errors.Is(err, lease.ErrWorker) {
		t.Fatalf("other worker: %v", err)
	}
	renewed, err := f.open(t).Heartbeat(it.ID, "laptop")
	if err != nil || !renewed.ExpiresAt.Equal(f.now.Add(lease.DefaultTTL)) {
		t.Fatalf("heartbeat: %+v, %v", renewed, err)
	}
	if renewed.Since.IsZero() || !renewed.Since.Equal(held.Since) || !renewed.HeartbeatAt.Equal(f.now) {
		t.Fatalf("hold times: pulled %+v renewed %+v", held, renewed)
	}
	f.now = held.ExpiresAt
	expired, err := f.desk.Expire()
	if err != nil || len(expired) != 0 {
		t.Fatalf("expired before renewal: %+v, %v", expired, err)
	}
	f.now = renewed.ExpiresAt
	if _, err := f.desk.Heartbeat(it.ID, "laptop"); !errors.Is(err, lease.ErrNoLease) {
		t.Fatalf("late heartbeat: %v", err)
	}
	f.state(t, it.ID, factory.StateQueued)
}

func TestExpiryRequeuesAtTheDeadlineAndCanBePulledAgain(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Starter: "sam"})
	held, err := f.desk.Pull(worker("laptop", "sam"))
	if err != nil {
		t.Fatal(err)
	}
	f.now = held.ExpiresAt.Add(-time.Nanosecond)
	if got, err := f.desk.Expire(); err != nil || len(got) != 0 {
		t.Fatalf("before deadline: %+v, %v", got, err)
	}
	f.now = held.ExpiresAt
	expired, err := f.open(t).Expire()
	if err != nil || !reflect.DeepEqual(expired, []lease.Lease{held}) {
		t.Fatalf("at deadline: %+v, %v", expired, err)
	}
	f.state(t, it.ID, factory.StateQueued)
	lost, err := f.store.Get(it.ID)
	if err != nil || lost.Failure == nil || lost.Failure.Code != factory.CodeMachineLost || lost.Failure.Next != factory.FailureRequeued || lost.Failure.Detail != "the machine holding the work was lost" {
		t.Fatalf("expiry failure: %+v %v", lost.Failure, err)
	}
	if got, err := f.desk.Expire(); err != nil || len(got) != 0 {
		t.Fatalf("second expiry: %+v, %v", got, err)
	}
	next, err := f.desk.Pull(worker("desktop", "sam"))
	if err != nil || next.Job != it.ID || !next.ExpiresAt.After(held.ExpiresAt) {
		t.Fatalf("requeued pull: %+v, %v", next, err)
	}
	if _, err := f.desk.Heartbeat(it.ID, "laptop"); !errors.Is(err, lease.ErrWorker) {
		t.Fatalf("old worker renewed replacement: %v", err)
	}
}

func TestExpiryKeepsTheStepAndRecordsIt(t *testing.T) {
	f := setup(t)
	rec, err := record.Open(f.store, record.Options{Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	desk, err := lease.Open(f.store, lease.Options{Now: func() time.Time { return f.now }, Record: rec})
	if err != nil {
		t.Fatal(err)
	}
	it := f.add(t, factory.Item{Assignee: "sam", Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "write", State: factory.PhaseRunning, Round: 2}}}})
	held, err := desk.Pull(lease.Worker{ID: "laptop", Handle: "sam", Machine: "ci-box"})
	if err != nil {
		t.Fatal(err)
	}
	f.now = held.ExpiresAt
	if _, err := desk.Expire(); err != nil {
		t.Fatal(err)
	}
	lost, err := f.store.Get(it.ID)
	if err != nil || lost.State != factory.StateQueued || lost.Failure == nil {
		t.Fatalf("requeued = %+v %v", lost, err)
	}
	if lost.Failure.Attempts != 2 || lost.Failure.Machine != "ci-box" || lost.Failure.Next != factory.FailureRequeued || lost.Failure.Code != factory.CodeMachineLost {
		t.Fatalf("failure = %+v", lost.Failure)
	}
	ph := lost.Stream.Phases[0]
	if ph.State == factory.PhaseFailed || ph.Failure == nil || ph.Failure.Next != factory.FailureRequeued {
		t.Fatalf("step was finished by the expiry: %+v", ph)
	}
	events, err := rec.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 1 || events[0].Kind != record.StepFailed || events[0].Job != it.ID || events[0].Detail != factory.CodeMachineLost+" · write · "+factory.FailureRequeued {
		t.Fatalf("record = %+v %v", events, err)
	}
}

func TestPullExpiresWithoutATimer(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Assignee: "sam"})
	held, err := f.desk.Pull(worker("laptop", "sam"))
	if err != nil {
		t.Fatal(err)
	}
	f.now = held.ExpiresAt
	next, err := f.open(t).Pull(worker("desktop", "sam"))
	if err != nil || next.Job != it.ID {
		t.Fatalf("pull recovers expiry: %+v, %v", next, err)
	}
}

func TestRequeueRequiresTheHolderAndKeepsThePerson(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Taker: "sam"})
	if _, err := f.desk.Pull(worker("laptop", "sam")); err != nil {
		t.Fatal(err)
	}
	if err := f.desk.Requeue(it.ID, "desktop"); !errors.Is(err, lease.ErrWorker) {
		t.Fatalf("other worker requeue: %v", err)
	}
	f.state(t, it.ID, factory.StateRunning)
	if err := f.desk.Requeue(it.ID, "laptop"); err != nil {
		t.Fatal(err)
	}
	f.state(t, it.ID, factory.StateQueued)
	released, err := f.store.Get(it.ID)
	if err != nil || released.Failure != nil {
		t.Fatalf("a voluntary requeue recorded a failure: %+v %v", released.Failure, err)
	}
	if _, err := f.desk.Pull(worker("desktop", "other")); !errors.Is(err, lease.ErrNoJob) {
		t.Fatalf("person changed on release: %v", err)
	}
	if _, err := f.desk.Pull(worker("desktop", "sam")); err != nil {
		t.Fatal(err)
	}
}

func TestExpiryNeverRestartsPausedOrFinishedWork(t *testing.T) {
	for _, state := range []factory.State{factory.StateShipped, factory.StateDismissed, factory.StateNeedsYou} {
		f := setup(t)
		it := f.add(t, factory.Item{Assignee: "sam"})
		held, err := f.desk.Pull(worker("laptop", "sam"))
		if err != nil {
			t.Fatal(err)
		}
		if err := f.store.Update(it.ID, func(it *factory.Item) error { it.State = state; return nil }); err != nil {
			t.Fatal(err)
		}
		revision, err := f.store.Revision(it.ID)
		if err != nil {
			t.Fatal(err)
		}
		f.now = held.ExpiresAt
		if _, err := f.desk.Expire(); err != nil {
			t.Fatal(err)
		}
		f.state(t, it.ID, state)
		if after, err := f.store.Revision(it.ID); err != nil || after != revision {
			t.Fatalf("expiry rewrote finished work: revision %d -> %d, %v", revision, after, err)
		}
		if _, err := f.desk.Pull(worker("desktop", "sam")); !errors.Is(err, lease.ErrNoJob) {
			t.Fatalf("restarted %s: %v", state, err)
		}
	}
}

func TestExpiryKeepsPausedWorkPaused(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Assignee: "sam"})
	held, err := f.desk.Pull(worker("laptop", "sam"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Update(it.ID, func(it *factory.Item) error {
		it.Stream = &factory.Stream{Paused: true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.now = held.ExpiresAt
	if expired, err := f.desk.Expire(); err != nil || len(expired) != 1 {
		t.Fatalf("paused expiry: %+v, %v", expired, err)
	}
	f.state(t, it.ID, factory.StateRunning)
	if _, err := f.desk.Pull(worker("desktop", "sam")); !errors.Is(err, lease.ErrNoJob) {
		t.Fatalf("restarted paused work: %v", err)
	}
}

func TestCustomTTLAndOldestEligibleJob(t *testing.T) {
	f := setup(t)
	f.add(t, factory.Item{Assignee: "other"})
	first := f.add(t, factory.Item{Assignee: "sam"})
	f.now = f.now.Add(time.Second)
	f.add(t, factory.Item{Assignee: "sam"})
	desk, err := lease.Open(f.store, lease.Options{TTL: 10 * time.Second, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	held, err := desk.Pull(worker("laptop", "sam"))
	if err != nil || held.Job != first.ID || !held.ExpiresAt.Equal(f.now.Add(10*time.Second)) {
		t.Fatalf("oldest/custom TTL: %+v, %v", held, err)
	}
}

func TestBadDeskNeverBecomesAnEmptyDesk(t *testing.T) {
	for _, data := range []string{
		`{`,
		`{"schema":2,"leases":[]}`,
		`{"schema":1,"leases":[{"job":1}]}`,
		`{"schema":1,"leases":[{"job":1,"worker":"a","handle":"sam","expires_at":"2026-10-11T13:00:00Z"},{"job":1,"worker":"b","handle":"sam","expires_at":"2026-10-11T13:00:00Z"}]}`,
	} {
		f := setup(t)
		it := f.add(t, factory.Item{Assignee: "sam"})
		if err := os.WriteFile(f.desk.Path(), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := f.desk.Pull(worker("laptop", "sam")); err == nil {
			t.Fatal("pulled through a bad desk")
		}
		f.state(t, it.ID, factory.StateQueued)
		got, err := os.ReadFile(f.desk.Path())
		if err != nil || string(got) != data {
			t.Fatalf("rewrote a bad desk: %q, %v", got, err)
		}
	}
}

func TestOnlyQueuedWorkIsOfferedAndInputsAreRequired(t *testing.T) {
	f := setup(t)
	f.add(t, factory.Item{Assignee: "sam", State: factory.StateNew})
	if _, err := f.desk.Pull(worker("laptop", "sam")); !errors.Is(err, lease.ErrNoJob) {
		t.Fatalf("pulled backlog: %v", err)
	}
	for _, w := range []lease.Worker{worker("", "sam"), worker("laptop", "")} {
		if _, err := f.desk.Pull(w); err == nil {
			t.Fatal("accepted missing identity")
		}
	}
	if _, err := lease.Open(nil, lease.Options{}); err == nil {
		t.Fatal("opened without a store")
	}
	if _, err := lease.Open(f.store, lease.Options{TTL: -time.Second}); err == nil {
		t.Fatal("accepted negative TTL")
	}
}

func TestReleaseClearsOnlyThePersonsTake(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{})
	if err := f.desk.Release(it.ID, "sam"); !errors.Is(err, lease.ErrNotTaken) {
		t.Fatalf("untaken: %v", err)
	}
	if err := f.desk.Assign(it.ID, "sam"); err != nil {
		t.Fatal(err)
	}
	if err := f.desk.Release(it.ID, "other"); !errors.Is(err, lease.ErrPerson) {
		t.Fatalf("someone else: %v", err)
	}
	if err := f.desk.Release(it.ID, " @Sam "); err != nil {
		t.Fatal(err)
	}
	read, err := f.store.Get(it.ID)
	if err != nil || read.Taker != "" || read.State != factory.StateQueued {
		t.Fatalf("after release: %+v %v", read, err)
	}
}
