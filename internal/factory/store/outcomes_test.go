package store_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
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
		{"failed", factory.Item{State: factory.StateRunning}, factory.Item{State: factory.StateNew, Failure: &factory.Failure{At: at, Code: factory.CodeToolError, Step: "check"}}, "failed", func(j api.Job) time.Time { return j.Failure.At }},
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
		{"plan gate", func(it *factory.Item) {
			planDone(it)
			it.Stream.Cur, it.Stream.Phases[1].State = 1, factory.PhaseWaiting
			ask(it, "approve", "plan is ready · continue, or send it back?")
			it.QKind = factory.QKindApprove
		}, factory.NeedsApprovePlan, "approval_plan"},
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
