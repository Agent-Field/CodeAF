//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// A PIPE SWAPPED IN FOR A CLAIM AFTER IT WAS LOOKED AT NEVER HOLDS THE READ.
// The name is a plain file when it is looked at and a pipe with no writer when
// it is opened — the window a same-user hand on the folder has. An ordinary
// open would wait for a writer for ever, holding the request and its read
// slot; this one opens at once, sees the descriptor is not the file that was
// looked at, and refuses it.
func TestAClaimSwappedForAPipeIsRefusedWithoutWaiting(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	const name = "claim.json"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	seen, err := root.Lstat(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, name), 0o600); err != nil {
		t.Skipf("no pipes here: %v", err)
	}
	answered := make(chan error, 1)
	go func() {
		_, err := appReadClaimAt(root, name, seen, appActionClaimMax)
		answered <- err
	}()
	select {
	case err := <-answered:
		if err == nil {
			t.Fatal("a pipe was read as a claim")
		}
	case <-time.After(5 * time.Second):
		// Unblock the stuck open so the test binary can end.
		if w, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			w.Close()
		}
		t.Fatal("reading a claim swapped for a pipe waited in open")
	}
}

// A PIPE WHERE A CLAIM SHOULD BE IS UNCERTAIN THROUGH THE WIRE, AT ONCE, and the
// producer is not called: the record says nothing anybody can read, which is
// not "new".
func TestAPipeWhereAClaimShouldBeIsUncertain(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	if _, e := h.w.act(taskAsk(agent, "t-first", "x")); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(filepath.Dir(h.b.state.where()), appActionClaimsName, appClaimName("t-pipe"))
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("no pipes here: %v", err)
	}
	_, e := h.w.act(taskAsk(agent, "t-pipe", "x"))
	wantCode(t, e, appwire.CodeUncertain)
	_, e = h.w.get(agent, "t-pipe")
	wantCode(t, e, appwire.CodeUncertain)
	if tasks, _, _ := h.p.counts(); tasks != 1 {
		t.Fatalf("the producer ran %d times", tasks)
	}
}
