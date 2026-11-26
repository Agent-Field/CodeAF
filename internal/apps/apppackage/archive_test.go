package apppackage_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestPackIsDeterministicAndChecksItsManifest(t *testing.T) {
	app := `{"id":"crm","name":"CRM","version":1,"kit":0,"records":{"contact":{"common":"person","fields":{"name":{"type":"text","required":true}}}},"capabilities":{"records":{"contact":"r"}}}`
	screen := `{"screen":"main","title":"Contacts","body":{"part":"Stack"}}`
	input := apppackage.PackOptions{Publisher: "example", Version: "1.0.0"}
	first, err := apppackage.Pack(map[string]string{"app.json": app, "screens/main.json": screen}, nil, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := apppackage.Pack(map[string]string{"screens/main.json": screen, "app.json": app}, nil, input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("same files produced different packages")
	}
	pkg, err := apppackage.Read(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.Entities["contact"].Common != "person" || !pkg.Manifest.Entities["contact"].Fields["name"].Required {
		t.Fatalf("contact requirements = %+v", pkg.Manifest.Entities)
	}
	if pkg.Manifest.Hash == "" || pkg.Manifest.Permissions.Records["contact"] != "r" {
		t.Fatalf("manifest = %+v", pkg.Manifest)
	}
	tampered := strings.Replace(string(first), base64.StdEncoding.EncodeToString([]byte(screen)), base64.StdEncoding.EncodeToString([]byte(strings.ReplaceAll(screen, "Contacts", "People"))), 1)
	if _, err := apppackage.Read(strings.NewReader(tampered)); err == nil {
		t.Fatal("package accepted files with a different hash")
	}
}

func TestPackDoesNotInferACommonBindingFromATypeName(t *testing.T) {
	app := `{"id":"crm","name":"CRM","version":1,"records":{"contact":{"fields":{"name":{"type":"text"}}}}}`
	raw, err := apppackage.Pack(map[string]string{"app.json": app}, nil, apppackage.PackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := apppackage.Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if common := pkg.Manifest.Entities["contact"].Common; common != "" {
		t.Fatalf("private contact was treated as a common %q", common)
	}
}

func TestPackExportsCRMCommandsRunsAndApprovalRequests(t *testing.T) {
	def, err := schema.Load("../schema/testdata/crm")
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]string, len(def.Files))
	for name, raw := range def.Files {
		files[name] = string(raw)
	}
	raw, err := apppackage.Pack(files, nil, apppackage.PackOptions{Publisher: "Example"})
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := apppackage.Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(pkg.Manifest.Actions, "command/mark-won") ||
		!slices.Contains(pkg.Manifest.Automations, "follow-up") ||
		!slices.Contains(pkg.Manifest.Permissions.Connectors["gmail"].Approve, "send") ||
		!pkg.Manifest.Sample {
		t.Fatalf("CRM package inventory: %+v", pkg.Manifest)
	}
}

func TestPackageUpdatesFollowSemVerPrecedence(t *testing.T) {
	source := func(version string) apppackage.Source {
		return apppackage.Source{ID: "crm", Publisher: "example", Version: version}
	}
	for _, tc := range []struct {
		update, previous string
		newer            bool
	}{
		{"1.0.0-rc.10", "1.0.0-rc.2", true},
		{"1.0.0", "1.0.0-rc.10", true},
		{"1.0.0+second", "1.0.0+first", false},
		{"1.0.0-rc.01", "0.9.0", false},
		{"1.0.0-1", "1.0.0-alpha", false},
	} {
		if got := apppackage.Newer(source(tc.update), source(tc.previous)); got != tc.newer {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.update, tc.previous, got, tc.newer)
		}
	}
}

