package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
)

func TestReadInjectsNestedInstructionsOncePerSession(t *testing.T) {
	// Contract: a nearby AGENTS.md is appended as the TS-identical reminder
	// and its path is recorded; persisted read metadata suppresses it later.
	workDir := t.TempDir()
	nested := filepath.Join(workDir, "src", "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	rules := filepath.Join(workDir, "src", "AGENTS.md")
	target := filepath.Join(nested, "main.go")
	if err := os.WriteFile(rules, []byte("keep the nested contract"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := New(workDir)
	input := json.RawMessage(`{"filePath":"src/pkg/main.go"}`)
	first, err := registry.Execute(context.Background(), steploop.ToolCall{
		ID: "call_1", Name: "read", Input: input,
		SessionID: "ses_1", MessageID: "msg_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantReminder := "\n\n<system-reminder>\nInstructions from: " + rules +
		"\nkeep the nested contract\n</system-reminder>"
	if !strings.HasSuffix(first.Output, wantReminder) {
		t.Fatalf("first output missing reminder:\n%s", first.Output)
	}
	var metadata readMetadata
	if err := json.Unmarshal(first.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(metadata.Loaded, []string{rules}) {
		t.Fatalf("loaded = %#v, want %q", metadata.Loaded, rules)
	}

	history := []msgmodel.WithParts{{Parts: msgmodel.Parts{msgmodel.ToolPart{
		PartBase: msgmodel.PartBase{ID: "part_1", SessionID: "ses_1", MessageID: "msg_1"},
		CallID:   "call_1",
		Tool:     "read",
		State: msgmodel.CompletedToolState(
			msgmodel.RawObject(input), first.Output, first.Title, first.Metadata, 1, 2, nil,
		),
	}}}}
	secondCtx := steploop.WithToolMessages(context.Background(), history)
	second, err := registry.Execute(secondCtx, steploop.ToolCall{
		ID: "call_2", Name: "read", Input: input,
		SessionID: "ses_1", MessageID: "msg_2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(second.Output, "<system-reminder>") {
		t.Fatalf("second output duplicated reminder:\n%s", second.Output)
	}
	if err := json.Unmarshal(second.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	if len(metadata.Loaded) != 0 {
		t.Fatalf("second loaded = %#v, want empty", metadata.Loaded)
	}
}

func TestReadInstructionClaimsClearAfterAssistantTurn(t *testing.T) {
	// Round-2 claim-lifecycle contract: an in-flight claim suppresses duplicate
	// reads within one assistant turn, but clearing that turn permits a retry
	// when no completed read metadata recorded the path.
	workDir := t.TempDir()
	nested := filepath.Join(workDir, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, nested, "AGENTS.md", "retry this rule")
	writeTestFile(t, nested, "main.go", "package main")
	registry := New(workDir)
	call := steploop.ToolCall{
		ID: "call", Name: "read", Input: json.RawMessage(`{"filePath":"src/main.go"}`),
		SessionID: "session", MessageID: "assistant",
	}
	first, err := registry.Execute(context.Background(), call)
	if err != nil || !strings.Contains(first.Output, "retry this rule") {
		t.Fatalf("first read = (%q, %v)", first.Output, err)
	}
	second, err := registry.Execute(context.Background(), call)
	if err != nil || strings.Contains(second.Output, "retry this rule") {
		t.Fatalf("same-turn read = (%q, %v)", second.Output, err)
	}
	registry.ClearInstructionClaims(context.Background(), call.MessageID)
	third, err := registry.Execute(context.Background(), call)
	if err != nil || !strings.Contains(third.Output, "retry this rule") {
		t.Fatalf("post-clear retry = (%q, %v)", third.Output, err)
	}
}

func TestReadNestedInstructionPrecedenceAndNoMatch(t *testing.T) {
	// Contract: nested lookup prefers AGENTS.md, then CLAUDE.md, then the
	// deprecated CONTEXT.md; a file with no nearby instructions is unchanged.
	tests := []struct {
		name    string
		files   map[string]string
		want    string
		content string
	}{
		{
			name: "agents", want: "AGENTS.md", content: "agents wins",
			files: map[string]string{
				"AGENTS.md": "agents wins", "CLAUDE.md": "claude loses", "CONTEXT.md": "context loses",
			},
		},
		{
			name: "claude", want: "CLAUDE.md", content: "claude wins",
			files: map[string]string{"CLAUDE.md": "claude wins", "CONTEXT.md": "context loses"},
		},
		{
			name: "context", want: "CONTEXT.md", content: "context remains",
			files: map[string]string{"CONTEXT.md": "context remains"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workDir := t.TempDir()
			nested := filepath.Join(workDir, "nested")
			if err := os.Mkdir(nested, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, content := range test.files {
				if err := os.WriteFile(filepath.Join(nested, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(nested, "target.txt"), []byte("target"), 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := execute(t, New(workDir), "read", map[string]any{"filePath": "nested/target.txt"})
			if err != nil {
				t.Fatal(err)
			}
			want := "Instructions from: " + filepath.Join(nested, test.want) + "\n" + test.content
			if !strings.Contains(result.Output, want) {
				t.Fatalf("output missing %q:\n%s", want, result.Output)
			}
			for name := range test.files {
				if name != test.want && strings.Contains(result.Output, filepath.Join(nested, name)) {
					t.Fatalf("output included lower-precedence %s:\n%s", name, result.Output)
				}
			}
		})
	}

	workDir := t.TempDir()
	writeTestFile(t, workDir, "plain.txt", "plain")
	result, err := execute(t, New(workDir), "read", map[string]any{"filePath": "plain.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Output, "<system-reminder>") || !strings.HasSuffix(result.Output, "\n</content>") {
		t.Fatalf("no-match output changed:\n%s", result.Output)
	}
}

func TestReadDirectoryParity(t *testing.T) {
	workDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workDir, "beta"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, workDir, "alpha.txt", "alpha")
	if err := os.Symlink("beta", filepath.Join(workDir, "linked")); err != nil {
		t.Fatal(err)
	}

	result, err := execute(t, New(workDir), "read", map[string]any{
		"filePath": ".",
		"limit":    2,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "<path>" + workDir + "</path>\n<type>directory</type>\n<entries>\n" +
		"alpha.txt\nbeta/\n\n" +
		"(Showing 2 of 3 entries. Use 'offset' parameter to read beyond entry 3)\n</entries>"
	if result.Output != want {
		t.Fatalf("Output:\n%s\nwant:\n%s", result.Output, want)
	}
	if result.Title != "." {
		t.Fatalf("Title = %q", result.Title)
	}
	if got := string(result.Metadata); got != `{"preview":"alpha.txt\nbeta/","truncated":true,"loaded":[]}` {
		t.Fatalf("Metadata = %s", got)
	}
}

func TestReadLongLineAndByteCapParity(t *testing.T) {
	workDir := t.TempDir()
	long := strings.Repeat("界", maxLineLength+1)
	writeTestFile(t, workDir, "long.txt", long+"\nend\n")

	result, err := execute(t, New(workDir), "read", map[string]any{"filePath": "long.txt"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	wantLine := strings.Repeat("界", maxLineLength) + maxLineSuffix
	if !strings.Contains(result.Output, "1: "+wantLine+"\n2: end") {
		t.Fatalf("long line was not truncated by UTF-16 length")
	}

	lines := make([]string, 40)
	for i := range lines {
		lines[i] = strings.Repeat("x", maxLineLength)
	}
	writeTestFile(t, workDir, "cap.txt", strings.Join(lines, "\n"))
	result, err = execute(t, New(workDir), "read", map[string]any{"filePath": "cap.txt"})
	if err != nil {
		t.Fatalf("Execute capped: %v", err)
	}
	if !strings.Contains(result.Output, "(Output capped at 50 KB. Showing lines 1-25. Use offset=26 to continue.)") {
		t.Fatalf("cap message missing from %q", result.Output[len(result.Output)-160:])
	}
}

func TestReadImagePDFAndBinaryParity(t *testing.T) {
	workDir := t.TempDir()
	png := append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, []byte("payload")...)
	if err := os.WriteFile(filepath.Join(workDir, "misnamed.dat"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := execute(t, New(workDir), "read", map[string]any{"filePath": "misnamed.dat"})
	if err != nil {
		t.Fatalf("read image: %v", err)
	}
	if result.Output != "Image read successfully" || result.Attachments == nil || len(*result.Attachments) != 1 {
		t.Fatalf("image result = %#v", result)
	}
	attachment := (*result.Attachments)[0]
	wantURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	if attachment.Mime != "image/png" || attachment.URL != wantURL {
		t.Fatalf("attachment = %#v", attachment)
	}

	if err := os.WriteFile(filepath.Join(workDir, "binary.bin"), []byte("plain text"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = execute(t, New(workDir), "read", map[string]any{"filePath": "binary.bin"})
	if err == nil || err.Error() != "Cannot read binary file: "+filepath.Join(workDir, "binary.bin") {
		t.Fatalf("binary error = %v", err)
	}
}

func TestReadOffsetAndZeroLimitParity(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, workDir, "sample.txt", "one\ntwo\n")
	registry := New(workDir)

	result, err := execute(t, registry, "read", map[string]any{
		"filePath": "sample.txt",
		"offset":   0,
		"limit":    0,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "(Showing lines 1-0 of 2. Use offset=1 to continue.)") {
		t.Fatalf("zero-limit output = %q", result.Output)
	}

	_, err = execute(t, registry, "read", map[string]any{
		"filePath": "sample.txt",
		"offset":   3,
	})
	if err == nil || err.Error() != "Offset 3 is out of range for this file (2 lines)" {
		t.Fatalf("offset error = %v", err)
	}
}

func TestReadDescriptionParity(t *testing.T) {
	if !strings.HasPrefix(readDescription, "Read a file or directory from the local filesystem.") {
		t.Fatalf("read description changed: %q", readDescription)
	}
	if !strings.HasSuffix(readDescription, "return them as file attachments.\n") {
		t.Fatalf("read description changed: %q", readDescription)
	}
}
