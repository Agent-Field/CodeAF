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
			name: "explicit stop enters needs-you",
			setup: func(it *factory.Item) {
				it.State = factory.StateNew
				stopped := false
				it.PersonStopped = &stopped
			},
			change: func(it *factory.Item) { *it.PersonStopped = true },
			reason: "approval_plan", since: at,
		},
		{
			name: "phase stop enters needs-you",
			setup: func(it *factory.Item) {
				it.State, it.Stream.Ended = factory.StateNew, at
				it.Stream.Phases[0].State = factory.PhaseFailed
				it.Stream.Phases[0].Failure = &factory.Failure{Code: factory.CodeToolError}
			},
			change: func(it *factory.Item) { it.Stream.Phases[it.Stream.Cur].Failure.Code = factory.CodeStopped },
			reason: "approval_plan", since: at,
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
				if len(got.Events) != 0 || got.Cursor != 0 || stored.Stream.Phases[0].Note != "Plan clarified" {
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
