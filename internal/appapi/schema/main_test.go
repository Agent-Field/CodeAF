package main

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageRoutePublishesABinaryResponse(t *testing.T) {
	documents, err := schemaDocuments()
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Routes []struct {
			Method   string `json:"method"`
			Path     string `json:"path"`
			Response string `json:"response"`
		} `json:"x-routes"`
	}
	if err := json.Unmarshal(documents["file.json"], &file); err != nil {
		t.Fatal(err)
	}
	for _, route := range file.Routes {
		if route.Method == "GET" && route.Path == "/app/v1/agents/{id}/image" {
			if route.Response != "binary" {
				t.Fatalf("image route advertises a JSON shape: %s", route.Response)
			}
			return
		}
	}
	t.Fatal("the image route is absent from the published contract")
}

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

// Reading the generation directive makes this freshness law part of test-laws
// and therefore the light gate. Publishing schemas alone must not bypass it.
func TestPublishedTypescriptIsFresh(t *testing.T) {
	tree, err := parser.ParseFile(token.NewFileSet(), "../contract.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, group := range tree.Comments {
		for _, comment := range group.List {
			if strings.HasPrefix(comment.Text, "//go:generate ") && strings.Contains(comment.Text, "go run ./schema") {
				found = strings.Contains(comment.Text, "-out ../../docs/design/app-v2/schema") && strings.Contains(comment.Text, "-typescript ../../docs/design/app-v2/wire.ts")
			}
		}
	}
	if !found {
		t.Fatal("go generate must publish schemas and TypeScript together")
	}
	want, err := typescriptDocument()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../../docs/design/app-v2/wire.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("published wire.ts is stale; run go generate ./internal/appapi")
	}
}
