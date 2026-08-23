package bare

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requireExternalTool skips a test whose subject is a program this package
// shells out to rather than implements. grep and find are ripgrep and fd — the
// tool bodies resolve them with exec.LookPath and return pi's own "is not
// available and could not be downloaded" string when they are absent, which is
// correct behaviour and is what these tests were reporting as a failure on any
// machine without them.
//
// Quarantined this way — a skip conditioned on the real thing being missing,
// rather than an unconditional one — as part of the PR that put
// ./internal/exec/... back into the release gate: where rg and fd exist the
// assertions below still run in full, and where they do not the gate stays
// deterministic instead of failing for a reason that is not about aforge.
// Installing both on the runner so CI exercises them is the follow-up.
func requireExternalTool(t *testing.T, program string) {
	t.Helper()
	if _, err := exec.LookPath(program); err != nil {
		t.Skipf("%s is not on PATH; the tool under test shells out to it", program)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────

func mustWriteFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if dir := filepath.Dir(p); dir != "" {
		os.MkdirAll(dir, 0755)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func runTool(t *testing.T, tool Tool, args any) (text string, isError bool) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	text, isError, err2 := tool.Execute(context.Background(), raw)
	if err2 != nil {
		t.Fatalf("unexpected harness error: %v", err2)
	}
	return text, isError
}

func runToolErr(t *testing.T, tool Tool, args any) (text string, isError bool, herr error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return tool.Execute(context.Background(), raw)
}

// ── wire schema + description pinning ──────────────────────────────────────

func TestToolsOrder(t *testing.T) {
	tools := Tools(t.TempDir())
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}
	names := []string{tools[0].Name, tools[1].Name, tools[2].Name, tools[3].Name}
	want := []string{"read", "bash", "edit", "write"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("tool[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestReadSchemaVerbatim(t *testing.T) {
	tools := Tools(t.TempDir())
	if string(tools[0].Schema) != readSchemaJSON {
		t.Errorf("read schema mismatch:\ngot:  %s\nwant: %s", tools[0].Schema, readSchemaJSON)
	}
}

func TestBashSchemaVerbatim(t *testing.T) {
	tools := Tools(t.TempDir())
	if string(tools[1].Schema) != bashSchemaJSON {
		t.Errorf("bash schema mismatch:\ngot:  %s\nwant: %s", tools[1].Schema, bashSchemaJSON)
	}
}

func TestEditSchemaVerbatim(t *testing.T) {
	tools := Tools(t.TempDir())
	if string(tools[2].Schema) != editSchemaJSON {
		t.Errorf("edit schema mismatch:\ngot:  %s\nwant: %s", tools[2].Schema, editSchemaJSON)
	}
}

func TestWriteSchemaVerbatim(t *testing.T) {
	tools := Tools(t.TempDir())
	if string(tools[3].Schema) != writeSchemaJSON {
		t.Errorf("write schema mismatch:\ngot:  %s\nwant: %s", tools[3].Schema, writeSchemaJSON)
	}
}

func TestDescriptionsVerbatim(t *testing.T) {
	tools := Tools(t.TempDir())
	if tools[0].Description != readDescription {
		t.Errorf("read description mismatch")
	}
	if tools[1].Description != bashDescription {
		t.Errorf("bash description mismatch")
	}
	if tools[2].Description != editDescription {
		t.Errorf("edit description mismatch")
	}
	if tools[3].Description != writeDescription {
		t.Errorf("write description mismatch")
	}
}

// ── read: basic read, no prefix in body ────────────────────────────────────

func TestReadBasicNoPrefix(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "line one\nline two\nline three\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[0], map[string]any{"path": "f.txt"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	want := "line one\nline two\nline three\n"
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestReadNoTrailingNewlineOmitsEmptyLine(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "a\nb\nc")
	tools := Tools(dir)
	text, _ := runTool(t, tools[0], map[string]any{"path": "f.txt"})
	if text != "a\nb\nc" {
		t.Errorf("got %q, want %q", text, "a\nb\nc")
	}
}

// THE ADDRESS THIS PROGRAM PRINTS IS THE ADDRESS ITS TOOLS ACCEPT.
//
// A row of the project's task record spells its transcript as
// `file:///…/tasks/20260819-120133_7.jsonl`, and both the system prompt and the
// `tasks` tool's own description tell the model to read that URI when it needs
// what a task actually did. Before [stripFileScheme] the URI was not an
// absolute path, so it was joined to the working directory and the read failed
// on a file nobody had named — which broke the one gesture that answers "why
// did that task do X" at the last step of it.
func TestReadOpensAFileURITheWayThisProgramPrintsOne(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "7.jsonl", "the story\n")
	tools := Tools(dir)
	for _, spelling := range []string{
		"file://" + dir + "/7.jsonl",
		"file://localhost" + dir + "/7.jsonl",
	} {
		text, isErr := runTool(t, tools[0], map[string]any{"path": spelling})
		if isErr {
			t.Fatalf("%s: unexpected error: %s", spelling, text)
		}
		if text != "the story\n" {
			t.Errorf("%s: got %q, want %q", spelling, text, "the story\n")
		}
	}
}

// A path that merely begins with those letters is a path, and an authority that
// names another machine is not this machine's file: neither is touched.
func TestReadLeavesANonLocalFileURIAlone(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "filed.txt", "kept\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[0], map[string]any{"path": "filed.txt"})
	if isErr || text != "kept\n" {
		t.Errorf("a plain name starting with file: got %q (isErr=%v)", text, isErr)
	}
	if got := stripFileScheme("file://elsewhere/etc/passwd"); got != "file://elsewhere/etc/passwd" {
		t.Errorf("another machine's URI was rewritten to %q", got)
	}
}

// ── read: offset 1-indexed ─────────────────────────────────────────────────

func TestReadOffset(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "l1\nl2\nl3\nl4\nl5\n")
	tools := Tools(dir)
	text, _ := runTool(t, tools[0], map[string]any{"path": "f.txt", "offset": 3})
	want := "l3\nl4\nl5\n"
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestReadOffsetBeyondEnd(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "a\nb\nc\n")
	tools := Tools(dir)
	text, isError, herr := runToolErr(t, tools[0], map[string]any{"path": "f.txt", "offset": 10})
	if !isError {
		t.Fatal("expected isError for offset beyond end")
	}
	if herr != nil {
		t.Fatalf("expected no harness error, got: %v", herr)
	}
	// allLines = split("\n") on "a\nb\nc\n" = ["a","b","c",""] = 4 elements.
	want := "Offset 10 is beyond end of file (4 lines total)"
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

// ── read: limit ────────────────────────────────────────────────────────────

func TestReadLimit(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "l1\nl2\nl3\nl4\nl5\n")
	tools := Tools(dir)
	text, _ := runTool(t, tools[0], map[string]any{"path": "f.txt", "limit": 2})
	want := "l1\nl2\n\n[4 more lines in file. Use offset=3 to continue.]"
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestReadLimitExactFile(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "l1\nl2\nl3\n")
	tools := Tools(dir)
	text, _ := runTool(t, tools[0], map[string]any{"path": "f.txt", "limit": 3})
	// allLines = split("\n") on "l1\nl2\nl3\n" = ["l1","l2","l3",""] = 4 elements.
	// limit=3 reads 3, remaining = 4-3 = 1 (the trailing empty element).
	want := "l1\nl2\nl3\n\n[1 more lines in file. Use offset=4 to continue.]"
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}
func TestReadOffsetAndLimit(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "l1\nl2\nl3\nl4\nl5\n")
	tools := Tools(dir)
	text, _ := runTool(t, tools[0], map[string]any{"path": "f.txt", "offset": 2, "limit": 2})
	want := "l2\nl3\n\n[3 more lines in file. Use offset=4 to continue.]"
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

// ── read: line truncation at 2000 lines ───────────────────────────────────

func TestReadLineTruncation(t *testing.T) {
	dir := t.TempDir()
	// 3000 lines, each 1 byte + newline = 2 bytes → total 6000 bytes < 50KB.
	var sb strings.Builder
	for i := range 3000 {
		sb.WriteString("l")
		if i < 2999 {
			sb.WriteString("\n")
		}
	}
	mustWriteFile(t, dir, "f.txt", sb.String())
	tools := Tools(dir)
	text, _ := runTool(t, tools[0], map[string]any{"path": "f.txt"})
	// Should show 2000 lines, footer says "Showing lines 1-2000 of 3000".
	if !strings.Contains(text, "\n\n[Showing lines 1-2000 of 3000. Use offset=2001 to continue.]") {
		t.Errorf("missing line truncation footer in:\n...%s", text[len(text)-120:])
	}
}

func TestReadByteTruncation(t *testing.T) {
	dir := t.TempDir()
	// ~60KB of content across ~1000 lines (well under line limit but over byte limit).
	var sb strings.Builder
	for range 1000 {
		sb.WriteString(strings.Repeat("x", 60))
		sb.WriteString("\n")
	}
	mustWriteFile(t, dir, "f.txt", sb.String())
	tools := Tools(dir)
	text, _ := runTool(t, tools[0], map[string]any{"path": "f.txt"})
	// Byte truncation footer includes "(50.0KB limit)" — formatSize(51200).
	if !strings.Contains(text, "(50.0KB limit). Use offset=") {
		t.Errorf("missing byte truncation footer in:\n...%s", text[len(text)-120:])
	}
}

func TestReadFirstLineExceedsByteLimit(t *testing.T) {
	dir := t.TempDir()
	// One line > 50KB.
	big := strings.Repeat("x", 60000)
	mustWriteFile(t, dir, "f.txt", big+"\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[0], map[string]any{"path": "f.txt"})
	if isErr {
		t.Fatal("expected not isError (first-line-exceeds is a footer, not an error)")
	}
	// formatSize(60000) = "58.6KB", formatSize(51200) = "50.0KB".
	want := "[Line 1 is 58.6KB, exceeds 50.0KB limit. Use bash: sed -n '1p' f.txt | head -c 51200]"
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

// ── bash: basic ───────────────────────────────────────────────────────────

func TestBashBasicOutput(t *testing.T) {
	dir := t.TempDir()
	tools := Tools(dir)
	text, isErr := runTool(t, tools[1], map[string]any{"command": "echo hello"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if text != "hello\n" {
		t.Errorf("got %q, want %q", text, "hello\n")
	}
}
func TestBashNoOutput(t *testing.T) {
	dir := t.TempDir()
	tools := Tools(dir)
	text, isErr := runTool(t, tools[1], map[string]any{"command": "true"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if text != "(no output)" {
		t.Errorf("got %q, want %q", text, "(no output)")
	}
}

func TestBashNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	tools := Tools(dir)
	text, isErr := runTool(t, tools[1], map[string]any{"command": "echo out; exit 3"})
	if !isErr {
		t.Fatal("expected isError for non-zero exit")
	}
	want := "out\n\n\nCommand exited with code 3"
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestBashStderrInterleaved(t *testing.T) {
	dir := t.TempDir()
	tools := Tools(dir)
	// stdout and stderr both produce output.
	text, _ := runTool(t, tools[1], map[string]any{"command": "echo out1; echo err1 1>&2; echo out2"})
	// Both stdout and stderr should appear; we can't guarantee exact order with
	// pipe interleaving, but both should be present.
	if !strings.Contains(text, "out1") || !strings.Contains(text, "err1") || !strings.Contains(text, "out2") {
		t.Errorf("expected stdout+stderr interleaved, got: %q", text)
	}
}

func TestBashCwdScope(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "marker.txt", "found")
	tools := Tools(dir)
	text, _ := runTool(t, tools[1], map[string]any{"command": "cat marker.txt"})
	if text != "found" {
		t.Errorf("got %q, want %q", text, "found")
	}
}

// ── bash: timeout ──────────────────────────────────────────────────────────

func TestBashTimeout(t *testing.T) {
	dir := t.TempDir()
	tools := Tools(dir)
	text, isErr := runTool(t, tools[1], map[string]any{"command": "sleep 10", "timeout": 1})
	if !isErr {
		t.Fatal("expected isError for timeout")
	}
	if !strings.Contains(text, "Command timed out after 1 seconds") {
		t.Errorf("expected timeout message, got: %q", text)
	}
}

func TestBashTimeoutInvalidZero(t *testing.T) {
	dir := t.TempDir()
	tools := Tools(dir)
	text, isErr := runTool(t, tools[1], map[string]any{"command": "echo hi", "timeout": 0})
	if !isErr {
		t.Fatal("expected isError for invalid timeout")
	}
	want := "Invalid timeout: must be a finite number of seconds"
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestBashTimeoutTooLarge(t *testing.T) {
	dir := t.TempDir()
	tools := Tools(dir)
	text, isErr := runTool(t, tools[1], map[string]any{"command": "echo hi", "timeout": 99999999})
	if !isErr {
		t.Fatal("expected isError for too-large timeout")
	}
	if !strings.Contains(text, "Invalid timeout: maximum is") {
		t.Errorf("got %q", text)
	}
}

// escapingChild is a shell fragment that starts a background process in its OWN
// process group while still holding the command's stdout. It is the shape of the
// fault the WaitDelay defence exists for: a daemon that re-parents, or anything
// that calls setsid, is out of reach of the timeout's process-group kill and
// keeps the write end of the output pipe open behind it.
//
// It is looked up rather than written literally because there is no portable
// setsid binary — macOS ships none — and a machine with neither interpreter
// cannot stage the fault at all, which is a skip and not a failure.
func escapingChild(t *testing.T) string {
	t.Helper()
	if perl, err := exec.LookPath("perl"); err == nil {
		return perl + ` -e 'use POSIX qw(setsid); setsid(); sleep 10;' &`
	}
	if python, err := exec.LookPath("python3"); err == nil {
		return python + ` -c 'import os, time; os.setsid(); time.sleep(10)' &`
	}
	t.Skip("no interpreter here can start a process in its own process group")
	return ""
}

// A STUCK CALL COSTS ITS TIMEOUT, NEVER THE RUN. Killing the shell at the
// timeout is not enough on its own: Wait blocks until every holder of the
// output pipe's write end is gone, and a grandchild that escaped the process
// group holds it for as long as it lives — so the call, the batch it is in, and
// the turn behind that all waited on a process nobody could reach.
func TestBashReturnsAtItsTimeoutWhileAGrandchildStillHoldsThePipe(t *testing.T) {
	dir := t.TempDir()
	tools := Tools(dir)
	command := escapingChild(t) + " sleep 10"
	started := time.Now()
	text, isErr := runTool(t, tools[1], map[string]any{"command": command, "timeout": 1})
	elapsed := time.Since(started)
	if !isErr || !strings.Contains(text, "Command timed out after 1 seconds") {
		t.Fatalf("expected the timeout result, got isError=%v %q", isErr, text)
	}
	// One second of timeout, three of WaitDelay, and the rest is slack for a
	// loaded machine. The grandchild lives ten, which is what this would have
	// cost — and did cost — before the defence.
	if elapsed > 8*time.Second {
		t.Fatalf("the call took %s: it waited on the grandchild, not on its own timeout", elapsed)
	}
}

// ── edit: basic exact match ───────────────────────────────────────────────

func TestEditBasic(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "hello world\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path":  "f.txt",
		"edits": []map[string]any{{"oldText": "hello", "newText": "goodbye"}},
	})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	want := "Successfully replaced 1 block(s) in f.txt."
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(data) != "goodbye world\n" {
		t.Errorf("file content got %q, want %q", string(data), "goodbye world\n")
	}
}

func TestEditMultipleBlocks(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "a\nb\nc\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path": "f.txt",
		"edits": []map[string]any{
			{"oldText": "a", "newText": "A"},
			{"oldText": "c", "newText": "C"},
		},
	})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if text != "Successfully replaced 2 block(s) in f.txt." {
		t.Errorf("got %q", text)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(data) != "A\nb\nC\n" {
		t.Errorf("file content got %q", string(data))
	}
}

// ── edit: error strings ───────────────────────────────────────────────────

func TestEditNotFound(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "hello world\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path":  "f.txt",
		"edits": []map[string]any{{"oldText": "xyz", "newText": "abc"}},
	})
	if !isErr {
		t.Fatal("expected isError for not found")
	}
	want := "Could not find the exact text in f.txt. The old text must match exactly including all whitespace and newlines."
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestEditNotFoundMulti(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "hello world\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path": "f.txt",
		"edits": []map[string]any{
			{"oldText": "hello", "newText": "hi"},
			{"oldText": "xyz", "newText": "abc"},
		},
	})
	if !isErr {
		t.Fatal("expected isError")
	}
	want := "Could not find edits[1] in f.txt. The oldText must match exactly including all whitespace and newlines."
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestEditDuplicate(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "dup\ndup\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path":  "f.txt",
		"edits": []map[string]any{{"oldText": "dup", "newText": "x"}},
	})
	if !isErr {
		t.Fatal("expected isError for duplicate")
	}
	want := "Found 2 occurrences of the text in f.txt. The text must be unique. Please provide more context to make it unique."
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestEditDuplicateMulti(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "dup\ndup\nuniq\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path": "f.txt",
		"edits": []map[string]any{
			{"oldText": "uniq", "newText": "U"},
			{"oldText": "dup", "newText": "x"},
		},
	})
	if !isErr {
		t.Fatal("expected isError")
	}
	want := "Found 2 occurrences of edits[1] in f.txt. Each oldText must be unique. Please provide more context to make it unique."
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestEditEmptyOldText(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "hello\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path":  "f.txt",
		"edits": []map[string]any{{"oldText": "", "newText": "x"}},
	})
	if !isErr {
		t.Fatal("expected isError for empty oldText")
	}
	want := "oldText must not be empty in f.txt."
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestEditEmptyOldTextMulti(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "hello\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path": "f.txt",
		"edits": []map[string]any{
			{"oldText": "hello", "newText": "hi"},
			{"oldText": "", "newText": "x"},
		},
	})
	if !isErr {
		t.Fatal("expected isError")
	}
	want := "edits[1].oldText must not be empty in f.txt."
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestEditNoChange(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "same\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path":  "f.txt",
		"edits": []map[string]any{{"oldText": "same", "newText": "same"}},
	})
	if !isErr {
		t.Fatal("expected isError for no change")
	}
	want := "No changes made to f.txt. The replacement produced identical content. This might indicate an issue with special characters or the text not existing as expected."
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestEditOverlap(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "abcdef\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path": "f.txt",
		"edits": []map[string]any{
			{"oldText": "abc", "newText": "ABC"},
			{"oldText": "cde", "newText": "CDE"},
		},
	})
	if !isErr {
		t.Fatal("expected isError for overlap")
	}
	want := "edits[0] and edits[1] overlap in f.txt. Merge them into one edit or target disjoint regions."
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestEditFileNotFound(t *testing.T) {
	dir := t.TempDir()
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path":  "noexist.txt",
		"edits": []map[string]any{{"oldText": "a", "newText": "b"}},
	})
	if !isErr {
		t.Fatal("expected isError for missing file")
	}
	if !strings.Contains(text, "Could not edit file: noexist.txt.") {
		t.Errorf("got %q", text)
	}
}

