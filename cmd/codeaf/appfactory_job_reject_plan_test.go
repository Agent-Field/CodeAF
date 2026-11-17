package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// A PLAN SENT BACK, AGAINST THE REAL SERVER, STORE, RECORD AND LEASE DESK, with
// no model: the app's "Request changes" on a plan is ONE POST to the factory's
// typed /v1/jobs/{id}/reject_plan, and the factory decides on the locked job
// document whether the page the person reviewed is still a plan it can send
// back. Nothing here ever reaches /answer.

// planJob is a job at its plan gate: the plan step is done, the approve step
// waits, and a build step follows. It already carries one note.
func planJob(assignee string) factory.Item {
	return factory.Item{
		State: factory.StateNeedsYou, Assignee: assignee, Notes: []string{"an earlier note"},
		Question: factory.ApproveQuestion("plan"), QKind: factory.QKindApprove,
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1},
			{Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting},
			{Name: "build", Kind: factory.StageChat, State: factory.PhasePending},
		}},
	}
}

// firstGateJob waits at an approve step with no finished step before it: the
// same no on the answer route would STOP this job.
func firstGateJob(assignee string) factory.Item {
	it := planJob(assignee)
	it.Question = factory.ApproveQuestion("")
	it.Stream = &factory.Stream{Cur: 0, Phases: []factory.Phase{
		{Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting},
		{Name: "plan", Kind: factory.StageChat, State: factory.PhasePending},
	}}
	return it
}

// snapshot is the whole stored job and its revision, to prove a refused write
// changed nothing.
func (f *actionFactory) snapshot(t *testing.T, id int) string {
	t.Helper()
	it, err := f.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := f.st.Revision(id)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(it)
	r, _ := json.Marshal(rev)
	return string(b) + "@" + string(r)
}

// rejectPOSTs is how many POSTs reached the typed route, and answerPOSTs how
// many reached /answer, which this method must never touch.
func (f *actionFactory) routePOSTs(suffix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.posts {
		if strings.HasSuffix(p, suffix) {
			n++
		}
	}
	return n
}

// unchanged is everything a refusal must leave as it was: the job and its
// revision, every receipt, and every POST that is not the one refused.
type unchanged struct {
	snap                       string
	answered, approved, undone int
	answers                    int
}

func (f *actionFactory) hold(t *testing.T, id int) unchanged {
	t.Helper()
	return unchanged{f.snapshot(t, id), f.count(record.Answered), f.count(record.Approved), f.count(record.Undone), f.routePOSTs("/answer")}
}

func (f *actionFactory) same(t *testing.T, id int, was unchanged, what string) {
	t.Helper()
	if now := f.hold(t, id); now != was {
		t.Fatalf("%s changed something: before %+v\nafter %+v", what, was, now)
	}
}

// rejectParams is the request the app sends: the version just read, and the
// state the person saw, which for a plan is always needs_you.
func rejectParams(t *testing.T, p *person, n int, words string) map[string]any {
	t.Helper()
	return map[string]any{"factory": p.conn.ID, "job": n, "words": words, "expect": map[string]any{"version": p.job(t, n).Version, "state": "needs_you"}}
}

