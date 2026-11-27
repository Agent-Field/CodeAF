package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/pdfx"
	"github.com/Agent-Field/codeaf/internal/session"
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
	// The CLI client reaches the same kept template on the private socket.
	params, err := json.Marshal(appwire.AppRenderParams{App: id, RenderAction: schema.RenderAction{Output: "contact-card", Record: record}})
	if err != nil {
		t.Fatal(err)
	}
	client := exec.CommandContext(t.Context(), os.Args[0], "app", "--home", root, "render", string(params))
	client.Env = append(os.Environ(), carriedChildEnv+"=real")
	reply, err := client.Output()
	var cli data.Output
	if err != nil || json.Unmarshal(reply, &cli) != nil || cli.Blob != person.Blob || cli.ID == person.ID {
		t.Fatalf("codeaf app render: %s, %v", reply, err)
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
	if len(inbox.Items) != 1 || inbox.Items[0].Action != "weekly-digest" || inbox.Items[0].Run != digest.ID || inbox.Items[0].Matched != 1 {
		t.Fatalf("digest inbox = %+v", inbox)
	}
	w.must("apps.output_file", appwire.AppOutputFileParams{App: id, ID: inbox.Items[0].ID}, &file)
	pdf := filepath.Join(t.TempDir(), "digest.pdf")
	if err := os.WriteFile(pdf, file.Content, 0o600); err != nil {
		t.Fatal(err)
	}
	summary, err := pdfx.Extract(pdf)
	if err != nil || !strings.Contains(summary, "Contacts: 1") {
		t.Fatalf("digest PDF = %q, %v", summary, err)
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
		Agent:        &schema.AgentRef{Name: "Contacts agent", File: "agent.md"},
		Records:      map[string]schema.RecordType{"contact": contact},
		Capabilities: schema.Capabilities{Records: map[string]string{"contact": "rw"}},
		Outputs: map[string]schema.OutputTemplate{
			"contact-card": {Label: "Contact card", Format: "csv", Record: "contact", Columns: []schema.OutputColumn{{Label: "Contact", Value: "{{name}}"}}},
			"weekly-digest": {Label: "Weekly contacts", Format: "pdf", Query: &schema.QueryIR{Type: "contact", Aggregate: []schema.QueryAggregate{{Op: "count", As: "total"}}},
				Body: "Contacts: {{total}}"},
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
		"agent.md":                "Generate contact cards from supplied record IDs.",
		"app.json":                string(raw),
		"plan.json":               `{"ask":"Export CRM contacts","agent":{"line":"Generate contact cards."},"data":[{"id":"contact","line":"CRM contact"}],"screens":[{"id":"home","line":"Contacts"}],"automation":[{"id":"contact-card","line":"Render contact"},{"id":"weekly-digest","line":"Weekly digest"}],"needs":"computed"}`,
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
		scan.Buffer(make([]byte, 64<<10), 16<<20)
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
	app, err := backend.outputApp(id)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	command := "codeaf app render " + string(raw)
	grant := appwire.AppAgentGrant{App: id, Agent: id, Group: "actions", ID: "render/contact-card", Allowed: true}
	if err := backend.GrantAppAgent(grant); err != nil {
		t.Fatal(err)
	}
	var calls int
	var expectGrantRefusal bool
	const pdfData = "data:application/pdf;base64,JVBERi0xLjQK"
	attachment := ai.ContentPart{Type: "file", InputFile: &ai.InputFileData{FileData: pdfData}}
	client := pricingCompleter(func(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
		calls++
		var attached bool
		for _, previous := range messages {
			for _, part := range previous.Content {
				attached = attached || previous.Role == "user" && part.Type == "file" && part.InputFile != nil && part.InputFile.FileData == pdfData
			}
		}
		if !attached {
			return nil, errors.New("intake PDF did not reach the output agent")
		}
		message := ai.Message{Role: "assistant"}
		if calls == 1 {
			for i, text := range []string{command, command, `codeaf app render {"app":"another-app","output":"contact-card"}`} {
				args, _ := json.Marshal(map[string]string{"command": text})
				message.ToolCalls = append(message.ToolCalls, ai.ToolCall{
					ID: fmt.Sprintf("render-%d", i), Type: "function",
					Function: ai.ToolCallFunction{Name: "bash", Arguments: string(args)},
				})
			}
		} else {
			var refused, grantRefused bool
			for _, previous := range messages {
				for _, part := range previous.Content {
					refused = refused || strings.Contains(part.Text, "scope comes from this session")
					grantRefused = grantRefused || strings.Contains(part.Text, "the agent needs a grant for this render Action")
				}
			}
			if grantRefused != expectGrantRefusal {
				return nil, fmt.Errorf("render grant refusal = %v, want %v", grantRefused, expectGrantRefusal)
			}
			if !refused {
				return nil, fmt.Errorf("scoped executor did not refuse the supplied app")
			}
			message.Content = []ai.ContentPart{{Type: "text", Text: "Saved the contact card."}}
		}
		usd := 0.02
		return &ai.Response{
			Model: "openai/gpt-4o-mini", Choices: []ai.Choice{{Message: message}},
			Usage: &ai.Usage{PromptTokens: 10, CompletionTokens: 10, Cost: &usd},
		}, nil
	})
	call := runs.AgentCall{Call: runs.Call{App: id, ID: "run-for-contact", Step: "draft", Key: "run-for-contact-1"}}
	result, err := backend.completeOutputAgent(t.Context(), app, call, "openai/gpt-4o-mini", "Help with contact cards.", "Render the supplied contact.", client, &session.SpendGuard{}, attachment)
	if err != nil || result.Output != "Saved the contact card." || result.Cost != 0.04 {
		t.Fatalf("scoped agent answer: %+v, %v", result, err)
	}
	if !session.FlushUsage() {
		t.Fatal("the session's usage ledger did not flush")
	}
	used, err := session.DailySpendUSD(session.UsageLedgerPath(), time.Now())
	if err != nil || used != result.Cost {
		t.Fatalf("session ledger = %v, want one charge per call totaling %v: %v", used, result.Cost, err)
	}
	page, err := db.Outputs(data.Scope{App: id}, call.ID, "", false)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("scoped agent outputs: %+v, %v", page, err)
	}
	agent := page.Items[0]
	if agent.Blob != person.Blob || agent.ActorKind != data.ByAI || agent.Actor != data.AgentAssigneePrefix+id {
		t.Fatalf("engine-bound agent receipt: %+v", agent)
	}
	_, bytes, err := db.OutputFile(data.Scope{App: id}, agent.ID)
	if err != nil || string(bytes) != "Contact\nSam\n" {
		t.Fatalf("agent file: %q, %v", bytes, err)
	}
	if _, _, err := db.OutputFile(data.Scope{App: "another-app"}, agent.ID); err == nil {
		t.Fatal("another app read the output by id")
	}
	grant.Allowed = false
	if err := backend.GrantAppAgent(grant); err != nil {
		t.Fatal(err)
	}
	calls, expectGrantRefusal = 0, true
	if _, err := backend.completeOutputAgent(t.Context(), app, call, "openai/gpt-4o-mini", "Help with contact cards.", "Render the supplied contact.", client, &session.SpendGuard{}, attachment); err != nil {
		t.Fatalf("revoked grant on replay: %v", err)
	}
}

func TestAppOutputCheckUsesOnlySampleRecords(t *testing.T) {
	root := resolvedTempDir(t)
	id := keepOutputApp(t, root)
	t.Setenv("CODEAF_HOME", root)
	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { backend.Close() })
	def, err := schema.Load(filepath.Join(root, "apps", id, "current"))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := backend.AppDefinitions().NewDraft(id)
	if err != nil {
		t.Fatal(err)
	}
	sampleRows := map[string][]map[string]any{"contact": {{"name": "Example contact"}}}
	if err := backend.seedBuildSample(draft.ID(), def.Files[schema.AppFile], sampleRows); err != nil {
		t.Fatal(err)
	}
	if err := backend.CheckDraftRun(t.Context(), draft.ID(), "contact-card", def); err != nil {
		t.Fatal(err)
	}
	scope := data.Scope{App: id}
	sample, err := backend.dataStore(draft.ID())
	if err != nil {
		t.Fatal(err)
	}
	page, err := sample.Outputs(scope, "", "", false)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("sample file receipts: %+v, %v", page, err)
	}
	_, content, err := sample.OutputFile(scope, page.Items[0].ID)
	if err != nil || string(content) != "Contact\nExample contact\n" {
		t.Fatalf("sample file content: %q, %v", content, err)
	}
	live, err := backend.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	page, err = live.Outputs(scope, "", "", false)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("live file receipts after Check: %+v, %v", page, err)
	}
}

