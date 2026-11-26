package main

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

const packageApp = `{"id":"crm","name":"CRM","version":1,"kit":0,"records":{"contact":{"title":"name","common":"person","fields":{"name":{"type":"text","required":true}}}},"capabilities":{"records":{"contact":"r"}}}`
const packagePlan = `{"ask":"Keep a CRM","data":[{"id":"contact","line":"Contact records"}],"screens":[{"id":"main","line":"Contacts"}]}`
const packageScreen = `{"screen":"main","title":"Contacts","body":{"part":"Stack","id":"main"}}`

func stagePackageFixture(t *testing.T, defs *store.Store, app string, base int, files map[string]string) store.Meta {
	t.Helper()
	draft, err := defs.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	defer defs.Discard(draft)
	def := schema.Definition{Files: map[string][]byte{schema.PlanFile: []byte(packagePlan)}}
	for name, raw := range files {
		def.Files[name] = []byte(raw)
	}
	if strings.Contains(files[schema.AppFile], `"people":`) {
		def.Files[schema.PlanFile] = []byte(strings.Replace(packagePlan, `"contact"`, `"people"`, 1))
	}
	meta, err := defs.PublishBuild(draft.ID(), app, base, def, "Save CRM definition")
	if err != nil {
		t.Fatal(err)
	}
	return meta
}

