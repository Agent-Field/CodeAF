package keys

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
)

// THE VAULT CARRIES SECRETS, NOT FILES OF ANY SIZE ([MaxSlotValue]).
func TestAValueLargerThanASlotIsRefused(t *testing.T) {
	v, _ := mustOpen(t)
	err := v.Put("big", Entry{Name: "BIG", Value: strings.Repeat("x", MaxSlotValue+1), Scope: "p"})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Put of a value over the bound answered %v, want ErrTooLarge", err)
	}
	if err := v.Put("edge", Entry{Name: "EDGE", Value: strings.Repeat("x", MaxSlotValue), Scope: "p"}); err != nil {
		t.Fatalf("a value of exactly the bound was refused: %v", err)
	}
}

// A vault written before the bound, holding a slot over it, is written back
// without that slot the first time it is opened: the file shrinks, the small
// secrets stay, and the slot is gone rather than tombstoned, so no machine's
// copy of the file it carried is deleted. On the owner's machine that slot was
// a 462 MB graph.db, and every read of the vault paid for it.
func TestAnOversizedSlotIsShedOnOpenWithoutATombstone(t *testing.T) {
	v, home := mustOpen(t)
	if err := v.Put("small", Entry{Name: "K", Value: "v", Scope: "p"}); err != nil {
		t.Fatal(err)
	}
	stamp := writeBehindTheBound(t, v, "home:file:chat/graph.db", strings.Repeat("A", 3*MaxSlotValue))
	before := fileSize(t, v.path)

	again, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := again.Get("home:file:chat/graph.db"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the oversized slot is still read back: %v", err)
	}
	if gone, _ := again.Deleted("home:file:chat/graph.db"); gone {
		t.Fatal("the oversized slot was tombstoned; a tombstone deletes the file on every machine")
	}
	if got, err := again.Get("small"); err != nil || got.Value != "v" {
		t.Fatalf("the small secret did not survive: %+v %v", got, err)
	}
	after := fileSize(t, v.path)
	if after >= before || after > MaxSlotValue {
		t.Fatalf("vault.enc is %d bytes after the drop (was %d)", after, before)
	}
	// Dropping a slot is not an edit: the document keeps its stamp.
	d, err := again.readLocked()
	if err != nil {
		t.Fatal(err)
	}
	if d.Updated != stamp {
		t.Fatalf("the document stamp moved from %d to %d", stamp, d.Updated)
	}
}

// A merge cannot bring an oversized slot back from a machine still running a
// build that kept it.
func TestAMergeDoesNotTakeAnOversizedSlot(t *testing.T) {
	theirs, theirHome := mustOpen(t)
	writeBehindTheBound(t, theirs, "home:file:chat/pack", strings.Repeat("A", MaxSlotValue+1))
	enc, err := os.ReadFile(theirs.path)
	if err != nil {
		t.Fatal(err)
	}
	// The same person's other machine: another home holding the same identity.
	id, err := identity.Load(theirHome)
	if err != nil {
		t.Fatal(err)
	}
	ourHome := filepath.Join(t.TempDir(), "home")
	if err := identity.Save(ourHome, id); err != nil {
		t.Fatal(err)
	}
	ours, err := Open(ourHome)
	if err != nil {
		t.Fatal(err)
	}
	if err := ours.Merge(enc); err != nil {
		t.Fatal(err)
	}
	if _, err := ours.Get("home:file:chat/pack"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a merge took an oversized slot: %v", err)
	}
}

// writeBehindTheBound seals a slot past the bound the way a build before it
// could, and answers the document stamp it wrote.
func writeBehindTheBound(t *testing.T, v *Vault, id, value string) int64 {
	t.Helper()
	var stamp int64
	err := v.update(func(d *vaultDoc) error {
		d.Secrets[id] = record{Entry: Entry{Name: id, Value: value, Scope: "home:file:chat"}, Updated: 1}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Read the stamp straight from the sealed bytes, without the open that
	// would drop the slot.
	blob, err := os.ReadFile(v.path)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := open(v.key, blob)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plain), id) {
		t.Fatal("the oversized slot was not written")
	}
	d, err := openDoc(v.key, blob)
	if err != nil {
		t.Fatal(err)
	}
	stamp = d.Updated
	return stamp
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}
