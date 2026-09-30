package vaultsync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

const (
	// CredentialsFile is the name the connections store keeps its keys under.
	CredentialsFile = "credentials.json"
	// homeScope matches no project (a project is a remote or a cell id), so
	// Entries never offers a carried slot to a workspace's .env.
	homeScope = "home:carried"
)

// Credentials carries the connections store at path. The slot id is not a random
// secret id, so every machine addresses the same slot.
func Credentials(path string) Carried {
	return Carried{ID: "home:" + CredentialsFile, Scope: homeScope, Name: CredentialsFile, Medium: credentialsFile{path}}
}

// credentialsFile is the connections file as a Medium.
type credentialsFile struct{ path string }

func (f credentialsFile) Read() (string, time.Time, error) {
	info, err := os.Stat(f.path)
	if err != nil {
		return "", time.Time{}, err
	}
	raw, err := os.ReadFile(f.path)
	if err == nil && !json.Valid(raw) {
		err = ErrDamaged
	}
	return string(raw), info.ModTime(), err
}

func (f credentialsFile) Realized(content string) string { return content }

func (f credentialsFile) Clear() error { return ignoreMissing(os.Remove(f.path)) }

// Write replaces the file under the lock the connections store takes, so a save
// in another process is not overwritten between its read and its rename. A
// damaged file already there is kept beside it first: the person may have been
// mid-edit.
func (f credentialsFile) Write(content string) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	unlock, err := lockFile(f.path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	if same, err := f.holds(content); same || err != nil {
		return err
	}
	return writeAtomic(f.path, []byte(content), 0o600)
}

// holds reports whether the file already has content, and sets a damaged file
// aside so the write that follows does not destroy it.
func (f credentialsFile) holds(content string) (bool, error) {
	have, err := os.ReadFile(f.path)
	if err != nil {
		return false, ignoreMissing(err)
	}
	if string(have) == content {
		return true, nil
	}
	if json.Valid(have) {
		return false, nil
	}
	return false, os.Rename(f.path, f.path+".damaged")
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

// writeAtomic writes through a temporary file that already has mode and a
// rename, so the keys are never briefly readable by more than the final file
// allows and a killed process leaves the old file.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".vaultsync-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
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
