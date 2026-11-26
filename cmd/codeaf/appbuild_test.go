package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/check"
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

func TestPersonalAppCheckLookRequestsAPinnedSyntheticViewport(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	rec := newAppRecorder()
	backend := newFakeBackend(rec, nil)
	t.Cleanup(func() { backend.Close() })
	defs := backend.AppDefinitions()
	draft, err := defs.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(draft.Dir(), os.DirFS("../../internal/apps/schema/testdata/crm")); err != nil {
		t.Fatal(err)
	}
	app, err := defs.Promote(draft, "Keep the CRM")
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := backend.AppBuilds()
	if err != nil {
		t.Fatal(err)
	}
	edit, err := jobs.Change(app.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	backend.AppLookReady(true)
	type result struct {
		problems []string
		err      error
	}
	done := make(chan result, 1)
	go func() {
		problems, err := backend.checkAppLook(t.Context(), edit, "deals", check.LookViewport{Theme: "light", Width: 800})
		done <- result{problems, err}
	}()
	var request appwire.AppLookRequest
	rec.waitFor(t, 3*time.Second, func(event string, data any) bool {
		if event != appwire.EventAppLookRequested {
			return false
		}
		request = data.(appwire.AppLookRequest)
		return true
	})
	if request.Theme != "light" || request.Viewport == nil || request.Viewport.Width != 800 ||
		request.Viewport.Height != 760 || request.Fixture != "synthetic" || request.App != app.ID {
		t.Fatalf("Look request = %+v", request)
	}
	preview, err := jobs.Preview(builder.PreviewRequest{Draft: request.Scope})
	if err != nil || !preview.Sample || preview.Scope != request.Scope {
		t.Fatalf("Look did not pin its synthetic preview: %+v, %v", preview, err)
	}
	var pixels bytes.Buffer
	if err := png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 800, 760))); err != nil {
		t.Fatal(err)
	}
	finding := "[deals] close date truncates"
	err = backend.FinishAppLook(appwire.AppLookCapture{
		ID: request.ID, PNG: base64.StdEncoding.EncodeToString(pixels.Bytes()), Problems: []string{finding},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || len(got.problems) != 1 || got.problems[0] != finding {
			t.Fatalf("Check Look = %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Check did not receive the Desk finding")
	}
}
