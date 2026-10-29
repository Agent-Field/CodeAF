package pair

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
)

const mergeFile = "pair-merge.json"

var mergeMu sync.Mutex

// mergeRecord keeps the approved destination before the first local change.
// The old key allows startup to finish staging; the target device makes every
// interrupted installation repeatable. This private file is removed at the end.
type mergeRecord struct {
	OldKey     []byte          `json:"old_key"`
	Identity   json.RawMessage `json:"identity"`
	Device     json.RawMessage `json:"device"`
	SyncURL    string          `json:"sync_url,omitempty"`
	Replace    bool            `json:"replace,omitempty"`
	ProfileDir string          `json:"profile_dir,omitempty"`
}

// Resume completes an approved local merge before sync opens its clients.
// The live chats and their engine store are never moved or rewritten.
func Resume(home string, saveSyncURL func(string) error) error {
	mergeMu.Lock()
	defer mergeMu.Unlock()
	return (Joining{Home: home, SaveSyncURL: saveSyncURL}).resume()
}

func (j Joining) after(step string) error {
	if j.After != nil {
		return j.After(step)
	}
	return nil
}

func (j Joining) merge(g Grant, dev *identity.Dev, oldKey []byte) error {
	if dev == nil {
		made, err := identity.NewDevice(g.Identity)
		if err != nil {
			return err
		}
		dev = &made
	}
	idDoc, err := g.Identity.Marshal()
	if err != nil {
		return err
	}
	devDoc, err := dev.Marshal()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(mergeRecord{OldKey: oldKey, Identity: idDoc, Device: devDoc, SyncURL: g.SyncURL, Replace: j.Replace, ProfileDir: j.ProfileDir})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(j.Home, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(j.Home, ".pair-merge-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(raw)
	if err := errors.Join(f.Chmod(0600), writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	if err := fsreplace.Rename(f.Name(), filepath.Join(j.Home, mergeFile)); err != nil {
		return err
	}
	if err := j.after("recorded"); err != nil {
		return err
	}
	return j.resume()
}

func (j Joining) resume() error {
	raw, err := os.ReadFile(filepath.Join(j.Home, mergeFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var rec mergeRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return errors.New("Pairing could not finish reading its saved progress; your chats are still on this computer.")
	}
	id, err := identity.Unmarshal(rec.Identity)
	if err != nil {
		return err
	}
	dev, err := identity.UnmarshalDev(rec.Device)
	if err != nil {
		return err
	}
	if rec.Replace {
		if err := replaceState(j.Home, rec.ProfileDir); err != nil {
			return err
		}
	} else {
		if _, err := keys.StageReseal(j.Home, rec.OldKey, id.CellKey()); err != nil {
			return err
		}
	}
	if err := j.after("staged"); err != nil {
		return err
	}
	if err := identity.Install(j.Home, id, dev); err != nil {
		return err
	}
	if err := j.after("installed"); err != nil {
		return err
	}
	if err := keys.CommitReseal(j.Home); err != nil {
		return err
	}
	if err := j.after("keys"); err != nil {
		return err
	}
	if err := identity.EndSolo(j.Home); err != nil {
		return err
	}
	if err := j.after("shared"); err != nil {
		return err
	}
	if rec.SyncURL != "" && j.SaveSyncURL != nil {
		if err := j.SaveSyncURL(rec.SyncURL); err != nil {
			return err
		}
	}
	if err := j.after("relay"); err != nil {
		return err
	}
	return os.Remove(filepath.Join(j.Home, mergeFile))
}
