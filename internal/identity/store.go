package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	statehome "github.com/Agent-Field/codeaf/internal/home"
)

const (
	// File is the persisted identity under the codeaf home.
	File = "identity.json"
	// legacyKeyFile held the vault key before identities existed.
	legacyKeyFile = "vault.key"
)

// ErrNone reports that no identity exists under the home yet.
var ErrNone = errors.New("identity: none on this machine")

// ErrHomeRemoved refuses to make new keys in a home this process has lost.
var ErrHomeRemoved = statehome.ErrRemoved

func checkHome(home string) error {
	if statehome.Removed(home) {
		return ErrHomeRemoved
	}
	return nil
}

func path(home string) string { return filepath.Join(home, File) }

// Load reads the identity under home, or ErrNone.
func Load(home string) (Identity, error) {
	raw, err := os.ReadFile(path(home))
	if errors.Is(err, fs.ErrNotExist) {
		return Identity{}, ErrNone
	}
	if err != nil {
		return Identity{}, err
	}
	var d document
	if err := json.Unmarshal(raw, &d); err != nil {
		return Identity{}, fmt.Errorf("identity: corrupt %s: %w", File, err)
	}
	return fromDocument(d)
}

// Ensure returns the identity under home, and makes this machine's device
// (device.go) beside it, creating either on first use.
func Ensure(home string) (Identity, error) {
	id, err := ensureRoot(home)
	if err != nil {
		return Identity{}, err
	}
	_, err = deviceFor(home, id)
	return id, err
}

// EnsureSolo is Ensure for a computer that needs an identity for itself (a
// vault key, a first launch) and has not been asked to share it: an identity
// it makes is solo (solo.go). One that already exists is left as it is.
func EnsureSolo(home string) (id Identity, err error) {
	defer func() { err = homeWriteError(home, err) }()
	if err := checkHome(home); err != nil {
		return Identity{}, err
	}
	if _, err := Load(home); errors.Is(err, ErrNone) {
		if err := statehome.MkdirAllUnder(home, home, 0o700); err != nil {
			return Identity{}, err
		}
		statehome.Witness(home)
		if err := markSolo(home); err != nil {
			return Identity{}, err
		}
	}
	return Ensure(home)
}

// ensureRoot returns the root identity, creating it on first use. A machine
// that already has a vault.key keeps it as the cell key, so its vault still
// opens. Two processes racing to create agree on whichever wrote first.
func ensureRoot(home string) (Identity, error) {
	if err := checkHome(home); err != nil {
		return Identity{}, err
	}
	statehome.Witness(home)
	if id, err := Load(home); !errors.Is(err, ErrNone) {
		return id, err
	}
	if err := statehome.MkdirAllUnder(home, home, 0o700); err != nil {
		return Identity{}, err
	}
	statehome.Witness(home)
	cell, err := inheritedCellKey(home)
	if err != nil {
		return Identity{}, err
	}
	fresh, err := generate(cell)
	if err != nil {
		return Identity{}, err
	}
	err = write(path(home), fresh.document(), os.Link)
	if errors.Is(err, fs.ErrExist) {
		return Load(home)
	}
	return fresh, err
}

// Save replaces the identity under home.
func Save(home string, id Identity) error {
	if err := checkHome(home); err != nil {
		return err
	}
	if err := statehome.MkdirAllUnder(home, home, 0o700); err != nil {
		return err
	}
	statehome.Witness(home)
	return write(path(home), id.document(), fsreplace.Rename)
}

func hasLegacyKey(home string) bool {
	_, err := os.Stat(filepath.Join(home, legacyKeyFile))
	return err == nil
}

// inheritedCellKey is the old vault key when there is one, else a new key.
func inheritedCellKey(home string) (key, error) {
	raw, err := os.ReadFile(filepath.Join(home, legacyKeyFile))
	if errors.Is(err, fs.ErrNotExist) {
		return randomKey()
	}
	if err != nil {
		return key{}, err
	}
	var k key
	if err := decodeKey(strings.TrimSpace(string(raw)), &k); err != nil {
		return key{}, fmt.Errorf("identity: %s is not a valid key file", legacyKeyFile)
	}
	return k, nil
}

// write puts v as JSON at target through a private temp file and publish,
// which is fsreplace.Rename to replace and os.Link to refuse replacing.
func write(target string, v any, publish func(from, to string) error) (err error) {
	defer func() { err = homeWriteError(filepath.Dir(target), err) }()
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := checkHome(filepath.Dir(target)); err != nil {
		return err
	}
	tmp, err := privateTemp(filepath.Dir(target), raw)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	return publish(tmp, target)
}

func privateTemp(dir string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return "", err
	}
	err = errors.Join(f.Chmod(0o600), writeSync(f, data))
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func writeSync(f *os.File, data []byte) error {
	_, werr := f.Write(data)
	serr := f.Sync()
	return errors.Join(werr, serr, f.Close())
}

// homeWriteError preserves the removed-home contract when removal follows a check.
func homeWriteError(home string, err error) error {
	if errors.Is(err, fs.ErrNotExist) && statehome.Removed(home) {
		return ErrHomeRemoved
	}
	return err
}
