// Package upgradenotice keeps recovery reports and their home-owned acknowledgements together.
package upgradenotice

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

// Write replaces a local receipt only after its bytes are durable.
func Write(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".upgrade-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	err = errors.Join(err, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	if err := fsreplace.Rename(f.Name(), path); err != nil {
		return err
	}
	// Windows has no supported directory flush; the file itself was flushed above.
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

// Once publishes the recovery list before recording that this home has been told.
// The lock covers both writes so simultaneous windows cannot both claim the notice.
// Numeric keys are person-turn counts and retain the highest count already shown.
func Once(ack, key, report string, body []byte, line string) (string, error) {
	if line == "" {
		return "", nil
	}
	if err := os.MkdirAll(filepath.Dir(ack), 0700); err != nil {
		return "", err
	}
	lock, err := os.OpenFile(ack+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return "", err
	}
	var held struct {
		Key string `json:"key"`
	}
	if raw, err := os.ReadFile(ack); err == nil && json.Unmarshal(raw, &held) == nil {
		if held.Key == key {
			return "", nil
		}
		seen, seenErr := strconv.Atoi(held.Key)
		current, currentErr := strconv.Atoi(key)
		if seenErr == nil && currentErr == nil && current <= seen {
			return "", nil
		}
	}
	if err := Write(report, body); err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		Version int    `json:"version"`
		Key     string `json:"key"`
	}{1, key})
	if err != nil {
		return "", err
	}
	if err := Write(ack, append(raw, '\n')); err != nil {
		return line, err
	}
	return line, nil
}

// Path keeps recovery locations readable without exposing the home prefix on screen.
func Path(path string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if rel == "." {
				return "~"
			}
			return "~" + string(filepath.Separator) + rel
		}
	}
	return path
}

// IsRecovery identifies launch news that must remain visible on Home too.
func IsRecovery(line string) bool {
	return strings.Contains(line, "from an earlier version of codeaf") || strings.Contains(line, "typed in an older codeaf are kept in") || strings.Contains(line, "typed in an older codeaf is kept in")
}