func TestAppFactoryRejectPlanAgainstTheRealServer(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	bob := signedIn(t, f, 2, "bob")

	alice.w.mu.Lock()
	hello := alice.w.lines[0]
	alice.w.mu.Unlock()
	if !strings.Contains(hello, appwire.FeatureFactoryJobRejectPlan) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryJobRejectPlan, hello)
	}

	t.Run("the assignee sends a plan back to the step before the gate with the words", func(t *testing.T) {
		job := f.create(t, planJob("alice"))
		before := alice.job(t, job.ID)
		if before.NeedsYou == nil || before.NeedsYou.Kind != factory.NeedsApprovePlan || before.NeedsYou.BackTo != "plan" || !before.Can.RejectPlan {
			t.Fatalf("the read does not offer the revision: can %+v needs %+v", before.Can, before.NeedsYou)
		}
		if b := bob.job(t, job.ID); b.Can.RejectPlan {
			t.Fatal("another person is offered alice's plan")
		}
		rejects, approved, answered := f.routePOSTs("/reject_plan"), f.count(record.Approved), f.count(record.Answered)

		got := alice.act2(t, "factory.job_reject_plan", rejectParams(t, alice, job.ID, "  use the other file\nand keep the tests  "))
		if got.Action != appwire.JobActRejectPlan || got.Job != job.ID {
			t.Fatalf("acted: %+v", got)
		}
		// NOT A DECISION TO PUBLISH: no approval receipt, no Undo window.
		if got.Approval != nil || got.UndoUntil != nil {
			t.Fatalf("a plan no carried an approval or an Undo: %+v", got)
		}
		if f.routePOSTs("/reject_plan")-rejects != 1 || f.routePOSTs("/answer") != 0 {
			t.Fatalf("one send was %d typed POSTs and %d answers", f.routePOSTs("/reject_plan")-rejects, f.routePOSTs("/answer"))
		}
		if f.count(record.Approved) != approved || f.count(record.Answered) != answered+1 {
			t.Fatalf("receipts: approved %d→%d answered %d→%d", approved, f.count(record.Approved), answered, f.count(record.Answered))
		}
		if got.Current == nil || got.Current.State != api.StateQueued || got.Current.Can.RejectPlan {
			t.Fatalf("current: %+v", got.Current)
		}

		it, _ := f.st.Get(job.ID)
		ph := it.Stream.Phases
		if it.State != factory.StateQueued || it.Question != "" || it.QKind != "" || it.Asking != nil {
			t.Fatalf("job after: state %q question %q kind %q", it.State, it.Question, it.QKind)
		}
		// Back to the step BEFORE the gate, in a new round, gate waiting again.
		if it.Stream.Cur != 0 || ph[0].State != factory.PhasePending || ph[0].Round != 2 || ph[1].State != factory.PhasePending {
			t.Fatalf("not rewound to plan: cur %d phases %+v", it.Stream.Cur, ph)
		}
		if len(it.Notes) != 2 || it.Notes[0] != "an earlier note" || it.Notes[1] != "use the other file\nand keep the tests" {
			t.Fatalf("notes = %q", it.Notes)
		}
		if len(it.AnsweredQuestions) != 1 || it.AnsweredQuestions[0].Answer == nil || it.AnsweredQuestions[0].Answer.By != "alice" {
			t.Fatalf("the answered question was not kept: %+v", it.AnsweredQuestions)
		}
		if f.sentBack.Load() != nil || f.launched.Load() != 0 {
			t.Fatalf("a proof send-back or a launch happened: %v %d", f.sentBack.Load(), f.launched.Load())
		}
		// NO UNDO: nothing was held, so there is nothing to withdraw.
		if _, code, _ := alice.w.call("factory.job_undo", alice.params(t, job.ID, nil)); code == "" {
			t.Fatal("an Undo succeeded on a plan no")
		}
		if f.count(record.Undone) != 0 {
			t.Fatal("an Undo withdrew something")
		}
		// THE SAME PAGE SENT AGAIN is stale: the job moved when it was sent back.
		was := f.hold(t, job.ID)
		_, d := alice.refused(t, "factory.job_reject_plan", map[string]any{"factory": alice.conn.ID, "job": job.ID, "words": "again", "expect": map[string]any{"version": before.Version, "state": "needs_you"}}, appwire.CodeStale)
		if d == nil || d.Reason != "stale" || d.NowState != api.StateQueued {
			t.Fatalf("second send detail = %+v", d)
		}
		f.same(t, job.ID, was, "a second send")
	})

	t.Run("a different person is forbidden and nothing is written", func(t *testing.T) {
		job := f.create(t, planJob("alice"))
		was := f.hold(t, job.ID)
		_, d := bob.refused(t, "factory.job_reject_plan", rejectParams(t, bob, job.ID, "I would do it differently"), appwire.CodeForbidden)
		if d == nil || d.Reason != "not_job_person" || d.JobPerson != "@alice" {
			t.Fatalf("forbidden detail = %+v", d)
		}
		f.same(t, job.ID, was, "a forbidden plan no")
	})

	t.Run("a plan with no person yet follows the answer route's rule", func(t *testing.T) {
		job := f.create(t, planJob(""))
		if !bob.job(t, job.ID).Can.RejectPlan {
			t.Fatal("a plan nobody holds is not offered to a signed-in caller")
		}
		got := bob.act2(t, "factory.job_reject_plan", rejectParams(t, bob, job.ID, "nobody has this plan yet"))
		if got.Action != appwire.JobActRejectPlan || got.Current == nil || got.Current.State != api.StateQueued {
			t.Fatalf("acted: %+v", got)
		}
		if it, _ := f.st.Get(job.ID); it.Stream.Cur != 0 || it.Stream.Phases[0].Round != 2 {
			t.Fatalf("not rewound: %+v", it.Stream)
		}
	})

	t.Run("a caller the factory cannot name is forbidden and writes nothing", func(t *testing.T) {
		job := f.create(t, planJob("alice"))
		anon := newFactoryWire(t, testFactoryHub(t))
		var conn appwire.FactoryConnection
		anon.must("factory.attach", map[string]any{"url": f.URL, "token": testHostToken}, &conn)
		was, rejects := f.hold(t, job.ID), f.routePOSTs("/reject_plan")
		anonParams := map[string]any{"factory": conn.ID, "job": job.ID, "words": "no", "expect": map[string]any{"version": alice.job(t, job.ID).Version, "state": "needs_you"}}
		// The factory knows no handle for this caller, and no handle is not the
		// job's person: the locked document refuses it.
		_, d := (&person{w: anon, conn: conn}).refused(t, "factory.job_reject_plan", anonParams, appwire.CodeForbidden)
		if d == nil || d.Reason != "not_job_person" || d.JobPerson != "@alice" {
			t.Fatalf("detail = %+v", d)
		}
		f.same(t, job.ID, was, "an unidentified caller")
		if f.routePOSTs("/reject_plan") != rejects+1 {
			t.Fatal("the one refused write is not the one typed POST")
		}
	})

	t.Run("blank, too long and malformed requests are refused before any request", func(t *testing.T) {
		job := f.create(t, planJob("alice"))
		was, posts := f.hold(t, job.ID), f.postCount()
		v := alice.job(t, job.ID).Version
		for _, params := range []map[string]any{
			{"words": ""},
			{"words": " \n\t "},
			{"words": strings.Repeat("é", 4001)},
			{"words": "x", "expect": map[string]any{"state": "needs_you"}},
			{"words": "x", "expect": map[string]any{"version": v}},
			{"words": "x", "expect": map[string]any{"version": v, "state": "running"}},
			{"words": "x", "commit": "abc1234"},
			{"words": "x", "choice": "cancel_job"},
			{"words": "x", "yes": true},
		} {
			p := map[string]any{"factory": alice.conn.ID, "job": job.ID, "expect": map[string]any{"version": v, "state": "needs_you"}}
			for k, val := range params {
				p[k] = val
			}
			alice.w.mustFail("factory.job_reject_plan", p, appwire.CodeBadParams)
		}
		f.same(t, job.ID, was, "a malformed request")
		if f.postCount() != posts {
			t.Fatal("a malformed request reached the factory")
		}
	})

	t.Run("4000 characters are allowed, counted as characters", func(t *testing.T) {
		job := f.create(t, planJob("alice"))
		long := strings.Repeat("é", 4000)
		alice.act2(t, "factory.job_reject_plan", rejectParams(t, alice, job.ID, long))
		if it, _ := f.st.Get(job.ID); len(it.Notes) != 2 || it.Notes[1] != long {
			t.Fatalf("the words were not kept whole")
		}
	})

	t.Run("a version the person did not read is refused and nothing is written", func(t *testing.T) {
		job := f.create(t, planJob("alice"))
		read := alice.job(t, job.ID).Version
		bob.act(t, "factory.job_set_priority", job.ID, map[string]any{"priority": "urgent"})
		was := f.hold(t, job.ID)
		_, d := alice.refused(t, "factory.job_reject_plan", map[string]any{"factory": alice.conn.ID, "job": job.ID, "words": "redo", "expect": map[string]any{"version": read, "state": "needs_you"}}, appwire.CodeStale)
		if d == nil || d.Reason != "stale" || d.NowState != api.StateNeedsYou {
			t.Fatalf("stale detail = %+v", d)
		}
		f.same(t, job.ID, was, "a stale plan no")
	})

	t.Run("a plan with no finished step before it is refused, never stopped", func(t *testing.T) {
		job := f.create(t, firstGateJob("alice"))
		read := alice.job(t, job.ID)
		if read.Can.RejectPlan || read.NeedsYou == nil || read.NeedsYou.Kind != factory.NeedsApprovePlan || read.NeedsYou.BackTo != "" {
			t.Fatalf("a first gate is offered a revision: can %+v needs %+v", read.Can, read.NeedsYou)
		}
		was := f.hold(t, job.ID)
		msg, d := alice.refused(t, "factory.job_reject_plan", rejectParams(t, alice, job.ID, "redo"), appwire.CodeConflict)
		if msg != "This plan has no finished step before it to send back to." || d == nil || d.Reason != "conflict" || d.NowState != api.StateNeedsYou {
			t.Fatalf("first gate: %q %+v", msg, d)
		}
		f.same(t, job.ID, was, "a first-gate plan no")
	})

	t.Run("a plan whose run cannot be read is refused", func(t *testing.T) {
		noStream := planJob("alice")
		noStream.Stream = nil
		unknown := planJob("alice")
		unknown.Stream.Cur = 0
		unknown.Stream.Phases[0].State = factory.PhaseWaiting
		unknown.Stream.Phases[1].State = factory.PhasePending
		for name, it := range map[string]factory.Item{"no run": noStream, "an unrecognised shape": unknown} {
			job := f.create(t, it)
			if alice.job(t, job.ID).Can.RejectPlan {
				t.Fatalf("%s: offered a revision", name)
			}
			was := f.hold(t, job.ID)
			msg, _ := alice.refused(t, "factory.job_reject_plan", rejectParams(t, alice, job.ID, "redo"), appwire.CodeConflict)
			if msg != "This plan's run is not in a shape that can be sent back." {
				t.Fatalf("%s: %q", name, msg)
			}
			f.same(t, job.ID, was, name)
		}
	})

	t.Run("the shape older workers stored is sent back, and a nameless step has no back_to", func(t *testing.T) {
		misfiled := planJob("alice")
		misfiled.Stream.Cur = 0
		misfiled.Stream.Phases[0] = factory.Phase{Kind: factory.StageChat, State: factory.PhaseWaiting, Round: 1, Result: &factory.StageResult{Done: true}}
		misfiled.Stream.Phases[1].State = factory.PhasePending
		job := f.create(t, misfiled)
		read := alice.job(t, job.ID)
		if !read.Can.RejectPlan || read.NeedsYou == nil || read.NeedsYou.BackTo != "" {
			t.Fatalf("the old shape: can %+v needs %+v", read.Can, read.NeedsYou)
		}
		alice.act2(t, "factory.job_reject_plan", rejectParams(t, alice, job.ID, "redo the plan"))
		it, _ := f.st.Get(job.ID)
		if it.State != factory.StateQueued || it.Stream.Cur != 0 || it.Stream.Phases[0].Round != 2 || it.Stream.Phases[1].State != factory.PhasePending {
			t.Fatalf("not rewound: %s %+v", it.State, it.Stream)
		}
	})

	t.Run("anything but a plan waiting for its person is refused and nothing is written", func(t *testing.T) {
		stream := &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "build", Kind: factory.StageChat, State: factory.PhaseWaiting}}}
		for name, it := range map[string]factory.Item{
			"spending limit":  {State: factory.StateNeedsYou, Assignee: "alice", Question: "budget?", QKind: "cap", Cap: 5, Stream: stream},
			"out of attempts": {State: factory.StateNeedsYou, Assignee: "alice", Question: "try once more, send it back, or cancel the job?", QKind: factory.QKindOutOfAttempts, Stream: stream},
			"start question":  {State: factory.StateNeedsYou, Assignee: "alice", Question: "start this?", QKind: factory.QKindAskFirst},
			"free question":   {State: factory.StateNeedsYou, Assignee: "alice", Question: "which file?", QKind: "step", Stream: stream},
		} {
			job := f.create(t, it)
			if alice.job(t, job.ID).Can.RejectPlan {
				t.Fatalf("%s: offered a revision", name)
			}
			was := f.hold(t, job.ID)
			msg, d := alice.refused(t, "factory.job_reject_plan", rejectParams(t, alice, job.ID, "redo it"), appwire.CodeConflict)
			if msg != "Not a plan waiting for you." || d == nil || d.Reason != "conflict" {
				t.Fatalf("%s: %q %+v", name, msg, d)
			}
			f.same(t, job.ID, was, name)
		}
		// The state the person saw is part of the page: a job that is not
		// waiting is refused as moved on, with nothing written either.
		for name, it := range map[string]factory.Item{
			"running":     {State: factory.StateRunning, Assignee: "alice", Stream: stream},
			"not started": {State: factory.StateNew, Assignee: "alice"},
		} {
			job := f.create(t, it)
			was := f.hold(t, job.ID)
			alice.refused(t, "factory.job_reject_plan", rejectParams(t, alice, job.ID, "redo it"), appwire.CodeStale)
			f.same(t, job.ID, was, name)
		}
	})

	alice.w.neverSaid(f.secrets(alice, bob)...)
	bob.w.neverSaid(f.secrets(alice, bob)...)
}

