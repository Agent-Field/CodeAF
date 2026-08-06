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
	raw, err := os.ReadFile(BudgetConfigPath(profileDir))
	if os.IsNotExist(err) {
		return DefaultDailyBudgetUSD, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read budget config: %w", err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return 0, fmt.Errorf("read budget config: %w", err)
	}
	encoded, ok := values["daily_budget_usd"]
	if !ok {
		return DefaultDailyBudgetUSD, nil
	}
	var value float64
	if err := json.Unmarshal(encoded, &value); err != nil {
		return 0, fmt.Errorf("read budget config: daily_budget_usd: %w", err)
	}
	return validateDailyBudget(value, "daily_budget_usd")
}

// WriteDailyBudgetUSD atomically persists the default rail while preserving any
// unrelated future keys in the JSON object.
func WriteDailyBudgetUSD(profileDir string, amount float64) error {
	amount, err := validateDailyBudget(amount, "daily_budget_usd")
	if err != nil {
		return err
	}
	path := BudgetConfigPath(profileDir)
	values := make(map[string]json.RawMessage)
	if raw, readErr := os.ReadFile(path); readErr == nil {
		if err := json.Unmarshal(raw, &values); err != nil {
			return fmt.Errorf("write budget config: preserve existing file: %w", err)
		}
	} else if !os.IsNotExist(readErr) {
		return fmt.Errorf("write budget config: %w", readErr)
	}
	encodedAmount, _ := json.Marshal(amount)
	values["daily_budget_usd"] = encodedAmount
	encoded, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return fmt.Errorf("write budget config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("write budget config: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return fmt.Errorf("write budget config: %w", err)
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
		return fmt.Errorf("write budget config: %w", err)
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write budget config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("write budget config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("write budget config: %w", err)
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
