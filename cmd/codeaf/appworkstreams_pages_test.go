package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

// WP-F1 (c): creation alone chooses the title default; explicit booleans win,
// renames retain the existing choice, and the initial Overview remains off.
func TestAppWorkstreamPageReadByJobsDefaults(t *testing.T) {
	b, _, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Read defaults")
	pages, err := b.WorkstreamPageList(appwire.WorkstreamPageListParams{WorkstreamID: w.ID})
	if err != nil || len(pages.Pages) != 1 || pages.Pages[0].ReadByJobs {
		t.Fatalf("initial Overview: %+v %v", pages, err)
	}
	for _, tc := range []struct {
		title string
		value *bool
		want  bool
	}{
		{"Spec", nil, true}, {" decisions ", nil, true}, {"DECISIONS", nil, true},
		{"Spec notes", nil, false}, {"Overview", nil, false}, {"Notes", nil, false},
		{"Spec", workstreamPtr(false), false}, {"Notes", workstreamPtr(true), true},
	} {
		t.Run(tc.title, func(t *testing.T) {
			made, err := b.WorkstreamPageCreate(appwire.WorkstreamPageCreateParams{WorkstreamID: w.ID, Title: tc.title, ReadByJobs: tc.value})
			if err != nil || made.Page.ReadByJobs != tc.want {
				t.Fatalf("created: %+v %v, want read_by_jobs=%v", made, err, tc.want)
			}
			rename := "Spec"
			if tc.want {
				rename = "Notes"
			}
			updated, err := b.WorkstreamPageUpdate(appwire.WorkstreamPageUpdateParams{ID: made.Page.ID, Title: &rename})
			if err != nil || updated.Page.ReadByJobs != tc.want {
				t.Fatalf("rename changed read_by_jobs: %+v %v", updated, err)
			}
		})
	}
}

