package keys

import (
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// LockHome serializes vault transactions and identity switches across handles
// and processes. The state-sync lock cannot serve this purpose: its owner calls
// vault transactions while holding it during a network round.
func LockHome(home string) (func(), error) {
	if err := os.MkdirAll(home, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(home, "vault.enc.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(f, true, false); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = filelock.Unlock(f); _ = f.Close() }, nil
}

func (v *Vault) lock() (func(), error) {
	v.mu.Lock()
	unlock, err := LockHome(filepath.Dir(v.path))
	if err != nil {
		v.mu.Unlock()
		return nil, err
	}
	done := func() { unlock(); v.mu.Unlock() }
	if v.home != "" {
		id, err := identity.Load(v.home)
		if err != nil {
			done()
			return nil, err
		}
		v.key = id.CellKey()
	}
	return done, nil
}
