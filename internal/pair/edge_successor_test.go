package pair

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
)

// pairGrant is pairOnce with a grant the test chose.
func pairGrant(t *testing.T, r *chatRig, grant Grant, j Joining) (Joined, error) {
	t.Helper()
	a := newScreen()
	offer := startOffer(t, r, bounded(t, time.Minute), grant, a)
	code := a.nextCode(t)
	joined, err := Join(bounded(t, 20*time.Second), r.route, j, code.Shown(), newScreen())
	offer.wait(t)
	return joined, err
}

// A COMPUTER ON AN IDENTITY THAT WAS ROTATED FOLLOWS IT. It needs no --replace,
// keeps its secrets (sealed again under the new key), and pairs others in turn.
func TestJoinAcceptsSuccessor(t *testing.T) {
	r := newChatRig(t)
	old, err := identity.Ensure(r.homeB)
	if err != nil {
		t.Fatal(err)
	}
	vault, err := keys.Open(r.homeB)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Put("p/KEY", keys.Entry{Name: "KEY", Value: "kept", Scope: "p"}); err != nil {
		t.Fatal(err)
	}

	joined, err := pairGrant(t, r, Grant{Identity: r.a, Replaces: []string{old.ID()}}, joiningAt(r.homeB))
	if err != nil || !joined.Successor || joined.Already {
		t.Fatalf("joined = %+v, %v", joined, err)
	}
	now, err := identity.Load(r.homeB)
	if err != nil || now.ID() != r.a.ID() {
		t.Fatalf("identity after following = %v, %v", now.ID(), err)
	}
	dev, err := identity.Device(r.homeB)
	if err != nil || dev.Cert.Verify(now.PublicKey()) != nil {
		t.Fatalf("device does not belong to the new identity: %v", err)
	}
	reopened, err := keys.Open(r.homeB)
	if err != nil {
		t.Fatal(err)
	}
	if e, err := reopened.Get("p/KEY"); err != nil || e.Value != "kept" {
		t.Fatalf("secret after following = %+v, %v", e, err)
	}
	if got := identity.Predecessors(r.homeB); len(got) != 1 || got[0] != old.ID() {
		t.Fatalf("predecessors on the follower = %v", got)
	}
}

// A computer on an identity the grant does not name is treated as any other
// computer with chats of its own: refused, and left exactly as it was.
func TestJoinRefusesUnrelated(t *testing.T) {
	r := newChatRig(t)
	mine, err := identity.Ensure(r.homeB)
	if err != nil {
		t.Fatal(err)
	}
	stranger, _ := identity.Mint()
	before := filesIn(t, r.homeB)
	_, err = pairGrant(t, r, Grant{Identity: r.a, Replaces: []string{stranger.ID()}}, joiningAt(r.homeB))
	if !errors.Is(err, ErrDifferentChats) {
		t.Fatalf("B was told %v", err)
	}
	same(t, before, filesIn(t, r.homeB))
	if held, _ := identity.Load(r.homeB); held.ID() != mine.ID() {
		t.Fatal("this computer's identity was disturbed")
	}
}

// The grant carries the lineage to the joiner intact.
func TestGrantCarriesReplaces(t *testing.T) {
	old, _ := identity.Mint()
	got := roundTrip(t, Grant{Identity: old, Replaces: []string{old.ID()}})
	if len(got.Replaces) != 1 || got.Replaces[0] != old.ID() || !bytes.Equal(got.Identity.CellKey(), old.CellKey()) {
		t.Fatalf("grant after the wire = %+v", got.Replaces)
	}
}

// A lineage that is not ids, or is longer than an identity keeps, is no grant.
func TestGrantBoundsReplaces(t *testing.T) {
	id, _ := identity.Mint()
	bad := map[string][]string{
		"not an id": {"../etc/passwd"},
		"too many":  make([]string, identity.MaxPredecessors+1),
	}
	for i := range bad["too many"] {
		other, _ := identity.Mint()
		bad["too many"][i] = other.ID()
	}
	for name, lineage := range bad {
		raw, err := Grant{Identity: id, Replaces: lineage}.verdictOf()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := hearGrant(raw); !errors.Is(err, ErrBadGrant) {
			t.Errorf("%s: %v, want ErrBadGrant", name, err)
		}
	}
}

func roundTrip(t *testing.T, g Grant) Grant {
	t.Helper()
	raw, err := g.verdictOf()
	if err != nil {
		t.Fatal(err)
	}
	got, err := hearGrant(raw)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// Each way a join can end has its own sentence, and following a rotation does
// not claim the computer was given someone's chats.
func TestJoinedSentence(t *testing.T) {
	for joined, want := range map[Joined]string{
		{}:                JoinedLine,
		{Already: true}:   ErrAlreadyPaired.Error(),
		{Successor: true}: FollowedLine,
	} {
		if got := joined.Sentence(); got != want {
			t.Errorf("%+v says %q, want %q", joined, got, want)
		}
	}
}
