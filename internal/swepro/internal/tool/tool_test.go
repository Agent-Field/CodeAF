package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/outputoffload"
)

type failingBashOutputSink struct{}

func (failingBashOutputSink) WriteOutput(string, string) (string, error) {
	return "", errors.New("spill failed")
}

func TestBash(t *testing.T) {
	registry := New(t.TempDir())

	t.Run("echo roundtrip", func(t *testing.T) {
		result, err := execute(t, registry, "bash", map[string]any{"command": "echo roundtrip"})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.Output != "roundtrip\n" {
			t.Fatalf("Output = %q", result.Output)
		}
	})

	t.Run("non-zero exit", func(t *testing.T) {
		result, err := execute(t, registry, "bash", map[string]any{"command": "printf failure; exit 7"})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !strings.Contains(result.Output, "failure\nexit status 7") {
			t.Fatalf("Output = %q", result.Output)
		}
	})

	t.Run("timeout kills process group", func(t *testing.T) {
		start := time.Now()
		result, err := execute(t, registry, "bash", map[string]any{
			"command":    "sleep 30",
			"timeout_ms": 200,
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Fatalf("timeout took %v", elapsed)
		}
		if !strings.Contains(result.Output, "command timed out after 200ms") {
			t.Fatalf("Output = %q", result.Output)
		}
	})

	t.Run("large output spills full content", func(t *testing.T) {
		// Validation contract 1: a large bash result keeps the existing preview
		// and makes the complete output recoverable by tool-call ID.
		workDir := t.TempDir()
		registry := New(workDir)
		input, _ := json.Marshal(map[string]any{
			"command": "head -c 30001 /dev/zero | tr '\\0' x",
		})
		result, err := registry.Execute(context.Background(), steploop.ToolCall{
			ID: "call-large-output", Name: "bash", Input: input, SessionID: "ses_large",
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		parts := strings.Split(result.Output, "\n\nThe tool call succeeded but the output was truncated. ")
		if len(parts) != 2 {
			t.Fatalf("missing recovery message in %q", result.Output)
		}
		preview := parts[0]
		if len(preview) != maxBashOutputBytes {
			t.Fatalf("preview length = %d, want %d", len(preview), maxBashOutputBytes)
		}
		if !strings.Contains(preview, "[... 29 bytes truncated ...]") {
			t.Fatalf("missing truncation marker in %q", preview)
		}
		if !strings.HasPrefix(preview, "xxx") || !strings.HasSuffix(preview, "xxx") {
			t.Fatalf("truncation did not preserve head and tail")
		}
		wantPath := filepath.Join(workDir, ".codeaf", "tool-output", "ses_large", "call-large-output.log")
		if !strings.Contains(result.Output, "Full output saved to: "+wantPath+"\n") {
			t.Fatalf("recovery path missing from %q", result.Output)
		}
		full, err := os.ReadFile(wantPath)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", wantPath, err)
		}
		if string(full) != strings.Repeat("x", maxBashOutputBytes+1) {
			t.Fatalf("saved output length = %d, want %d complete bytes", len(full), maxBashOutputBytes+1)
		}
	})

	t.Run("large output reports spill failure", func(t *testing.T) {
		// Finding 6: a failed spill keeps the bash preview and appends the
		// offloader's could-not-save notice.
		previous := bashOffloader
		bashOffloader = outputoffload.Offloader{Sink: failingBashOutputSink{}}
		t.Cleanup(func() { bashOffloader = previous })
		registry := New(t.TempDir())
		input, _ := json.Marshal(map[string]any{
			"command": "head -c 30001 /dev/zero | tr '\\0' x",
		})
		result, err := registry.Execute(context.Background(), steploop.ToolCall{
			ID: "call-spill-failure", Name: "bash", Input: input,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result.Output, "full output could not be saved") {
			t.Fatalf("spill failure notice missing from %q", result.Output)
		}
	})

	t.Run("small output remains inline", func(t *testing.T) {
		// Validation contract 2: output at or below the cap is unchanged and
		// creates no spill directory.
		workDir := t.TempDir()
		registry := New(workDir)
		input := json.RawMessage(`{"command":"printf unchanged"}`)
		result, err := registry.Execute(context.Background(), steploop.ToolCall{
			ID: "call-small-output", Name: "bash", Input: input,
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.Output != "unchanged" {
			t.Fatalf("Output = %q", result.Output)
		}
		_, err = os.Stat(filepath.Join(workDir, ".codeaf", "tool-output"))
		if !os.IsNotExist(err) {
			t.Fatalf("spill directory exists or stat failed: %v", err)
		}
	})
}

func TestRegistryExecuteNonBashBehaviorUnchanged(t *testing.T) {
	// Validation contract 3: threading call metadata does not alter non-bash dispatch.
	workDir := t.TempDir()
	input := json.RawMessage(`{"filePath":"result.txt","content":"unchanged"}`)
	result, err := New(workDir).Execute(context.Background(), steploop.ToolCall{
		ID: "call-write", Name: "write", Input: input,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output != "Wrote file successfully." {
		t.Fatalf("Output = %q", result.Output)
	}
	assertTestFile(t, workDir, "result.txt", "unchanged")
}

func TestRead(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "sample.txt"), []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := New(workDir)

	result, err := execute(t, registry, "read", map[string]any{"filePath": "sample.txt"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "<path>" + filepath.Join(workDir, "sample.txt") + "</path>\n<type>file</type>\n<content>\n" +
		"1: alpha\n2: beta\n3: gamma\n\n(End of file - total 3 lines)\n</content>"
	if result.Output != want {
		t.Fatalf("Output = %q, want %q", result.Output, want)
	}

	result, err = execute(t, registry, "read", map[string]any{
		"filePath": "sample.txt",
		"offset":   2,
		"limit":    1,
	})
	if err != nil {
		t.Fatalf("Execute offset/limit: %v", err)
	}
	want = "<path>" + filepath.Join(workDir, "sample.txt") + "</path>\n<type>file</type>\n<content>\n" +
		"2: beta\n\n(Showing lines 2-2 of 3. Use offset=3 to continue.)\n</content>"
	if result.Output != want {
		t.Fatalf("offset/limit output = %q", result.Output)
	}

	_, err = execute(t, registry, "read", map[string]any{"filePath": "missing.txt"})
	if err == nil || err.Error() != "File not found: "+filepath.Join(workDir, "missing.txt") {
		t.Fatalf("missing file error = %v", err)
	}
}

func TestWrite(t *testing.T) {
	workDir := t.TempDir()
	registry := New(workDir)
	result, err := execute(t, registry, "write", map[string]any{
		"filePath": "nested/deep/file.txt",
		"content":  "roundtrip",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output != "Wrote file successfully." {
		t.Fatalf("Output = %q", result.Output)
	}
	content, err := os.ReadFile(filepath.Join(workDir, "nested", "deep", "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "roundtrip" {
		t.Fatalf("content = %q", content)
	}
}

func TestEdit(t *testing.T) {
	t.Run("unique replace", func(t *testing.T) {
		workDir := t.TempDir()
		writeTestFile(t, workDir, "file.txt", "before middle after")
		registry := New(workDir)
		result, err := execute(t, registry, "edit", map[string]any{
			"filePath":  "file.txt",
			"oldString": "middle",
			"newString": "changed",
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.Output != "Edit applied successfully." {
			t.Fatalf("Output = %q", result.Output)
		}
		assertTestFile(t, workDir, "file.txt", "before changed after")
	})

	t.Run("not found", func(t *testing.T) {
		workDir := t.TempDir()
		writeTestFile(t, workDir, "file.txt", "content")
		registry := New(workDir)
		_, err := execute(t, registry, "edit", map[string]any{
			"filePath":  "file.txt",
			"oldString": "missing",
			"newString": "changed",
		})
		if err == nil || err.Error() != "Could not find oldString in the file. It must match exactly, including whitespace, indentation, and line endings." {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("not unique", func(t *testing.T) {
		workDir := t.TempDir()
		writeTestFile(t, workDir, "file.txt", "same and same")
		registry := New(workDir)
		_, err := execute(t, registry, "edit", map[string]any{
			"filePath":  "file.txt",
			"oldString": "same",
			"newString": "changed",
		})
		if err == nil || err.Error() != "Found multiple matches for oldString. Provide more surrounding context to make the match unique." {
			t.Fatalf("error = %v", err)
		}
		assertTestFile(t, workDir, "file.txt", "same and same")
	})

	t.Run("replace all", func(t *testing.T) {
		workDir := t.TempDir()
		writeTestFile(t, workDir, "file.txt", "same and same")
		registry := New(workDir)
		result, err := execute(t, registry, "edit", map[string]any{
			"filePath":   "file.txt",
			"oldString":  "same",
			"newString":  "changed",
			"replaceAll": true,
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.Output != "Edit applied successfully." {
			t.Fatalf("Output = %q", result.Output)
		}
		assertTestFile(t, workDir, "file.txt", "changed and changed")
	})
}

func TestPathsCannotEscapeWorkspace(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "workspace")
	if err := os.Mkdir(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	absoluteOutside := filepath.Join(root, "outside.txt")
	registry := New(workDir)

	for _, toolName := range []string{"read", "write", "edit"} {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			for _, path := range []string{"../outside.txt", absoluteOutside} {
				input := map[string]any{"path": path}
				switch toolName {
				case "read":
					delete(input, "path")
					input["filePath"] = path
				case "write":
					delete(input, "path")
					input["filePath"] = path
					input["content"] = "blocked"
				case "edit":
					delete(input, "path")
					input["filePath"] = path
					input["oldString"] = "old"
					input["newString"] = "new"
				}
				_, err := execute(t, registry, toolName, input)
				if err == nil || err.Error() != "path escapes workspace: "+path {
					t.Fatalf("path %q error = %v", path, err)
				}
			}
		})
	}
}

func TestDefinitions(t *testing.T) {
	definitions := New(t.TempDir()).Definitions()
	if len(definitions) != 11 {
		t.Fatalf("len(Definitions) = %d", len(definitions))
	}

	var names []string
	for _, definition := range definitions {
		names = append(names, definition.Provider.Name)
		if definition.Provider.Type != "function" {
			t.Errorf("%s type = %q", definition.Provider.Name, definition.Provider.Type)
		}
		var schema map[string]any
		if err := json.Unmarshal(definition.Provider.InputSchema, &schema); err != nil {
			t.Errorf("%s schema: %v", definition.Provider.Name, err)
		}
		if schema["type"] != "object" {
			t.Errorf("%s schema type = %v", definition.Provider.Name, schema["type"])
		}
	}
	if want := []string{"bash", "read", "glob", "grep", "edit", "write", "task", "webfetch", "plandb", "websearch", "apply_patch"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
}

func TestValidationAndUnknownTool(t *testing.T) {
	definitions := New(t.TempDir()).Definitions()
	if err := definitions[0].Validate(json.RawMessage(`{"timeout_ms":200}`)); err == nil || !strings.Contains(err.Error(), `missing required field "command"`) {
		t.Fatalf("missing field error = %v", err)
	}
	if err := definitions[0].Validate(json.RawMessage(`{"command":7}`)); err == nil || !strings.Contains(err.Error(), "cannot unmarshal number") {
		t.Fatalf("wrong type error = %v", err)
	}
	if err := definitions[0].Validate(json.RawMessage(`{"command":null}`)); err == nil || !strings.Contains(err.Error(), "must not be null") {
		t.Fatalf("null field error = %v", err)
	}
	if err := definitions[0].Validate(json.RawMessage(`{"command":"true","extra":1}`)); err == nil || !strings.Contains(err.Error(), `unknown field "extra"`) {
		t.Fatalf("unknown field error = %v", err)
	}

	_, err := New(t.TempDir()).Execute(context.Background(), steploop.ToolCall{Name: "missing", Input: json.RawMessage(`{}`)})
	if err == nil || err.Error() != "unknown tool: missing" {
		t.Fatalf("unknown tool error = %v", err)
	}
}

func execute(t *testing.T, registry *Registry, name string, input any) (steploop.ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return registry.Execute(context.Background(), steploop.ToolCall{Name: name, Input: raw})
}

func writeTestFile(t *testing.T, workDir, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(workDir, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertTestFile(t *testing.T, workDir, path, want string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(workDir, path))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != want {
		t.Fatalf("content = %q, want %q", content, want)
	}
}

func TestExecuteHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(t.TempDir()).Execute(ctx, steploop.ToolCall{
		Name:  "bash",
		Input: json.RawMessage(`{"command":"sleep 30"}`),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