// ── edit: legacy oldText/newText ───────────────────────────────────────────

func TestEditLegacyFormat(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "hello\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path":    "f.txt",
		"oldText": "hello",
		"newText": "world",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if text != "Successfully replaced 1 block(s) in f.txt." {
		t.Errorf("got %q", text)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(data) != "world\n" {
		t.Errorf("file content got %q", string(data))
	}
}

// ── edit: string-encoded edits array ──────────────────────────────────────

func TestEditStringEncodedEdits(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "hello\n")
	tools := Tools(dir)
	editsJSON, _ := json.Marshal([]map[string]any{{"oldText": "hello", "newText": "world"}})
	text, isErr := runTool(t, tools[2], map[string]any{
		"path":  "f.txt",
		"edits": string(editsJSON),
	})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if text != "Successfully replaced 1 block(s) in f.txt." {
		t.Errorf("got %q", text)
	}
}

// ── edit: BOM preservation ────────────────────────────────────────────────

func TestEditPreservesBOM(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "\uFEFFhello world\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path":  "f.txt",
		"edits": []map[string]any{{"oldText": "hello", "newText": "goodbye"}},
	})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if !strings.HasPrefix(string(data), "\uFEFF") {
		t.Errorf("BOM not preserved, got: %q", string(data))
	}
	if string(data) != "\uFEFFgoodbye world\n" {
		t.Errorf("got %q, want %q", string(data), "\uFEFFgoodbye world\n")
	}
}

