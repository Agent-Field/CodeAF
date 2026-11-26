package apppackage_test

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
)

func TestPackIsDeterministicAndChecksItsManifest(t *testing.T) {
	app := `{"id":"crm","name":"CRM","version":1,"kit":0,"records":{"contact":{"fields":{"name":{"type":"text","required":true}}}},"capabilities":{"records":{"contact":"r"}}}`
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
