package vaultsync

import (
	"fmt"
	"maps"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
)

// countedVault counts every call into the vault file. Each one opens the whole
// sealed vault (read, decrypt, decode), so the count is the work a step does.
type countedVault struct {
	VaultFile
	calls int
}

func (c *countedVault) Export() ([]byte, error) { c.calls++; return c.VaultFile.Export() }
func (c *countedVault) Merge(enc []byte) error  { c.calls++; return c.VaultFile.Merge(enc) }
func (c *countedVault) Entries(p string) ([]keys.Entry, error) {
	c.calls++
	return c.VaultFile.Entries(p)
}
func (c *countedVault) Get(id string) (keys.Entry, error) { c.calls++; return c.VaultFile.Get(id) }
func (c *countedVault) IDs(prefix string) ([]string, error) {
	c.calls++
	return c.VaultFile.IDs(prefix)
}
func (c *countedVault) PutAt(id string, e keys.Entry, at time.Time) error {
	c.calls++
	return c.VaultFile.PutAt(id, e, at)
}
func (c *countedVault) Delete(id string) error { c.calls++; return c.VaultFile.Delete(id) }
func (c *countedVault) Deleted(id string) (bool, error) {
	c.calls++
	return c.VaultFile.Deleted(id)
}
func (c *countedVault) Removed(prefix string) ([]string, error) {
	c.calls++
	return c.VaultFile.Removed(prefix)
}

func (c *countedVault) Revisions(scope string) (map[string]keys.Revision, error) {
	c.calls++
	return c.VaultFile.(*keys.Vault).Revisions(scope)
}

// heldKeys is a Keyset that holds its keys in memory.
type heldKeys map[string]string

func (h heldKeys) Values() (map[string]string, error) { return maps.Clone(h), nil }
func (h heldKeys) Holds(string) bool                  { return true }
func (h heldKeys) Apply(changes map[string]string) error {
	for name, value := range changes {
		if value == "" {
			delete(h, name)
		} else {
			h[name] = value
		}
	}
	return nil
}

type rejectedKeys struct{ heldKeys }

func (rejectedKeys) Accept(string, string, string) bool { return false }

func TestSelectiveRestoreM1BoundedVaultReads(t *testing.T) {
	for _, n := range []int{16, 256} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			home := t.TempDir()
			_, err := identity.Ensure(home)
			must(t, err)
			v, err := keys.Open(home)
			must(t, err)
			set := rejectedKeys{heldKeys{}}
			for i := range n {
				set.heldKeys[fmt.Sprint(i)] = "before"
			}
			carrier := Keys("selective", set, filepath.Join(home, "selective.ledger"))
			must(t, carrier.capture(v))
			for name := range set.heldKeys {
				must(t, v.PutAt(carrier.id(name), keys.Entry{Name: name, Value: "incoming", Scope: carrier.scope}, time.Now().Add(time.Minute)))
			}
			counted := &countedVault{VaultFile: v}
			must(t, carrier.restore(counted))
			if counted.calls > 3 {
				t.Fatalf("M1 %d rejected keys opened the vault %d times; cap 3", n, counted.calls)
			}
			for name, value := range set.heldKeys {
				if value != "before" {
					t.Fatalf("M1 rejected value applied to %s", name)
				}
			}
			t.Logf("%d rejected keys: %d vault reads", n, counted.calls)
		})
	}
}

// TestARestoreOpensTheVaultAFixedNumberOfTimes holds the idle cost of a state
// round flat in the number of keys a machine shares. A restore runs after every
// pull, which an idle terminal makes every few seconds; a profile with a
// thousand shared memories once opened its whole vault once per key per round.
func TestARestoreOpensTheVaultAFixedNumberOfTimes(t *testing.T) {
	for _, n := range []int{16, 256} {
		t.Run(fmt.Sprint(n, " keys"), func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			_, err := identity.Ensure(home)
			must(t, err)
			v, err := keys.Open(home)
			must(t, err)
			set := heldKeys{}
			for i := range n {
				set[fmt.Sprintf("key-%04d", i)] = fmt.Sprintf("value %d", i)
			}
			carrier := Keys("test", set, filepath.Join(home, "test.ledger"))
			must(t, carrier.capture(v))

			counted := &countedVault{VaultFile: v}
			must(t, carrier.restore(counted))
			if counted.calls > 2 {
				t.Fatalf("a restore of %d unchanged keys opened the vault %d times, want at most 2", n, counted.calls)
			}

			// Reading the tombstones once still passes a removal on.
			must(t, v.Delete(carrier.id("key-0003")))
			must(t, carrier.restore(v))
			if _, still := set["key-0003"]; still {
				t.Fatal("a key the vault removed is still held after a restore")
			}
			if len(set) != n-1 {
				t.Fatalf("a restore after one removal holds %d keys, want %d", len(set), n-1)
			}
		})
	}
}
