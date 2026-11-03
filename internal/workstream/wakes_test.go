package workstream

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

func workstreamWakeForTest(t *testing.T, s *Store, w Workstream, key string, items ...WakeProposal) WakeRun {
	t.Helper()
	ctx := context.Background()
	run, woke, err := s.BeginWake(ctx, w.ID, key, WakeEvent{Kind: "merged", Factory: "f", Job: 7, At: s.now(), Label: "api#7 merged"})
	if err != nil || !woke {
		t.Fatalf("begin: %+v %v %v", run, woke, err)
	}
	run, c, err := s.SettleWake(ctx, run.ID, WakeAnswer{Summary: "Review next steps.", Items: items}, nil, "")
	if err != nil || c.ID != w.ID {
		t.Fatalf("settle: %+v %+v %v", run, c, err)
	}
	return run
}
func workstreamWakeMember() workspace.Ref {
	return workspace.Ref{Kind: workspace.FactoryJobKind, ID: "7", Factory: "f"}
}
func workstreamWakeBudget(n float64) *float64 { return &n }

func TestWakeC_ProposeChangesNothing(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Proposals", workstreamWakeMember())
	before, _ := s.Plan(ctx, w.ID)
	pages, _ := s.Pages(ctx, w.ID)
	run := workstreamWakeForTest(t, s, w, "c", WakeProposal{Kind: "move", Factory: "f", Job: 7, ToWave: 2}, WakeProposal{Kind: "draft", Title: "Follow up", BudgetUSD: workstreamWakeBudget(3)}, WakeProposal{Kind: "page", Title: "Where it stands", Markdown: "Review"}, WakeProposal{Kind: "decision", Text: "Review first"})
	if len(run.Items) != 4 {
		t.Fatal(run)
	}
	for _, i := range run.Items {
		if i.State != "proposed" {
			t.Fatal(i)
		}
	}
	after, _ := s.Plan(ctx, w.ID)
	afterPages, _ := s.Pages(ctx, w.ID)
	records, _ := s.Records(ctx, w.ID, "", "")
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(pages, afterPages) || len(records) != 0 {
		t.Fatal("propose changed workstream content")
	}
}

