package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	values, err := readProfileConfig(profileDir)
	if err != nil {
		return fmt.Errorf("write config: preserve existing file: %w", err)
	}
	encodedValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("write config %s: %w", key, err)
	}
	values[key] = encodedValue
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
	return nil
}

// BudgetConfigPath is ~/.aforge/config.json unless AFORGE_PROFILE_DIR supplies
// the same alternate state root used by measured profiles.
func BudgetConfigPath(profileDir string) string {
	profileDir = strings.TrimSpace(profileDir)
	if profileDir != "" {
		return filepath.Join(profileDir, "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".aforge", "config.json")
	}
	return filepath.Join(home, ".aforge", "config.json")
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
