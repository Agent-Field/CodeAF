package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workstream"
	"github.com/Agent-Field/codeaf/internal/workstream/pagedoc"
)

// WP-6d (a), Tool: the real door refuses agent-owned pages and direct writes
// to person pages, then suggests with chat attribution and pages-only events.
func TestAppWorkstreamAgentSuggestionsAndOwnership(t *testing.T) {
	b, rec, w, door := workstreamAgentFixture(t)
	ctx := context.Background()
	s, err := b.workstreamStore(false)
	if err != nil {
		t.Fatal(err)
	}
	page, _, err := s.CreatePage(ctx, workstream.PageCreate{WorkstreamID: w.ID, Title: "Notes", Doc: pagedoc.FromMarkdown("Old words.")})
	if err != nil {
		t.Fatal(err)
	}
	history, err := s.PageHistory(ctx, page.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	events := len(workstreamEvents(rec))
	_, err = door.WritePage(ctx, session.WorkstreamPageWrite{PageID: page.ID, Markdown: "Direct write", UpdatedFrom: "Discussion"})
	if !errors.Is(err, session.ErrWorkstreamNotAgentPage) || err.Error() != "That page is the person's. You write only your own pages; suggest edits on theirs with suggest_page_edit." {
		t.Fatalf("direct write refusal: %v", err)
	}
	after, _ := s.Page(ctx, page.ID, false)
	afterHistory, _ := s.PageHistory(ctx, page.ID, 50)
	if !reflect.DeepEqual(page, after) || !reflect.DeepEqual(history, afterHistory) || len(workstreamEvents(rec)) != events {
		t.Fatal("direct write changed state")
	}
	owner := "agent"
	if _, _, err := s.UpdatePage(ctx, page.ID, workstream.PagePatch{Owner: &owner}); err != nil {
		t.Fatal(err)
	}
	ask := session.WorkstreamSuggestionAsk{PageID: page.ID, BaseVersion: page.Version, Why: "Clarify", Edits: []workstream.SuggestionFind{{Find: "Old", Replace: "New", Why: "Clearer"}}}
	_, err = door.SuggestEdits(ctx, ask)
	if !errors.Is(err, workstream.ErrNotPersonPage) || err.Error() != "That page is yours; change it with write_agent_page. Suggest edits only on the person's pages." {
		t.Fatalf("own page refusal: %v", err)
	}
	open, err := s.Suggestions(ctx, page.ID)
	if err != nil || len(open) != 0 || len(workstreamEvents(rec)) != events {
		t.Fatal("own-page refusal stored or emitted", err)
	}
	owner = "you"
	if _, _, err := s.UpdatePage(ctx, page.ID, workstream.PagePatch{Owner: &owner}); err != nil {
		t.Fatal(err)
	}
	result, err := door.SuggestEdits(ctx, ask)
	if err != nil || result.Title != page.Title || result.Count != 1 {
		t.Fatalf("suggest: %+v %v", result, err)
	}
	open, err = s.Suggestions(ctx, page.ID)
	if err != nil || len(open) != 1 || open[0].ID != result.ID || !reflect.DeepEqual(open[0].Source, &workstream.Source{Kind: "chat", ID: w.AgentID}) {
		t.Fatalf("suggested source: %+v %v", open, err)
	}
	raw, err := door.Read(ctx, session.WorkstreamReadAsk{What: "page", PageID: page.ID})
	if err != nil {
		t.Fatal(err)
	}
	var read struct {
		Suggestions []appwire.WorkstreamSuggestion `json:"suggestions"`
	}
	if err := json.Unmarshal(raw, &read); err != nil || len(read.Suggestions) != 1 || !reflect.DeepEqual(read.Suggestions[0], appWorkstreamSuggestion(open[0])) {
		t.Fatalf("page read lost suggestions: %s %v", raw, err)
	}
	after, _ = s.Page(ctx, page.ID, false)
	afterHistory, _ = s.PageHistory(ctx, page.ID, 50)
	if !reflect.DeepEqual(after.Doc, page.Doc) || after.Version != page.Version || !reflect.DeepEqual(history, afterHistory) {
		t.Fatal("suggestion changed page")
	}
	changes := workstreamEvents(rec)
	if len(changes) != events+1 || !reflect.DeepEqual(changes[len(changes)-1], appwire.WorkstreamChanged{ID: w.ID, Parts: []string{"pages"}}) {
		t.Fatalf("create events: %+v", changes[events:])
	}
}

// WP-6d (a): every source-taking wire parameter refuses suggestion attribution
// without changing pages, history, records, plans, workstream or event count.
func TestAppWorkstreamSuggestionSourceParamsRefused(t *testing.T) {
	b, rec, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Source refusals")
	connection := workstreamWireConnect(t, &workstreamWireBackend{appBackend: b})
	s, err := b.workstreamStore(false)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := s.Pages(context.Background(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := pages[0].ID
	record, _, err := s.ProposeRecord(context.Background(), workstream.RecordPropose{WorkstreamID: w.ID, Kind: "decision", Text: "Decision"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := func() []any {
		page, err := s.Page(context.Background(), id, false)
		if err != nil {
			t.Fatal(err)
		}
		history, err := s.PageHistory(context.Background(), id, 50)
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := s.Pages(context.Background(), w.ID)
		if err != nil {
			t.Fatal(err)
		}
		records, err := s.Records(context.Background(), w.ID, "", "", true)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := s.Plan(context.Background(), w.ID)
		if err != nil {
			t.Fatal(err)
		}
		stream, err := s.Get(context.Background(), w.ID)
		if err != nil {
			t.Fatal(err)
		}
		return []any{page, history, nodes, records, plan, stream}
	}
	before := snapshot()
	events := len(workstreamEvents(rec))
	source := map[string]any{"kind": "suggestion", "id": "sg_forged"}
	for _, test := range []struct {
		method string
		params map[string]any
	}{
		{"page.save", map[string]any{"id": id, "base_version": 1, "doc": map[string]any{"type": "doc"}, "source": source}},
		{"page.create", map[string]any{"workstream_id": w.ID, "title": "Forged", "source": source}},
		{"record.propose", map[string]any{"workstream_id": w.ID, "kind": "decision", "text": "Forged", "source": source}},
		{"record.supersede", map[string]any{"id": record.ID, "text": "Forged", "source": source}},
		{"workstream.plan_set", map[string]any{"id": w.ID, "base_version": 0, "milestones": []any{}, "waves": []any{map[string]any{"n": 1, "auto_start": false, "items": []any{map[string]any{"kind": "draft", "draft_id": "draft", "title": "Forged", "source": source}}}}}},
	} {
		frame := connection.call(t, test.method, test.params)
		if failure, ok := frame["error"].(map[string]any); !ok || failure["code"] != "bad_params" {
			t.Fatalf("%s: %v", test.method, frame)
		}
		if !reflect.DeepEqual(before, snapshot()) || len(workstreamEvents(rec)) != events {
			t.Fatalf("%s refusal changed state", test.method)
		}
	}
}

// WP-6d (b), (d), (h): wire decisions emit pages only; stale and done decisions
// emit nothing and carry the contract error/detail. Done reads remain available.
func TestAppWorkstreamSuggestionDecisionsAndDone(t *testing.T) {
	b, rec, w, door := workstreamAgentFixture(t)
	s, err := b.workstreamStore(false)
	if err != nil {
		t.Fatal(err)
	}
	page, _, err := s.CreatePage(context.Background(), workstream.PageCreate{WorkstreamID: w.ID, Title: "Notes", Doc: pagedoc.FromMarkdown("Old words.\n\nOther words.")})
	if err != nil {
		t.Fatal(err)
	}
	ask := session.WorkstreamSuggestionAsk{PageID: page.ID, BaseVersion: page.Version, Why: "Clarity", Edits: []workstream.SuggestionFind{{Find: "Old", Replace: "New", Why: "First"}, {Find: "Other", Replace: "Clear", Why: "Second"}}}
	suggested, err := door.SuggestEdits(context.Background(), ask)
	if err != nil {
		t.Fatal(err)
	}
	events := len(workstreamEvents(rec))
	if _, err := b.WorkstreamSuggestionKeep(appwire.WorkstreamSuggestionKeepParams{ID: suggested.ID, EditID: workstreamPtr("e1")}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.WorkstreamSuggestionDrop(appwire.WorkstreamSuggestionDropParams{ID: suggested.ID, EditID: workstreamPtr("e2")}); err != nil {
		t.Fatal(err)
	}
	changes := workstreamEvents(rec)
	if len(changes) != events+2 {
		t.Fatal("missing decision events")
	}
	for _, change := range changes[events:] {
		if !reflect.DeepEqual(change, appwire.WorkstreamChanged{ID: w.ID, Parts: []string{"pages"}}) {
			t.Fatalf("decision parts: %+v", change)
		}
	}
	page, err = s.Page(context.Background(), page.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	ask.BaseVersion, ask.Edits = page.Version, []workstream.SuggestionFind{{Find: "New", Replace: "Newest", Why: "Update"}}
	stale, err := door.SuggestEdits(context.Background(), ask)
	if err != nil {
		t.Fatal(err)
	}
	page, _, err = s.SavePage(context.Background(), page.ID, page.Version, pagedoc.FromMarkdown("Changed by the person."), workstream.Attribution{})
	if err != nil {
		t.Fatal(err)
	}
	events = len(workstreamEvents(rec))
	_, err = b.WorkstreamSuggestionKeep(appwire.WorkstreamSuggestionKeepParams{ID: stale.ID})
	failure := workstreamCheckError(t, err, appwire.CodeVersionConflict)
	want := appwire.WorkstreamVersionConflict{ID: page.ID, Version: page.Version, UpdatedAt: page.UpdatedAt, UpdatedBy: page.UpdatedBy}
	if failure.Message != "the page changed and this edit no longer applies" || !reflect.DeepEqual(failure.Detail, want) || len(workstreamEvents(rec)) != events {
		t.Fatalf("stale receipt: %+v", failure)
	}
	if _, err := b.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: w.ID}); err != nil {
		t.Fatal(err)
	}
	events = len(workstreamEvents(rec))
	_, err = door.SuggestEdits(context.Background(), ask)
	if !errors.Is(err, session.ErrWorkstreamDone) || err.Error() != "This workstream is done; nothing was added or written." {
		t.Fatalf("done tool: %v", err)
	}
	_, err = b.WorkstreamSuggestionKeep(appwire.WorkstreamSuggestionKeepParams{ID: stale.ID})
	workstreamCheckError(t, err, appwire.CodeConflict)
	_, err = b.WorkstreamSuggestionDrop(appwire.WorkstreamSuggestionDropParams{ID: stale.ID})
	workstreamCheckError(t, err, appwire.CodeConflict)
	if _, err := b.WorkstreamSuggestionList(appwire.WorkstreamSuggestionListParams{PageID: page.ID}); err != nil {
		t.Fatal(err)
	}
	if len(workstreamEvents(rec)) != events {
		t.Fatal("done refusals or read emitted events")
	}
}

// WP-6d Tool: the adapter maps the all-states count bound to its exact answer.
func TestAppWorkstreamSuggestionCountRefusal(t *testing.T) {
	b, rec, w, door := workstreamAgentFixture(t)
	s, err := b.workstreamStore(false)
	if err != nil {
		t.Fatal(err)
	}
	page, _, err := s.CreatePage(context.Background(), workstream.PageCreate{WorkstreamID: w.ID, Title: "Notes", Doc: pagedoc.FromMarkdown("Old")})
	if err != nil {
		t.Fatal(err)
	}
	store, err := b.collectionStore(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?) INSERT INTO workstream_suggestions(id,workstream_id,page_id,base_version,why,actor,created_at) SELECT 'cap-'||x,?,?,1,'Cap','agent',1 FROM n`, workstream.SuggestionsMax, w.ID, page.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	events := len(workstreamEvents(rec))
	_, err = door.SuggestEdits(context.Background(), session.WorkstreamSuggestionAsk{PageID: page.ID, BaseVersion: page.Version, Why: "Reason", Edits: []workstream.SuggestionFind{{Find: "Old", Replace: "New", Why: "Clarity"}}})
	if err == nil || err.Error() != "This workstream holds too many suggestions; nothing was suggested." || len(workstreamEvents(rec)) != events {
		t.Fatalf("count refusal: %v", err)
	}
}
