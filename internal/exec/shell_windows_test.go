//go:build windows

package exec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
	"github.com/Agent-Field/codeaf/internal/rtk"
	commandshell "github.com/Agent-Field/codeaf/internal/shell"
	"github.com/Agent-Field/codeaf/internal/store"
)

// N2 proves the detached-service door gives Git Bash a writable output handle,
// appending stdout and stderr after earlier bytes instead of dying silently.
func TestWindowsDetachedServiceWritesLog(t *testing.T) {
	requireWindowsBash(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "service.log")
	if err := os.WriteFile(path, []byte("earlier\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pid, _, err := StartDetachedService("printf 'stdout\\n'; printf 'stderr\\n' >&2", dir, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = processgroup.Kill(pid) })
	deadline := time.After(20 * time.Second)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) == "earlier\nstdout\nstderr\n" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("service log = %q", body)
		case <-tick.C:
		}
	}
}

func requireWindowsBash(t *testing.T) {
	t.Helper()
	if _, err := commandshell.Bash(); err != nil {
		t.Skipf("Git Bash is required for native command-door tests: %v", err)
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

func windowsBackgroundOutput(t *testing.T, box *Toolbox, command string) string {
	t.Helper()
	result := box.startBackground(context.Background(), command, map[string]any{})
	if result.IsError {
		t.Fatalf("job refused: %s", result.Content)
	}
	box.jobs.mutex.Lock()
	var job *backgroundJob
	for _, one := range box.jobs.jobs {
		if job == nil || one.id > job.id {
			job = one
		}
	}
	box.jobs.mutex.Unlock()
	if job == nil {
		t.Fatal("job was not registered")
	}
	select {
	case <-job.done:
	case <-time.After(20 * time.Second):
		t.Fatal("Windows job did not finish")
	}
	body, err := os.ReadFile(job.fullPath)
	if err != nil || job.exitCode != 0 {
		t.Fatalf("job output %q, exit %d, error %v", body, job.exitCode, err)
	}
	return string(body)
}

// N2 proves both real Toolbox shell doors parse once and retain multiline text,
// quotes, backslashes, percent signs, substitutions and non-ASCII bytes.
func TestWindowsToolboxSingleParse(t *testing.T) {
	requireWindowsBash(t)
	box, _ := backgroundToolbox(t)
	literal := "\" ' \\ %PATH% $(echo WRONG) héllo ✓"
	command := "export PATH='/c/Program Files (x86)/test':$PATH\nx=inner; echo \"x=[$x]\"\nprintf 'line two\\n'\ncat <<'CODEAF_LITERAL'\n" + literal + "\nCODEAF_LITERAL\n"
	want := "x=[inner]\nline two\n" + literal + "\n"
	run := box.runShell(context.Background(), command, 20, "")
	if run.err != nil || run.body != want {
		t.Fatalf("foreground: %q %v", run.body, run.err)
	}
	if got := windowsBackgroundOutput(t, box, command); got != want {
		t.Fatalf("background: %q, want %q", got, want)
	}
}

// N2 proves skills and rtk shelf programs run by bare name through foreground
// and background doors while their exported variables retain native paths.
func TestWindowsToolboxShelves(t *testing.T) {
	requireWindowsBash(t)
	history, err := store.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	box := NewToolboxWithStore(workspace(t), "windows", nil, history)
	t.Cleanup(func() { box.Close() })
	skills, err := store.SkillsBinDir()
	if err != nil {
		t.Fatal(err)
	}
	rtkDir := filepath.Join(t.TempDir(), "rtk shelf (héllo)")
	for _, shelf := range []struct{ dir, name, body string }{
		{skills, "windows-skill", "printf 'SKILL=%s\\n' \"$CODEAF_SKILLS_BIN\""},
		{rtkDir, "windows-rtk", "printf 'RTK=%s\\n' \"$CODEAF_RTK_BIN\""},
		{rtkDir, "rtk.exe", "unused stub for shelf discovery"},
	} {
		if err := os.MkdirAll(shelf.dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(shelf.dir, shelf.name), []byte("#!/bin/sh\n"+shelf.body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	rtkBin := filepath.Join(rtkDir, "rtk.exe")
	t.Setenv(rtk.EnvBinary, rtkBin)
	command := "windows-skill; windows-rtk"
	want := "SKILL=" + skills + "\nRTK=" + rtkDir + "\n"
	run := box.runShell(context.Background(), command, 20, rtkBin)
	if run.err != nil || run.body != want {
		t.Fatalf("foreground shelves: %q %v; want %q", run.body, run.err, want)
	}
	if got := windowsBackgroundOutput(t, box, command); got != want {
		t.Fatalf("background shelves: %q; want %q", got, want)
	}
}

// N2 proves refusal happens before either real door starts a process or creates
// a job. A marker would expose a command run despite the explicit bad choice.
func TestWindowsToolboxRefusesMissingBash(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	box, space := backgroundToolbox(t)
	missing := filepath.Join(t.TempDir(), "missing bash.exe")
	t.Setenv("CODEAF_BASH", missing)
	for _, definition := range box.Definitions() {
		if definition.Function.Name == "sh" {
			t.Fatal("unusable shell is advertised")
		}
	}
	command := "echo WRONG > should-not-exist"
	run := box.runShell(context.Background(), command, 20, "").result(20)
	job := box.startBackground(context.Background(), command, map[string]any{})
	for _, result := range []Result{run, job} {
		if !result.IsError || !strings.Contains(result.Content, "CODEAF_BASH") || !strings.Contains(result.Content, missing) {
			t.Fatalf("refusal: %+v", result)
		}
	}
	if len(box.jobs.jobs) != 0 {
		t.Fatal("refused job was registered")
	}
	if _, err := os.Stat(filepath.Join(space.Root(), "should-not-exist")); !os.IsNotExist(err) {
		t.Fatalf("command ran: %v", err)
	}
}
