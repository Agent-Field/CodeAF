package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/outputs"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestAppOutputActionParity(t *testing.T) {
	root := resolvedTempDir(t)
	id := keepOutputApp(t, root)
	w := outputAppCommand(t, root)
	var row appwire.DataWriteResult
	w.must("data.create", map[string]any{
		"app": id, "type": "contact", "set": map[string]any{"name": "Café & Co."},
	}, &row)
	record, _ := row.Rows[0]["id"].(string)

	var person data.Output
	w.must("apps.render", appwire.AppRenderParams{App: id, RenderAction: schema.RenderAction{Output: "contact-card", Record: record}}, &person)
	if person.App != id || person.Version != 1 || len(person.Sources) != 1 || person.Sources[0] != (data.OutputSource{ID: record, Rev: 1}) {
		t.Fatalf("direct render receipt: %+v", person)
	}
	var started appwire.RunStarted
	w.must("runs.start", appwire.RunStartParams{App: id, Run: "contact-card", Params: map[string]any{"contact": record}}, &started)
	run := waitForOutputRun(t, w, id, started.ID)
	var artifacts data.OutputPage
	w.must("apps.outputs", appwire.AppOutputListParams{App: id, Run: run.ID}, &artifacts)
	if len(artifacts.Items) != 1 {
		t.Fatalf("run outputs = %+v", artifacts)
	}
	rendered := artifacts.Items[0]
	if rendered.ID == person.ID || rendered.Blob != person.Blob || rendered.ActorKind != data.ByFlow || rendered.Step != "render" || rendered.Run != run.ID {
		t.Fatalf("run receipt: %+v, direct: %+v", rendered, person)
	}
	var saved appwire.DataRowResult
	w.must("data.get", map[string]string{"app": id, "type": "contact", "id": record}, &saved)
	if saved.Row["title"] != "contact-card.csv" {
		t.Fatalf("the next step could not read the saved file's name: %+v", saved)
	}
	var file appwire.AppOutputFileResult
	w.must("apps.output_file", appwire.AppOutputFileParams{App: id, ID: rendered.ID}, &file)
	if got := string(file.Content); got != "Contact\nCafé & Co.\n" {
		t.Fatalf("stored CSV = %q", got)
	}
	if _, code, _ := w.call("apps.render", map[string]string{"app": id, "output": "not-declared"}); code != appwire.CodeNotFound {
		t.Fatalf("undeclared action returned %q", code)
	}
	if _, code, _ := w.call("apps.render", map[string]string{"app": id, "output": "contact-card", "record": record, "body": "{{phone}}"}); code != appwire.CodeBadParams {
		t.Fatalf("client-supplied template returned %q", code)
	}
	w.must("runs.start", appwire.RunStartParams{App: id, Run: "weekly-digest"}, &started)
	digest := waitForOutputRun(t, w, id, started.ID)
	var inbox data.OutputPage
	w.must("apps.outputs", appwire.AppOutputListParams{App: id, Inbox: true}, &inbox)
	if len(inbox.Items) != 1 || inbox.Items[0].Action != "weekly-digest" || inbox.Items[0].Run != digest.ID {
		t.Fatalf("digest inbox = %+v", inbox)
	}
	w.must("apps.output_file", appwire.AppOutputFileParams{App: id, ID: inbox.Items[0].ID}, &file)
	if got := string(file.Content); got != "Contacts\n1\n" {
		t.Fatalf("digest CSV = %q", got)
	}
}

func keepOutputApp(t *testing.T, root string) string {
	t.Helper()
	// The contact definition comes from the shared CRM fixture.
	crm, err := os.ReadFile("../../internal/apps/store/testdata/crm/app.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture schema.App
	if err := json.Unmarshal(crm, &fixture); err != nil {
		t.Fatal(err)
	}
	contact := fixture.Records["contact"]
	delete(contact.Fields, "company")
	app := schema.App{
		ID: "contacts", Name: "Contacts", Version: 1, Kit: 2,
		Records:      map[string]schema.RecordType{"contact": contact},
		Capabilities: schema.Capabilities{Records: map[string]string{"contact": "rw"}},
		Outputs: map[string]schema.OutputTemplate{
			"contact-card": {Label: "Contact card", Format: "csv", Record: "contact", Columns: []schema.OutputColumn{{Label: "Contact", Value: "{{name}}"}}},
			"weekly-digest": {Label: "Weekly contacts", Format: "csv", Query: &schema.QueryIR{Type: "contact", Aggregate: []schema.QueryAggregate{{Op: "count", As: "total"}}},
				Columns: []schema.OutputColumn{{Label: "Contacts", Value: "{{total}}"}}},
		},
		Triggers: []schema.Trigger{{ID: "weekly", On: "schedule", Cron: "0 9 * * 1", Run: "weekly-digest"}},
	}
	raw, err := json.Marshal(app)
	if err != nil {
		t.Fatal(err)
	}
	s := store.NewAt(root, nil)
	draft, err := s.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"app.json":                string(raw),
		"plan.json":               `{"ask":"Export CRM contacts","data":[{"id":"contact","line":"CRM contact"}],"screens":[{"id":"home","line":"Contacts"}],"automation":[{"id":"contact-card","line":"Render contact"},{"id":"weekly-digest","line":"Weekly digest"}],"needs":"computed"}`,
		"screens/home.json":       `{"screen":"home","title":"Contacts","body":{"part":"Button","id":"export","label":"Weekly contacts","on":{"click":[{"render":{"output":"weekly-digest"}}]}}}`,
		"runs/contact-card.yaml":  "run: contact-card\nparams:\n  contact: {type: ref, to: contact}\nsteps:\n  - id: render\n    render: {output: contact-card, record: $params.contact.id}\n  - id: remember\n    records.update: {type: contact, id: $params.contact.id, set: {title: $steps.render.name}}\n",
		"runs/weekly-digest.yaml": "run: weekly-digest\nsteps:\n  - id: deliver\n    render: {output: weekly-digest, inbox: true}\n",
	}
	for name, content := range files {
		path := filepath.Join(draft.Dir(), name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	meta, err := s.Promote(draft, "Keep CRM outputs")
	if err != nil {
		t.Fatal(err)
	}
	return meta.ID
}

func outputAppCommand(t *testing.T, root string) *factoryWire {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	cmd := exec.CommandContext(ctx, os.Args[0], "app", "--home", root)
	cmd.Env = append(os.Environ(), carriedChildEnv+"=real")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = in.Close()
		_ = cmd.Wait()
		cancel()
		if t.Failed() && stderr.Len() > 0 {
			t.Log(stderr.String())
		}
	})
	w := &factoryWire{t: t, in: in, answers: make(chan json.RawMessage, 16)}
	go func() {
		defer close(w.answers)
		scan := bufio.NewScanner(out)
		for scan.Scan() {
			select {
			case w.answers <- json.RawMessage(scan.Text()):
			case <-ctx.Done():
				return
			}
		}
	}()
	if hello := <-w.answers; !bytes.Contains(hello, []byte(`"event":"hello"`)) {
		t.Fatalf("codeaf app hello = %s", hello)
	}
	return w
}