// WORKER HELD VERSUS UNHELD. Unheld, the door itself rewinds the run (above).
// Held, the door takes the question down, sets the gate back to pending and
// signals the worker, whose loop does the rewind and keeps the words when it
// hears the no. A refusal on a held job leaves the lease and its signals alone.
func TestAppFactoryRejectPlanOnAJobAWorkerHolds(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	desk, err := lease.Open(f.st, lease.Options{Now: f.clock.now, Record: f.rec})
	if err != nil {
		t.Fatal(err)
	}
	hold := func(it factory.Item) (factory.Item, lease.Lease) {
		t.Helper()
		it.State = factory.StateQueued
		job := f.create(t, it)
		held, err := desk.Pull(lease.Worker{ID: "w" + string(rune('0'+job.ID)), Handle: "alice", Machine: "m1", Team: true})
		if err != nil || held.Job != job.ID {
			t.Fatalf("pull: %+v %v", held, err)
		}
		if err := f.st.Update(job.ID, func(cur *factory.Item) error { cur.State = factory.StateNeedsYou; return nil }); err != nil {
			t.Fatal(err)
		}
		return job, held
	}

	first, firstHeld := hold(firstGateJob("alice"))
	was := f.hold(t, first.ID)
	alice.refused(t, "factory.job_reject_plan", rejectParams(t, alice, first.ID, "redo"), appwire.CodeConflict)
	f.same(t, first.ID, was, "a held first gate")
	if signals, err := desk.Drain(firstHeld.ID, firstHeld.Worker); err != nil || len(signals) != 0 {
		t.Fatalf("a refusal signaled the worker: %+v %v", signals, err)
	}

	job, held := hold(planJob("alice"))
	got := alice.act2(t, "factory.job_reject_plan", rejectParams(t, alice, job.ID, "split it in two"))
	if got.Action != appwire.JobActRejectPlan || got.Approval != nil || got.UndoUntil != nil {
		t.Fatalf("acted: %+v", got)
	}
	cur, _ := f.st.Get(job.ID)
	if cur.State != factory.StateRunning || cur.Question != "" || cur.Stream.Cur != 1 || cur.Stream.Phases[1].State != factory.PhasePending || cur.Stream.Phases[0].State != factory.PhaseDone || len(cur.Notes) != 1 {
		t.Fatalf("held job after: state %q cur %d phases %+v notes %q", cur.State, cur.Stream.Cur, cur.Stream.Phases, cur.Notes)
	}
	signals, err := desk.Drain(held.ID, held.Worker)
	if err != nil || len(signals) != 1 || signals[0].Verb != lease.SignalAnswer || signals[0].Yes || signals[0].Words != "split it in two" {
		t.Fatalf("signals = %+v %v", signals, err)
	}
	if f.count(record.Answered) != 1 || f.count(record.Approved) != 0 || f.routePOSTs("/answer") != 0 {
		t.Fatal("receipts or routes")
	}
}