func TestAppPackageInstallUpdateAndForkOnWire(t *testing.T) {
	root := t.TempDir()
	var v1, v2, v3 string
	t.Run("pack publisher versions", func(t *testing.T) {
		t.Setenv(home.EnvVar, filepath.Join(root, "publisher"))
		backend := newFakeBackend(newAppRecorder(), nil)
		t.Cleanup(func() { backend.Close() })
		wire := wireOver(t, backend)
		files := map[string]string{schema.AppFile: packageApp, "screens/main.json": packageScreen}
		meta := stagePackageFixture(t, backend.AppDefinitions(), "", 0, files)
		pack := func(version string) string {
			dest := filepath.Join(root, version+".codeaf-app")
			var out struct{ Manifest apppackage.Manifest }
			wire.must("apps.pack", map[string]string{"app": meta.ID, "path": dest, "publisher": "Example CRM", "version": version}, &out)
			if out.Manifest.ID != meta.ID || out.Manifest.Version != version || out.Manifest.Hash == "" {
				t.Fatalf("pack result: %+v", out)
			}
			return dest
		}
		v1 = pack("1.0.0")
		files[schema.AppFile] = strings.Replace(packageApp, `"name":{"type":"text","required":true}`, `"name":{"type":"text","required":true},"renewal":{"type":"date"}`, 1)
		files["screens/main.json"] = strings.Replace(packageScreen, "Contacts", "Publisher contacts", 1)
		meta = stagePackageFixture(t, backend.AppDefinitions(), meta.ID, meta.Version, files)
		v2 = pack("2.0.0")
		files[schema.AppFile] = strings.Replace(files[schema.AppFile], `"renewal":{"type":"date"}`, `"renewal":{"type":"date"},"region":{"type":"text"}`, 1)
		meta = stagePackageFixture(t, backend.AppDefinitions(), meta.ID, meta.Version, files)
		v3 = pack("3.0.0")
	})

	t.Run("reuse people and preserve a local edit", func(t *testing.T) {
		t.Setenv(home.EnvVar, filepath.Join(root, "receiver"))
		recorder := newAppRecorder()
		backend := newFakeBackend(recorder, nil)
		t.Cleanup(func() { backend.Close() })
		wire := wireOver(t, backend)
		defs := backend.AppDefinitions()
		peopleDefinition := strings.ReplaceAll(packageApp, "contact", "people")
		peopleDefinition = strings.Replace(peopleDefinition, `"people":"r"`, `"people":"rw"`, 1)
		people := stagePackageFixture(t, defs, "", 0, map[string]string{schema.AppFile: peopleDefinition, "screens/main.json": packageScreen})
		var added appwire.DataWriteResult
		wire.must("data.create", appwire.DataCreateParams{
			DataScope: appwire.DataScope{App: people.ID}, Type: "people", Set: map[string]any{"name": "Ada Example"},
		}, &added)
		ada := added.Rows[0]["id"]

		var install builder.Proposal
		wire.must("apps.install", map[string]string{"path": v1}, &install)
		if install.Source != "package_install" || install.Checkpoint == "" || install.Package.Bindings["contact"].Type != "people" ||
			!maps.Equal(install.Package.Permissions.Records, map[string]string{"contact": "r"}) {
			t.Fatalf("install card: %+v / %+v", install, install.Package)
		}
		var listing struct {
			Apps   []store.Summary
			Builds []builder.Summary
		}
		wire.must("apps.list", map[string]string{}, &listing)
		if len(listing.Apps) != 1 || len(listing.Builds) != 1 || listing.Builds[0].Proposal.ID != install.ID {
			t.Fatalf("install is missing from the pending builds: %+v", listing)
		}
		var applied struct{ Meta store.Meta }
		wire.must("apps.apply", map[string]string{"draft": install.Draft}, &applied)
		app := applied.Meta.ID
		if source := applied.Meta.Lineage.Source; source.Version != "1.0.0" || source.Hash != install.Package.Manifest.Hash {
			t.Fatalf("installed lineage: %+v", source)
		}
		p := appwire.DataQueryParams{DataScope: appwire.DataScope{App: app}, Type: "contact"}
		var records appwire.DataQueryResult
		wire.must("data.query", p, &records)
		if len(records.Rows) != 1 || records.Rows[0]["id"] != ada {
			t.Fatalf("installed CRM did not reuse the existing person: %+v", records)
		}
		wire.mustFail("data.create", appwire.DataCreateParams{DataScope: p.DataScope, Type: p.Type, Set: map[string]any{"name": "Extra"}}, appwire.CodeNotAllowed)

		local := applied.Meta
		def, err := defs.Get(app)
		if err != nil {
			t.Fatal(err)
		}
		files := def.Files()
		files["screens/main.json"] = strings.Replace(files["screens/main.json"], "Contacts", "My contacts", 1)
		local = stagePackageFixture(t, defs, app, local.Version, files)
		var update builder.Proposal
		wire.must("apps.update", map[string]string{"app": app, "path": v2}, &update)
		recorder.waitFor(t, time.Second, func(event string, value any) bool {
			card, ok := value.(builder.Proposal)
			return event == appwire.EventAppChangeProposed && ok && card.ID == update.ID
		})
		var state builder.State
		wire.must("apps.build_state", map[string]string{"draft": update.Draft}, &state)
		if state.Proposal == nil || state.Proposal.ID != update.ID || state.State != builder.NeedsYou {
			t.Fatalf("update is missing from the durable job: %+v", state)
		}
		if len(update.Package.Conflicts) != 1 || update.Package.Conflicts[0].ID != "screens/main.json#/title" {
			t.Fatalf("update conflicts: %+v", update.Package.Conflicts)
		}
		wire.mustFail("apps.apply", map[string]string{"draft": update.Draft}, appwire.CodeConflict)
		if live, _ := defs.Get(app); live.Meta.Version != local.Version || strings.Contains(string(live.App), "renewal") {
			t.Fatalf("proposal leaked into live app: %+v", live.Meta)
		}
		choices := map[string]any{"draft": update.Draft, "proposal_id": update.ID, "choices": map[string]string{"screens/main.json#/title": "local"}}
		wire.must("apps.resolve_package", choices, &update)
		if update.Checkpoint == "" {
			t.Fatal("the resolved card was never checked")
		}
		wire.must("apps.apply", map[string]string{"draft": update.Draft}, &applied)
		after, err := defs.Get(app)
		if err != nil {
			t.Fatal(err)
		}
		if after.Meta.Lineage.Source.Version != "2.0.0" || !strings.Contains(string(after.App), "renewal") ||
			!strings.Contains(after.Files()["screens/main.json"], "My contacts") {
			t.Fatalf("resolved update: %+v", after)
		}

		update = builder.Proposal{}
		wire.must("apps.update", map[string]string{"app": app, "path": v3}, &update)
		if len(update.Package.Conflicts) != 0 || update.Checkpoint == "" {
			t.Fatalf("clean update: %+v / %+v", update, update.Package)
		}
		wire.must("apps.apply", map[string]string{"draft": update.Draft}, &applied)
		after, err = defs.Get(app)
		if err != nil || !strings.Contains(string(after.App), "region") || !strings.Contains(after.Files()["screens/main.json"], "My contacts") {
			t.Fatalf("clean update did not keep local title: %+v, %v", after, err)
		}

		var fork builder.Proposal
		wire.must("apps.fork", map[string]string{"app": app}, &fork)
		var copied struct{ Meta store.Meta }
		wire.must("apps.apply", map[string]string{"draft": fork.Draft}, &copied)
		var got struct{ Lineage store.Lineage }
		wire.must("apps.lineage", map[string]string{"app": copied.Meta.ID}, &got)
		if copied.Meta.ID == app || got.Lineage.Source != nil || got.Lineage.ForkedFrom.App != app ||
			got.Lineage.ForkedFrom.Version != applied.Meta.Version || got.Lineage.ForkedFrom.Source.Version != "3.0.0" {
			t.Fatalf("fork lineage: %s %+v", copied.Meta.ID, got.Lineage)
		}
	})

	t.Run("reject incompatible mapped field", func(t *testing.T) {
		t.Setenv(home.EnvVar, filepath.Join(root, "incompatible"))
		backend := newFakeBackend(newAppRecorder(), nil)
		t.Cleanup(func() { backend.Close() })
		wire := wireOver(t, backend)
		people := strings.ReplaceAll(packageApp, "contact", "people")
		people = strings.Replace(people, `"name":{"type":"text"`, `"name":{"type":"longtext"`, 1)
		stagePackageFixture(t, backend.AppDefinitions(), "", 0, map[string]string{schema.AppFile: people, "screens/main.json": packageScreen})
		message := wire.mustFail("apps.install", map[string]string{"path": v1}, appwire.CodeBadParams)
		if !strings.Contains(message, "/records/contact/fields/name") || !strings.Contains(message, "people") {
			t.Fatalf("missing precise field finding: %s", message)
		}
	})
}

