package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

func workstreamAgentFixture(t *testing.T) (*appBackend, *appRecorder, appwire.Workstream, session.WorkstreamDoor) {
	t.Helper()
	b, rec, _, _ := workstreamBackend(t)
	b.now = func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }
	w := workstreamCreateForTest(t, b, "Agent", workstreamJobRef("7"))
	if _, err := b.keepCreatedBy(w.AgentID, workstreamCreatedBy); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), w.AgentID, "transcript.jsonl")
	return b, rec, w, b.workstreamAgentDoor(file)
}

// Contract (a) binds the capability to its own conversation, even before creation.
func TestWorkstreamAgentDoorIsScopedAndResolvesLazily(t *testing.T) {
	b, _, _, person := workstreamBackend(t)
	file := filepath.Join(t.TempDir(), "workstream-agent-1", "transcript.jsonl")
	if b.workstreamAgentDoor(file) != nil || b.workstreamAgentDoor(filepath.Join(t.TempDir(), person.ID, "transcript.jsonl")) != nil {
		t.Fatal("a person's conversation has a workstream door")
	}
	if _, err := b.keepCreatedBy("workstream-agent-1", workstreamCreatedBy); err != nil {
		t.Fatal(err)
	}
	door := b.workstreamAgentDoor(file)
	if door == nil {
		t.Fatal("a workstream conversation needs a door before the row exists")
	}
	ctx := context.Background()
	if brief, ok := door.Brief(); ok || brief != (session.WorkstreamBrief{}) {
		t.Fatalf("early brief: %+v %v", brief, ok)
	}
	_, readErr := door.Read(ctx, session.WorkstreamReadAsk{What: "workstream"})
	_, proposeErr := door.Propose(ctx, session.WorkstreamProposal{Kind: "decision", Text: "Words"})
	_, planErr := door.Plan(ctx)
	_, setErr := door.SetPlan(ctx, 0, session.WorkstreamPlanState{})
	_, pageErr := door.WritePage(ctx, session.WorkstreamPageWrite{Title: "Notes", UpdatedFrom: "Discussion"})
	for _, err := range []error{readErr, proposeErr, planErr, setErr, pageErr} {
		if !errors.Is(err, session.ErrWorkstreamMissing) || err.Error() != "No workstream holds this conversation." {
			t.Fatalf("early tool: %v", err)
		}
	}
	w := workstreamCreateForTest(t, b, "Lazy")
	brief, ok := door.Brief()
	if !ok || brief.Name != w.Name {
		t.Fatalf("late resolution: %+v %v", brief, ok)
	}
	previous := appWorkstreamAgents.Swap(nil)
	t.Cleanup(func() { appWorkstreamAgents.Store(previous) })
	if appWorkstreamAgentDoor(file) != nil {
		t.Fatal("an engine outside the app has no backend")
	}
	appWorkstreamAgents.Store(b)
	if appWorkstreamAgentDoor(file) == nil {
		t.Fatal("the app engine must receive the door")
	}
}

// Contract (b) records proposals by the agent and never creates grants.
func TestWorkstreamAgentRecordsStayProposed(t *testing.T) {
	b, rec, w, door := workstreamAgentFixture(t)
	ctx := context.Background()
	for _, kind := range []string{"decision", "question"} {
		if _, err := door.Propose(ctx, session.WorkstreamProposal{Kind: kind, Text: "Keep this?"}); err != nil {
			t.Fatal(err)
		}
	}
	plan := session.WorkstreamPlanState{Milestones: []workstream.Milestone{{ID: "m1", Title: "Ship"}}, Waves: []workstream.Wave{{N: 2, Items: []workstream.PlanItem{}}}}
	if _, err := door.SetPlan(ctx, 0, plan); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		ask  session.WorkstreamGoAheadAsk
		text string
	}{
		{session.WorkstreamGoAheadAsk{Scope: "job", Factory: "f", Job: 7, CapUSD: 12.5, EndsOn: "2026-11-04", Why: "Review"}, "Review"},
		{session.WorkstreamGoAheadAsk{Scope: "wave", Wave: 2, CapUSD: 20, EndsOn: "2026-11-04", Why: "Read"}, "Read"},
		{session.WorkstreamGoAheadAsk{Scope: "milestone", MilestoneID: "m1", CapUSD: 30, EndsOn: "2026-11-04", Why: "Ship"}, "Ship"},
	}
	s, _ := b.workstreamStore(false)
	for _, tc := range cases {
		p, err := door.Propose(ctx, session.WorkstreamProposal{Kind: "question", Text: "untrusted", GoAhead: &tc.ask})
		if err != nil {
			t.Fatal(err)
		}
		rows, err := s.Records(ctx, w.ID, "goahead", "proposed")
		want := &workstream.GoAheadProposal{Scope: tc.ask.Scope, Factory: tc.ask.Factory, Job: tc.ask.Job, Wave: tc.ask.Wave, MilestoneID: tc.ask.MilestoneID, CapUSD: tc.ask.CapUSD, EndsAt: time.Date(2026, 11, 4, 23, 59, 59, 0, time.UTC)}
		if err != nil || len(rows) == 0 || rows[0].ID != p.ID || rows[0].Text != tc.text || !reflect.DeepEqual(rows[0].GoAhead, want) || rows[0].GoAheadID != "" {
			t.Fatalf("go-ahead proposal: %+v %v", rows, err)
		}
	}
	rows, err := s.Records(ctx, w.ID, "", "", true)
	if err != nil || len(rows) != 5 {
		t.Fatalf("records: %+v %v", rows, err)
	}
	for _, row := range rows {
		if row.State != "proposed" || row.By != "agent" || row.Source == nil || row.Source.Kind != "chat" || row.Source.ID != w.AgentID || !row.CreatedAt.Equal(b.now()) {
			t.Fatalf("proposal attribution: %+v", row)
		}
	}
	grants, err := s.GoAheads(ctx, w.ID, true)
	if err != nil || len(grants) != 0 {
		t.Fatalf("a proposal granted permission: %+v %v", grants, err)
	}
	if len(workstreamEvents(rec)) != 7 {
		t.Fatalf("each committed write needs one event: %+v", workstreamEvents(rec))
	}
}

