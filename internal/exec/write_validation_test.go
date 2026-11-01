package exec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRejectsInvalidTextWithoutChangingFiles(t *testing.T) {
	root := t.TempDir()
	workspace, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	tools := NewToolbox(workspace, "1", nil)
	for _, fields := range []string{`"content":"intended content"`, `"text":null`, `"text":42`, `"text":true`, `"text":{}`, `"text":[]`} {
		t.Run(fields, func(t *testing.T) {
			for _, path := range []string{"new/file.txt", "existing.txt"} {
				t.Run(path, func(t *testing.T) {
					full := filepath.Join(root, path)
					if path == "existing.txt" {
						if err := os.WriteFile(full, []byte("keep these bytes"), 0o644); err != nil {
							t.Fatal(err)
						}
					}
					result := tools.Execute(context.Background(), "write", `{"path":"`+path+`",`+fields+`}`)
					if !result.IsError || !strings.Contains(result.Content, "text") {
						t.Errorf("missing text must report an actionable error, got %+v", result)
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

func TestWriteAcceptsExplicitText(t *testing.T) {
	for _, text := range []string{"nonempty text", ""} {
		t.Run(text, func(t *testing.T) {
			root := t.TempDir()
			workspace, err := NewWorkspace(root)
			if err != nil {
				t.Fatal(err)
			}
			tools := NewToolbox(workspace, "1", nil)
			full := filepath.Join(root, "file.txt")
			if err := os.WriteFile(full, []byte("old bytes"), 0o644); err != nil {
				t.Fatal(err)
			}
			result := tools.Execute(context.Background(), "write", `{"path":"file.txt","text":"`+text+`"}`)
			if result.IsError {
				t.Fatalf("valid write failed: %+v", result)
			}
			body, err := os.ReadFile(full)
			if err != nil || string(body) != text {
				t.Fatalf("write = %q, %v; want %q", body, err, text)
			}
		})
	}
}

func TestWriteRequiresPath(t *testing.T) {
	workspace, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result := NewToolbox(workspace, "1", nil).Execute(context.Background(), "write", `{"text":"hello"}`)
	if !result.IsError || !strings.Contains(result.Content, "path") {
		t.Fatalf("missing path = %+v", result)
	}
}
