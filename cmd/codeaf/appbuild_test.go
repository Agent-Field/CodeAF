package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestPersonalAppPreviewUsesSyntheticDataAndOpensFromInbox(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	store := store.New()
	draft, err := store.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(draft.Dir(), os.DirFS("../../internal/apps/schema/testdata/crm")); err != nil {
		t.Fatal(err)
	}
	meta, err := store.Promote(draft, "Keep the CRM")
	if err != nil {
		t.Fatal(err)
	}
	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { backend.Close() })
	wire := wireOver(t, backend)
	scope := appwire.DataScope{App: meta.ID}
	var created appwire.DataWriteResult
	wire.must("data.create", appwire.DataCreateParams{DataScope: scope, Type: "company", Set: map[string]any{"name": "Live company"}}, &created)
	id, _ := created.Rows[0]["id"].(string)
	if id == "" {
		t.Fatal("missing live row id")
	}
	var editing builder.PlanResult
	wire.must("apps.change", map[string]string{"app": meta.ID}, &editing)

	var sample builder.Preview
	wire.mustFail("apps.preview", builder.PreviewRequest{Draft: editing.Draft, Data: "real"}, appwire.CodeForbidden)
	wire.must("apps.preview", builder.PreviewRequest{Draft: editing.Draft}, &sample)
	if !sample.Sample || sample.Scope == "" {
		t.Fatalf("expected a synthetic preview: %+v", sample)
	}
	var screen struct{ Screen json.RawMessage }
	wire.must("apps.screen", map[string]string{"screen": "companies", "draft": sample.Scope}, &screen)
	if len(screen.Screen) == 0 {
		t.Fatal("missing preview screen")
	}
	scratch := appwire.DataScope{App: meta.ID, Draft: sample.Scope}
	wire.mustFail("data.get", appwire.DataRecordParams{DataScope: scratch, Type: "company", ID: id}, appwire.CodeNotFound)
	var got appwire.DataRowResult
	wire.must("data.get", appwire.DataRecordParams{DataScope: scope, Type: "company", ID: id}, &got)
	if got.Row["name"] != "Live company" {
		t.Fatalf("preview changed live data: %+v", got.Row)
	}
	wire.mustFail("apps.look", appwire.AppLookParams{Draft: editing.Draft, Screen: "companies"}, appwire.CodeDeskUnavailable)

	backend.appBuildNotice(builder.Summary{Draft: editing.Draft, Name: "CRM", State: builder.Ready})
	var opened struct {
		AppDraft string `json:"app_draft"`
	}
	wire.must("inbox.open", map[string]string{"id": appInboxID(appwire.AppDraftInboxPrefix+editing.Draft, appBuildInboxAsk)}, &opened)
	if opened.AppDraft != editing.Draft {
		t.Fatalf("Inbox opened %q, want editing job %q", opened.AppDraft, editing.Draft)
	}

	wire.must("apps.discard", map[string]string{"draft": editing.Draft}, nil)
	wire.mustFail("data.get", appwire.DataRecordParams{DataScope: scratch, Type: "company", ID: id}, appwire.CodeNotFound)
}