// Contract (c) leaves membership, stage and Factory seams alone when saving drafts.
func TestWorkstreamAgentDraftPlanNeverTouchesFactory(t *testing.T) {
	b, _, w, door := workstreamAgentFixture(t)
	ctx := context.Background()
	b.workstreams.queue = func(context.Context, string) (api.Queue, error) {
		t.Fatal("plan write called the Factory queue")
		return api.Queue{}, nil
	}
	b.workstreams.job = func(context.Context, string, int) (api.Job, error) {
		t.Fatal("plan write called a Factory job")
		return api.Job{}, nil
	}
	s, _ := b.workstreamStore(false)
	before, _ := s.Get(ctx, w.ID)
	p, err := door.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.Waves = []workstream.Wave{{N: 1, AutoStart: false, Items: []workstream.PlanItem{{Kind: "draft", DraftID: "draft_fixed", Title: "Proposed"}}}}
	saved, err := door.SetPlan(ctx, p.Version, p)
	if err != nil || saved.Waves[0].Items[0].Kind != "draft" {
		t.Fatalf("draft plan: %+v %v", saved, err)
	}
	after, _ := s.Get(ctx, w.ID)
	if !reflect.DeepEqual(before.Members, after.Members) || after.Stage != "shaping" {
		t.Fatalf("drafts changed membership or stage: %+v", after)
	}
	if _, err := door.SetPlan(ctx, 0, p); !errors.Is(err, session.ErrWorkstreamVersion) {
		t.Fatalf("typed conflict: %v", err)
	}
}

