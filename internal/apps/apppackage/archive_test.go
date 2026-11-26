package apppackage_test

import (
	"bytes"
	"encoding/base64"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
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