// THE RACE THAT MATTERS IS INSIDE THE FACTORY: after the handler has read the
// job and found it fresh, and before the door's own read, a write lands (a
// worker turning the plan into a spending limit, the job changing hands). The
// insideDesk hook runs while the server's desk lock is held and before the job
// document is read under it, which is exactly that window; the revision the
// person reviewed has moved, so the factory refuses the revision as stale and
// writes nothing of its own. A clock was never what kept this safe.
func TestAppFactoryRejectPlanRefusesAWriteThatLandsAfterTheHandlersRead(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	for name, move := range map[string]func(*factory.Item){
		"the plan becomes a spending limit": func(it *factory.Item) { it.Question, it.QKind, it.Cap = "budget?", "cap", 5 },
		"the job changes hands":             func(it *factory.Item) { it.Assignee = "bob" },
	} {
		t.Run(name, func(t *testing.T) {
			job := f.create(t, planJob("alice"))
			params := rejectParams(t, alice, job.ID, "redo the plan")
			fired := ""
			hook := func() bool {
				// Every request reads the desk first (who holds what); only
				// the door's own lock is the window under test.
				stack := string(debug.Stack())
				if !strings.Contains(stack, "(*Doors).RejectPlan") {
					return false
				}
				fired = stack
				if err := f.st.Update(job.ID, func(it *factory.Item) error { move(it); return nil }); err != nil {
					t.Error(err)
				}
				return true
			}
			f.insideDesk.Store(&hook)
			answered := f.count(record.Answered)
			_, d := alice.refused(t, "factory.job_reject_plan", params, appwire.CodeStale)
			if !strings.Contains(fired, "(*Doors).RejectPlan") || !strings.Contains(fired, "serve.(*Handler).rejectPlan") {
				t.Fatalf("the write did not land inside the door, after the handler's read:\n%s", fired)
			}
			if d == nil || d.Reason != "stale" {
				t.Fatalf("detail = %+v", d)
			}
			it, _ := f.st.Get(job.ID)
			if it.State != factory.StateNeedsYou || len(it.Notes) != 1 || it.Stream.Cur != 1 || it.Question == "" || f.count(record.Answered) != answered {
				t.Fatalf("the moved job was answered: %+v", it)
			}
			if rev, _ := f.st.Revision(job.ID); rev != *jobVersion(params)+1 {
				t.Fatalf("revision %d: only the landed write may have moved it", rev)
			}
		})
	}
}