// ── edit: EOL preservation (CRLF) ─────────────────────────────────────────

func TestEditPreservesCRLF(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "line1\r\nline2\r\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path":  "f.txt",
		"edits": []map[string]any{{"oldText": "line1", "newText": "LINE1"}},
	})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if !strings.Contains(string(data), "\r\n") {
		t.Errorf("CRLF not preserved, got: %q", string(data))
	}
	want := "LINE1\r\nline2\r\n"
	if string(data) != want {
		t.Errorf("got %q, want %q", string(data), want)
	}
}

// ── edit: fuzzy matching ──────────────────────────────────────────────────

func TestEditFuzzySmartQuotes(t *testing.T) {
	dir := t.TempDir()
	// File has smart quotes; oldText uses straight quotes.
	mustWriteFile(t, dir, "f.txt", "say \u201Chello\u201D world\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path":  "f.txt",
		"edits": []map[string]any{{"oldText": `say "hello" world`, "newText": "say 'hi' world"}},
	})
	if isErr {
		t.Fatalf("unexpected error for fuzzy match: %s", text)
	}
	if text != "Successfully replaced 1 block(s) in f.txt." {
		t.Errorf("got %q", text)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(data) != "say 'hi' world\n" {
		t.Errorf("file content got %q", string(data))
	}
}

