package store_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// E1: each stored transition uses the exact timestamp of the factory.job fact.
func TestStoredOutcomesMatchJobFacts(t *testing.T) {
	at := time.Date(2026, 11, 1, 12, 3, 4, 567, time.FixedZone("offset", 3600))
	tests := []struct {
		name   string
		before factory.Item
		after  factory.Item
		kind   string
		fact   func(api.Job) time.Time
	}{
		{"merged", factory.Item{State: factory.StateShipped, PullRequest: &factory.PullRequest{State: factory.PROpen}}, factory.Item{State: factory.StateShipped, PullRequest: &factory.PullRequest{State: factory.PRMerged, Number: 42, URL: "https://github.com/acme/api/pull/42", MergedAt: at}}, "merged", func(j api.Job) time.Time { return *j.PR.MergedAt }},
		{"answered", factory.Item{State: factory.StateRunning}, factory.Item{State: factory.StateShipped, DoneAt: at}, "finished", func(j api.Job) time.Time { return j.DoneAt }},
		{"closed", factory.Item{State: factory.StateShipped, PullRequest: &factory.PullRequest{State: factory.PROpen}}, factory.Item{State: factory.StateShipped, PullRequest: &factory.PullRequest{State: factory.PRClosed}, DoneAt: at}, "finished", func(j api.Job) time.Time { return j.DoneAt }},
		{"dropped", factory.Item{State: factory.StateNew}, factory.Item{State: factory.StateDismissed, DoneAt: at}, "finished", func(j api.Job) time.Time { return j.DoneAt }},
		{"closed locally", factory.Item{State: factory.StateNew}, factory.Item{State: factory.StateNew, DoneHow: "closed_locally", DoneAt: at, Manual: true}, "finished", func(j api.Job) time.Time { return j.DoneAt }},
		{"failed", factory.Item{State: factory.StateRunning}, factory.Item{State: factory.StateNew, Failure: &factory.Failure{At: at, Code: factory.CodeToolError, Step: "check"}}, "failed", func(j api.Job) time.Time { return j.Failure.At }},
		{"runner lost", factory.Item{State: factory.StateRunning}, factory.Item{State: factory.StateRunning, Failure: &factory.Failure{At: at, Code: factory.CodeMachineLost, Step: "plan", Next: factory.FailureStopped}}, "failed", func(j api.Job) time.Time { return j.Failure.At }},
		{"question", factory.Item{State: factory.StateRunning}, factory.Item{State: factory.StateNeedsYou, Asking: &factory.Asked{At: at, With: factory.AskedYou, Question: "Which file?"}}, "needs_you", func(j api.Job) time.Time { return j.NeedsYou.Since }},
		{"need changed", factory.Item{State: factory.StateNeedsYou, QKind: factory.QKindApprove, Asking: &factory.Asked{At: at, With: factory.AskedYou}}, factory.Item{State: factory.StateNeedsYou, QKind: factory.QKindAskFirst, Asking: &factory.Asked{At: at, With: factory.AskedYou}}, "needs_you", func(j api.Job) time.Time { return j.NeedsYou.Since }},
		{"since changed", factory.Item{State: factory.StateNeedsYou, Asking: &factory.Asked{At: at.Add(-time.Second), With: factory.AskedYou}}, factory.Item{State: factory.StateNeedsYou, Asking: &factory.Asked{At: at, With: factory.AskedYou}}, "needs_you", func(j api.Job) time.Time { return j.NeedsYou.Since }},
		{"reason changed", factory.Item{State: factory.StateNeedsYou, Asking: &factory.Asked{At: at, With: factory.AskedYou}}, factory.Item{State: factory.StateNeedsYou, Asking: &factory.Asked{At: at, With: factory.AskedYou}, Merging: &factory.MergeRun{State: factory.MergeFailed}}, "needs_you", func(j api.Job) time.Time { return j.NeedsYou.Since }},
		{"failure repeated", factory.Item{State: factory.StateNew, Failure: &factory.Failure{At: at.Add(-time.Second)}}, factory.Item{State: factory.StateNew, Failure: &factory.Failure{At: at, Code: factory.CodeToolError, Step: "check"}}, "failed", func(j api.Job) time.Time { return j.Failure.At }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			it, err := st.Create(tt.before)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Update(it.ID, func(cur *factory.Item) error {
				id := cur.ID
				*cur = tt.after
				cur.ID, cur.Title = id, "A job"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			stored, err := st.Get(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			zero := int64(0)
			got, err := live.ReadOutcomes(st.Root(), &zero, 100)
			if err != nil || got.Reset || len(got.Events) != 1 {
				t.Fatalf("outcomes: %+v %v", got, err)
			}
			e, j := got.Events[0], api.FromItem(stored)
			actual, _ := json.Marshal(e.At)
			want, _ := json.Marshal(tt.fact(j))
			if string(actual) != string(want) || e.Kind != tt.kind || e.Job != it.ID || e.Title != "A job" {
				t.Fatalf("outcome %+v: at %s, want %s", e, actual, want)
			}
			switch e.Kind {
			case "merged":
				if e.PRNumber != 42 || e.PRURL != tt.after.PullRequest.URL {
					t.Fatalf("merged receipt: %+v", e)
				}
			case "finished":
				if e.How != j.DoneHow {
					t.Fatalf("finished how: %+v", e)
				}
			case "failed":
				if e.Code != j.Failure.Code || e.Step != j.Failure.Step {
					t.Fatalf("failure: %+v", e)
				}
			case "needs_you":
				if e.Need != j.NeedsYou.Kind || e.Reason != j.Reason {
					t.Fatalf("need: %+v", e)
				}
				waiting, _ := json.Marshal(j.WaitingSince)
				if string(actual) != string(waiting) {
					t.Fatalf("needs-you time %s differs from waiting_since %s", actual, waiting)
				}
			}
			if err := st.Update(it.ID, func(cur *factory.Item) error { cur.Title = "Renamed"; return nil }); err != nil {
				t.Fatal(err)
			}
			cursor := got.Cursor
			quiet, err := live.ReadOutcomes(st.Root(), &cursor, 100)
			if err != nil || len(quiet.Events) != 0 {
				t.Fatalf("unchanged facts: %+v %v", quiet, err)
			}
		})
	}
}

// E1, E3: creates are silent; Save updates persist across a reopened store.
func TestOutcomeCreateIsSilentAndSaveSurvivesReopen(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range []factory.Item{{State: factory.StateShipped}, {State: factory.StateNeedsYou, Question: "Which file?"}} {
		if _, err := st.Create(it); err != nil {
			t.Fatal(err)
		}
	}
	it, err := st.Create(factory.Item{State: factory.StateRunning})
	if err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	initial, err := live.ReadOutcomes(st.Root(), &zero, 100)
	if err != nil || initial.Reset || initial.Cursor != 0 || len(initial.Events) != 0 {
		t.Fatalf("create outcomes: %+v %v", initial, err)
	}
	it.State, it.DoneAt = factory.StateShipped, time.Now().UTC()
	if err := st.Save(it); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := live.ReadOutcomes(reopened.Root(), &zero, 100)
	if err != nil || got.Reset || len(got.Events) != 1 || got.Events[0].How != "answered" {
		t.Fatalf("reopened: %+v %v", got, err)
	}
}

// runningPlan is a job whose plan step runs, with the default pipeline's
// approve gate after it: the shape a run has just before it waits on a person.
func runningPlan(at time.Time) factory.Item {
	return factory.Item{
		State: factory.StateRunning, Title: "A waiting job",
		Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1, At: at.Add(-time.Minute)},
			{Name: "approve", Kind: factory.StageGate, State: factory.PhasePending},
			{Name: "write", Kind: factory.StageChat, State: factory.PhasePending},
			{Name: "proof", Kind: factory.StageChat, State: factory.PhasePending},
		}},
	}
}

