package main

import (
	"encoding/json"
	"path/filepath"

	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestBuiltinAppsUseThePackageAndRecordDoors(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	b := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { b.Close() })
	defs := b.AppDefinitions()
	wire := wireOver(t, b)
	var shelf struct{ Apps []store.Summary }
	wire.must("apps.list", struct{}{}, &shelf)
	if len(shelf.Apps) != 4 {
		t.Fatalf("default apps: %v", shelf.Apps)
	}
	var kit struct {
		Templates []apppackage.Manifest `json:"app_templates"`
	}
	wire.must("apps.kit", struct{}{}, &kit)
	names := []string{}
	for _, entry := range kit.Templates {
		names = append(names, entry.Name)
		var live store.Definition
		wire.must("apps.get", map[string]string{"app": entry.ID}, &live)
		if live.Meta.Name != entry.Name || live.Meta.Lineage.Source.Hash != entry.Hash {
			t.Fatalf("installed %s has lineage %+v", entry.Name, live.Meta.Lineage)
		}
		app, _, err := b.appAssistantDefinition(entry.ID)
		if err != nil || app.Agent.Name != entry.Name+" agent" {
			t.Fatalf("installed %s has no named assistant: %v", entry.Name, err)
		}
	}
	if !slices.Equal(names, []string{"Canvas", "Docs", "Sheets", "Slides"}) {
		t.Fatalf("Kit templates: %v", names)
	}

	docs := kit.Templates[1].ID
	scope := appwire.DataScope{App: docs}
	var created appwire.DataWriteResult
	wire.must("data.create", appwire.DataCreateParams{DataScope: scope, Type: "docs_document", Set: map[string]any{
		"title": "Review notes", "body": "Confirm the numbers.",
	}}, &created)
	id := created.Rows[0]["id"]
	var screen struct {
		CustomParts map[string]schema.PartProgram `json:"custom_parts"`
	}
	wire.must("apps.screen", map[string]string{"app": docs, "screen": "open"}, &screen)
	if screen.CustomParts["custom/document-body"].Elements[0].Field != "body" {
		t.Fatalf("document screen: %+v", screen)
	}
	var output struct {
		ID string
	}
	wire.must("apps.render", map[string]any{"app": docs, "output": "docx", "record": id}, &output)
	var file appwire.AppOutputFileResult
	wire.must("apps.output_file", map[string]string{"app": docs, "id": output.ID}, &file)
	if !strings.HasPrefix(string(file.Content), "PK") || len(file.File.Sources) != 1 || file.File.Sources[0].ID != id {
		t.Fatalf("export from wrong record: %+v", file.File)
	}

	call := func(verb string, params any) json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		reply, err := b.runAppCLICommand(t.Context(), verb, raw)
		if err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
		result, err := json.Marshal(reply)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	wire.must("data.create", appwire.DataCreateParams{DataScope: scope, Type: "docs_document", Set: map[string]any{
		"title": "Agent draft",
	}}, &created)
	id = created.Rows[0]["id"]
	call("agent_grant", appwire.AppAgentGrant{App: docs, Agent: docs, Group: "actions", ID: "edit-body", Allowed: true})
	edited := call("invoke", map[string]any{"app": docs, "params": map[string]any{
		"action": "edit-body", "expected_rev": created.Rows[0]["_rev"],
		"args": map[string]any{"record": id, "body": "The figures agree."},
	}})
	var history appwire.DataHistoryResult
	wire.must("data.history", map[string]any{"app": docs, "type": "docs_document", "id": id}, &history)
	found := false
	for _, entry := range history.History {
		found = found || entry.Actor == "Docs agent" && entry.ByKind == "ai" && entry.After["body"] == "The figures agree."
	}
	if !found {
		t.Fatalf("record edit has no agent provenance: edit %s, history %+v", edited, history.History)
	}
	call("agent_grant", appwire.AppAgentGrant{App: docs, Agent: docs, Group: "data", ID: "library/listed", Allowed: true})
	read := call("query", map[string]any{"app": docs, "params": map[string]string{"view": "library/listed"}})
	if !strings.Contains(string(read), "The figures agree.") {
		t.Fatalf("agent view did not return the kept edit: %s", read)
	}
	call("agent_grant", appwire.AppAgentGrant{App: docs, Agent: docs, Group: "actions", ID: "render/share", Allowed: true})
	shared := call("invoke", map[string]any{"app": docs, "params": map[string]any{
		"action": "render/share", "args": map[string]any{"record": id},
	}})
	if err := json.Unmarshal(shared, &output); err != nil || output.ID == "" {
		t.Fatalf("share action receipt = %s, error = %v", shared, err)
	}
	wire.must("apps.output_file", map[string]string{"app": docs, "id": output.ID}, &file)
	if string(file.Content) != "Agent draft\n\nThe figures agree." || file.File.Actor != "agent:"+docs || file.File.ActorKind != "ai" ||
		len(file.File.Sources) != 1 || file.File.Sources[0].ID != id {
		t.Fatalf("share output from wrong record: %+v", file.File)
	}

	var fork builder.Proposal
	wire.must("apps.fork", map[string]string{"app": docs}, &fork)
	if len(fork.SummaryLines) != 1 || !strings.HasSuffix(fork.SummaryLines[0], "from codeaf.") {
		t.Fatalf("fork card lost its publisher: %v", fork.SummaryLines)
	}
	var kept struct{ Meta store.Meta }
	wire.must("apps.apply", map[string]string{"draft": fork.Draft}, &kept)
	if kept.Meta.Lineage.ForkedFrom.App != docs {
		t.Fatalf("fork lost the original app: %+v", kept.Meta.Lineage)
	}
	var forkRow struct{ Row map[string]any }
	wire.must("data.get", map[string]any{"app": kept.Meta.ID, "type": "docs_document", "id": id}, &forkRow)
	if forkRow.Row["body"] != "The figures agree." {
		t.Fatalf("fork did not retain access to the document: %+v", forkRow.Row)
	}
	wire.must("apps.archive", map[string]string{"app": docs}, nil)
	if err := defs.InstallBuiltins(); err != nil {
		t.Fatal(err)
	}
	var archive store.Definition
	wire.must("apps.get", map[string]string{"app": docs}, &archive)
	if !archive.Meta.Archived || archive.Meta.Version != 1 {
		t.Fatalf("default installation changed an archived app: %+v", archive.Meta)
	}
}

