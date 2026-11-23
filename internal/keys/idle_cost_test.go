package keys

import (
	"fmt"
	"testing"
)

// THE IDLE-COST LAW (PERF.md): a vault whose bytes have not changed is
// decrypted once, however many times it is read. Every chat's sync start used
// to open the vault once per slot it captured, twice over, and on a machine
// with a few dozen conversations that kept two cores busy for minutes after
// launch with nobody typing.
func TestAnUnchangedVaultIsDecryptedOnce(t *testing.T) {
	v, home := mustOpen(t)
	for i := range 40 {
		if err := v.Put(fmt.Sprintf("id%d", i), Entry{Name: fmt.Sprintf("K%d", i), Value: "v", Scope: "p"}); err != nil {
			t.Fatal(err)
		}
	}
	// Read once so the bound below counts only the idle reads that follow.
	if _, err := v.Get("id0"); err != nil {
		t.Fatal(err)
	}
	before := Opened()
	const reads = 200
	for i := range reads {
		// A fresh handle each time, as every sync pass opens its own.
		again, err := Open(home)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := again.Get(fmt.Sprintf("id%d", i%40)); err != nil {
			t.Fatal(err)
		}
		if _, err := again.Entries("p"); err != nil {
			t.Fatal(err)
		}
	}
	got := Opened() - before
	t.Logf("%d reads of an unchanged vault decrypted it %d times", 2*reads, got)
	if got != 0 {
		t.Fatalf("%d reads of an unchanged vault decrypted it %d times, want 0", 2*reads, got)
	}
}

// A write is seen by the very next read, and a change made through one read's
// document never leaks into what another read answers.
func TestAVaultReadSeesEveryWrite(t *testing.T) {
	v, home := mustOpen(t)
	if err := v.Put("a", Entry{Name: "A", Value: "1", Scope: "p"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Get("a"); got.Value != "1" {
		t.Fatalf("got %+v", got)
	}
	other, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Put("a", Entry{Name: "A", Value: "2", Scope: "p"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Get("a"); got.Value != "2" {
		t.Fatalf("a read after another handle's write answered %+v", got)
	}
	// An update that changes nothing must not have touched the shared copy.
	if err := v.update(func(d *vaultDoc) error {
		d.Secrets["ghost"] = record{Entry: Entry{Name: "G", Value: "x", Scope: "p"}}
		return errUnchanged
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get("ghost"); err != ErrNotFound {
		t.Fatalf("an unwritten update leaked into later reads: %v", err)
	}
	if err := v.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get("a"); err != ErrNotFound {
		t.Fatalf("a deleted secret was still answered: %v", err)
	}
}
