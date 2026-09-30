package pairboxtest

import (
	"bytes"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// claimers is how many devices race for side b.
const claimers = 20

// Of many devices that write side b with keys of their own, the first claims
// it and every other is told the side belongs to someone else.
func claimRace(t *testing.T, e env) {
	m := e.open(t)
	keys := make([]pairbox.Key, claimers)
	for i := range keys {
		keys[i] = newKey(t)
	}
	errs := make([]error, claimers)
	var wg sync.WaitGroup
	for i := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = e.Box.Post(e.ctx, m.plate, pairbox.SideB, keys[i], []byte{byte(i)})
		}()
	}
	wg.Wait()
	winners := 0
	for _, err := range errs {
		if err == nil {
			winners++
			continue
		}
		wantIs(t, "a losing claimant", err, pairbox.ErrForbidden)
	}
	if winners != 1 {
		t.Fatalf("%d claimants won side b, want exactly 1", winners)
	}
	if got := e.poll(t, m, pairbox.SideB, 0); len(got.Msgs) != 1 {
		t.Errorf("side b holds %d messages after the race, want 1", len(got.Msgs))
	}
}

// A key opens only its own side, and only a mailbox's own keys delete it.
func wrongKey(t *testing.T, e env) {
	m := e.open(t)
	keyB, stranger := newKey(t), newKey(t)
	e.post(t, m, pairbox.SideA, m.key, []byte("a"))
	e.post(t, m, pairbox.SideB, keyB, []byte("b"))

	_, err := e.Box.Post(e.ctx, m.plate, pairbox.SideA, stranger, []byte("x"))
	wantIs(t, "a stranger writing side a", err, pairbox.ErrForbidden)
	_, err = e.Box.Post(e.ctx, m.plate, pairbox.SideB, m.key, []byte("x"))
	wantIs(t, "side a's key writing side b", err, pairbox.ErrForbidden)
	_, err = e.Box.Post(e.ctx, m.plate, pairbox.SideA, keyB, []byte("x"))
	wantIs(t, "side b's key writing side a", err, pairbox.ErrForbidden)
	wantIs(t, "a stranger deleting", e.Box.Delete(e.ctx, m.plate, stranger), pairbox.ErrForbidden)

	if got := e.poll(t, m, pairbox.SideA, 0); len(got.Msgs) != 1 {
		t.Errorf("side a holds %d messages after refused writes, want 1", len(got.Msgs))
	}
	if err := e.Box.Delete(e.ctx, m.plate, keyB); err != nil {
		t.Errorf("side b's key deleting: %v", err)
	}
	wantIs(t, "Poll after the delete", e.verbs(m)["Poll"], pairbox.ErrGone)
}

// Every byte value survives a round trip on both sides: the mailbox carries
// ciphertext and must neither interpret nor alter it.
func opaque(t *testing.T, e env) {
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	if e.lim.MaxMsg < len(all) {
		t.Skipf("a message limit of %d cannot carry 256 bytes", e.lim.MaxMsg)
	}
	m := e.open(t)
	keyB := newKey(t)
	for side, key := range map[pairbox.Side]pairbox.Key{pairbox.SideA: m.key, pairbox.SideB: keyB} {
		if n := e.post(t, m, side, key, all); n != 0 {
			t.Errorf("first message on side %s has index %d", side, n)
		}
		got := e.poll(t, m, side, 0)
		if len(got.Msgs) != 1 || !bytes.Equal(got.Msgs[0], all) || got.Next != 1 {
			t.Errorf("side %s round trip changed the bytes: %+v", side, got)
		}
	}
}

// nameplatesShown is how many live mailboxes NameplatesDistinct holds at once.
const nameplatesShown = 5

// Live mailboxes never share a nameplate, and a nameplate is one to four
// digits so a person can read it to another.
func nameplatesDistinct(t *testing.T, e env) {
	seen := map[string]bool{}
	for range min(nameplatesShown, e.lim.CreatePerHour) {
		plate := e.open(t).plate
		if seen[plate] {
			t.Errorf("nameplate %q was given to two live mailboxes", plate)
		}
		seen[plate] = true
		if !isPlate(plate) {
			t.Errorf("nameplate %q is not one to four digits", plate)
		}
	}
}

func isPlate(s string) bool {
	if len(s) < 1 || len(s) > 4 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