func TestBuiltinPublisherUpdatePreservesTheLocalVersionUntilApply(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	b := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { b.Close() })
	wire := wireOver(t, b)
	old, err := filepath.Abs("testdata/firstparty/docs-1.0.0.codeaf-app")
	if err != nil {
		t.Fatal(err)
	}
	var install builder.Proposal
	wire.must("apps.install", map[string]string{"path": old}, &install)
	var kept struct{ Meta store.Meta }
	wire.must("apps.apply", map[string]string{"draft": install.Draft}, &kept)

	defs := b.AppDefinitions()
	live, err := defs.Get(kept.Meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	files := live.Files()
	files["screens/library.json"] = strings.Replace(files["screens/library.json"], `"title": "Docs"`, `"title": "Team documents"`, 1)
	draft, err := defs.NewDraft(live.Meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer defs.Discard(draft)
	def := schema.Definition{Files: map[string][]byte{}}
	for name, content := range files {
		def.Files[name] = []byte(content)
	}
	local, err := defs.PublishBuild(draft.ID(), live.Meta.ID, live.Meta.Version, def, "Name our document library")
	if err != nil {
		t.Fatal(err)
	}
	var fork builder.Proposal
	wire.must("apps.fork", map[string]string{"app": local.ID}, &fork)
	wire.must("apps.apply", map[string]string{"draft": fork.Draft}, &kept)
	if kept.Meta.ID == local.ID || kept.Meta.Lineage.Source.Version != "1.0.0" || kept.Meta.Lineage.ForkedFrom.App != local.ID {
		t.Fatalf("fork lost the local copy or its publisher baseline: %+v", kept.Meta)
	}
	local = kept.Meta

	var update builder.Proposal
	wire.must("apps.update", map[string]string{"app": local.ID, "package": install.Package.Manifest.ID}, &update)
	if update.Source != "package_update" || len(update.Package.Conflicts) != 1 || update.Checkpoint != "" {
		t.Fatalf("expected a pending title conflict: %+v", update.Package)
	}
	var current store.Definition
	wire.must("apps.get", map[string]string{"app": local.ID}, &current)
	if current.Meta.Version != local.Version {
		t.Fatal("publisher update became live before Apply")
	}
	wire.must("apps.resolve_package", map[string]any{
		"draft": update.Draft, "proposal_id": update.ID,
		"choices": map[string]string{update.Package.Conflicts[0].ID: "local"},
	}, &update)
	wire.must("apps.apply", map[string]string{"draft": update.Draft}, &kept)
	got, err := defs.Get(local.ID)
	if err != nil {
		t.Fatal(err)
	}
	screen, _ := got.Screen("library")
	if got.Meta.Lineage.Source.Version != "1.1.0" || !strings.Contains(string(screen), "Team documents") || !slices.Contains(got.Runs, "duplicate") {
		t.Fatalf("kept definition has wrong publisher or changes: %+v", got.Meta)
	}
}

func TestBuiltinEditorExportsUseTheKeptRecord(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	b := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { b.Close() })
	if err := b.AppDefinitions().InstallBuiltins(); err != nil {
		t.Fatal(err)
	}
	wire := wireOver(t, b)
	archives, err := apppackage.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	for _, archive := range archives {
		if archive.Manifest.Name == "Docs" {
			continue
		}
		t.Run(archive.Manifest.Name, func(t *testing.T) {
			app, err := schema.ParseApp(archive.Files[schema.AppFile])
			if err != nil {
				t.Fatal(err)
			}
			var typ string
			for typ = range app.Records {
				break
			}
			body := "First line\nSecond line"
			if archive.Manifest.Name == "Sheets" {
				body = "Item\tAmount\nMaterials\t=5*5"
			}
			var row appwire.DataWriteResult
			wire.must("data.create", appwire.DataCreateParams{
				DataScope: appwire.DataScope{App: app.ID}, Type: typ,
				Set: map[string]any{"title": "Proof", "body": body},
			}, &row)
			if archive.Manifest.Name == "Sheets" {
				var window appwire.WorksheetPage
				wire.must("apps.worksheet_window", map[string]any{
					"app": app.ID, "id": row.Rows[0]["id"], "type": typ, "field": "body",
					"row": 1, "column": 1, "rows": 1, "columns": 1,
				}, &window)
				cell := window.Rows[0].Cells[0]
				if cell.Value != float64(25) || cell.Formula != "=5*5" || window.Revision != "1" {
					t.Fatalf("worksheet cell = %+v, revision %s", cell, window.Revision)
				}
			}
			for name, template := range app.Outputs {
				params := map[string]any{"app": app.ID, "output": name}
				if template.Record != "" {
					params["record"] = row.Rows[0]["id"]
				}
				var receipt struct{ ID string }
				wire.must("apps.render", params, &receipt)
				var file appwire.AppOutputFileResult
				wire.must("apps.output_file", map[string]string{"app": app.ID, "id": receipt.ID}, &file)
				want := map[string]string{"pdf": "%PDF", "pptx": "PK", "csv": "Item,Amount\nMaterials,25\n", "txt": "Proof\n\nFirst line\nSecond line"}
				if !strings.HasPrefix(string(file.Content), want[template.Format]) ||
					len(file.File.Sources) != 1 || file.File.Sources[0].ID != row.Rows[0]["id"] {
					t.Fatalf("%s exported the wrong source or format: %+v", name, file.File)
				}
			}
		})
	}
}
