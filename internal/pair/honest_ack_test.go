package pair

// The word that ends a pairing on the device that showed the code. The joining
// device says it has the answer only once it has decided to keep it, and before
// it writes any of it; a device that refuses says nothing, so the device that
// approved never says two computers are paired when one of them refused.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// ownChat gives a home one chat with a turn in it, which is what a refusal counts.
func ownChat(t *testing.T, home, name string) {
	t.Helper()
	dir := filepath.Join(home, "v3", "projects", "project", name, ".cell")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(`{"type":"message","role":"user","content":"hello"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// homeOfItsOwn is a joining computer whose identity is not solo (an older build
// ended the marker when it showed a code, or the computers it shared with were
// removed) and that holds a chat: the computer the approver used to hear was paired.
func homeOfItsOwn(t *testing.T, r *chatRig) {
	t.Helper()
	if _, err := identity.Ensure(r.homeB); err != nil {
		t.Fatal(err)
	}
	ownChat(t, r.homeB, "mine")
}

// vanish deletes the mailbox instead of storing a write, as a relay that was
// restarted or dropped the mailbox early would, and tells the writer it is gone.
func vanish(h *hostileBox, w write) (int, error) {
	h.mu.Lock()
	inner, key := h.inner, h.keyOf[w.plate]
	h.mu.Unlock()
	_ = inner.Delete(context.Background(), w.plate, key)
	return 0, pairbox.ErrGone
}

// storeThenVanish stores a write and deletes the mailbox straight after, so the
// reader may or may not see the write before the mailbox is gone.
func storeThenVanish(h *hostileBox, w write) (int, error) {
	n, err := h.store(w)
	_, _ = vanish(h, w)
	return n, err
}

// changedBesidesLock is every file of a home that differs from before, except the
// home's lock file, which is taken after the answer and before the decision.
func changedBesidesLock(t *testing.T, before, after map[string][]byte) []string {
	t.Helper()
	var changed []string
	for name, now := range after {
		if filepath.Base(name) == "vault.enc.lock" {
			continue
		}
		if was, ok := before[name]; !ok || string(was) != string(now) {
			changed = append(changed, filepath.Base(name))
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			changed = append(changed, filepath.Base(name))
		}
	}
	return changed
}

// A COMPUTER THAT REFUSES THE GRANT SAYS NOTHING BACK. It wrote the first and
// third messages and never the word, so the computer that approved waits for it
// and says the pairing was not confirmed, naming the computer to look at.
func TestRefusingJoinerSendsNoWord(t *testing.T) {
	r, hostile := hostileRig(t)
	homeOfItsOwn(t, r)
	before := filesIn(t, r.homeB)

	a, err := pairRefused(t, r, Grant{Identity: r.a}, joiningAt(r.homeB))
	if !errors.Is(err, ErrDifferentChats) || OwnChats(err) != 1 {
		t.Fatalf("B was told %v (%d chats), want its 1 chat of its own", err, OwnChats(err))
	}
	if wrote := len(hostile.wroteBy(pairbox.SideB)); wrote != 2 {
		t.Fatalf("B wrote %d messages, want messages 1 and 3 and no word", wrote)
	}
	wantNotConfirmed(t, a, "laptop")
	same(t, before, filesIn(t, r.homeB))
}

// A NO FROM THE OTHER SCREEN IS READ AND SAID SO, so the computer that said no
// ends at once rather than waiting out a word that would never come.
func TestRefusalIsStillAcknowledged(t *testing.T) {
	r, hostile := hostileRig(t)
	a := newScreen()
	a.answer = func(context.Context, string, string) bool { return false }
	offer := startOffer(t, r, bounded(t, time.Minute), Grant{Identity: r.a}, a)
	code := a.nextCode(t)
	if _, err := Join(bounded(t, 20*time.Second), r.route, joiningAt(r.homeB), code.Shown(), newScreen()); !errors.Is(err, ErrRefused) {
		t.Fatalf("B was told %v, want refused", err)
	}
	if end := offer.wait(t); !errors.Is(end.err, ErrRefused) {
		t.Fatalf("A ended with %v, want refused", end.err)
	}
	if wrote := len(hostile.wroteBy(pairbox.SideB)); wrote != 3 {
		t.Fatalf("B wrote %d messages, want the word after the no", wrote)
	}
	untouched(t, r.homeB)
}

// THE WORD GOES OUT BEFORE THE GRANT IS WRITTEN. When the joining computer posts
// it, nothing of the grant is on its disk yet; the pairing then finishes on
// both, and the approver names the computer it paired.
func TestWordPrecedesTheFirstWrite(t *testing.T) {
	r, hostile := hostileRig(t)
	// A computer that has been started and not used: it keeps its state through
	// the recorded switch, whose every step comes after the word.
	if _, err := identity.EnsureSolo(r.homeB); err != nil {
		t.Fatal(err)
	}
	before := filesIn(t, r.homeB)
	var early []string
	posted := false
	hostile.on(5, func(h *hostileBox, w write) (int, error) {
		posted = true
		early = changedBesidesLock(t, before, filesIn(t, r.homeB))
		return h.storeAndRelease(w)
	})

	a := newScreen()
	offer := startOffer(t, r, bounded(t, time.Minute), Grant{Identity: r.a}, a)
	code := a.nextCode(t)
	if _, err := Join(bounded(t, 20*time.Second), r.route, joiningAt(r.homeB), code.Shown(), newScreen()); err != nil {
		t.Fatalf("B was told %v", err)
	}
	if end := offer.wait(t); end.err != nil || end.label != "laptop" {
		t.Fatalf("A ended with %q, %v, want paired with laptop", end.label, end.err)
	}
	if !posted {
		t.Fatal("B never said it kept the grant")
	}
	if len(early) != 0 {
		t.Fatalf("B wrote %v before it said it kept the grant", early)
	}
	if held, err := identity.Load(r.homeB); err != nil || held.ID() != r.a.ID() {
		t.Fatalf("B does not hold the grant (%v)", err)
	}
}

// A WORD THE RELAY LOSES LEAVES THE JOINING COMPUTER PAIRED, and the approver
// saying it was not confirmed and to look there: the one disagreement a lost
// message can cause, and the one whose sentence stays true.
func TestLostWordIsNotConfirmed(t *testing.T) {
	r, hostile := hostileRig(t)
	hostile.on(5, swallow)
	a := newScreen()
	offer := startOffer(t, r, bounded(t, refuseWithin), Grant{Identity: r.a}, a)
	code := a.nextCode(t)
	if _, err := Join(bounded(t, 20*time.Second), r.route, joiningAt(r.homeB), code.Shown(), newScreen()); err != nil {
		t.Fatalf("B was told %v", err)
	}
	wantNotConfirmed(t, offer.wait(t), "laptop")
	if held, err := identity.Load(r.homeB); err != nil || held.ID() != r.a.ID() {
		t.Fatalf("B does not hold the grant (%v)", err)
	}
}

// A MAILBOX THAT VANISHES WHILE THE APPROVER WAITS FOR THE WORD IS NOT A PAIRING
// THAT FINISHED. Whether the joining computer read the answer first or not, it
// keeps nothing, and the approver says it was not confirmed.
func TestMailboxVanishesDuringTheWait(t *testing.T) {
	for _, c := range []struct {
		name string
		n    int
		fate fate
	}{
		{"under the joining computer's word", 5, vanish},
		{"just after the answer", 4, storeThenVanish},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, hostile := hostileRig(t)
			hostile.on(c.n, c.fate)
			a := newScreen()
			offer := startOffer(t, r, bounded(t, time.Minute), Grant{Identity: r.a}, a)
			code := a.nextCode(t)
			_, err := Join(bounded(t, 20*time.Second), r.route, joiningAt(r.homeB), code.Shown(), newScreen())
			if !errors.Is(err, ErrDidNotFinish) && !errors.Is(err, ErrCodeDidNotWork) {
				t.Fatalf("B was told %v, want that the pairing did not finish", err)
			}
			wantNotConfirmed(t, offer.wait(t), "laptop")
			if changed := changedBesidesLock(t, map[string][]byte{}, filesIn(t, r.homeB)); len(changed) != 0 {
				t.Fatalf("B wrote %v", changed)
			}
		})
	}
}

// A COMPUTER WHOSE IDENTITY NO OTHER COMPUTER HOLDS IS REFUSED BEFORE THE CODE IS
// USED, as a solo one is: its chats are its own, so nobody else could be the one
// offering them. It does not even ask the relay; one with nothing to lose goes on.
func TestAloneRefusesBeforeTheCode(t *testing.T) {
	for _, c := range []struct {
		name    string
		alone   bool
		used    bool
		refused bool
	}{
		{"alone with a chat", true, true, true},
		{"alone with nothing to lose", true, false, false},
		{"not known to be alone", false, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, hostile := hostileRig(t)
			if _, err := identity.Ensure(r.homeB); err != nil {
				t.Fatal(err)
			}
			if c.used {
				ownChat(t, r.homeB, "mine")
			}
			j := joiningAt(r.homeB)
			j.Alone = c.alone
			_, err := Join(bounded(t, 10*time.Second), r.route, j, "42-715-302", newScreen())
			asked := hostile.calledTimes("Limits")
			if c.refused {
				if !errors.Is(err, ErrDifferentChats) || OwnChats(err) != 1 || asked != 0 {
					t.Fatalf("B was told %v after %d relay calls, want its 1 chat before any", err, asked)
				}
				return
			}
			if errors.Is(err, ErrDifferentChats) || asked != 1 {
				t.Fatalf("B was told %v after %d relay calls, want the relay asked", err, asked)
			}
		})
	}
}