func waitForOutputRun(t *testing.T, w *factoryWire, app, id string) runs.Run {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		var run runs.Run
		w.must("runs.get", appwire.RunGetParams{App: app, ID: id}, &run)
		if run.State == runs.Done {
			return run
		}
		if run.State != runs.Running || time.Now().After(deadline) {
			t.Fatalf("rendering run: %+v", run)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAppOutputScheduleDeliversOnceAfterRestart(t *testing.T) {
	root := resolvedTempDir(t)
	id := keepOutputApp(t, root)
	t.Setenv("CODEAF_HOME", root)
	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { backend.Close() })
	db, err := backend.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	committed, err := backend.AppDefinitions().Get(id)
	if err != nil {
		t.Fatal(err)
	}
	now := committed.Meta.UpdatedAt
	files := runs.Files(filepath.Join(root, "apps"))
	cfg := runs.Config{
		Root: filepath.Join(root, "journal"), Load: files.Load, Triggers: files.Triggers,
		Now: func() time.Time { return now }, Render: backend.renderRunOutput,
	}
	open := func() *runs.Engine {
		t.Helper()
		e, err := runs.Open(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = e.Close() })
		return e
	}
	e := open()
	if err := e.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if page, err := db.Outputs(data.Scope{App: id}, "", "", true); err != nil || len(page.Items) != 0 {
		t.Fatalf("inbox before schedule: %+v, %v", page, err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	now = now.AddDate(0, 0, 8)
	e = open()
	if err := e.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	scheduled, err := e.List(id, "weekly-digest")
	if err != nil || len(scheduled) != 1 || !scheduled[0].CaughtUp || scheduled[0].State != runs.Done {
		t.Fatalf("scheduled digest runs: %+v, %v", scheduled, err)
	}
	// Revisit the same step, as if its file had committed before the journal
	// recorded the response. It must return the original saved receipt.
	instance := scheduled[0].ID
	replayed, err := backend.renderRunOutput(t.Context(), runs.Call{App: id, ID: instance, Step: "deliver", Key: instance + "-1"}, schema.RenderAction{Output: "weekly-digest", Inbox: true})
	if err != nil {
		t.Fatal(err)
	}
	page, err := db.Outputs(data.Scope{App: id}, "", "", true)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != replayed.Output.(map[string]any)["id"] || page.Items[0].Run != instance {
		t.Fatalf("scheduled digest inbox: %+v, %v", page, err)
	}
}

func TestAppOutputUsesEngineBoundAgentPrincipal(t *testing.T) {
	root := resolvedTempDir(t)
	id := keepOutputApp(t, root)
	t.Setenv("CODEAF_HOME", root)
	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { backend.Close() })
	db, err := backend.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	created, err := db.Create(data.Scope{App: id}, "contact", []map[string]any{{"name": "Sam", "phone": "+1 555 6500"}})
	if err != nil {
		t.Fatal(err)
	}
	action := schema.RenderAction{Output: "contact-card", Record: created.Rows[0].ID()}
	person, err := backend.RenderAppOutput(t.Context(), appwire.AppRenderParams{App: id, RenderAction: action})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := backend.invokeOutput(t.Context(), outputs.Invocation{
		Scope:  data.Scope{App: id, Agent: true, Actor: "contact assistant"},
		Render: action,
	})
	if err != nil || agent.Blob != person.Blob || agent.ActorKind != data.ByAI || agent.Actor != "contact assistant" {
		t.Fatalf("engine-bound agent receipt: %+v, %v", agent, err)
	}
	_, bytes, err := db.OutputFile(data.Scope{App: id}, agent.ID)
	if err != nil || string(bytes) != "Contact\nSam\n" {
		t.Fatalf("agent file: %q, %v", bytes, err)
	}
	if _, _, err := db.OutputFile(data.Scope{App: "another-app"}, agent.ID); err == nil {
		t.Fatal("another app read the output by id")
	}
}
