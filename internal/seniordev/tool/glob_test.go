//go:build !windows

package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type recordingRipgrep struct {
	cwd    string
	args   []string
	result ripgrepResult
	err    error
}

func (r *recordingRipgrep) Run(_ context.Context, cwd string, args []string) (ripgrepResult, error) {
	r.cwd = cwd
	r.args = append([]string(nil), args...)
	return r.result, r.err
}

func TestGlobInvocationSortAndOutput(t *testing.T) {
	workDir := t.TempDir()
	for _, name := range []string{"older.go", "newer.go"} {
		writeTestFile(t, workDir, name, name)
	}
	old := time.Unix(100, 0)
	newer := time.Unix(200, 0)
	if err := os.Chtimes(filepath.Join(workDir, "older.go"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(workDir, "newer.go"), newer, newer); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRipgrep{result: ripgrepResult{
		stdout: []byte("./older.go\n./newer.go\n"),
		code:   0,
	}}
	registry := New(workDir)
	registry.rg = runner

	result, err := execute(t, registry, "glob", map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	wantArgs := []string{
		"--no-config",
		"--files",
		"--glob=!.git/*",
		"--hidden",
		"--glob=*.go",
		".",
	}
	if runner.cwd != workDir || !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("invocation cwd=%q args=%q", runner.cwd, runner.args)
	}
	want := filepath.Join(workDir, "newer.go") + "\n" + filepath.Join(workDir, "older.go")
	if result.Output != want || result.Title != "." {
		t.Fatalf("result = %#v, want output %q", result, want)
	}
	if string(result.Metadata) != `{"count":2,"truncated":false}` {
		t.Fatalf("Metadata = %s", result.Metadata)
	}
}

func TestGlobTruncatesBeforeMtimeSort(t *testing.T) {
	workDir := t.TempDir()
	var stdout strings.Builder
	for i := 0; i < 101; i++ {
		name := "file-" + itoa(i) + ".txt"
		writeTestFile(t, workDir, name, "")
		stdout.WriteString(name)
		stdout.WriteByte('\n')
	}
	// The 101st item is newest but must be discarded before sorting.
	newest := filepath.Join(workDir, "file-100.txt")
	when := time.Unix(500, 0)
	if err := os.Chtimes(newest, when, when); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRipgrep{result: ripgrepResult{stdout: []byte(stdout.String()), code: 0}}
	registry := New(workDir)
	registry.rg = runner
	result, err := execute(t, registry, "glob", map[string]any{"pattern": "*.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Output, newest) {
		t.Fatalf("101st item survived pre-sort truncation")
	}
	if !strings.HasSuffix(result.Output, "(Results are truncated: showing first 100 results. Consider using a more specific path or pattern.)") {
		t.Fatalf("Output suffix = %q", result.Output[len(result.Output)-140:])
	}
	if string(result.Metadata) != `{"count":100,"truncated":true}` {
		t.Fatalf("Metadata = %s", result.Metadata)
	}
}

func TestGlobRealRipgrep(t *testing.T) {
	// This exercises the real rg invocation and skips when rg is not installed;
	// the built-in searcher is covered by ripgrep_fallback_test.go.
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not on PATH")
	}
	workDir := t.TempDir()
	writeTestFile(t, workDir, "visible.go", "package visible")
	writeTestFile(t, workDir, ".hidden.go", "package hidden")
	writeTestFile(t, workDir, "ignored.txt", "text")
	result, err := execute(t, New(workDir), "glob", map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, name := range []string{"visible.go", ".hidden.go"} {
		if !strings.Contains(result.Output, filepath.Join(workDir, name)) {
			t.Fatalf("%s missing from %q", name, result.Output)
		}
	}
}