// atPlanGate moves a [runningPlan] job to wait at its approve gate, the plan
// done and the person asked, as a run leaves it when its plan finishes.
func atPlanGate(it *factory.Item, at time.Time) {
	it.Stream.Phases[0].State = factory.PhaseDone
	it.Stream.Cur, it.Stream.Phases[1].State = 1, factory.PhaseWaiting
	it.State, it.QKind, it.Question = factory.StateNeedsYou, factory.QKindApprove, "plan is ready · continue, or send it back?"
	it.Asking = &factory.Asked{Stage: "approve", Question: it.Question, At: at, With: factory.AskedYou}
}

// E1: one wait on a person is one needs-you outcome, however many writes the
// job takes while it waits: its runner's reports, its priority, its budget, a
// rename. A wait at the plan gate recorded another on every write (hand test,
// 2026-11-12: six identical approve_plan outcomes for one approval).
func TestStoredNeedsYouOncePerWait(t *testing.T) {
	at := time.Date(2026, 11, 12, 17, 29, 26, 345727000, time.FixedZone("offset", -5*3600))
	ask := func(it *factory.Item, stage, question string, options ...string) {
		it.State, it.Question = factory.StateNeedsYou, question
		it.Asking = &factory.Asked{Stage: stage, Question: question, Options: options, At: at, With: factory.AskedYou}
	}
	planDone := func(it *factory.Item) {
		it.Stream.Phases[0].State = factory.PhaseDone
	}
	tests := []struct {
		name         string
		wait         func(*factory.Item)
		need, reason string
	}{
		{"plan gate", func(it *factory.Item) { atPlanGate(it, at) }, factory.NeedsApprovePlan, "approval_plan"},
		{"hold before a step", func(it *factory.Item) {
			planDone(it)
			it.Stream.Phases[1].State = factory.PhaseDone
			it.Stream.Cur, it.Stream.Phases[2].State = 2, factory.PhaseWaiting
			ask(it, "write", "write is next · carry on?")
			it.QKind = factory.QKindApprove
		}, factory.NeedsApproveStep, "approval_step"},
		{"proof", func(it *factory.Item) {
			for i := range it.Stream.Phases {
				it.Stream.Phases[i].State = factory.PhaseDone
			}
			it.State, it.Stream.Cur, it.Stream.Ended = factory.StateLanded, 3, at
		}, factory.NeedsApproveProof, "approval_proof"},
		{"question", func(it *factory.Item) {
			ask(it, "plan", "Which file?", "a.go", "b.go")
			it.QKind = factory.QKindStep
		}, factory.NeedsQuestion, "question"},
		{"paused decision", func(it *factory.Item) {
			ask(it, "plan", "Which file?", "a.go", "b.go")
			it.QKind = factory.QKindStep
			it.PausedRecoveryQuestion = "decision:7"
		}, factory.NeedsQuestion, "decision_paused"},
	}
	writes := []struct {
		name   string
		change func(*factory.Item)
	}{
		{"priority", func(it *factory.Item) { it.Priority = &factory.JobPriority{Value: "high", SetBy: "sam"} }},
		{"budget", func(it *factory.Item) { it.Cap = 10 }},
		{"rename", func(it *factory.Item) { it.Title = "Renamed while waiting" }},
		{"runner report", func(it *factory.Item) { it.Stream.Phases[0].Note = "Plan clarified" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			it, err := st.Create(runningPlan(at))
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Update(it.ID, func(cur *factory.Item) error { tt.wait(cur); return nil }); err != nil {
				t.Fatal(err)
			}
			check := func(after string) {
				t.Helper()
				stored, err := st.Get(it.ID)
				if err != nil {
					t.Fatal(err)
				}
				zero := int64(0)
				got, err := live.ReadOutcomes(st.Root(), &zero, 100)
				if err != nil || got.Reset || len(got.Events) != 1 {
					t.Fatalf("after %s: %d outcomes, want 1: %+v %v", after, len(got.Events), got.Events, err)
				}
				e := got.Events[0]
				if e.Kind != "needs_you" || e.Need != tt.need || e.Reason != tt.reason || !e.At.Equal(at) || !e.At.Equal(factory.NeedsSince(stored)) || factory.NeedsKind(stored) != tt.need {
					t.Fatalf("after %s: outcome %+v, want %s/%s at %s", after, e, tt.need, tt.reason, at)
				}
			}
			check("the wait began")
			for _, w := range writes {
				if err := st.Update(it.ID, func(cur *factory.Item) error { w.change(cur); return nil }); err != nil {
					t.Fatal(err)
				}
				check(w.name)
			}
		})
	}
}

