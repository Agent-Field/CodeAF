package pair

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
)

const mergeFile = "pair-merge.json"
const sharingFile = "pair-sharing.pending"

// SharingPending reports an approved merge whose home-state pass is unfinished.
func SharingPending(home, id string) bool {
	raw, err := os.ReadFile(filepath.Join(home, sharingFile))
	return err == nil && string(raw) == id
}

// CompleteSharing records the first successful home-state pass after pairing.
func CompleteSharing(home, id string) error {
	unlock, err := keys.LockHome(home)
	if err != nil {
		return err
	}
	defer unlock()
	if !SharingPending(home, id) {
		return nil
	}
	err = os.Remove(filepath.Join(home, sharingFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

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
	Phase      string          `json:"phase,omitempty"`
	TeamsAside string          `json:"teams_aside,omitempty"`
}

// Resume completes an approved local merge before sync opens its clients.
// The live chats and their engine store are never moved or rewritten.
func Resume(home string, saveSyncURL func(string) error) error {
	_, err := (Joining{Home: home, SaveSyncURL: saveSyncURL}).resumeApproved()
	return err
}

// resumeApproved returns the recovered receipt when approved work was pending.
// The home lock covers both the saved progress and its installation.
func (j Joining) resumeApproved() (*Joined, error) {
	if _, err := os.Stat(filepath.Join(j.Home, mergeFile)); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	unlock, err := keys.LockHome(j.Home)
	if err != nil {
		return nil, err
	}
	defer unlock()
	joined, err := j.resume()
	return &joined, err
}

func (j Joining) after(step string) error {
	if j.After != nil {
		return j.After(step)
	}
	return nil
}

func (j Joining) merge(g Grant, dev *identity.Dev, oldKey []byte) (Joined, error) {
	if dev == nil {
		made, err := identity.NewDevice(g.Identity)
		if err != nil {
			return Joined{}, err
		}
		dev = &made
	}
	idDoc, err := g.Identity.Marshal()
	if err != nil {
		return Joined{}, err
	}
	devDoc, err := dev.Marshal()
	if err != nil {
		return Joined{}, err
	}
	rec := mergeRecord{OldKey: oldKey, Identity: idDoc, Device: devDoc, SyncURL: g.SyncURL, Replace: j.Replace, ProfileDir: j.ProfileDir}
	if err := rec.save(j.Home); err != nil {
		return Joined{}, err
	}
	if err := j.after("recorded"); err != nil {
		return Joined{}, err
	}
	return j.resume()
}

func (rec mergeRecord) save(home string) error {
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(home, ".pair-merge-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(raw)
	if err := errors.Join(f.Chmod(0600), writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	return fsreplace.Rename(f.Name(), filepath.Join(home, mergeFile))
}

func (j Joining) resume() (Joined, error) {
	joined := Joined{}
	raw, err := os.ReadFile(filepath.Join(j.Home, mergeFile))
	if errors.Is(err, os.ErrNotExist) {
		return joined, nil
	}
	if err != nil {
		return joined, err
	}
	var rec mergeRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return joined, errors.New("Pairing could not finish reading its saved progress; your chats are still on this computer.")
	}
	joined.TeamsAside = rec.TeamsAside
	id, err := identity.Unmarshal(rec.Identity)
	if err != nil {
		return joined, err
	}
	dev, err := identity.UnmarshalDev(rec.Device)
	if err != nil {
		return joined, err
	}
	if rec.Replace {
		if rec.Phase != "installed" {
			// Preparation can precede a failed install or new writes under the
			// old key. An installed destination proves the reset already ran,
			// even if its process stopped before saving progress.
			current, err := identity.Load(j.Home)
			if err != nil && !errors.Is(err, identity.ErrNone) {
				return joined, err
			}
			if current.ID() != id.ID() {
				aside, replaceErr := replaceState(j.Home, rec.ProfileDir, func() bool { return true })
				if aside != "" {
					rec.TeamsAside = aside
					joined.TeamsAside = aside
					if err := rec.save(j.Home); err != nil {
						return joined, errors.Join(replaceErr, err)
					}
				}
				if replaceErr != nil {
					return joined, replaceErr
				}
			}
		}
	} else {
		if _, err := keys.StageReseal(j.Home, rec.OldKey, id.CellKey()); err != nil {
			return joined, err
		}
	}
	if rec.Phase == "" {
		rec.Phase = "prepared"
		if err := rec.save(j.Home); err != nil {
			return joined, err
		}
	}
	if err := j.after("staged"); err != nil {
		return joined, err
	}
	if err := identity.Install(j.Home, id, dev); err != nil {
		return joined, err
	}
	rec.Phase = "installed"
	if err := rec.save(j.Home); err != nil {
		return joined, err
	}
	if err := j.after("installed"); err != nil {
		return joined, err
	}
	if err := keys.CommitReseal(j.Home); err != nil {
		return joined, err
	}
	if err := j.after("keys"); err != nil {
		return joined, err
	}
	if err := identity.EndSolo(j.Home); err != nil {
		return joined, err
	}
	if err := j.after("shared"); err != nil {
		return joined, err
	}
	if rec.SyncURL != "" && j.SaveSyncURL != nil {
		if err := j.SaveSyncURL(rec.SyncURL); err != nil {
			return joined, err
		}
	}
	if err := j.after("relay"); err != nil {
		return joined, err
	}
	// Save the background obligation before discarding local switch progress.
	if err := markSharing(j.Home, id.ID()); err != nil {
		return joined, err
	}
	return joined, os.Remove(filepath.Join(j.Home, mergeFile))
}

func markSharing(home, id string) error {
	f, err := os.OpenFile(filepath.Join(home, sharingFile), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.WriteString(id)
	return errors.Join(writeErr, f.Sync(), f.Close())
}