// WP-F1 (a): handlers expose historical content and attribution, optional
// markdown and current metadata, including deleted_at for a deleted page.
func TestAppWorkstreamPageGetVersion(t *testing.T) {
	b, rec, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Version handler")
	store, err := b.workstreamStore(false)
	if err != nil || store == nil {
		t.Fatalf("store: %v", err)
	}
	doc := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Earlier"}]}]}`)
	first, _, err := store.CreatePage(context.Background(), workstream.PageCreate{WorkstreamID: w.ID, Title: "Before", Doc: doc,
		Attribution: workstream.Attribution{By: "agent", From: &workstream.Source{Kind: "chat", ID: "old-source"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.WorkstreamPageSave(appwire.WorkstreamPageSaveParams{ID: first.ID, BaseVersion: 1, Doc: json.RawMessage(`{"type":"doc","content":[]}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.WorkstreamPageUpdate(appwire.WorkstreamPageUpdateParams{ID: first.ID, Title: workstreamPtr("After"), ReadByJobs: workstreamPtr(true)}); err != nil {
		t.Fatal(err)
	}
	before := len(workstreamEvents(rec))
	current, err := b.WorkstreamPageGet(appwire.WorkstreamPageGetParams{ID: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := b.WorkstreamPageGet(appwire.WorkstreamPageGetParams{ID: first.ID, Version: workstreamPtr(2)})
	if err != nil || !reflect.DeepEqual(selected, current) {
		t.Fatalf("current version: %+v, want %+v: %v", selected, current, err)
	}
	old, err := b.WorkstreamPageGet(appwire.WorkstreamPageGetParams{ID: first.ID, Version: workstreamPtr(1), Markdown: workstreamPtr(true)})
	if err != nil || old.Page.Title != "Before" || string(old.Page.Doc) != string(doc) || old.Page.Version != 1 || old.Page.UpdatedBy != "agent" || !old.Page.UpdatedAt.Equal(first.UpdatedAt) || old.Page.UpdatedFrom == nil || old.Page.UpdatedFrom.ID != "old-source" || !old.Page.ReadByJobs || old.Page.Markdown == nil || *old.Page.Markdown != "Earlier\n" {
		t.Fatalf("historical page: %+v %v", old, err)
	}
	_, err = b.WorkstreamPageGet(appwire.WorkstreamPageGetParams{ID: first.ID, Version: workstreamPtr(99)})
	workstreamCheckError(t, err, appwire.CodeNotFound)
	for _, version := range []int{0, -1} {
		_, err := b.WorkstreamPageGet(appwire.WorkstreamPageGetParams{ID: first.ID, Version: &version})
		workstreamCheckError(t, err, appwire.CodeBadParams)
	}
	if len(workstreamEvents(rec)) != before {
		t.Fatal("version reads emitted changes")
	}
	if _, err := b.WorkstreamPageDelete(appwire.WorkstreamPageDeleteParams{ID: first.ID}); err != nil {
		t.Fatal(err)
	}
	old, err = b.WorkstreamPageGet(appwire.WorkstreamPageGetParams{ID: first.ID, Version: workstreamPtr(1)})
	if err != nil || old.Page.DeletedAt.IsZero() || old.Page.Markdown != nil || old.Page.Title != "Before" {
		t.Fatalf("deleted historical page: %+v %v", old, err)
	}
}

// WP-F1 (b): list presence, deleted write refusals, restore parameter dispatch
// and the committed pages event are visible through the backend handlers.
func TestAppWorkstreamDeletedPageHandlers(t *testing.T) {
	b, rec, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Deleted handler")
	list := func(include *bool) map[string]json.RawMessage {
		t.Helper()
		result, err := b.WorkstreamPageList(appwire.WorkstreamPageListParams{WorkstreamID: w.ID, IncludeDeleted: include})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		return fields
	}
	for _, include := range []*bool{nil, workstreamPtr(false)} {
		if _, present := list(include)["deleted"]; present {
			t.Fatal("deleted present without include_deleted=true")
		}
	}
	if raw := string(list(workstreamPtr(true))["deleted"]); raw != "[]" {
		t.Fatalf("empty deleted list = %s", raw)
	}
	made, err := b.WorkstreamPageCreate(appwire.WorkstreamPageCreateParams{WorkstreamID: w.ID, Title: "Spec"})
	if err != nil {
		t.Fatal(err)
	}
	id := made.Page.ID
	_, err = b.WorkstreamPageRestore(appwire.WorkstreamPageRestoreParams{ID: id})
	workstreamCheckError(t, err, appwire.CodeBadParams)
	if _, err := b.WorkstreamPageDelete(appwire.WorkstreamPageDeleteParams{ID: id}); err != nil {
		t.Fatal(err)
	}
	fields := list(workstreamPtr(true))
	var nodes []appwire.WorkstreamPageNode
	if err := json.Unmarshal(fields["deleted"], &nodes); err != nil || len(nodes) != 1 || nodes[0].ID != id || nodes[0].DeletedAt.IsZero() {
		t.Fatalf("deleted list: %s %v", fields["deleted"], err)
	}
	for name, call := range map[string]func() error{
		"save": func() error {
			_, err := b.WorkstreamPageSave(appwire.WorkstreamPageSaveParams{ID: id, BaseVersion: 1, Doc: made.Page.Doc})
			return err
		},
		"update": func() error { _, err := b.WorkstreamPageUpdate(appwire.WorkstreamPageUpdateParams{ID: id}); return err },
		"move":   func() error { _, err := b.WorkstreamPageMove(appwire.WorkstreamPageMoveParams{ID: id}); return err },
		"delete": func() error { _, err := b.WorkstreamPageDelete(appwire.WorkstreamPageDeleteParams{ID: id}); return err },
		"history": func() error {
			_, err := b.WorkstreamPageHistory(appwire.WorkstreamPageHistoryParams{ID: id})
			return err
		},
		"create parent": func() error {
			_, err := b.WorkstreamPageCreate(appwire.WorkstreamPageCreateParams{WorkstreamID: w.ID, ParentID: &id, Title: "No"})
			return err
		},
		"create after": func() error {
			_, err := b.WorkstreamPageCreate(appwire.WorkstreamPageCreateParams{WorkstreamID: w.ID, AfterID: &id, Title: "No"})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) { workstreamCheckError(t, call(), appwire.CodeNotFound) })
	}
	for _, p := range []appwire.WorkstreamPageRestoreParams{
		{ID: id, Version: workstreamPtr(1)}, {ID: id, BaseVersion: workstreamPtr(1)},
		{ID: id, Version: workstreamPtr(1), BaseVersion: workstreamPtr(1)},
	} {
		_, err := b.WorkstreamPageRestore(p)
		workstreamCheckError(t, err, appwire.CodeBadParams)
	}
	_, err = b.WorkstreamPageRestore(appwire.WorkstreamPageRestoreParams{ID: "missing"})
	workstreamCheckError(t, err, appwire.CodeNotFound)
	before := len(workstreamEvents(rec))
	restored, err := b.WorkstreamPageRestore(appwire.WorkstreamPageRestoreParams{ID: id})
	if err != nil || restored.Page.Version != 1 || !restored.Page.DeletedAt.IsZero() || restored.Page.UpdatedBy != "you" || restored.Page.UpdatedFrom != nil {
		t.Fatalf("undelete: %+v %v", restored, err)
	}
	events := workstreamEvents(rec)
	if len(events) != before+1 || events[before].ID != w.ID || !reflect.DeepEqual(events[before].Parts, []string{"workstream", "pages"}) {
		t.Fatalf("undelete event: %+v", events[before:])
	}
	history, err := b.WorkstreamPageHistory(appwire.WorkstreamPageHistoryParams{ID: id})
	if err != nil || len(history.Versions) != 1 {
		t.Fatalf("undelete added history: %+v %v", history, err)
	}
	if raw := string(list(workstreamPtr(true))["deleted"]); raw != "[]" {
		t.Fatalf("deleted list after restore = %s", raw)
	}
}