func TestPackMakesReusedTypesInstallableInEitherOrder(t *testing.T) {
	types := schema.TypeSet{
		"people":    {ID: "1", Common: "person", Fields: map[string]schema.Field{"name": {Type: "text", Required: true}}},
		"companies": {ID: "2", Common: "organization", Fields: map[string]schema.Field{"name": {Type: "text", Required: true}}},
		"contact":   {ID: "3", Fields: map[string]schema.Field{"name": {Type: "text"}, "phone": {Type: "phone"}}},
	}
	archives := map[string]apppackage.Archive{}
	for _, app := range []string{"queue", "roadmap"} {
		raw, err := apppackage.Pack(map[string]string{schema.AppFile: fmt.Sprintf(`{
			"id":%q,"name":"Work","version":1,"kit":2,"records":{
				"person":{"common":"person","uses":true,"fields":{"company":{"type":"ref","to":"company"}}},
				"company":{"common":"organization","uses":true,"fields":{"website":{"type":"url"}}},
				"contact":{"uses":true,"fields":{}}
			},"capabilities":{"records":{"person":"rw","company":"rw","contact":"r"}}
		}`, app)}, types, apppackage.PackOptions{})
		if err != nil {
			t.Fatal(err)
		}
		archives[app], err = apppackage.Read(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, first := range []string{"queue", "roadmap"} {
		t.Run(first, func(t *testing.T) {
			store, err := data.Open(filepath.Join(t.TempDir(), "data.db"), data.Options{NoRefresh: true})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			second := "queue"
			if first == second {
				second = "roadmap"
			}
			for _, app := range []string{first, second} {
				// An installed app can be exported again, including when
				// its original pack created the dependency locally.
				local, _, err := archives[app].Bind(nil, app, false)
				if err != nil {
					t.Fatal(err)
				}
				packed, err := apppackage.Pack(local, nil, apppackage.PackOptions{})
				if err != nil {
					t.Fatal(err)
				}
				repacked, err := apppackage.Read(bytes.NewReader(packed))
				if err != nil {
					t.Fatal(err)
				}
				files, _, err := repacked.Bind(store.TypeSet(), app, false)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.RegisterApp([]byte(files[schema.AppFile])); err != nil {
					t.Fatal(err)
				}
			}
			for _, alias := range []string{"person", "company", "contact"} {
				left, err := store.Type(data.Scope{App: first}, alias)
				if err != nil {
					t.Fatal(err)
				}
				right, err := store.Type(data.Scope{App: second}, alias)
				if err != nil || left.ID != right.ID {
					t.Fatalf("separate %s types: %+v, %+v (%v)", alias, left, right, err)
				}
			}
		})
	}
}

func TestPackRebindsReferencesFromTheSourceTypeName(t *testing.T) {
	types := schema.TypeSet{
		"contacts":      {ID: "1", Common: "person", Fields: map[string]schema.Field{"company": {Type: "ref", To: "organizations"}}},
		"organizations": {ID: "2", Common: "organization", Fields: map[string]schema.Field{"name": {Type: "text"}}},
	}
	raw, err := apppackage.Pack(map[string]string{schema.AppFile: `{"id":"crm","name":"CRM","version":1,"kit":2,
		"records":{"person":{"common":"person","uses":true,"fields":{}},"company":{"common":"organization","uses":true,"fields":{}}},
		"capabilities":{"records":{"person":"rw","company":"rw"}}}`}, types, apppackage.PackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := apppackage.Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	files, _, err := archive.Bind(nil, "crm", false)
	if err != nil {
		t.Fatal(err)
	}
	app, err := schema.ParseApp([]byte(files[schema.AppFile]))
	if err != nil || len(app.Records) != 2 || app.Records["person"].Fields["company"].To != "company" {
		t.Fatalf("reference did not follow the package alias: %+v (%v)", app.Records, err)
	}
}

func TestPackageReusesAnIdenticalSameNameType(t *testing.T) {
	raw, err := apppackage.Pack(map[string]string{schema.AppFile: `{
		"id":"desk","name":"Desk","version":1,"kit":2,
		"records":{"note":{"fields":{"title":{"type":"text","required":true}}}},
		"capabilities":{"records":{"note":"rw"}}
	}`}, nil, apppackage.PackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := apppackage.Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []schema.Field{{Type: "text", Required: true}, {Type: "longtext", Required: true}} {
		local := schema.TypeSet{"note": {ID: "1", Owner: "other", Fields: map[string]schema.Field{"title": field}}}
		files, bindings, err := archive.Bind(local, "desk", false)
		if err != nil {
			t.Fatal(err)
		}
		app, err := schema.ParseApp([]byte(files[schema.AppFile]))
		shared := field.Type == "text"
		if err != nil || app.Records["note"].Uses != shared || (bindings["note"].Type != "") != shared {
			t.Fatalf("note binding for %s: %+v, %+v (%v)", field.Type, app.Records["note"], bindings, err)
		}
	}
}