// E1: a person's stop is no outcome, on every road that writes one. Each stop
// that cuts a step (a wait at a gate, a running step, a step's own Stop) puts
// the person's stop on the job as a failure coded stopped, and that write
// recorded a failed outcome (hand test, 2026-11-12: factory.job_stop at the
// plan gate recorded failed, code stopped, step approve), which the desktop
// app takes as a failed job to wake its agent for. The job waits in Backlog.
func TestStoredPersonStopIsNoOutcome(t *testing.T) {
	at := time.Date(2026, 11, 12, 17, 29, 26, 345727000, time.FixedZone("offset", -5*3600))
	now := at.Add(2 * time.Minute)
	// The stop write of the Factory's Stop door (lease.Doors.Stop and StopAs,
	// which factory.job_stop and `codeaf factory stop` reach) and of a stop
	// with no run to cancel here.
	stopDoor := func(it *factory.Item) error {
		if err := run.StopItem(it, now); err != nil {
			return err
		}
		factory.MarkPersonStopped(it)
		it.Asking = nil
		return nil
	}
	tests := []struct {
		name  string
		setup func(*factory.Item)
		stop  []func(*factory.Item) error
		step  string
	}{
		{"stop at the plan gate", func(it *factory.Item) { atPlanGate(it, at) }, []func(*factory.Item) error{stopDoor}, "approve"},
		{"stop during a running step", func(*factory.Item) {}, []func(*factory.Item) error{stopDoor}, "plan"},
		{
			// Runner.Stop of a job its loop holds: the stop is recorded, then
			// the cancelled loop ends the run ([run.StopItem] writes the same end).
			"stop with a run to cancel", func(it *factory.Item) { atPlanGate(it, at) },
			[]func(*factory.Item) error{
				func(it *factory.Item) error { factory.MarkPersonStopped(it); it.Asking = nil; return nil },
				func(it *factory.Item) error { return run.StopItem(it, now) },
			}, "approve",
		},
		{"a step's own Stop", func(*factory.Item) {}, []func(*factory.Item) error{
			func(it *factory.Item) error { return run.StepControl(it, "plan", false, "", now) },
		}, "plan"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			initial := runningPlan(at)
			tt.setup(&initial)
			// A created job records nothing, so every outcome below is the stop's.
			it, err := st.Create(initial)
			if err != nil {
				t.Fatal(err)
			}
			for _, stop := range tt.stop {
				if err := st.Update(it.ID, stop); err != nil {
					t.Fatal(err)
				}
			}
			stored, err := st.Get(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			if f := stored.Failure; f == nil || f.Code != factory.CodeStopped || f.Step != tt.step || api.FromItem(stored).State != "backlog" {
				t.Fatalf("the stop's own write: failure %+v, state %s", stored.Failure, api.FromItem(stored).State)
			}
			zero := int64(0)
			got, err := live.ReadOutcomes(st.Root(), &zero, 100)
			if err != nil || got.Reset || len(got.Events) != 0 || got.Cursor != 0 {
				t.Fatalf("a person's stop recorded outcomes: %+v %v", got.Events, err)
			}
		})
	}
}

