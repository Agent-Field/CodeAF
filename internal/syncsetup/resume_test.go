package syncsetup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/preflight"
)

// runningServer is a real process group that stands for a dev server the chat
// started.
func runningServer(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "300") //codeaf:plumbing test fixture: a process group for the chat to have started
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })
	return cmd.Process.Pid
}

// One chat with a lockfile, a folder that lockfile rebuilds and a dev server, taken
// on the other machine: the takeover says what it did not bring and what is not
// running, the folder is not on B, and what the takeover found is told to the
// agent once.
func TestTwoHomesTakeSaysWhatWasLeftBehind(t *testing.T) {
	h := newTwoHomes(t)
	ctx := context.Background()
	seedTree(t, h.work)
	for path, body := range map[string]string{
		"package-lock.json":              `{"lockfileVersion":3}`,
		"node_modules/left-pad/index.js": "module.exports = 1\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(h.work, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(h.work, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	machine, err := preflight.OpenMachine(h.cell.Root, h.work)
	if err != nil {
		t.Fatal(err)
	}
	a := h.openObserved(h.a, h.engine, h.cell, h.work, nameA, machine.Observer())
	machine.Started(executor.Job{ID: 1, Command: "npm run dev --token abc123", PGID: runningServer(t)})
	a.mustSay("started the server")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	work := workspaceOf(got.Taken.Cell.Root)
	if _, err := os.Stat(filepath.Join(work, "node_modules")); err == nil {
		t.Fatal("the folder a lockfile rebuilds travelled to the other machine")
	}
	if _, err := os.Stat(filepath.Join(work, "package-lock.json")); err != nil {
		t.Fatalf("the lockfile did not travel: %v", err)
	}
	r := got.Resume
	if r.From != nameA || len(r.Missing) != 1 || r.Missing[0].Path != "node_modules" || r.Missing[0].Lock != "package-lock.json" {
		t.Fatalf("resume = %+v", r)
	}
	if len(r.Stopped) != 1 || r.Stopped[0].Command != "npm run dev --token …" {
		t.Fatalf("stopped = %+v: the command as recorded, cleaned of its secret", r.Stopped)
	}

	// What the takeover found is told once, whichever surface opens the chat.
	b, err := preflight.OpenMachine(got.Taken.Cell.Root, work)
	if err != nil {
		t.Fatal(err)
	}
	news := b.News()
	if !strings.Contains(news, "node_modules") || !strings.Contains(news, "npm run dev") || strings.Contains(news, "abc123") {
		t.Fatalf("news:\n%s", news)
	}
	if b.News() != "" {
		t.Fatal("told twice")
	}

	// A setup turn that brings the folder back does not put it into the next seal.
	if err := os.MkdirAll(filepath.Join(work, "node_modules", "left-pad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "node_modules", "left-pad", "index.js"), []byte("rebuilt"), 0o644); err != nil {
		t.Fatal(err)
	}
	on := h.openObserved(h.b, h.engB, got.Taken.Cell, work, nameB, b.Observer())
	on.mustSay("b1")
	if plan := b.Plan(); len(plan.Resume.Missing) != 0 {
		t.Fatalf("still missing %+v once the folder is back", plan.Resume.Missing)
	}
}

// A takeover with nothing to say leaves nothing behind for the agent.
func TestTwoHomesTakeWithNothingToSayIsSilent(t *testing.T) {
	h := newTwoHomes(t)
	seedTree(t, h.work)
	a := h.openA()
	a.mustSay("plain")
	got := h.takeOnB(a)
	if !got.Resume.Empty() {
		t.Fatalf("resume = %+v for a chat that left nothing behind", got.Resume)
	}
	b, err := preflight.OpenMachine(got.Taken.Cell.Root, workspaceOf(got.Taken.Cell.Root))
	if err != nil {
		t.Fatal(err)
	}
	if b.News() != "" {
		t.Fatal("news for a takeover with nothing to say")
	}
}
