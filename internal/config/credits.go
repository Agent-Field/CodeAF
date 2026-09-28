package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/filememo"
	"github.com/Agent-Field/codeaf/internal/roles"
)

type creditsRecord struct {
	Low    bool      `json:"low"`
	Known  bool      `json:"known"`
	Key    string    `json:"key"`
	ReadAt time.Time `json:"read_at"`
}

var creditsMemo = filememo.Stamped(SettingsGeneration, func(_ string, data []byte, missing bool) (creditsRecord, error) {
	if missing {
		return creditsRecord{}, nil
	}
	var record creditsRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return creditsRecord{}, err
	}
	return record, nil
})

func creditsAt(profileDir string) creditsRecord {
	record, _ := creditsMemo.Read(ProfilePath(profileDir, "credits.json"))
	return record
}

// CreditsKeyPrint makes a stable identifier for a key without persisting the key.
func CreditsKeyPrint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

// CreditsLowAt reads only a known-low record; damaged and missing records are unknown.
func CreditsLowAt(profileDir string) bool {
	r := creditsAt(profileDir)
	return r.Known && r.Low && r.Key == CreditsKeyPrint(APIKeyAt(profileDir))
}

// CreditsNeedRead asks again at launch for a missing, changed, or low record.
func CreditsNeedRead(profileDir, key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	r := creditsAt(profileDir)
	return r.Key != CreditsKeyPrint(key) || r.Low
}

// WriteCreditsReading atomically replaces machine state. Dollars and the key
// stay out of the file; a successful rename wakes the live crew's generation.
func WriteCreditsReading(profileDir, key string, reading credits.Reading) error {
	path := ProfilePath(profileDir, "credits.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	value := creditsRecord{Low: reading.Known && reading.Low, Known: reading.Known, Key: CreditsKeyPrint(key), ReadAt: time.Now().UTC()}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credits-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write credits: %w", err)
	}
	bumpSettingsGeneration()
	return nil
}

// ChatDefaultAt is the one bottom rung for a newly opened conversation and a
// headless work seat. Explicit model rows are resolved above this function.
func ChatDefaultAt(profileDir string) string {
	if useFreeDefaultsAt(profileDir) {
		return FreeChatModel
	}
	return DefaultModel
}

// useFreeDefaultsAt is the single balance check for implicit chat and crew
// bottom rungs. Both callers resolve explicit choices before asking it.
func useFreeDefaultsAt(profileDir string) bool { return CreditsLowAt(profileDir) }

// IsFreeModel accepts either the published free suffix or a catalog row whose
// three charges are known zeros. An unknown price never claims to be free.
func IsFreeModel(id string, priced bool, prompt, completion, request float64) bool {
	id, _ = roles.SplitEffort(strings.TrimPrefix(strings.TrimSpace(id), "~"))
	return strings.HasSuffix(id, ":free") || priced && prompt == 0 && completion == 0 && request == 0
}