// Contracts (d) and (e) cover receipts, attribution, history and refusal without events.
func TestWorkstreamAgentPagesAreOwnedAndMarked(t *testing.T) {
	b, rec, w, door := workstreamAgentFixture(t)
	ctx := context.Background()
	s, _ := b.workstreamStore(false)
	nodes, _ := s.Pages(ctx, w.ID)
	before, _ := s.Page(ctx, nodes[0].ID, false)
	history, _ := s.PageHistory(ctx, before.ID, 50)
	events := len(workstreamEvents(rec))
	_, err := door.WritePage(ctx, session.WorkstreamPageWrite{PageID: before.ID, Markdown: "Forbidden", UpdatedFrom: "Discussion"})
	after, _ := s.Page(ctx, before.ID, false)
	afterHistory, _ := s.PageHistory(ctx, before.ID, 50)
	if !errors.Is(err, session.ErrWorkstreamNotAgentPage) || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(history, afterHistory) || len(workstreamEvents(rec)) != events {
		t.Fatalf("person page refusal: %v", err)
	}
	ask := session.WorkstreamPageWrite{Title: "Notes", ParentID: before.ID, Markdown: "# Notes\n\n[live: jobs]\n", UpdatedFrom: "Discussion"}
	created, err := door.WritePage(ctx, ask)
	if err != nil || created.Version != 1 {
		t.Fatalf("create: %+v %v", created, err)
	}
	p, _ := s.Page(ctx, created.ID, true)
	if p.Owner != "agent" || p.ReadByJobs || p.ParentID != before.ID || p.UpdatedBy != "agent" || !p.UpdatedAt.Equal(b.now()) || p.UpdatedFrom.Label != "Discussion" || p.UpdatedFrom.ID != w.AgentID || *p.Markdown != ask.Markdown {
		t.Fatalf("marked page: %+v", p)
	}
	b.now = func() time.Time { return time.Date(2026, 11, 3, 13, 0, 0, 0, time.UTC) }
	ask = session.WorkstreamPageWrite{PageID: p.ID, Markdown: "Updated", UpdatedFrom: "Review", Source: &workstream.Source{Kind: "factory_job", Factory: "f", ID: "7", Label: "Replaced"}}
	updated, err := door.WritePage(ctx, ask)
	if err != nil || updated.Version != 2 {
		t.Fatalf("update: %+v %v", updated, err)
	}
	p, _ = s.Page(ctx, p.ID, false)
	history, _ = s.PageHistory(ctx, p.ID, 50)
	if p.UpdatedBy != "agent" || !p.UpdatedAt.Equal(b.now()) || p.UpdatedFrom.Label != "Review" || p.UpdatedFrom.Kind != "factory_job" || history[0].SavedBy != "agent" || !history[0].SavedAt.Equal(b.now()) || !reflect.DeepEqual(p.UpdatedFrom, history[0].From) || len(workstreamEvents(rec)) != events+2 {
		t.Fatalf("updated attribution: %+v %+v", p, history)
	}
	if wireCode(appWorkstreamError(workstream.ErrNotAgentPage)) != appwire.CodeBadParams {
		t.Fatal("the wire needs a bad_params ownership refusal")
	}
}

// Contract (a) scopes reads to the owner while preserving the app's wire shapes.
func TestWorkstreamAgentReadUsesWireShapesAndScopesPages(t *testing.T) {
	b, _, w, door := workstreamAgentFixture(t)
	ctx := context.Background()
	s, _ := b.workstreamStore(false)
	nodes, _ := s.Pages(ctx, w.ID)
	raw, err := door.Read(ctx, session.WorkstreamReadAsk{What: "page", PageID: nodes[0].ID})
	var page map[string]json.RawMessage
	if err != nil || json.Unmarshal(raw, &page) != nil || page["doc"] != nil || string(page["markdown"]) != `""` || page["id"] == nil {
		t.Fatalf("page read: %s %v", raw, err)
	}
	other := workstreamCreateForTest(t, b, "Other")
	otherPages, _ := s.Pages(ctx, other.ID)
	if _, err := door.Read(ctx, session.WorkstreamReadAsk{What: "page", PageID: otherPages[0].ID}); !errors.Is(err, workstream.ErrNotFound) {
		t.Fatalf("cross-workstream read: %v", err)
	}
	got, err := door.Read(ctx, session.WorkstreamReadAsk{What: "workstream"})
	result, _ := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
	want, _ := json.Marshal(result.Workstream)
	if err != nil || string(got) != string(want) {
		t.Fatalf("workstream wire: %s %s %v", got, want, err)
	}
	for _, part := range []string{"pages", "records", "plan", "goaheads"} {
		raw, err := door.Read(ctx, session.WorkstreamReadAsk{What: part})
		var object map[string]json.RawMessage
		if err != nil || json.Unmarshal(raw, &object) != nil || object[part] == nil {
			t.Fatalf("%s wire: %s %v", part, raw, err)
		}
	}
}

// Contracts (b), (d) and (g) refuse invalid sources and writes to a done workstream.
func TestWorkstreamAgentInvalidSourcesAndDoneWritesChangeNothing(t *testing.T) {
	b, rec, w, door := workstreamAgentFixture(t)
	ctx := context.Background()
	other := workstreamCreateForTest(t, b, "Other")
	s, _ := b.workstreamStore(false)
	pages, _ := s.Pages(ctx, other.ID)
	events := len(workstreamEvents(rec))
	for _, source := range []*workstream.Source{
		{Kind: "factory_job", Factory: "f", ID: "8"},
		{Kind: "factory_job", ID: "7"},
		{Kind: "factory_job", Factory: "f", ID: "07"},
		{Kind: "page", ID: pages[0].ID},
		{Kind: "url", ID: "file:///private"},
		{Kind: "url", ID: "https://"},
		{Kind: "chat", ID: "another-chat"},
	} {
		if _, err := door.Propose(ctx, session.WorkstreamProposal{Kind: "decision", Text: "Words", Source: source}); err == nil {
			t.Fatalf("accepted source %+v", source)
		}
		if _, err := door.WritePage(ctx, session.WorkstreamPageWrite{Title: "Notes", Markdown: "Words", UpdatedFrom: "Discussion", Source: source}); err == nil {
			t.Fatalf("accepted page source %+v", source)
		}
	}
	if len(workstreamEvents(rec)) != events {
		t.Fatal("a refusal emitted a change")
	}
	if _, _, err := s.MarkDone(ctx, w.ID, "Done"); err != nil {
		t.Fatal(err)
	}
	_, proposalErr := door.Propose(ctx, session.WorkstreamProposal{Kind: "decision", Text: "Words"})
	_, planErr := door.SetPlan(ctx, 0, session.WorkstreamPlanState{})
	_, pageErr := door.WritePage(ctx, session.WorkstreamPageWrite{Title: "Notes", Markdown: "Words", UpdatedFrom: "Discussion"})
	for _, err := range []error{proposalErr, planErr, pageErr} {
		if !errors.Is(err, session.ErrWorkstreamDone) {
			t.Fatalf("done write: %v", err)
		}
	}
	if len(workstreamEvents(rec)) != events {
		t.Fatal("a done write emitted a change")
	}
}

