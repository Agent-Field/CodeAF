package tool

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fallbackRegistry builds a Registry pinned to the in-process searcher, so
// these tests exercise the no-ripgrep path regardless of what is on PATH.
func fallbackRegistry(t *testing.T, workDir string) *Registry {
	t.Helper()
	registry := New(workDir)
	registry.rg = builtinSearchRunner{}
	return registry
}

func initTestRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		command := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, output)
		}
	}
}

// Contract 1: rg stays authoritative when it is installed, and only a genuine
// absence selects the fallback.
func TestPickRipgrepRunnerPrefersRealRipgrep(t *testing.T) {
	stub := t.TempDir()
	name := "rg"
	if os.PathListSeparator == ';' {
		name = "rg.exe"
	}
	if err := os.WriteFile(filepath.Join(stub, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", stub)
	if _, ok := pickRipgrepRunner().(execRipgrepRunner); !ok {
		t.Fatalf("rg on PATH must select the real runner, got %T", pickRipgrepRunner())
	}

	t.Setenv("PATH", t.TempDir())
	if _, ok := pickRipgrepRunner().(builtinSearchRunner); !ok {
		t.Fatalf("rg absent must select the fallback, got %T", pickRipgrepRunner())
	}
}

// Contract 2: glob still finds files, hidden ones included, non-matching
// extensions excluded.
func TestFallbackGlobFindsHiddenAndFiltersByPattern(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, workDir, "visible.go", "package visible")
	writeTestFile(t, workDir, ".hidden.go", "package hidden")
	writeTestFile(t, workDir, "ignored.txt", "text")

	result, err := execute(t, fallbackRegistry(t, workDir), "glob", map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, name := range []string{"visible.go", ".hidden.go"} {
		if !strings.Contains(result.Output, filepath.Join(workDir, name)) {
			t.Fatalf("%s missing from %q", name, result.Output)
		}
	}
	if strings.Contains(result.Output, "ignored.txt") {
		t.Fatalf("non-matching file leaked into %q", result.Output)
	}
}

// Contract 2 (cont.): a pattern with a separator is anchored at the root and
// matches nested paths rather than bare basenames.
func TestFallbackGlobNestedPattern(t *testing.T) {
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, "src", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, workDir, filepath.Join("src", "a.go"), "package a")
	writeTestFile(t, workDir, filepath.Join("src", "inner", "b.go"), "package b")
	writeTestFile(t, workDir, "top.go", "package top")

	result, err := execute(t, fallbackRegistry(t, workDir), "glob", map[string]any{"pattern": "src/**/*.go"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, name := range []string{filepath.Join("src", "a.go"), filepath.Join("src", "inner", "b.go")} {
		if !strings.Contains(result.Output, filepath.Join(workDir, name)) {
			t.Fatalf("%s missing from %q", name, result.Output)
		}
	}
	if strings.Contains(result.Output, filepath.Join(workDir, "top.go")) {
		t.Fatalf("unanchored match leaked into %q", result.Output)
	}
}

// Contract 3: grep reports path, 1-based line number and the line text.
func TestFallbackGrepReportsPathLineAndText(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, workDir, "one.go", "first\nneedle here\nthird\n")

	result, err := execute(t, fallbackRegistry(t, workDir), "grep", map[string]any{"pattern": "needle"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "Found 1 matches") {
		t.Fatalf("missing count in %q", result.Output)
	}
	if !strings.Contains(result.Output, filepath.Join(workDir, "one.go")+":") {
		t.Fatalf("missing path in %q", result.Output)
	}
	if !strings.Contains(result.Output, "  Line 2: needle here") {
		t.Fatalf("missing line 2 in %q", result.Output)
	}
}

// Contract 4: .git contents are never searched, whatever the pattern.
func TestFallbackSkipsGitDirectory(t *testing.T) {
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, ".git", "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, workDir, filepath.Join(".git", "config"), "needle")
	writeTestFile(t, workDir, filepath.Join(".git", "refs", "heads", "main"), "needle")
	writeTestFile(t, workDir, "kept.txt", "needle")

	result, err := execute(t, fallbackRegistry(t, workDir), "grep", map[string]any{"pattern": "needle"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(result.Output, ".git") {
		t.Fatalf(".git leaked into %q", result.Output)
	}
	if !strings.Contains(result.Output, "kept.txt") {
		t.Fatalf("kept.txt missing from %q", result.Output)
	}
}

// Contract 5: inside a work tree .gitignore is honoured, and untracked files
// that are not ignored are still searched.
func TestFallbackHonoursGitignoreButKeepsUntracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	workDir := t.TempDir()
	initTestRepo(t, workDir)
	writeTestFile(t, workDir, ".gitignore", "ignored.go\n")
	writeTestFile(t, workDir, "ignored.go", "needle")
	writeTestFile(t, workDir, "untracked.go", "needle")

	result, err := execute(t, fallbackRegistry(t, workDir), "grep", map[string]any{"pattern": "needle"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(result.Output, "ignored.go") {
		t.Fatalf("gitignored file searched: %q", result.Output)
	}
	if !strings.Contains(result.Output, "untracked.go") {
		t.Fatalf("untracked file missing from %q", result.Output)
	}
}

// Contract 6: the include filter narrows grep to matching files.
func TestFallbackGrepHonoursInclude(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, workDir, "code.go", "needle")
	writeTestFile(t, workDir, "notes.txt", "needle")

	result, err := execute(t, fallbackRegistry(t, workDir), "grep", map[string]any{
		"pattern": "needle", "include": "*.go",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "code.go") || strings.Contains(result.Output, "notes.txt") {
		t.Fatalf("include not applied: %q", result.Output)
	}
}

// Contract 6 (cont.): a path pointing at one file searches only that file.
func TestFallbackGrepSingleFilePath(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, workDir, "one.txt", "needle")
	writeTestFile(t, workDir, "two.txt", "needle")

	result, err := execute(t, fallbackRegistry(t, workDir), "grep", map[string]any{
		"pattern": "needle", "path": "one.txt",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "one.txt") || strings.Contains(result.Output, "two.txt") {
		t.Fatalf("path not honoured: %q", result.Output)
	}
}

// Contract 7: an empty result is "No files found", not an error, for both tools.
func TestFallbackEmptyResults(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, workDir, "one.go", "content")
	registry := fallbackRegistry(t, workDir)

	grepResult, err := execute(t, registry, "grep", map[string]any{"pattern": "absent"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if grepResult.Output != "No files found" {
		t.Fatalf("grep output = %q", grepResult.Output)
	}
	globResult, err := execute(t, registry, "glob", map[string]any{"pattern": "*.rs"})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if globResult.Output != "No files found" {
		t.Fatalf("glob output = %q", globResult.Output)
	}
}

// Contract 8: binary files are skipped rather than dumped into the output.
func TestFallbackSkipsBinaryFiles(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(workDir, "blob.bin"), []byte("needle\x00needle"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, workDir, "text.txt", "needle")

	result, err := execute(t, fallbackRegistry(t, workDir), "grep", map[string]any{"pattern": "needle"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(result.Output, "blob.bin") {
		t.Fatalf("binary file searched: %q", result.Output)
	}
	if !strings.Contains(result.Output, "text.txt") {
		t.Fatalf("text file missing from %q", result.Output)
	}
}

// Contract 9: an invalid pattern surfaces as an error, not a panic.
func TestFallbackInvalidPatternErrors(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, workDir, "one.go", "content")

	if _, err := execute(t, fallbackRegistry(t, workDir), "grep", map[string]any{
		"pattern": "([unclosed",
	}); err == nil {
		t.Fatal("invalid pattern must error")
	}
}

// Contract 10: the fallback plugs in below the shared caps, so the 100-result
// limit and its notice still apply.
func TestFallbackGlobTruncatesAtLimit(t *testing.T) {
	workDir := t.TempDir()
	for index := 0; index < globResultLimit+10; index++ {
		writeTestFile(t, workDir, "file"+itoa(index)+".go", "package p")
	}

	result, err := execute(t, fallbackRegistry(t, workDir), "glob", map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "Results are truncated") {
		t.Fatalf("missing truncation notice in %q", result.Output)
	}
	if string(result.Metadata) != `{"count":100,"truncated":true}` {
		t.Fatalf("Metadata = %s", result.Metadata)
	}
}

// The exit-code contract the two callers depend on: glob rejects anything but
// 0/1, so an unreadable root must never surface as rg's exit 2 there.
func TestFallbackMissingRootExitCodes(t *testing.T) {
	workDir := t.TempDir()
	runner := builtinSearchRunner{}

	globResult, err := runner.Run(t.Context(), workDir, []string{
		"--no-config", "--files", "--glob=!.git/*", "--hidden", "--glob=*.go", "absent",
	})
	if err != nil {
		t.Fatalf("glob run: %v", err)
	}
	if globResult.code != 1 {
		t.Fatalf("glob code = %d, want 1", globResult.code)
	}

	grepResult, err := runner.Run(t.Context(), workDir, []string{
		"--no-config", "--json", "--hidden", "--glob=!.git/*", "--no-messages", "--", "needle", "absent",
	})
	if err != nil {
		t.Fatalf("grep run: %v", err)
	}
	if grepResult.code != 2 {
		t.Fatalf("grep code = %d, want 2", grepResult.code)
	}
}
