package syncsetup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// An app provider edit records only the fingerprint of the key kept in the
// secure store. A zero-length fingerprint means an explicit disconnect.
type appProviderEdit struct {
	Digest string `json:"digest,omitempty"`
	At     int64  `json:"at"`
}

var appProviderEditsMu sync.Mutex

func appProviderKeyName(provider string) string {
	if provider == modelsource.DefaultID {
		return config.KeyAPIKey
	}
	return sourcePrefix + provider
}

func providerEditDigest(key string) string {
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// RememberAppProviderEdit keeps a user-confirmed change until the local
// encrypted vault captures it. It is independent of relay access and never
// stores the credential itself.
func RememberAppProviderEdit(dir, provider, key string) error {
	name := appProviderKeyName(provider)
	if _, ok := sharedAppProvider(name); !ok || home.Removed(dir) {
		return nil
	}
	appProviderEditsMu.Lock()
	defer appProviderEditsMu.Unlock()
	edits, err := readAppProviderEdits(dir)
	if err != nil {
		return err
	}
	if err := home.MkdirAllUnder(dir, dir, 0o700); err != nil {
		return err
	}
	edits[name] = appProviderEdit{
		Digest: providerEditDigest(key),
		At:     max(time.Now().UnixNano(), edits[name].At+1),
	}
	return writeAppProviderEdits(dir, edits)
}

func appProviderEditsPath(dir string) string { return filepath.Join(dir, "provider-key-edits.json") }

func readAppProviderEdits(dir string) (map[string]appProviderEdit, error) {
	edits := make(map[string]appProviderEdit)
	raw, err := os.ReadFile(appProviderEditsPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return edits, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &edits); err != nil {
		return nil, errors.New("could not read saved provider changes")
	}
	if edits == nil {
		edits = make(map[string]appProviderEdit)
	}
	return edits, nil
}

func completeAppProviderEdit(dir, name string, captured appProviderEdit) error {
	appProviderEditsMu.Lock()
	defer appProviderEditsMu.Unlock()
	edits, err := readAppProviderEdits(dir)
	if err != nil {
		return err
	}
	if edits[name] != captured {
		return nil
	}
	delete(edits, name)
	return writeAppProviderEdits(dir, edits)
}

func writeAppProviderEdits(dir string, edits map[string]appProviderEdit) error {
	raw, err := json.Marshal(edits)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".provider-key-edits-")
	if err != nil {
		return err
	}
	defer fsreplace.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err = fsreplace.Rename(f.Name(), appProviderEditsPath(dir)); err != nil {
		return err
	}
	return fsreplace.SyncDir(dir)
}