// Spend freshness in section 5 announces only the owning workstream.
func TestWorkstreamAgentSettledTurnRefreshesOnlyItsWorkstream(t *testing.T) {
	b, rec, w, _ := workstreamAgentFixture(t)
	events := len(workstreamEvents(rec))
	b.workstreamAgentTurnSettled("person")
	if _, err := b.keepCreatedBy("unheld", workstreamCreatedBy); err != nil {
		t.Fatal(err)
	}
	b.workstreamAgentTurnSettled("unheld")
	if len(workstreamEvents(rec)) != events {
		t.Fatal("an unheld conversation emitted a workstream change")
	}
	b.workstreamAgentTurnSettled(w.AgentID)
	all := workstreamEvents(rec)
	if len(all) != events+1 || all[len(all)-1].ID != w.ID || !reflect.DeepEqual(all[len(all)-1].Parts, []string{"workstream"}) {
		t.Fatalf("turn settlement: %+v", all)
	}
}

// Contract (g) bounds encoded editor JSON and emits nothing after refusal.
func TestWorkstreamAgentRejectsOversizeEditorDocument(t *testing.T) {
	_, rec, _, door := workstreamAgentFixture(t)
	events := len(workstreamEvents(rec))
	_, err := door.WritePage(context.Background(), session.WorkstreamPageWrite{Title: "Big", Markdown: strings.Repeat("x", workstream.DocMax), UpdatedFrom: "Discussion"})
	if !errors.Is(err, session.ErrWorkstreamTooLarge) || len(workstreamEvents(rec)) != events {
		t.Fatalf("oversize write: %v", err)
	}
}

// Contracts (b) and (g) validate membership, plan targets, scopes, cap and date.
func TestWorkstreamAgentGoAheadTargetsAreValidatedWithoutGranting(t *testing.T) {
	b, rec, w, door := workstreamAgentFixture(t)
	ctx := context.Background()
	events := len(workstreamEvents(rec))
	base := session.WorkstreamGoAheadAsk{Scope: "job", Factory: "f", Job: 7, CapUSD: 1, EndsOn: "2026-11-04", Why: "Review"}
	for _, mutate := range []func(*session.WorkstreamGoAheadAsk){
		func(a *session.WorkstreamGoAheadAsk) { a.Job = 8 },
		func(a *session.WorkstreamGoAheadAsk) { a.Factory = "another" },
		func(a *session.WorkstreamGoAheadAsk) { a.Scope, a.Factory, a.Job, a.Wave = "wave", "", 0, 9 },
		func(a *session.WorkstreamGoAheadAsk) {
			a.Scope, a.Factory, a.Job, a.MilestoneID = "milestone", "", 0, "missing"
		},
		func(a *session.WorkstreamGoAheadAsk) { a.CapUSD = 0 },
		func(a *session.WorkstreamGoAheadAsk) { a.EndsOn = "2026-11-03" },
		func(a *session.WorkstreamGoAheadAsk) { a.Wave = 1 },
	} {
		ask := base
		mutate(&ask)
		if _, err := door.Propose(ctx, session.WorkstreamProposal{GoAhead: &ask}); err == nil {
			t.Fatalf("accepted %+v", ask)
		}
	}
	s, _ := b.workstreamStore(false)
	records, _ := s.Records(ctx, w.ID, "", "")
	grants, _ := s.GoAheads(ctx, w.ID, true)
	if len(records) != 0 || len(grants) != 0 || len(workstreamEvents(rec)) != events {
		t.Fatal("a refused proposal changed records, grants or events")
	}
}
