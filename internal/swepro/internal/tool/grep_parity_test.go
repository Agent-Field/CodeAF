package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func grepEvent(t *testing.T, path string, line int, text string) string {
	t.Helper()
	value := map[string]any{
		"type": "match",
		"data": map[string]any{
			"path":        map[string]any{"text": path},
			"lines":       map[string]any{"text": text},
			"line_number": line,
		},
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestGrepInvocationGroupingAndPartialParity(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, workDir, "old.go", "needle")
	writeTestFile(t, workDir, "new.go", "needle")
	old := time.Unix(100, 0)
	newer := time.Unix(200, 0)
	if err := os.Chtimes(filepath.Join(workDir, "old.go"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(workDir, "new.go"), newer, newer); err != nil {
		t.Fatal(err)
	}
	stdout := strings.Join([]string{
		grepEvent(t, "./old.go", 1, "old needle\n"),
		grepEvent(t, "./new.go", 2, "new needle\n"),
	}, "\n") + "\n"
	runner := &recordingRipgrep{result: ripgrepResult{stdout: []byte(stdout), code: 2}}
	registry := New(workDir)
	registry.rg = runner

	result, err := execute(t, registry, "grep", map[string]any{
		"pattern": "needle",
		"include": "*.go",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	wantArgs := []string{
		"--no-config",
		"--json",
		"--hidden",
		"--glob=!.git/*",
		"--no-messages",
		"--glob=*.go",
		"--",
		"needle",
		".",
	}
	if runner.cwd != workDir || !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("invocation cwd=%q args=%q", runner.cwd, runner.args)
	}
	want := "Found 2 matches\n" +
		filepath.Join(workDir, "new.go") + ":\n" +
		"  Line 2: new needle\n\n" +
		"\n" +
		filepath.Join(workDir, "old.go") + ":\n" +
		"  Line 1: old needle\n\n" +
		"\n(Some paths were inaccessible and skipped)"
	if result.Output != want {
		t.Fatalf("Output:\n%q\nwant:\n%q", result.Output, want)
	}
	if string(result.Metadata) != `{"matches":2,"truncated":false}` {
		t.Fatalf("Metadata = %s", result.Metadata)
	}
}

func TestGrepFilePathInvocationAndLongLineParity(t *testing.T) {
	workDir := t.TempDir()
	long := strings.Repeat("界", maxGrepLineLength+1) + "\n"
	writeTestFile(t, workDir, "one.txt", long)
	runner := &recordingRipgrep{result: ripgrepResult{
		stdout: []byte(grepEvent(t, "one.txt", 1, long) + "\n"),
		code:   0,
	}}
	registry := New(workDir)
	registry.rg = runner
	result, err := execute(t, registry, "grep", map[string]any{
		"pattern": "界+",
		"path":    "one.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if runner.cwd != workDir || !reflect.DeepEqual(runner.args[len(runner.args)-3:], []string{"--", "界+", "one.txt"}) {
		t.Fatalf("invocation cwd=%q args=%q", runner.cwd, runner.args)
	}
	wantText := strings.Repeat("界", maxGrepLineLength) + "..."
	if !strings.Contains(result.Output, "  Line 1: "+wantText) {
		t.Fatalf("long output missing: %q", result.Output[len(result.Output)-100:])
	}
}

func TestGrepEmptyAndNoMatchesParity(t *testing.T) {
	workDir := t.TempDir()
	registry := New(workDir)
	_, err := execute(t, registry, "grep", map[string]any{"pattern": ""})
	if err == nil || err.Error() != "pattern is required" {
		t.Fatalf("empty pattern error = %v", err)
	}
	runner := &recordingRipgrep{result: ripgrepResult{code: 1}}
	registry.rg = runner
	result, err := execute(t, registry, "grep", map[string]any{"pattern": "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "No files found" || string(result.Metadata) != `{"matches":0,"truncated":false}` {
		t.Fatalf("result = %#v", result)
	}
}