func jobVersion(params map[string]any) *int {
	v := params["expect"].(map[string]any)["version"].(int)
	return &v
}

// TWO PEOPLE ON TWO WIRES send back one plan nobody holds, from the same
// page, at once: exactly one revision is written and the other is refused.
func TestAppFactoryRejectPlanFromTwoClientsAtOnceWritesOnce(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	bob := signedIn(t, f, 2, "bob")
	job := f.create(t, planJob(""))
	pa, pb := rejectParams(t, alice, job.ID, "from alice"), rejectParams(t, bob, job.ID, "from bob")
	var wg sync.WaitGroup
	codes := make([]string, 2)
	for i, c := range []struct {
		p      *person
		params map[string]any
	}{{alice, pa}, {bob, pb}} {
		wg.Add(1)
		go func(i int, p *person, params map[string]any) {
			defer wg.Done()
			_, codes[i], _ = p.w.call("factory.job_reject_plan", params)
		}(i, c.p, c.params)
	}
	wg.Wait()
	won := 0
	for _, code := range codes {
		switch code {
		case "":
			won++
		case appwire.CodeStale:
		default:
			t.Fatalf("codes = %q", codes)
		}
	}
	it, _ := f.st.Get(job.ID)
	if won != 1 || len(it.Notes) != 2 || it.Stream.Phases[0].Round != 2 || f.count(record.Answered) != 1 {
		t.Fatalf("codes %q, notes %q, round %d, answered %d", codes, it.Notes, it.Stream.Phases[0].Round, f.count(record.Answered))
	}
}

