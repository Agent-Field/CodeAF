package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// DailyBudgetUSDAt resolves env → persisted config → built-in default. The env
// remains the explicit headless override; /budget default writes the middle
// layer used by both chat and one-shot runs.
func DailyBudgetUSDAt(profileDir string) (float64, error) {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_DAILY_BUDGET")); raw != "" {
		return parseDailyBudget(raw, "AFORGE_DAILY_BUDGET")
	}
	values, err := readProfileConfig(profileDir)
	if err != nil {
		return 0, err
	}
	encoded, ok := values[KeyDailyBudget]
	if !ok {
		return DefaultDailyBudgetUSD, nil
	}
	var value float64
	if err := json.Unmarshal(encoded, &value); err != nil {
		return 0, fmt.Errorf("read budget config: %s: %w", KeyDailyBudget, err)
	}
	return validateDailyBudget(value, KeyDailyBudget)
}

// WriteDailyBudgetUSD atomically persists the default rail while preserving any
// unrelated future keys in the JSON object.
func WriteDailyBudgetUSD(profileDir string, amount float64) error {
	amount, err := validateDailyBudget(amount, KeyDailyBudget)
	if err != nil {
		return err
	}
	return writeProfileValue(profileDir, KeyDailyBudget, amount)
}

// readProfileConfig is the single reader of the profile's config.json. A file
// that is not there yet is an empty object, not an error.
func readProfileConfig(profileDir string) (map[string]json.RawMessage, error) {
	raw, err := os.ReadFile(BudgetConfigPath(profileDir))
	if os.IsNotExist(err) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	values := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return values, nil
}

// writeProfileValue is the single writer every persisted setting goes through:
// read, replace one key, write a temporary file, rename. Unrelated keys — the
// other settings and anything a later version adds — survive untouched.
func writeProfileValue(profileDir, key string, value any) error {
	return writeProfileValues(profileDir, map[string]any{key: value})
}

// writeProfileValues is the same write for SEVERAL KEYS AT ONCE, and the reason
// it exists is that some settings are one decision spelled in more than one row.
// The crew is four tier rows written from one word (crew.go): four separate
// writes would leave a window in which two classes belong to the old crew and
// two to the new, and the derived crew row would read "custom" about a state
// nobody chose. One read, one temporary file, one rename — the keys land
// together or not at all.
//
// The single-key writer above is this function with a map of one, so there is
// still exactly one place that knows how a setting reaches the disk.
func writeProfileValues(profileDir string, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	// The key in the error messages is a DETERMINISTIC one — a map has no order,
	// and a failure that named a different row on every attempt would be a
	// failure nobody could search for.
	key := errorKey(updates)
	values, err := readProfileConfig(profileDir)
	if err != nil {
		return fmt.Errorf("write config: preserve existing file: %w", err)
	}
	for name, value := range updates {
		encodedValue, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("write config %s: %w", name, err)
		}
		values[name] = encodedValue
	}
	encoded, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return fmt.Errorf("write config %s: %w", key, err)
	}
	path := BudgetConfigPath(profileDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("write config %s: %w", key, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return fmt.Errorf("write config %s: %w", key, err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write config %s: %w", key, err)
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write config %s: %w", key, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("write config %s: %w", key, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("write config %s: %w", key, err)
	}
	removeTemporary = false
	// EVERY PERSISTED WRITE PASSES HERE, which is what makes one counter enough
	// for a live reader to know its snapshot is stale (see [SettingsGeneration]).
	bumpSettingsGeneration()
	return nil
}

// errorKey is the key a multi-key write blames, chosen deterministically: the
// first in sorted order. It is a name for a failure message and nothing else.
func errorKey(updates map[string]any) string {
	names := make([]string, 0, len(updates))
	for name := range updates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names[0]
}

// ── the generation counter ──────────────────────────────────────────────────
//
// A live reader — the v3 door's crew source (cmd/aforge's v3RolesSource) — needs
// to know when what it read has changed, and it needs to know cheaply: an
// auxiliary model is resolved on the path of a turn, and a file read there would
// be a syscall per call for a file that changes once a week.
//
// So every write bumps a counter, and a reader compares one integer. This is not
// a cache of the file's contents — this package deliberately holds none, because
// a stale settings value is worse than a slow one — it is the SIGNAL a cache
// somewhere else invalidates on.
//
// WHAT IT DOES NOT SEE, stated plainly: a config.json edited by hand in another
// process, and a project file (`<workspace>/.aforge-v3/config.json`) edited by
// anything. Both are the same as the behaviour before a counter existed — those
// changes have always landed on the next launch — and a counter that pretended
// otherwise would need a watcher on two files per session.

var settingsGeneration atomic.Uint64

// SettingsGeneration is the number of persisted settings writes this process has
// made. A reader that holds a snapshot keeps the value it read at, and rebuilds
// when the two differ.
func SettingsGeneration() uint64 { return settingsGeneration.Load() }

func bumpSettingsGeneration() { settingsGeneration.Add(1) }

// BudgetConfigPath is config.json in aforge's state root unless
// AFORGE_PROFILE_DIR supplies the same alternate root used by measured
// profiles.
func BudgetConfigPath(profileDir string) string {
	profileDir = strings.TrimSpace(profileDir)
	if profileDir != "" {
		return filepath.Join(profileDir, "config.json")
	}
	return home.Join("config.json")
}

func parseDailyBudget(raw, source string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("%s: want a non-negative dollar amount, got %q", source, raw)
	}
	return validateDailyBudget(value, source)
}

func validateDailyBudget(value float64, source string) (float64, error) {
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%s: want a non-negative dollar amount, got %v", source, value)
	}
	return value, nil
}
