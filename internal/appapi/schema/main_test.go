package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedSchemasAreFresh(t *testing.T) {
	documents, err := schemaDocuments()
	if err != nil {
		t.Fatal(err)
	}
	directory := "../../../docs/design/app-v2/schema"
	files, err := filepath.Glob(filepath.Join(directory, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(documents) {
		t.Fatalf("schema file count %d, want %d; run go generate ./internal/appapi", len(files), len(documents))
	}
	for name, want := range documents {
		got, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is stale; run go generate ./internal/appapi", name)
		}
	}
}
