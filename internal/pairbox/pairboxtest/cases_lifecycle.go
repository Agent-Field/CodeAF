package pairboxtest

import (
	"bytes"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// A nameplate that was never made, or is gone, is answered as gone by every
// verb, and a malformed one is answered the same so a probe learns nothing.
func unknownIs404(t *testing.T, e env) {
	m := e.open(t)
	if err := e.Box.Delete(e.ctx, m.plate, m.key); err != nil {
		t.Fatalf("Delete of a live mailbox: %v", err)
	}
	for _, plate := range []string{m.plate, "not-a-plate"} {
		for verb, err := range e.verbs(mailbox{plate, m.key}) {
			wantIs(t, verb+" on "+plate, err, pairbox.ErrGone)
		}
	}
}

// liveTTLCap is the longest TTL a live relay may advertise for the suite to
// wait it out in real time.
const liveTTLCap = 5 * time.Second

// expirySlack is waited beyond the TTL so the boundary itself is not the test.
const expirySlack = time.Second

// A mailbox that has outlived its TTL answers gone to every verb.
func expiryDeletes(t *testing.T, e env) {
	if !e.fake() && e.lim.TTL > liveTTLCap {
		t.Skipf("a ttl of %v is too long to wait out on a live clock", e.lim.TTL)
	}
	m := e.open(t)
	e.post(t, m, pairbox.SideA, m.key, []byte("before"))
	e.Clock.Wait(e.lim.TTL + expirySlack)
	for verb, err := range e.verbs(m) {
		wantIs(t, verb+" after the ttl", err, pairbox.ErrGone)
	}
}

// Deleting a mailbox ends the polls waiting on it at once, with gone.
func deleteWakesPoll(t *testing.T, e env) {
	m := e.open(t)
	waiting := e.pollAsync(m, pairbox.SideA, 0)
	if err := e.Box.Delete(e.ctx, m.plate, m.key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	wantIs(t, "the waiting poll", await(t, waiting).err, pairbox.ErrGone)
}

// A poll with a wait answers the moment something is posted, answers empty
// when the wait runs out, resumes from a position, and answers gone when the
// mailbox is deleted under it.
func longPoll(t *testing.T, e env) {
	m := e.open(t)
	pollEndsEmpty(t, e, m)
	pollWokenByPost(t, e, m)
	pollResumes(t, e, m)
	pollWokenByDelete(t, e, m)
}

// shortWait is a wait long enough to measure and short enough to run often.
const shortWait = 300 * time.Millisecond

func pollEndsEmpty(t *testing.T, e env, m mailbox) {
	t.Helper()
	start := time.Now()
	got, err := e.Box.Poll(e.ctx, m.plate, pairbox.SideA, 0, shortWait)
	if err != nil || len(got.Msgs) != 0 || got.Next != 0 {
		t.Fatalf("an empty wait answered %+v, %v; want nothing at position 0", got, err)
	}
	if held := time.Since(start); held < shortWait/2 || held > promptly {
		t.Errorf("a wait of %v was held for %v", shortWait, held)
	}
}

func pollWokenByPost(t *testing.T, e env, m mailbox) {
	t.Helper()
	waiting := e.pollAsync(m, pairbox.SideA, 0)
	e.post(t, m, pairbox.SideA, m.key, []byte("first"))
	got := await(t, waiting)
	if got.err != nil || len(got.batch.Msgs) != 1 || !bytes.Equal(got.batch.Msgs[0], []byte("first")) {
		t.Fatalf("a post answered the waiting poll with %+v, %v", got.batch, got.err)
	}
}

func pollResumes(t *testing.T, e env, m mailbox) {
	t.Helper()
	e.post(t, m, pairbox.SideA, m.key, []byte("second"))
	got := e.poll(t, m, pairbox.SideA, 1)
	if len(got.Msgs) != 1 || !bytes.Equal(got.Msgs[0], []byte("second")) || got.Next != 2 {
		t.Fatalf("a poll after position 1 answered %+v, want only the second message and next 2", got)
	}
}

func pollWokenByDelete(t *testing.T, e env, m mailbox) {
	t.Helper()
	waiting := e.pollAsync(m, pairbox.SideB, 0)
	if err := e.Box.Delete(e.ctx, m.plate, m.key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	wantIs(t, "a poll waiting when the mailbox was deleted", await(t, waiting).err, pairbox.ErrGone)
}
