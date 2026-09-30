package pairboxtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// mailbox is a mailbox a case made and the key of its side a.
type mailbox struct {
	plate string
	key   pairbox.Key
}

func newKey(t *testing.T) pairbox.Key {
	t.Helper()
	k, err := pairbox.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// tryOpen creates a mailbox on box and arranges for it to be deleted when the
// case ends, ignoring any error because the case may have deleted it already.
func (e env) tryOpen(t *testing.T, box pairbox.Box) (mailbox, error) {
	t.Helper()
	key := newKey(t)
	made, err := box.Create(e.ctx, key)
	if err != nil {
		return mailbox{}, err
	}
	e.forget(t, box, made.Nameplate, key)
	return mailbox{made.Nameplate, key}, nil
}

// forget deletes a mailbox at the end of the case. It uses a context of its
// own, since the case's may have ended.
func (e env) forget(t *testing.T, box pairbox.Box, plate string, key pairbox.Key) {
	t.Helper()
	peer := pairbox.WithPeer(context.Background(), "cleanup")
	t.Cleanup(func() { _ = box.Delete(peer, plate, key) })
}

// open is tryOpen for a case that needs the mailbox to exist.
func (e env) open(t *testing.T) mailbox {
	t.Helper()
	m, err := e.tryOpen(t, e.Box)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return m
}

// post writes msg and fails the case if the mailbox refuses it.
func (e env) post(t *testing.T, m mailbox, side pairbox.Side, key pairbox.Key, msg []byte) int {
	t.Helper()
	n, err := e.Box.Post(e.ctx, m.plate, side, key, msg)
	if err != nil {
		t.Fatalf("Post to side %s: %v", side, err)
	}
	return n
}

// poll reads a side without waiting.
func (e env) poll(t *testing.T, m mailbox, side pairbox.Side, after int) pairbox.Batch {
	t.Helper()
	b, err := e.Box.Poll(e.ctx, m.plate, side, after, 0)
	if err != nil {
		t.Fatalf("Poll of side %s: %v", side, err)
	}
	return b
}

// verbs is the answer of every verb that takes a nameplate.
func (e env) verbs(m mailbox) map[string]error {
	_, post := e.Box.Post(e.ctx, m.plate, pairbox.SideA, m.key, []byte("x"))
	_, poll := e.Box.Poll(e.ctx, m.plate, pairbox.SideA, 0, 0)
	return map[string]error{"Post": post, "Poll": poll, "Delete": e.Box.Delete(e.ctx, m.plate, m.key)}
}

// wantIs fails the case unless err is the refusal the contract names.
func wantIs(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("%s: got %v, want %v", what, err, want)
	}
}

// fake reports whether the rig's clock is one a case moves by itself, which is
// what lets a case wait out a ten minute TTL.
func (e env) fake() bool {
	_, ok := e.Clock.(interface{ Advance(time.Duration) })
	return ok
}

// settle is how long a case lets a poll reach its wait before it acts, so the
// wake-up is what ends the poll and not a poll that had not started yet.
const settle = 100 * time.Millisecond

// promptly is how long a wake-up may take to reach a waiting poll.
const promptly = 5 * time.Second

type pollResult struct {
	batch   pairbox.Batch
	err     error
	elapsed time.Duration
}

// pollAsync starts a poll that waits as long as the mailbox allows and returns
// once it is waiting.
func (e env) pollAsync(m mailbox, side pairbox.Side, after int) <-chan pollResult {
	out := make(chan pollResult, 1)
	go func() {
		start := time.Now()
		b, err := e.Box.Poll(e.ctx, m.plate, side, after, pairbox.MaxWait)
		out <- pollResult{b, err, time.Since(start)}
	}()
	time.Sleep(settle)
	return out
}

// await is the poll's answer, failing the case if the poll is still waiting
// after the time a wake-up should take.
func await(t *testing.T, res <-chan pollResult) pollResult {
	t.Helper()
	select {
	case r := <-res:
		return r
	case <-time.After(promptly):
		t.Fatalf("the poll was not woken within %v", promptly)
		return pollResult{}
	}
}