func TestEditFuzzyTrailingWhitespace(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "line1   \nline2\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path":  "f.txt",
		"edits": []map[string]any{{"oldText": "line1", "newText": "LINE1"}},
	})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(data) != "LINE1   \nline2\n" {
		t.Errorf("file content got %q, want %q", string(data), "LINE1   \nline2\n")
	}
}

func TestEditFuzzyEmDash(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "a \u2014 b\n")
	tools := Tools(dir)
	text, isErr := runTool(t, tools[2], map[string]any{
		"path":  "f.txt",
		"edits": []map[string]any{{"oldText": "a - b", "newText": "a + b"}},
	})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(data) != "a + b\n" {
		t.Errorf("file content got %q", string(data))
	}
}

// ── write: basic ──────────────────────────────────────────────────────────

func TestWriteBasic(t *testing.T) {
	dir := t.TempDir()
	tools := Tools(dir)
	text, isErr := runTool(t, tools[3], map[string]any{"path": "out.txt", "content": "hello\n"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	want := "Successfully wrote 6 bytes to out.txt"
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "out.txt"))
	if string(data) != "hello\n" {
		t.Errorf("file content got %q", string(data))
	}
}

func TestWriteCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	tools := Tools(dir)
	text, isErr := runTool(t, tools[3], map[string]any{"path": "sub/dir/f.txt", "content": "x"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "sub", "dir", "f.txt"))
	if string(data) != "x" {
		t.Errorf("file content got %q", string(data))
	}
}

