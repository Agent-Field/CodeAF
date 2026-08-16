package exec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/rtk"
)

// compressorBench sets up a workspace, the stand-in rtk, and the log the stub
// writes to. Every test here asks the same question in a different way: did the
// leaf get the same answer it would have got with no rtk at all?
func compressorBench(t *testing.T) (*Toolbox, string, func() []string) {
	t.Helper()
	space := workspace(t)
	root := space.Root()

	if err := os.WriteFile(filepath.Join(root, "sample.txt"),
		[]byte(strings.Repeat("a line of perfectly ordinary output\n", 200)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cursed.txt"), []byte("the real bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	source, err := os.ReadFile(filepath.Join("testdata", "rtk"))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "rtk")
	if err := os.WriteFile(binary, source, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "asked.log")
	t.Setenv("AFORGE_RTK_STUB_LOG", logPath)
	t.Setenv(rtk.EnvBinary, binary)

	asked := func() []string {
		body, err := os.ReadFile(logPath)
		if err != nil {
			return nil
		}
		return strings.Split(strings.TrimSpace(string(body)), "\n")
	}
	return NewToolbox(space, "1", nil), root, asked
}

func shell(t *testing.T, box *Toolbox, command string) Result {
	t.Helper()
	return box.Execute(context.Background(), "sh", `{"cmd":`+quote(command)+`}`)
}

func quote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func TestShellCompressesAnEligibleRead(t *testing.T) {
	box, _, asked := compressorBench(t)
	result := shell(t, box, "cat sample.txt")
	if result.IsError {
		t.Fatalf("a wrapped read reported failure: %+v", result)
	}
	if !strings.Contains(result.Content, "sample.txt: 200 lines") {
		t.Fatalf("result = %q, want the compressed read", result.Content)
	}
	// The whole point: what reaches the model is a fraction of what the command
	// said, and it will be paid for again on every later turn.
	if len(result.Content) > 100 {
		t.Fatalf("compressed result was %d bytes, want far less than the 7200 the file costs", len(result.Content))
	}
	if got := asked(); len(got) != 2 || got[0] != "rewrite cat sample.txt" || got[1] != "read sample.txt" {
		t.Fatalf("rtk was asked %v, want one rewrite and one read", got)
	}
}

func TestShellAsksThePlainCommandWhenAWrappedReadFails(t *testing.T) {
	box, _, _ := compressorBench(t)
	result := shell(t, box, "cat missing.txt")
	if !result.IsError {
		t.Fatalf("a missing file came back a success: %+v", result)
	}
	// The model must read the shell's own sentence about the missing file, not
	// a proxy's paraphrase of it.
	if strings.Contains(result.Content, "rtk could not read") {
		t.Fatalf("result = %q, want the shell's own error", result.Content)
	}
	if !strings.Contains(result.Content, "missing.txt") || !strings.Contains(result.Content, "exit:") {
		t.Fatalf("result = %q, want cat's own failure", result.Content)
	}
}

func TestShellFallsBackAndStopsRetryingWhenRtkItselfBreaks(t *testing.T) {
	box, _, asked := compressorBench(t)

	first := shell(t, box, "cat cursed.txt")
	if first.IsError {
		t.Fatalf("rtk breaking made a working command fail: %+v", first)
	}
	if !strings.Contains(first.Content, "the real bytes") {
		t.Fatalf("result = %q, want the file's real content", first.Content)
	}
	if strings.Contains(first.Content, "[rtk:") {
		t.Fatalf("rtk's own failure leaked into the result: %q", first.Content)
	}

	second := shell(t, box, "cat cursed.txt")
	if !strings.Contains(second.Content, "the real bytes") {
		t.Fatalf("second result = %q, want the file's real content", second.Content)
	}
	// One broken rewrite costs one fallback, not one per call forever.
	for _, line := range asked()[1:] {
		if strings.HasPrefix(line, "rewrite cat cursed.txt") || strings.HasPrefix(line, "read cursed.txt") {
			continue
		}
		t.Fatalf("unexpected question to rtk after the ban: %q", line)
	}
	if got := asked(); len(got) != 2 {
		t.Fatalf("rtk was asked %v, want the one rewrite and one read from before the ban", got)
	}
}

func TestShellBelievesAFailingCheckRatherThanRunningItTwice(t *testing.T) {
	box, _, asked := compressorBench(t)
	result := shell(t, box, "go test ./...")
	if !result.IsError {
		t.Fatalf("a failing test suite came back a success: %+v", result)
	}
	if !strings.Contains(result.Content, "FAIL 1 of 2") {
		t.Fatalf("result = %q, want rtk's compressed failure", result.Content)
	}
	if got := asked(); len(got) != 2 || got[1] != "go test" {
		t.Fatalf("rtk was asked %v, want the suite run exactly once", got)
	}
}

func TestOffRunsEveryCommandPlain(t *testing.T) {
	box, _, asked := compressorBench(t)
	t.Setenv(rtk.EnvBinary, rtk.Off)

	result := shell(t, box, "cat sample.txt")
	if result.IsError {
		t.Fatalf("plain read failed: %+v", result)
	}
	if !strings.Contains(result.Content, "a line of perfectly ordinary output") {
		t.Fatalf("result = %q, want the file's own bytes", result.Content)
	}
	if got := asked(); len(got) != 0 {
		t.Fatalf("rtk was asked %v with it turned off, want nothing", got)
	}
}

func TestNoRtkAtAllChangesNothing(t *testing.T) {
	box, _, asked := compressorBench(t)
	t.Setenv(rtk.EnvBinary, filepath.Join(t.TempDir(), "not-installed"))

	result := shell(t, box, "cat sample.txt")
	if result.IsError || !strings.Contains(result.Content, "a line of perfectly ordinary output") {
		t.Fatalf("result = %+v, want the plain read", result)
	}
	if got := asked(); len(got) != 0 {
		t.Fatalf("rtk was asked %v when there is none, want nothing", got)
	}
}

func TestBackgroundJobsAreNeverWrapped(t *testing.T) {
	box, _, asked := compressorBench(t)
	t.Cleanup(func() { box.jobs.close() })

	result := box.Execute(context.Background(), "sh", `{"cmd":"cat sample.txt","bg":true}`)
	if result.IsError {
		t.Fatalf("background start failed: %+v", result)
	}
	if got := asked(); len(got) != 0 {
		t.Fatalf("rtk was asked %v about a background job, want nothing", got)
	}
}

func TestWrappedRunsPreserveExitCodes(t *testing.T) {
	box, _, _ := compressorBench(t)
	for _, want := range []struct {
		command string
		failure bool
	}{
		{"cat sample.txt", false},
		{"cat cursed.txt", false},
		{"cat missing.txt", true},
		{"go test ./...", true},
	} {
		result := shell(t, box, want.command)
		if result.IsError != want.failure {
			t.Errorf("sh(%q) IsError = %v, want %v: %q", want.command, result.IsError, want.failure, result.Content)
		}
	}
}
