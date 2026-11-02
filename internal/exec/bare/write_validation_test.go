package bare

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBareWriteRejectsInvalidContentWithoutChangingFiles(t *testing.T) {
	root := t.TempDir()
	tools := AllTools(root)
	var write Tool
	for _, tool := range tools {
		if tool.Name == "write" {
			write = tool
		}
	}
	for _, fields := range []string{`"text":"intended content"`, `"content":null`, `"content":42`, `"content":true`, `"content":{}`, `"content":[]`} {
		t.Run(fields, func(t *testing.T) {
			for _, path := range []string{"new/file.txt", "existing.txt"} {
				t.Run(path, func(t *testing.T) {
					full := filepath.Join(root, path)
					if path == "existing.txt" {
						if err := os.WriteFile(full, []byte("keep these bytes"), 0o644); err != nil {
							t.Fatal(err)
						}
					}
					content, isError, err := write.Execute(context.Background(), json.RawMessage(`{"path":"`+path+`",`+fields+`}`))
					result := struct {
						Content string
						IsError bool
					}{content, isError}
					if err != nil {
						t.Fatal(err)
					}
					if !result.IsError || !strings.Contains(result.Content, "content") {
						t.Errorf("invalid content must report an actionable error, got %+v", result)
					}
					if path == "existing.txt" {
						body, err := os.ReadFile(full)
						if err != nil || string(body) != "keep these bytes" {
							t.Errorf("existing file changed: %q, %v", body, err)
						}
					} else if _, err := os.Stat(filepath.Join(root, "new")); !os.IsNotExist(err) {
						t.Errorf("malformed write created its parent directory: %v", err)
					}
				})
			}
		})
	}
}

func TestBareWriteAcceptsExplicitEmptyContent(t *testing.T) {
	root := t.TempDir()
	for _, exists := range []bool{false, true} {
		if exists {
			mustWriteFile(t, root, "file.txt", "old bytes")
		}
		var write Tool
		for _, tool := range AllTools(root) {
			if tool.Name == "write" {
				write = tool
			}
		}
		text, isError, err := write.Execute(context.Background(), json.RawMessage(`{"path":"file.txt","content":""}`))
		if err != nil || isError {
			t.Fatalf("empty content rejected: %s, %v", text, err)
		}
		body, err := os.ReadFile(filepath.Join(root, "file.txt"))
		if err != nil || len(body) != 0 {
			t.Fatalf("empty write = %q, %v", body, err)
		}
	}
}
