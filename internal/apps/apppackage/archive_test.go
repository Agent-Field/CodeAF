package apppackage_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
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

func TestUnsignedPublisherCannotClaimCodeafIdentity(t *testing.T) {
	app := `{"id":"docs","name":"Docs","version":1,"records":{"document":{"fields":{"title":{"type":"text"}}}}}`
	files := map[string]string{"app.json": app}
	if _, err := apppackage.Pack(files, nil, apppackage.PackOptions{Publisher: "codeaf"}); err == nil {
		t.Fatal("ordinary pack can impersonate the built-in publisher")
	}
}

func TestReleaseSignatureAuthenticatesTheArchive(t *testing.T) {
	archives, err := apppackage.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range archives {
		if len(a.Signature) != ed25519.SignatureSize {
			t.Fatalf("%s has no release signature", a.Manifest.Name)
		}
		a.Signature[0] ^= 1
		raw, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := apppackage.Read(bytes.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "release signature") {
			t.Fatalf("tampered %s signature: %v", a.Manifest.Name, err)
		}
	}
}

func TestShippedDefinitionsMatchSignedReleases(t *testing.T) {
	archives, err := apppackage.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range archives {
		def, err := schema.Load(filepath.Join("definitions", strings.ToLower(a.Manifest.Name)))
		if err != nil {
			t.Fatal(err)
		}
		if len(def.Files) != len(a.Files) {
			t.Fatalf("%s must be signed again after editing its definition files", a.Manifest.Name)
		}
		for name, raw := range def.Files {
			if !bytes.Equal(raw, a.Files[name]) {
				t.Fatalf("%s %s differs from its signed release", a.Manifest.Name, name)
			}
		}
	}
}
