package taskdoor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

// R7-3 and contract 12 forbid re-executing an acknowledged attempt, even after restart.
func TestAnAttemptStartedAgainAfterItsAckNeverRunsAgain(t *testing.T) {
	r := newRig(t)
	stop := r.serve(r.config())
	c := r.dial()
	id := attempt(801)
	send(t, c, id, request())
	receive(t, c, "started")
	send(t, c, id, taskdoor.Watch{})
	receive(t, c, "finished")
	send(t, c, id, taskdoor.Ack{Disposition: "landed"})
	receive(t, c, "acked")
	check := func() {
		t.Helper()
		send(t, c, id, request())
		var no taskdoor.Refused
		decode(t, receive(t, c, "refused"), &no)
		if !strings.Contains(no.Reason, "already finished") {
			t.Fatalf("reason = %q", no.Reason)
		}
		if r.executor.calls.Load() != 1 {
			t.Fatal("acknowledged task ran again")
		}
	}
	check()
	_ = c.Close()
	stop()
	// A killed cleanup can leave only a directory fragment behind the tombstone.
	if err := os.MkdirAll(filepath.Join(r.root, id), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.root, id, "cleanup-fragment"), []byte("kept until restart"), 0600); err != nil {
		t.Fatal(err)
	}
	r.serve(r.config())
	c = r.dial()
	check()
}

// R7-4 and contract 13 recheck authority on an already authenticated connection.
func TestARevokedComputerCannotStartWorkOnAConnectionItAlreadyHolds(t *testing.T) {
	r := newRig(t)
	clock := newClock()
	cfg := r.config()
	cfg.Clock = clock
	r.serve(cfg)
	c := r.dial()
	r.rosterMu.Lock()
	r.active[r.a.ID()] = false
	r.rosterMu.Unlock()
	clock.advance(taskdoor.RosterFor + time.Second)
	send(t, c, attempt(802), request())
	var no taskdoor.Refused
	decode(t, receive(t, c, "refused"), &no)
	if no.Reason != taskdoor.RemovedComputerLine {
		t.Fatalf("reason = %q", no.Reason)
	}
	if _, err := c.Receive(); err == nil {
		t.Fatal("revoked connection stayed open")
	}
	if r.executor.calls.Load() != 0 {
		t.Fatal("revoked device ran work")
	}
}

// R7-4 also fences the live replay road after the bounded permission expires.
func TestARevokedComputerCannotKeepWatchingAnExistingAttempt(t *testing.T) {
	r := newRig(t)
	clock := newClock()
	cfg := r.config()
	cfg.Clock = clock
	r.executor.run = func(ctx context.Context, _ *taskdoor.Attempt, j taskdoor.Journal) error {
		if err := j.Append(taskdoor.Phase{Words: "running"}); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}
	r.serve(cfg)
	c := r.dial()
	id := attempt(803)
	send(t, c, id, request())
	receive(t, c, "started")
	send(t, c, id, taskdoor.Watch{})
	receive(t, c, "phase")
	r.rosterMu.Lock()
	r.active[r.a.ID()] = false
	r.rosterMu.Unlock()
	// Keep activity current while advancing the permission lifetime, so an
	// unrelated keepalive timeout cannot satisfy the revocation check.
	for n := 0; n < 3; n++ {
		clock.advance(taskdoor.PingEvery)
		send(t, c, id, taskdoor.Watch{})
		receive(t, c, "phase")
	}
	clock.advance(taskdoor.PingEvery)
	var no taskdoor.Refused
	decode(t, receive(t, c, "refused"), &no)
	if no.Reason != taskdoor.RemovedComputerLine {
		t.Fatalf("reason = %q", no.Reason)
	}
	if _, err := c.Receive(); err == nil {
		t.Fatal("revoked watcher stayed connected")
	}
}