func TestWriteOverwriteSilent(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "old")
	tools := Tools(dir)
	_, isErr := runTool(t, tools[3], map[string]any{"path": "f.txt", "content": "new"})
	if isErr {
		t.Fatal("overwrite should not be an error")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(data) != "new" {
		t.Errorf("got %q", string(data))
	}
}

// ── write: byte count is UTF-16 char count ────────────────────────────────

func TestWriteByteCountIsUTF16Units(t *testing.T) {
	dir := t.TempDir()
	tools := Tools(dir)
	// "héllo" = h(1) + é(1) + l(2) + l(1) + o(1) = 5 UTF-16 code units.
	// é is U+00E9, which is in the BMP → 1 UTF-16 unit.
	text, _ := runTool(t, tools[3], map[string]any{"path": "f.txt", "content": "héllo"})
	want := "Successfully wrote 5 bytes to f.txt"
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestWriteByteCountAstralPlane(t *testing.T) {
	dir := t.TempDir()
	tools := Tools(dir)
	// 𝄞 (U+1D11E, musical G clef) is outside the BMP → 2 UTF-16 code units.
	text, _ := runTool(t, tools[3], map[string]any{"path": "f.txt", "content": "𝄞"})
	want := "Successfully wrote 2 bytes to f.txt"
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

// ── per-file serialization ────────────────────────────────────────────────

func TestEditPerFileSerialization(t *testing.T) {
	// Two concurrent edits to the same file should both succeed without
	// corrupting each other. The mutation queue serializes them.
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "a\nb\nc\nd\ne\n")
	tools := Tools(dir)

	done := make(chan struct{}, 2)
	go func() {
		runTool(t, tools[2], map[string]any{
			"path":  "f.txt",
			"edits": []map[string]any{{"oldText": "a", "newText": "A"}},
		})
		done <- struct{}{}
	}()
	go func() {
		// This edit would fail if the first hadn't been applied yet, because
		// "A" doesn't exist in the original. But the mutation queue serializes,
		// so this runs after the first edit lands.
		// Actually — pi matches against the ORIGINAL file, not incrementally.
		// So both edits match against "a\nb\nc\nd\ne\n". This edit matches "e".
		runTool(t, tools[2], map[string]any{
			"path":  "f.txt",
			"edits": []map[string]any{{"oldText": "e", "newText": "E"}},
		})
		done <- struct{}{}
	}()
	<-done
	<-done

	data, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	// Both edits should have been applied (serially).
	if string(data) != "A\nb\nc\nd\nE\n" {
		t.Errorf("expected both edits applied, got %q", string(data))
	}
}

// ── formatSize ─────────────────────────────────────────────────────────────

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int
		want  string
	}{
		{0, "0B"},
		{1, "1B"},
		{512, "512B"},
		{1023, "1023B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{51200, "50.0KB"},
		{60000, "58.6KB"},
		{1024 * 1024, "1.0MB"},
	}
	for _, tc := range tests {
		got := formatSize(tc.bytes)
		if got != tc.want {
			t.Errorf("formatSize(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

// ── isFinite ──────────────────────────────────────────────────────────────

func TestIsFinite(t *testing.T) {
	if !isFinite(1.0) {
		t.Error("1.0 should be finite")
	}
	if !isFinite(0.0) {
		t.Error("0.0 should be finite")
	}
	inf := math.Inf(1)
	if isFinite(inf) {
		t.Error("Inf should not be finite")
	}
}

// ── AllTools: 7-tool registry order ────────────────────────────────────────

func TestAllToolsOrder(t *testing.T) {
	tools := AllTools(t.TempDir())
	if len(tools) != 7 {
		t.Fatalf("expected 7 tools, got %d", len(tools))
	}
	names := []string{tools[0].Name, tools[1].Name, tools[2].Name, tools[3].Name, tools[4].Name, tools[5].Name, tools[6].Name}
	want := []string{"read", "bash", "edit", "write", "grep", "find", "ls"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("tool[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestGrepSchemaVerbatim(t *testing.T) {
	tools := AllTools(t.TempDir())
	if string(tools[4].Schema) != grepSchemaJSON {
		t.Errorf("grep schema mismatch:\ngot:  %s\nwant: %s", tools[4].Schema, grepSchemaJSON)
	}
}

func TestFindSchemaVerbatim(t *testing.T) {
	tools := AllTools(t.TempDir())
	if string(tools[5].Schema) != findSchemaJSON {
		t.Errorf("find schema mismatch:\ngot:  %s\nwant: %s", tools[5].Schema, findSchemaJSON)
	}
}

func TestLsSchemaVerbatim(t *testing.T) {
	tools := AllTools(t.TempDir())
	if string(tools[6].Schema) != lsSchemaJSON {
		t.Errorf("ls schema mismatch:\ngot:  %s\nwant: %s", tools[6].Schema, lsSchemaJSON)
	}
}

func TestGrepFindLsDescriptionsVerbatim(t *testing.T) {
	tools := AllTools(t.TempDir())
	if tools[4].Description != grepDescription {
		t.Errorf("grep description mismatch")
	}
	if tools[5].Description != findDescription {
		t.Errorf("find description mismatch")
	}
	if tools[6].Description != lsDescription {
		t.Errorf("ls description mismatch")
	}
}

// ── grep ──────────────────────────────────────────────────────────────────

func TestGrepBasicMatch(t *testing.T) {
	requireExternalTool(t, "rg")
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "hello world\nfoo bar\nhello again\n")
	tools := AllTools(dir)
	text, isErr := runTool(t, tools[4], map[string]any{"pattern": "hello"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	// Should find matches in f.txt with line numbers.
	if !strings.Contains(text, "f.txt:1: hello world") {
		t.Errorf("expected match on line 1, got: %q", text)
	}
	if !strings.Contains(text, "f.txt:3: hello again") {
		t.Errorf("expected match on line 3, got: %q", text)
	}
}

func TestGrepNoMatches(t *testing.T) {
	requireExternalTool(t, "rg")
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "hello\n")
	tools := AllTools(dir)
	text, isErr := runTool(t, tools[4], map[string]any{"pattern": "xyz"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if text != "No matches found" {
		t.Errorf("got %q, want %q", text, "No matches found")
	}
}

func TestGrepCaseInsensitive(t *testing.T) {
	requireExternalTool(t, "rg")
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "Hello\nHELLO\nhello\n")
	tools := AllTools(dir)
	text, _ := runTool(t, tools[4], map[string]any{"pattern": "hello", "ignoreCase": true})
	lines := strings.Split(text, "\n")
	if len(lines) < 3 {
		t.Errorf("expected 3 matches, got: %q", text)
	}
}

func TestGrepLiteral(t *testing.T) {
	requireExternalTool(t, "rg")
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "a.b\ncd\n")
	tools := AllTools(dir)
	text, _ := runTool(t, tools[4], map[string]any{"pattern": "a.b", "literal": true})
	// Literal search: "a.b" should match "a.b" but not "aXb" (which regex would match).
	if !strings.Contains(text, "f.txt:1: a.b") {
		t.Errorf("expected literal match, got: %q", text)
	}
}

// ── find ──────────────────────────────────────────────────────────────────

func TestFindBasic(t *testing.T) {
	requireExternalTool(t, "fd")
	dir := t.TempDir()
	mustWriteFile(t, dir, "a.txt", "x")
	mustWriteFile(t, dir, "b.txt", "y")
	mustWriteFile(t, dir, "sub/c.txt", "z")
	tools := AllTools(dir)
	text, isErr := runTool(t, tools[5], map[string]any{"pattern": "*.txt"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	// Should find a.txt and b.txt (basename match via --glob).
	if !strings.Contains(text, "a.txt") {
		t.Errorf("expected a.txt, got: %q", text)
	}
	if !strings.Contains(text, "b.txt") {
		t.Errorf("expected b.txt, got: %q", text)
	}
}

func TestFindNoMatches(t *testing.T) {
	requireExternalTool(t, "fd")
	dir := t.TempDir()
	mustWriteFile(t, dir, "a.txt", "x")
	tools := AllTools(dir)
	text, isErr := runTool(t, tools[5], map[string]any{"pattern": "*.xyz"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if text != "No files found matching pattern" {
		t.Errorf("got %q, want %q", text, "No files found matching pattern")
	}
}

// ── ls ────────────────────────────────────────────────────────────────────

func TestLsBasic(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "b.txt", "x")
	mustWriteFile(t, dir, "a.txt", "y")
	os.MkdirAll(filepath.Join(dir, "subdir"), 0755)
	tools := AllTools(dir)
	text, isErr := runTool(t, tools[6], map[string]any{})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	// Should list sorted alphabetically with "/" for directories.
	if !strings.Contains(text, "a.txt") {
		t.Errorf("expected a.txt, got: %q", text)
	}
	if !strings.Contains(text, "b.txt") {
		t.Errorf("expected b.txt, got: %q", text)
	}
	if !strings.Contains(text, "subdir/") {
		t.Errorf("expected subdir/ with trailing slash, got: %q", text)
	}
}

func TestLsEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	tools := AllTools(dir)
	text, isErr := runTool(t, tools[6], map[string]any{})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if text != "(empty directory)" {
		t.Errorf("got %q, want %q", text, "(empty directory)")
	}
}

func TestLsPathNotFound(t *testing.T) {
	dir := t.TempDir()
	tools := AllTools(dir)
	text, isErr := runTool(t, tools[6], map[string]any{"path": "noexist"})
	if !isErr {
		t.Fatal("expected isError for path not found")
	}
	if !strings.Contains(text, "Path not found:") {
		t.Errorf("got %q", text)
	}
}

func TestLsNotADirectory(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "f.txt", "x")
	tools := AllTools(dir)
	text, isErr := runTool(t, tools[6], map[string]any{"path": "f.txt"})
	if !isErr {
		t.Fatal("expected isError for not a directory")
	}
	if !strings.Contains(text, "Not a directory:") {
		t.Errorf("got %q", text)
	}
}

func TestLsDotfilesIncluded(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, ".hidden", "x")
	mustWriteFile(t, dir, "visible.txt", "y")
	tools := AllTools(dir)
	text, _ := runTool(t, tools[6], map[string]any{})
	if !strings.Contains(text, ".hidden") {
		t.Errorf("expected dotfile in listing, got: %q", text)
	}
}