// E1: one failed publish is one needs-you outcome. On a Factory service the
// runner reports the failure while the service's copy still runs the publish,
// and its done then lands the job; the wait read the step's clock at the
// report and the landed run's at the done, and recorded publish_failed twice
// (hand test, 2026-11-12: 17:38:54.849 and 17:38:55.360 for one refusal).
func TestStoredPublishFailureOnce(t *testing.T) {
	landed := time.Date(2026, 11, 12, 17, 38, 55, 360679000, time.FixedZone("offset", -5*3600))
	checked, approved, refused := landed.Add(-511*time.Millisecond), landed.Add(100*time.Millisecond), landed.Add(258*time.Millisecond)
	head := "246897c8a6328a3a78276e32287415f58278612e"
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{
		State: factory.StateLanded, Title: "Check the readme exists", HeadCommit: head,
		Proof:  []factory.Claim{{Text: "README.md exists", OK: true, Evidence: "test -f README.md", Medium: "test"}},
		Stream: &factory.Stream{Cur: 0, Ended: landed, Phases: []factory.Phase{{Name: "verify", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1, At: checked}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	writes := []struct {
		name   string
		change func(*factory.Item) error
	}{
		// The approval queues the publish, and a runner takes it.
		{"approve", func(cur *factory.Item) error { return run.PublishItem(cur, false, approved) }},
		{"pull", func(cur *factory.Item) error { cur.State, cur.Publishing = factory.StateRunning, true; return nil }},
		// The runner's report, applied as the service applies every report.
		{"report", func(cur *factory.Item) error {
			return run.ApplyReport(cur, factory.RunReport{Publishing: &factory.Publishing{State: "failed", Code: "no_push_permission", Detail: "publishing permission requires the assignee's GitHub connection on this runner", Attempt: 1, At: refused}}, refused)
		}},
		// Its done: the service lands a job whose publish failed, and the
		// completed lease ends its publishing.
		{"done", func(cur *factory.Item) error { cur.State, cur.Publishing = factory.StateLanded, false; return nil }},
		{"priority", func(cur *factory.Item) error { cur.Priority = &factory.JobPriority{Value: "low"}; return nil }},
	}
	for _, w := range writes {
		if err := st.Update(it.ID, w.change); err != nil {
			t.Fatalf("%s: %v", w.name, err)
		}
	}
	stored, err := st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	j := api.FromItem(stored)
	if j.State != "needs_you" || j.NeedsYou == nil || j.NeedsYou.Kind != factory.NeedsPublishFailed {
		t.Fatalf("the refused publish: %+v", j.NeedsYou)
	}
	zero := int64(0)
	got, err := live.ReadOutcomes(st.Root(), &zero, 100)
	if err != nil || got.Reset || len(got.Events) != 1 {
		t.Fatalf("one refused publish recorded %d outcomes: %+v %v", len(got.Events), got.Events, err)
	}
	e := got.Events[0]
	if e.Kind != "needs_you" || e.Need != factory.NeedsPublishFailed || e.Reason != "publish_failed" || !e.At.Equal(j.NeedsYou.Since) {
		t.Fatalf("outcome %+v, job waits since %s", e, j.NeedsYou.Since)
	}
}

// E1: in-place nested edits retain the prior need, reason and wait timestamp.
func TestStoredOutcomesFromInPlaceNestedUpdates(t *testing.T) {
	at := time.Date(2026, 11, 1, 12, 3, 4, 567, time.FixedZone("offset", 3600))
	later := at.Add(time.Minute)
	tests := []struct {
		name   string
		setup  func(*factory.Item)
		change func(*factory.Item)
		reason string
		since  time.Time
		note   string
	}{
		{
			name:   "current phase time",
			setup:  func(*factory.Item) {},
			change: func(it *factory.Item) { it.Stream.Phases[it.Stream.Cur].At = later },
			reason: "approval_plan", since: later,
		},
		{
			name:   "merge reason",
			setup:  func(it *factory.Item) { it.Merging = &factory.MergeRun{State: factory.MergeQueued} },
			change: func(it *factory.Item) { it.Merging.State = factory.MergeFailed },
			reason: "merge_failed", since: at,
		},
		{
			name: "follow-up enters needs-you",
			setup: func(it *factory.Item) {
				it.State = factory.StateShipped
				it.PullRequest = &factory.PullRequest{State: factory.PROpen}
				it.FollowUp = &factory.PRFollowUp{State: "watching"}
			},
			change: func(it *factory.Item) { it.FollowUp.State = "needs_you" },
			reason: "approval_plan", since: at,
		},
		{
			// A job a person stopped waits in Backlog and asks nothing of
			// anyone ([factory.JobState]), so the stop is no outcome.
			name: "explicit stop is quiet",
			setup: func(it *factory.Item) {
				it.State = factory.StateNew
				stopped := false
				it.PersonStopped = &stopped
			},
			change: func(it *factory.Item) { *it.PersonStopped = true },
		},
		{
			name: "phase stop is quiet",
			setup: func(it *factory.Item) {
				it.State, it.Stream.Ended = factory.StateNew, at
				it.Stream.Phases[0].State = factory.PhaseFailed
				it.Stream.Phases[0].Failure = &factory.Failure{Code: factory.CodeToolError}
			},
			change: func(it *factory.Item) { it.Stream.Phases[it.Stream.Cur].Failure.Code = factory.CodeStopped },
		},
		{
			name: "publish enters needs-you",
			setup: func(it *factory.Item) {
				it.State = factory.StateRunning
				it.PublishStatus = &factory.Publishing{State: "pushing"}
			},
			change: func(it *factory.Item) { it.PublishStatus.State = "failed" },
			reason: "publish_failed", since: at,
		},
		{
			name:   "phase note is quiet",
			setup:  func(*factory.Item) {},
			change: func(it *factory.Item) { it.Stream.Phases[it.Stream.Cur].Note = "Plan clarified" },
			note:   "Plan clarified",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			initial := factory.Item{
				State: factory.StateNeedsYou, QKind: factory.QKindApprove, Title: "A nested edit",
				Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "plan", At: at}}},
			}
			tt.setup(&initial)
			it, err := st.Create(initial)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Update(it.ID, func(cur *factory.Item) error { tt.change(cur); return nil }); err != nil {
				t.Fatal(err)
			}
			stored, err := st.Get(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			zero := int64(0)
			got, err := live.ReadOutcomes(st.Root(), &zero, 100)
			if err != nil || got.Reset || got.More {
				t.Fatalf("outcomes: %+v %v", got, err)
			}
			if tt.reason == "" {
				if len(got.Events) != 0 || got.Cursor != 0 || stored.Stream.Phases[0].Note != tt.note || (tt.note == "" && api.FromItem(stored).State != "backlog") {
					t.Fatalf("quiet nested edit: %+v, phase %+v", got, stored.Stream.Phases[0])
				}
				return
			}
			if len(got.Events) != 1 || got.Cursor != 1 {
				t.Fatalf("outcomes: %+v", got)
			}
			e, j := got.Events[0], api.FromItem(stored)
			if j.State != "needs_you" || j.NeedsYou == nil || !j.NeedsYou.Since.Equal(tt.since) || j.Reason != tt.reason {
				t.Fatalf("stored job facts: %+v", j)
			}
			actual, _ := json.Marshal(e.At)
			want, _ := json.Marshal(j.NeedsYou.Since)
			if e.Kind != "needs_you" || e.Job != it.ID || e.Title != stored.Title || e.Need != j.NeedsYou.Kind || e.Reason != j.Reason || string(actual) != string(want) {
				t.Fatalf("outcome %+v: at %s, want since %s", e, actual, want)
			}
		})
	}
}