func TestAppPackageCheckRefusesUnusableExamples(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { backend.Close() })
	wire := wireOver(t, backend)
	path := filepath.Join(t.TempDir(), "bad.codeaf-app")
	raw, err := apppackage.Pack(map[string]string{
		schema.AppFile: packageApp, "screens/main.json": packageScreen,
		schema.PlanFile:   packagePlan,
		schema.SampleFile: `{"records":{"contact":[{"id":"00000000000000000000000001"}]}}`,
	}, nil, apppackage.PackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"path": path}
	wire.mustFail("apps.install", params, appwire.CodeBadParams)
	msg, _ := json.Marshal(params)
	if _, err := appwire.RunAppCommand(backend, "install", msg); err == nil || !strings.Contains(err.Error(), "example") {
		t.Fatalf("CLI Check result: %v", err)
	}
}

func TestAppPackRefusesExamplesThatFailInAnEmptyProfile(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { backend.Close() })
	wire := wireOver(t, backend)
	meta := stagePackageFixture(t, backend.AppDefinitions(), "", 0, map[string]string{
		schema.AppFile: packageApp, "screens/main.json": packageScreen,
		schema.SampleFile: `{"records":{"contact":[{"id":"00000000000000000000000001"}]}}`,
	})
	dest := filepath.Join(t.TempDir(), "uninstallable.codeaf-app")
	message := wire.mustFail("apps.pack", map[string]string{"app": meta.ID, "path": dest}, appwire.CodeBadParams)
	if !strings.Contains(message, "name is required") {
		t.Fatalf("expected the missing required field in the Pack finding: %s", message)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("Pack created an unusable file: %v", err)
	}
}