func TestWakeD_SmallClassificationAndUndo(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Small", workstreamWakeMember())
	small := "small"
	s.Update(ctx, w.ID, Patch{Autonomy: &small})
	page, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Where it stands", Owner: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	waves := []Wave{{N: 1, Items: []PlanItem{{Kind: "draft", DraftID: "d_before", Title: "Before"}, {Kind: "job", Factory: "f", Job: 7}, {Kind: "draft", DraftID: "d_after", Title: "After"}}}, {N: 2, Items: []PlanItem{}}, {N: 3, AutoStart: true, Items: []PlanItem{}}, {N: 4, Items: []PlanItem{}}}
	before, _, err := s.SetPlan(ctx, w.ID, 0, nil, waves, Attribution{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GrantGoAhead(ctx, GoAheadGrant{WorkstreamID: w.ID, Scope: "wave", Wave: 4, CapUSD: 20, EndsAt: s.now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	run := workstreamWakeForTest(t, s, w, "d",
		WakeProposal{Kind: "move", Factory: "f", Job: 7, ToWave: 2},
		WakeProposal{Kind: "draft", Title: "Small follow-up", BudgetUSD: workstreamWakeBudget(3), Wave: 2},
		WakeProposal{Kind: "page", PageID: page.ID, Markdown: "Updated"},
		WakeProposal{Kind: "page", Title: "Agent notes", Markdown: "New"},
		WakeProposal{Kind: "draft", Title: "Large follow-up", BudgetUSD: workstreamWakeBudget(7), Wave: 2},
		WakeProposal{Kind: "draft", Title: "Unknown budget", Wave: 2},
		WakeProposal{Kind: "move", DraftID: "d_before", ToWave: 3},
		WakeProposal{Kind: "move", DraftID: "d_after", ToWave: 4},
		WakeProposal{Kind: "decision", Text: "Keep the scope"},
		WakeProposal{Kind: "question", Text: "Ready?"})
	for i, item := range run.Items {
		want := "proposed"
		if i < 4 {
			want = "applied"
		}
		if item.State != want {
			t.Fatalf("%d: %+v", i, item)
		}
	}
	after, _ := s.Plan(ctx, w.ID)
	if after.Waves[1].Items[1].Kind != "draft" || after.Waves[1].Items[1].Source.Kind != "factory_job" {
		t.Fatal(after)
	}
	undone, c, err := s.ActOnWake(ctx, run.ID, "", "undo")
	if err != nil || c.ID != w.ID {
		t.Fatalf("undo: %+v %v", c, err)
	}
	restored, _ := s.Plan(ctx, w.ID)
	restoredPage, _ := s.Page(ctx, page.ID, false)
	pages, _ := s.Pages(ctx, w.ID)
	if !reflect.DeepEqual(restored.Waves, before.Waves) || string(restoredPage.Doc) != emptyDoc || restoredPage.Version != 3 || len(pages) != 2 {
		t.Fatalf("restore: %+v %+v %+v", restored, restoredPage, pages)
	}
	for i := 0; i < 4; i++ {
		if undone.Items[i].State != "undone" {
			t.Fatal(undone.Items)
		}
	}
	if _, c, err := s.ActOnWake(ctx, run.ID, "", "undo"); err != nil || c.ID != "" {
		t.Fatalf("repeat undo: %+v %v", c, err)
	}
}

func TestWakeD_LaterPageAndPlanChangesRefuseUndo(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Stale", workstreamWakeMember())
	small := "small"
	s.Update(ctx, w.ID, Patch{Autonomy: &small})
	p, _, _ := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Agent notes", Owner: "agent"})
	s.SetPlan(ctx, w.ID, 0, nil, []Wave{{N: 1, Items: []PlanItem{{Kind: "job", Factory: "f", Job: 7}}}, {N: 2, Items: []PlanItem{}}, {N: 3, Items: []PlanItem{}}}, Attribution{})
	run := workstreamWakeForTest(t, s, w, "stale", WakeProposal{Kind: "page", PageID: p.ID, Markdown: "Wake"}, WakeProposal{Kind: "move", Factory: "f", Job: 7, ToWave: 2})
	s.SavePage(ctx, p.ID, 2, []byte(emptyDoc), Attribution{})
	plan, _ := s.Plan(ctx, w.ID)
	plan.Waves[2].Items = plan.Waves[1].Items
	plan.Waves[1].Items = []PlanItem{}
	s.SetPlan(ctx, w.ID, plan.Version, plan.Milestones, plan.Waves, Attribution{})
	for _, item := range run.Items {
		if _, c, err := s.ActOnWake(ctx, run.ID, item.ID, "undo"); !errors.Is(err, ErrConflict) || c.ID != "" {
			t.Fatalf("single stale: %+v %v", c, err)
		}
	}
	noted, _, err := s.ActOnWake(ctx, run.ID, "", "undo")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range noted.Items {
		if item.State != "applied" || item.Note != "Changed since; not undone." {
			t.Fatal(item)
		}
	}
	_, c, err := s.ActOnWake(ctx, run.ID, "", "undo")
	if err != nil || c.ID != "" {
		t.Fatalf("same notes wrote again: %+v %v", c, err)
	}
}

func TestWakeE_PersonAndDeletedPagesAreDropped(t *testing.T) {
	for _, autonomy := range []string{"propose", "small"} {
		t.Run(autonomy, func(t *testing.T) {
			s, _, _ := testStore(t)
			ctx := context.Background()
			w := createStream(t, s, "Private", workstreamWakeMember())
			s.Update(ctx, w.ID, Patch{Autonomy: &autonomy})
			pages, _ := s.Pages(ctx, w.ID)
			person, _ := s.Page(ctx, pages[0].ID, false)
			deleted, _, _ := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Deleted", Owner: "agent"})
			s.DeletePage(ctx, deleted.ID)
			run := workstreamWakeForTest(t, s, w, "e", WakeProposal{Kind: "page", PageID: person.ID, Markdown: "Changed"}, WakeProposal{Kind: "page", PageID: deleted.ID, Markdown: "Changed"})
			after, _ := s.Page(ctx, person.ID, false)
			if len(run.Items) != 0 || !reflect.DeepEqual(person, after) {
				t.Fatal("page boundary violated")
			}
			if _, c, err := s.ActOnWake(ctx, run.ID, "", "apply"); err != nil || c.ID != "" {
				t.Fatalf("empty apply: %+v %v", c, err)
			}
		})
	}
}

func TestWakeG_ActionsAreAtomicAndIdempotent(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Actions", workstreamWakeMember())
	run := workstreamWakeForTest(t, s, w, "g", WakeProposal{Kind: "decision", Text: "Keep it"}, WakeProposal{Kind: "question", Text: "Ready?"}, WakeProposal{Kind: "move", DraftID: "gone", ToWave: 9})
	applied, c, err := s.ActOnWake(ctx, run.ID, run.Items[0].ID, "apply")
	if err != nil || c.ID != w.ID || applied.Items[0].RecordID == "" {
		t.Fatalf("apply: %+v %v", applied, err)
	}
	row, _ := s.Get(ctx, w.ID)
	again, c, err := s.ActOnWake(ctx, run.ID, run.Items[0].ID, "apply")
	after, _ := s.Get(ctx, w.ID)
	if err != nil || c.ID != "" || !reflect.DeepEqual(again, applied) || !row.UpdatedAt.Equal(after.UpdatedAt) {
		t.Fatal("repeated apply changed state")
	}
	all, _, err := s.ActOnWake(ctx, run.ID, "", "apply")
	if err != nil || all.Items[1].State != "applied" || all.Items[2].Note != "Its target is gone; not applied." {
		t.Fatalf("all: %+v %v", all, err)
	}
	dismissed, _, err := s.ActOnWake(ctx, run.ID, "", "not_now")
	if err != nil || dismissed.DismissedAt.IsZero() || dismissed.Items[2].State != "dismissed" {
		t.Fatal(dismissed, err)
	}
	if _, c, err := s.ActOnWake(ctx, run.ID, "", "not_now"); err != nil || c.ID != "" {
		t.Fatal("repeat dismissal changed state")
	}
	if _, _, err := s.ActOnWake(ctx, "unknown", "", "apply"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, err := s.ActOnWake(ctx, run.ID, "unknown", "apply"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, err := s.ActOnWake(ctx, run.ID, "", "undo"); err != nil {
		t.Fatal(err)
	}
	records, _ := s.Records(ctx, w.ID, "", "")
	for _, r := range records {
		if r.State != "dropped" {
			t.Fatal(r)
		}
	}
	s.MarkDone(ctx, w.ID, "")
	for _, action := range []string{"apply", "undo"} {
		if _, _, err := s.ActOnWake(ctx, run.ID, "", action); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
}

func TestWakeB_DeduplicationCapsAndAbandonment(t *testing.T) {
	s, _, now := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Events", workstreamWakeMember())
	event := WakeEvent{Kind: "finished", Factory: "f", Job: 7, At: *now}
	first, woke, err := s.BeginWake(ctx, "", "once", event)
	if err != nil || !woke {
		t.Fatal(err)
	}
	if _, woke, err := s.BeginWake(ctx, "", "once", event); err != nil || woke {
		t.Fatal("duplicate woke", err)
	}
	for i := 1; i < WakeWorkstreamDayMax; i++ {
		if _, woke, err := s.BeginWake(ctx, w.ID, fmt.Sprint(i), event); err != nil || !woke {
			t.Fatal(i, err)
		}
	}
	if _, woke, err := s.BeginWake(ctx, w.ID, "capped", event); !errors.Is(err, ErrWakeCap) || woke {
		t.Fatal("cap not enforced", err)
	}
	*now = now.Add(WakeAbandonedAfter + time.Millisecond)
	changes, err := s.ExpireWakes(ctx)
	if err != nil || len(changes) != 1 {
		t.Fatal(changes, err)
	}
	runs, _ := s.Wakes(ctx, w.ID, 100)
	if len(runs) != WakeWorkstreamDayMax {
		t.Fatal(len(runs))
	}
	for _, r := range runs {
		if r.State != "failed" || r.Error != WakeStoppedError || r.CostUSD != nil {
			t.Fatal(r)
		}
	}
	if run, c, err := s.SettleWake(ctx, first.ID, WakeAnswer{}, workstreamWakeBudget(.04), ""); err != nil || c.ID != "" || run.CostUSD != nil {
		t.Fatal("late worker overwrote expired wake")
	}
}

func TestWakeI_SchemaThreeWakeListIsReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "three.db")
	ws, err := workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := New(ws, Options{})
	w := createStream(t, s, "Earlier")
	if err := ws.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("DROP TABLE workstream_wake_items; DROP TABLE workstream_wakes; PRAGMA user_version=3")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ws.Close()
	before, _ := os.ReadFile(path)
	ws, err = workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s = New(ws, Options{})
	wakes, err := s.Wakes(context.Background(), w.ID, 20)
	if err != nil || len(wakes) != 0 {
		t.Fatal(wakes, err)
	}
	if changes, err := s.ExpireWakes(context.Background()); err != nil || len(changes) != 0 {
		t.Fatal(changes, err)
	}
	ws.Close()
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("wake read migrated schema 3")
	}
}

func TestWakeB_HomeCapAndLocalDayReset(t *testing.T) {
	s, _, now := testStore(t)
	ctx := context.Background()
	event := WakeEvent{Kind: "morning", At: *now}
	var last Workstream
	for i := 0; i < 9; i++ {
		last = createStream(t, s, fmt.Sprintf("Stream %d", i))
		s.Update(ctx, last.ID, Patch{Wake: &Wake{Morning: true}})
		n := WakeWorkstreamDayMax
		if i == 8 {
			n = 8
		}
		for j := 0; j < n; j++ {
			if _, woke, err := s.BeginWake(ctx, last.ID, fmt.Sprintf("%d/%d", i, j), event); err != nil || !woke {
				t.Fatal(i, j, err)
			}
		}
	}
	if _, woke, err := s.BeginWake(ctx, last.ID, "home-capped", event); !errors.Is(err, ErrWakeCap) || woke {
		t.Fatal("home cap ignored", err)
	}
	*now = now.In(time.Local).AddDate(0, 0, 1)
	event.At = *now
	if _, woke, err := s.BeginWake(ctx, last.ID, "next-day", event); err != nil || !woke {
		t.Fatal("cap did not reset", err)
	}
}

func TestWakeB_DuplicateAcrossStoreHandles(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "claims.db")
	a, err := workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	first := New(a, Options{})
	w := createStream(t, first, "Shared", workstreamWakeMember())
	b, err := workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	second := New(b, Options{})
	event := WakeEvent{Kind: "failed", Factory: "f", Job: 7, At: time.Now()}
	if _, woke, err := first.BeginWake(ctx, w.ID, "same", event); err != nil || !woke {
		t.Fatal(err)
	}
	if _, woke, err := second.BeginWake(ctx, w.ID, "same", event); err != nil || woke {
		t.Fatal("second handle won", err)
	}
	runs, err := second.Wakes(ctx, w.ID, 20)
	if err != nil || len(runs) != 1 {
		t.Fatal(runs, err)
	}
}

func TestWakeD_ExplicitApplyKeepsRecordsAndAgentSources(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Explicit", workstreamWakeMember())
	run := workstreamWakeForTest(t, s, w, "explicit", WakeProposal{Kind: "draft", Title: "Costly follow-up", BudgetUSD: workstreamWakeBudget(7)}, WakeProposal{Kind: "page", Title: "Spec", Markdown: "Keep these words"}, WakeProposal{Kind: "decision", Text: "The rollout is ready"})
	off := "off"
	s.Update(ctx, w.ID, Patch{Autonomy: &off})
	applied, _, err := s.ActOnWake(ctx, run.ID, "", "apply")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range applied.Items {
		if item.State != "applied" {
			t.Fatal(item)
		}
	}
	plan, _ := s.Plan(ctx, w.ID)
	if len(plan.Waves) != 1 || plan.Waves[0].AutoStart || plan.Waves[0].Items[0].Source.Kind != "factory_job" {
		t.Fatal(plan)
	}
	page, _ := s.Page(ctx, applied.Items[1].PageID, false)
	if page.Owner != "agent" || page.UpdatedBy != "agent" || page.UpdatedFrom.Factory != "f" || !page.ReadByJobs {
		t.Fatal(page)
	}
	r, err := s.Records(ctx, w.ID, "decision", "kept")
	if err != nil || len(r) != 1 || r[0].By != "agent" || r[0].Source.Kind != "factory_job" {
		t.Fatal(r, err)
	}
	if _, _, err := s.ActOnWake(ctx, run.ID, "", "undo"); err != nil {
		t.Fatal(err)
	}
	restored, _ := s.Plan(ctx, w.ID)
	if len(restored.Waves) != 0 {
		t.Fatal("created wave remained", restored)
	}
	deleted, err := s.Page(ctx, page.ID, false)
	if err != nil || deleted.DeletedAt.IsZero() {
		t.Fatal("new page was not restorably deleted", deleted, err)
	}
}

func TestWakeG_NamedApplyConflictRollsBackAndAllKeepsNote(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Gone", workstreamWakeMember())
	run := workstreamWakeForTest(t, s, w, "gone", WakeProposal{Kind: "move", DraftID: "missing", ToWave: 7})
	if _, _, err := s.ActOnWake(ctx, run.ID, run.Items[0].ID, "apply"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	runs, _ := s.Wakes(ctx, w.ID, 20)
	if !reflect.DeepEqual(runs[0], run) {
		t.Fatal("conflict wrote")
	}
	noted, _, err := s.ActOnWake(ctx, run.ID, "", "apply")
	if err != nil || noted.Items[0].State != "proposed" || noted.Items[0].Note != "Its target is gone; not applied." {
		t.Fatal(noted, err)
	}
}

func TestWakeE_DraftUndoCannotTargetAMemberJob(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Identities", workstreamWakeMember())
	if _, _, err := s.SetPlan(ctx, w.ID, 0, nil, []Wave{{N: 1, Items: []PlanItem{{Kind: "job", Factory: "f", Job: 7}}}}, Attribution{}); err != nil {
		t.Fatal(err)
	}
	run := workstreamWakeForTest(t, s, w, "identities", WakeProposal{Kind: "draft", Title: "Follow up", Factory: "f", Job: 7, DraftID: "untrusted", BudgetUSD: workstreamWakeBudget(3)})
	if _, _, err := s.ActOnWake(ctx, run.ID, "", "apply"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ActOnWake(ctx, run.ID, "", "undo"); err != nil {
		t.Fatal(err)
	}
	plan, _ := s.Plan(ctx, w.ID)
	if len(plan.Waves[0].Items) != 1 || plan.Waves[0].Items[0].Kind != "job" || plan.Waves[0].Items[0].Job != 7 {
		t.Fatal("undo touched member job", plan)
	}
}