func TestAppOutputInteractiveAgentRequiresRenderGrant(t *testing.T) {
	root := resolvedTempDir(t)
	id := keepOutputApp(t, root)
	t.Setenv("CODEAF_HOME", root)
	b := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { b.Close() })
	db, err := b.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	created, err := db.Create(data.Scope{App: id}, "contact", []map[string]any{{"name": "Sam"}})
	if err != nil {
		t.Fatal(err)
	}
	const action = "render/contact-card"
	raw, err := json.Marshal(map[string]any{"app": id, "params": map[string]any{
		"action": action, "args": map[string]string{"record": created.Rows[0].ID()},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var refused *appwire.Error
	if _, err := b.runAppCLICommand(t.Context(), "invoke", raw); !errors.As(err, &refused) || refused.Code != appwire.CodeForbidden {
		t.Fatalf("ungranted output invocation = %v", err)
	}
	if err := b.GrantAppAgent(appwire.AppAgentGrant{App: id, Agent: id, Group: "actions", ID: action, Allowed: true}); err != nil {
		t.Fatal(err)
	}
	result, err := b.runAppCLICommand(t.Context(), "invoke", raw)
	if err != nil {
		t.Fatal(err)
	}
	var file data.Output
	if err := json.Unmarshal(result.(json.RawMessage), &file); err != nil || file.ActorKind != data.ByAI || file.Action != "contact-card" {
		t.Fatalf("interactive output receipt: %+v, %v", file, err)
	}
}

func TestAppSourceWriteCheckUsesFakeConnector(t *testing.T) {
	t.Setenv("CODEAF_HOME", resolvedTempDir(t))
	b := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { b.Close() })
	const draft = "source-check"
	app := []byte(`{"id":"source-check","name":"Source check","records":{"deal":{"fields":{"name":{"type":"text"}}}},"capabilities":{"records":{"deal":"r"},"tools":["connections.crm.stage"]}}`)
	if err := b.seedBuildSample(draft, app, map[string][]map[string]any{
		"deal": {{"name": "Example deal"}},
	}); err != nil {
		t.Fatal(err)
	}
	def := schema.Definition{Files: map[string][]byte{
		schema.AppFile: app,
		"runs/review.yaml": []byte(`run: review
steps:
  - id: stage
    tool: connections.crm.stage
    as: me
    gate: {approval: proof}
    args: {id: "00000000000000000000000001", set: {stage: Qualified}}
`),
	}}
	if err := b.CheckDraftRun(t.Context(), draft, "review", def); err != nil {
		t.Fatalf("Check could not exercise the declared source write: %v", err)
	}
}

func TestAppBeforeRecordCheckReplaysSampleDate(t *testing.T) {
	t.Setenv("CODEAF_HOME", resolvedTempDir(t))
	b := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { b.Close() })
	const draft = "before-record-check"
	app := []byte(`{"id":"before-record-check","name":"Before record check","kit":2,"records":{"meeting":{"fields":{"name":{"type":"text"},"starts_at":{"type":"datetime"},"external":{"type":"bool"},"prepared":{"type":"bool"}}}},"capabilities":{"records":{"meeting":"rw"}},"triggers":[{"id":"prep","run":"prep","on":"record.before","type":"meeting","at":"starts_at","lead":"30m","when":"external == true"}]}`)
	if err := b.seedBuildSample(draft, app, map[string][]map[string]any{
		"meeting": {
			{"name": "Internal check", "starts_at": "2026-11-27T09:00:00-05:00", "external": false},
			{"name": "Founder evaluation", "starts_at": "2026-11-27T10:00:00-05:00", "external": true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	def := schema.Definition{Files: map[string][]byte{
		schema.AppFile: app,
		"runs/prep.yaml": []byte(`run: prep
steps:
  - id: prepare
    records.update: {type: meeting, id: $record.id, set: {prepared: true}}
`),
	}}
	if err := b.CheckDraftRun(t.Context(), draft, "prep", def); err != nil {
		t.Fatal(err)
	}
	db, err := b.dataStore(draft)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := db.Query(data.Scope{App: draft}, "meeting", "", "", 10)
	if err != nil || len(batch.Rows) != 2 {
		t.Fatalf("sample rows = %v, err %v", batch, err)
	}
	for _, row := range batch.Rows {
		if (row["prepared"] == true) != (row["external"] == true) {
			t.Fatalf("unexpected prepared value on sample row %v", row)
		}
	}
}
