package tool

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

func TestWriteBOMAndMetadataParity(t *testing.T) {
	workDir := t.TempDir()
	registry := New(workDir)
	path := filepath.Join(workDir, "nested", "file.txt")

	result, err := execute(t, registry, "write", map[string]any{
		"content":  "\ufefffirst",
		"filePath": "nested/file.txt",
	})
	if err != nil {
		t.Fatalf("Execute add: %v", err)
	}
	if result.Title != filepath.Join("nested", "file.txt") || result.Output != "Wrote file successfully." {
		t.Fatalf("result = %#v", result)
	}
	// Validation contract: write results expose the unified diff of the actual change.
	diffPrefix := "Index: " + path + "\n===================================================================\n--- " + path + "\n+++ " + path + "\n"
	wantMetadata := `{"diagnostics":{},"diff":` + quotedJSON(diffPrefix+"@@ -0,0 +1,1 @@\n+first\n\\ No newline at end of file\n") + `,"filepath":` + quotedJSON(path) + `,"exists":false}`
	if string(result.Metadata) != wantMetadata {
		t.Fatalf("Metadata = %s, want %s", result.Metadata, wantMetadata)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "\ufefffirst" {
		t.Fatalf("content = %q", data)
	}

	result, err = execute(t, registry, "write", map[string]any{
		"content":  "second",
		"filePath": "nested/file.txt",
	})
	if err != nil {
		t.Fatalf("Execute overwrite: %v", err)
	}
	wantMetadata = `{"diagnostics":{},"diff":` + quotedJSON(diffPrefix+"@@ -1,1 +1,1 @@\n-first\n\\ No newline at end of file\n+second\n\\ No newline at end of file\n") + `,"filepath":` + quotedJSON(path) + `,"exists":true}`
	if string(result.Metadata) != wantMetadata {
		t.Fatalf("Metadata = %s", result.Metadata)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "\ufeffsecond" {
		t.Fatalf("existing BOM was not preserved: %q", data)
	}
}

func quotedJSON(value string) string {
	data, err := jscompat.Stringify(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