// THE WIRE IS EXACTLY ONE TYPED POST AND THE RECEIPT READ, with the body the
// typed route defines and nothing else.
func TestAppFactoryRejectPlanSpeaksOnlyTheTypedRoute(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		b, _ := json.Marshal(body)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.EscapedPath()+" "+string(b))
		mu.Unlock()
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"document_id":4,"version":8,"state":"queued"}`))
			return
		}
		w.Write([]byte(`{"factory":{"name":"Stub"}}`))
	})
	b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
	out, err := b.FactoryJobRejectPlan(context.Background(), appwire.FactoryJobRejectPlanParams{Factory: conn.ID, Job: 4, Words: "redo", Expect: &api.Expect{Version: ptr(7), State: "needs_you"}})
	if err != nil || out.Action != appwire.JobActRejectPlan || out.Current == nil || out.Current.Version != 8 {
		t.Fatalf("%+v %v", out, err)
	}
	mu.Lock()
	defer mu.Unlock()
	var calls []string
	for _, s := range seen {
		if !strings.HasPrefix(s, "GET /v1/me ") {
			calls = append(calls, s)
		}
	}
	want := `POST /v1/jobs/4/reject_plan {"expect":{"state":"needs_you","version":7},"words":"redo"}`
	if len(calls) != 2 || calls[0] != want || calls[1] != "GET /v1/jobs/4 null" {
		t.Fatalf("requests = %q: want the one typed write, then the receipt read", calls)
	}
}

// A FACTORY FROM BEFORE THE TYPED ROUTE answers it as an endpoint it does not
// have. That refusal is passed on, and nothing is ever tried on /answer.
func TestAppFactoryRejectPlanOnAnOlderFactoryNeverAnswers(t *testing.T) {
	for name, reply := range map[string]struct {
		status int
		body   string
	}{
		"the root answers only GET": {http.StatusMethodNotAllowed, `{"error":"this endpoint accepts GET","reason":""}`},
		"no such endpoint":          {http.StatusNotFound, `{"error":"factory endpoint not found","reason":"not_found"}`},
	} {
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var posts []string
			stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					mu.Lock()
					posts = append(posts, r.URL.Path)
					mu.Unlock()
					w.WriteHeader(reply.status)
					w.Write([]byte(reply.body))
					return
				}
				w.Write([]byte(`{"document_id":4,"version":7,"state":"needs_you","needs_you":{"kind":"approve_plan"}}`))
			})
			b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
			_, err := b.FactoryJobRejectPlan(context.Background(), appwire.FactoryJobRejectPlanParams{Factory: conn.ID, Job: 4, Words: "redo", Expect: &api.Expect{Version: ptr(7), State: "needs_you"}})
			var we *appwire.Error
			if !errors.As(err, &we) || we.Code == appwire.CodeUncertain {
				t.Fatalf("an older factory answered %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(posts) != 1 || posts[0] != "/v1/jobs/4/reject_plan" {
				t.Fatalf("posts = %q", posts)
			}
		})
	}
}
