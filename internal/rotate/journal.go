package rotate

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// File is the journal under the codeaf home.
const File = "rotation.json"

// State is how far a rotation has got. Each state names what is already true.
type State string

const (
	Minted   State = "minted"   // the new root is in the journal; nothing was sent
	Frozen   State = "frozen"   // the old identity cannot be written to
	Sealed   State = "sealed"   // every chat and the vault are under the new identity
	Verified State = "verified" // the new identity's relay holds what the journal says
	Switched State = "switched" // this machine is the new identity
)

// Item is one chat being carried over.
type Item struct {
	ID      string `json:"id"`
	Head    string `json:"head"`               // the durable head that was carried
	HeadRID string `json:"head_rid,omitempty"` // its remote id under the new keys
	Done    bool   `json:"done"`
}

// Journal is everything a crashed rotation needs. It holds secrets (the new
// identity and both device keys) and is written 0600, atomically.
type Journal struct {
	V         uint16          `json:"V"`
	State     State           `json:"state"`
	OldID     string          `json:"old_id"`
	OldPublic string          `json:"old_public"` // hex of the old signing public key: enough to verify the old device's cert
	OldDevice json.RawMessage `json:"old_device"` // identity.Dev document, to ask the relay to retire
	NewID     string          `json:"new_id"`
	New       json.RawMessage `json:"new"`        // identity document
	NewDevice json.RawMessage `json:"new_device"` // identity.Dev document
	GraceMS   int64           `json:"grace_ms"`
	Items     []Item          `json:"items,omitempty"`
	Vault     bool            `json:"vault,omitempty"` // the resealed vault is on the new relay
}

func path(home string) string { return filepath.Join(home, File) }

// Pending says whether a rotation was started here and not finished.
func Pending(home string) bool {
	_, err := os.Stat(path(home))
	return err == nil
}

func load(home string) (Journal, bool, error) {
	raw, err := os.ReadFile(path(home))
	if errors.Is(err, fs.ErrNotExist) {
		return Journal{}, false, nil
	}
	if err != nil {
		return Journal{}, false, err
	}
	var j Journal
	if err := json.Unmarshal(raw, &j); err != nil {
		return Journal{}, false, errors.New("rotate: " + File + " is damaged; it holds your new keys, so do not delete it")
	}
	return j, true, nil
}

// save replaces the journal atomically and durably.
func save(home string, j Journal) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(home, ".rotation-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := errors.Join(tmp.Chmod(0o600), write(tmp, raw)); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path(home))
}

func write(f *os.File, raw []byte) error {
	_, werr := f.Write(raw)
	return errors.Join(werr, f.Sync(), f.Close())
}

func remove(home string) error {
	err := os.Remove(path(home))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
