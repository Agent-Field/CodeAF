package vaultsync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/keys"
)

const (
	// CredentialsFile is the name the connections store keeps its keys under.
	CredentialsFile = "credentials.json"
	// credentialsID is the vault slot that holds the file. It is not a random
	// secret id, so every machine addresses the same slot and merge compares
	// their copies of it.
	credentialsID = "home:" + CredentialsFile
	// credentialsScope matches no project (a project is a remote or a cell id),
	// so Entries never offers the file to a workspace's .env.
	credentialsScope = "home:credentials"
)

// credentials moves the one credentials.json between the disk and the vault.
type credentials struct {
	path  string
	vault VaultFile
}

func (s Syncer) credentials() credentials {
	return credentials{path: s.CredentialsPath, vault: s.Vault}
}

// Capture stores a changed credentials.json in the vault without any network,
// so a caller that fingerprints the vault sees the change.
func (s Syncer) Capture() error { return s.credentials().capture() }

// capture copies the file into the vault slot when it differs from the slot,
// stamped with the file's modification time rather than the time of capture: an
// edit made here yesterday must not beat a newer one another machine sent today
// just because this machine looked at the file later. A
// missing file tombstones the slot, which is how a removal travels; the vault
// leaves an already absent slot alone. A damaged file is left out, because
// storing it would replace a good copy with one nobody can read.
func (c credentials) capture() error {
	raw, at, err := c.read()
	if errors.Is(err, os.ErrNotExist) {
		return c.vault.Delete(credentialsID)
	}
	if err != nil || !json.Valid(raw) {
		return err
	}
	if held, err := c.vault.Get(credentialsID); err == nil && held.Value == string(raw) {
		return nil
	}
	return c.vault.PutAt(credentialsID, keys.Entry{Name: CredentialsFile, Value: string(raw), Scope: credentialsScope}, at)
}

// read answers the file and when it was last saved.
func (c credentials) read() ([]byte, time.Time, error) {
	info, err := os.Stat(c.path)
	if err != nil {
		return nil, time.Time{}, err
	}
	raw, err := os.ReadFile(c.path)
	return raw, info.ModTime(), err
}

// restore makes the file match the slot: a live slot is written, a tombstone
// removes the file, and a slot that never existed leaves the file alone.
func (c credentials) restore() error {
	held, err := c.vault.Get(credentialsID)
	if errors.Is(err, keys.ErrNotFound) {
		return c.restoreAbsence()
	}
	if err != nil {
		return err
	}
	return c.write([]byte(held.Value))
}

func (c credentials) restoreAbsence() error {
	gone, err := c.vault.Deleted(credentialsID)
	if err != nil || !gone {
		return err
	}
	return ignoreMissing(os.Remove(c.path))
}

// write replaces the file with raw under the lock the connections store takes,
// so a save in another process is not overwritten between its read and its
// rename. A damaged file already there is kept beside it first: the person may
// have been mid-edit.
func (c credentials) write(raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	unlock, err := lockFile(c.path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	if same, err := c.holds(raw); same || err != nil {
		return err
	}
	return writeAtomic(c.path, raw)
}

// holds reports whether the file already has raw, and sets a damaged file aside
// so the write that follows does not destroy it.
func (c credentials) holds(raw []byte) (bool, error) {
	have, err := os.ReadFile(c.path)
	if err != nil {
		return false, ignoreMissing(err)
	}
	if string(have) == string(raw) {
		return true, nil
	}
	if json.Valid(have) {
		return false, nil
	}
	return false, os.Rename(c.path, c.path+".damaged")
}

func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(f, true, false); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = filelock.Unlock(f); f.Close() }, nil
}

// writeAtomic writes through a 0600 temporary file and a rename, so the keys
// are never briefly readable by others and a killed process leaves the old file.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credentials-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	_, err = tmp.Write(data)
	if err = errors.Join(err, tmp.Close()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func ignoreMissing(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
