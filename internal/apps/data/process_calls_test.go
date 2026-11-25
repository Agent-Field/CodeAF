package data

import (
	"strings"
	"testing"
	"time"
)

func TestChecklistGuardUsesDeclaredSteps(t *testing.T) {
	s, _ := openCRM(t)
	raw := strings.Replace(opsApp, `"records": {`, `"policies":[{"id":"checks","kind":"checklist","config":{"record":"case","steps":[{"id":"review","label":"Review"},{"id":"file","label":"File"}]}}],"records": {`, 1)
	raw = strings.Replace(raw, `amount > 0`, `checklist_done(\"checks\") && checklist_done() && !checklist_done(\"missing\")`, 1)
	if err := s.RegisterApp([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	out, err := s.Create(person, "case", []map[string]any{{"name": "Quarter close"}})
	if err != nil {
		t.Fatal(err)
	}
	id := out.Rows[0].ID()
	rev := int64(1)
	for _, tc := range []struct {
		step  string
		done  bool
		ready bool
	}{
		{ready: false},
		{step: "review", done: true, ready: false},
		{step: "file", done: true, ready: true},
		{step: "review", done: false, ready: false},
	} {
		if tc.step != "" {
			if _, err := s.ChecklistSet(person, "case", id, "checks", tc.step, tc.done, rev, nil); err != nil {
				t.Fatal(err)
			}
			rev++
		}
		transitions, err := s.Transitions(person, "case", id)
		if err != nil || len(transitions) == 0 || transitions[0].ID != "submit" || transitions[0].Ready != tc.ready {
			t.Fatalf("after %q = %v, transitions = %+v, %v", tc.step, tc.done, transitions, err)
		}
	}
}

func TestTransitionBatchChecksEveryRowBeforeCommit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		amount int
		rev    int64
		code   string
	}{
		{name: "guard blocks whole batch", amount: 0, rev: 1, code: CodeLifecycle},
		{name: "stale row blocks whole batch", amount: 20, rev: 2, code: CodeStale},
		{name: "moves both rows", amount: 20, rev: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := openOps(t)
			created, err := s.Create(person, "case", []map[string]any{
				{"name": "first", "amount": 10}, {"name": "second", "amount": tc.amount},
			})
			if err != nil {
				t.Fatal(err)
			}
			refs := []TransitionRecord{
				{RecordRef: RecordRef{Type: "case", ID: created.Rows[0].ID()}, Rev: 1},
				{RecordRef: RecordRef{Type: "case", ID: created.Rows[1].ID()}, Rev: tc.rev},
			}
			cursor, err := s.Cursor()
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.TransitionBatch(person, "submit", refs, TransitionOptions{Reason: "ready for review"})
			if tc.code != "" {
				wantCode(t, err, tc.code)
				events, err := s.Outbox(cursor, 10)
				if err != nil || len(events) != 0 {
					t.Fatalf("refused batch emitted events: %+v, %v", events, err)
				}
				first, err := s.Process(person, "case", refs[0].ID)
				if err != nil || first.State != "Open" || first.Rev != 1 {
					t.Fatalf("refused batch changed first record: %+v, %v", first, err)
				}
				return
			}
			if err != nil || result.Batch == "" || len(result.Rows) != 2 {
				t.Fatalf("transition batch = %+v, %v", result, err)
			}
			for _, ref := range refs {
				out, err := s.Process(person, ref.Type, ref.ID)
				if err != nil || out.State != "Review" || out.Rev != 2 || len(out.Timeline) != 1 || out.Timeline[0].Reason != "ready for review" {
					t.Fatalf("process after batch = %+v, %v", out, err)
				}
			}
			events, err := s.Outbox(cursor, 10)
			if err != nil || len(events) != 4 {
				t.Fatalf("batch events = %+v, %v", events, err)
			}
			for _, e := range events {
				if e.Batch != result.Batch {
					t.Fatalf("event batch = %q, want %q", e.Batch, result.Batch)
				}
			}
		})
	}
}

func TestTransitionBatchPublishesMixedTypesTogether(t *testing.T) {
	s, _ := openOps(t)
	task := `{"id":"tasks","records":{"task":{"fields":{"stage":{"type":"enum","values":["New","Review"]}},
		"lifecycle":{"field":"stage","transitions":[{"id":"submit","from":["New"],"to":"Review"}]}}}}`
	if err := s.RegisterApp([]byte(task)); err != nil {
		t.Fatal(err)
	}
	refs := []TransitionRecord{
		{RecordRef: RecordRef{Type: "case", ID: mustCreate(t, s, person, "case", map[string]any{"name": "Case", "amount": 20})[0].ID()}, Rev: 1},
		{RecordRef: RecordRef{Type: "task", ID: mustCreate(t, s, person, "task", map[string]any{})[0].ID()}, Rev: 1},
	}
	deltas := make(chan Diff, len(refs))
	for _, ref := range refs {
		if _, err := s.Subscribe(person, SubSpec{ID: ref.Type, Type: ref.Type}, func(d Diff) { deltas <- d }); err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.TransitionBatch(person, "submit", refs, TransitionOptions{})
	if err != nil || len(result.Rows) != 2 || result.Rows[0]["stage"] != "Review" || result.Rows[1]["stage"] != "Review" {
		t.Fatalf("batch = %+v, %v", result, err)
	}
	want := map[string]string{"case": refs[0].ID, "task": refs[1].ID}
	for range refs {
		select {
		case d := <-deltas:
			if len(d.Upserts) != 1 || d.Upserts[0].ID() != want[d.Sub] {
				t.Fatalf("diff = %+v", d)
			}
			delete(want, d.Sub)
		case <-time.After(2 * time.Second):
			t.Fatalf("missing transition changes: %v", want)
		}
	}
}

func TestMyWorkClaimCompleteAndReassign(t *testing.T) {
	s, _ := openOps(t)
	one, err := s.CreateWorkItem(person, WorkItem{Title: "Check", Assignee: "agent:reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateWorkItem(person, WorkItem{Title: "Someone else's work", Assignee: "another-person"}); err != nil {
		t.Fatal(err)
	}
	mine, err := s.MyWork(person)
	if err != nil || len(mine) != 1 || mine[0].ID != one.ID {
		t.Fatalf("my work = %+v, %v", mine, err)
	}
	_, err = s.ClaimWorkItem(person, one.ID)
	wantCode(t, err, CodeNotAllowed)
	if _, err := s.ReassignWorkItem(person, one.ID, ""); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimWorkItem(person, one.ID)
	if err != nil || claimed.Assignee != s.Person() {
		t.Fatalf("claim = %+v, %v", claimed, err)
	}
	done, err := s.CompleteWorkItem(person, one.ID, map[string]any{"ok": true})
	if err != nil || done.Status != WorkDone || done.Outcome.(map[string]any)["ok"] != true {
		t.Fatalf("complete = %+v, %v", done, err)
	}
	_, err = s.ReassignWorkItem(person, one.ID, "agent:reviewer")
	wantCode(t, err, CodeStale)
	mine, err = s.MyWork(person)
	if err != nil || len(mine) != 0 {
		t.Fatalf("my work after complete = %+v, %v", mine, err)
	}
}
