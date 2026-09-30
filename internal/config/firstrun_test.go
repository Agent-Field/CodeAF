package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

// The first-run predicates read the profile the way the setup screen needs them
// read: a default is NOT an answer, and the environment always is.

func TestAFreshProfileIsMissingAllThreeAndAnAnsweredOneIsNot(t *testing.T) {
	t.Setenv(APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	dir := t.TempDir()

	if APIKeyConfigured(dir) || DailyBudgetConfigured(dir) {
		t.Fatal("a profile nobody has touched must read as unanswered on both")
	}
	// The crew RESOLVES on that profile — every seat auto — and that is
	// exactly what must not count.
	if pins := CrewPinsAt(dir); len(pins) != 0 {
		t.Fatalf("an untouched profile has pins %v", pins)
	}

	if err := WriteAPIKey(dir, " sk-or-v1-abc "); err != nil {
		t.Fatal(err)
	}
	if err := SetCrewCap(dir, "5"); err != nil {
		t.Fatal(err)
	}
	if err := WriteDailyBudgetUSD(dir, 7); err != nil {
		t.Fatal(err)
	}
	if !APIKeyConfigured(dir) || !DailyBudgetConfigured(dir) {
		t.Fatal("every answered row must read as configured")
	}
	if got := PersistedAPIKey(dir); got != "sk-or-v1-abc" {
		t.Fatalf("the key was written as %q", got)
	}
	// The file now holds a secret and must be the owner's alone.
	info, err := os.Stat(BudgetConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("config.json is %o after a key landed, want 0600", mode)
	}
}

func TestTheEnvironmentAnswersTheKeyAndTheCeiling(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(APIKeyEnv, "sk-or-v1-from-the-shell")
	t.Setenv("CODEAF_DAILY_BUDGET", "5")
	if !APIKeyConfigured(dir) || !DailyBudgetConfigured(dir) {
		t.Fatal("a variable in the shell is an answer")
	}
	if got := APIKeyAt(dir); got != "sk-or-v1-from-the-shell" {
		t.Fatalf("APIKeyAt read %q, want the shell's", got)
	}
}

func TestTheSetupMarkerIsATimestampAndSurvivesOtherWrites(t *testing.T) {
	dir := t.TempDir()
	if !SetupSeenAt(dir).IsZero() {
		t.Fatal("a fresh profile has never been shown the setup")
	}
	at := time.Date(2026, 8, 24, 9, 30, 0, 0, time.UTC)
	if err := MarkSetupSeen(dir, at); err != nil {
		t.Fatal(err)
	}
	if err := WriteDailyBudgetUSD(dir, 12); err != nil {
		t.Fatal(err)
	}
	if got := SetupSeenAt(dir); !got.Equal(at) {
		t.Fatalf("marker read back as %v, want %v", got, at)
	}
}

func TestTheKeyShapeCheckRefusesWhatIsNotAKey(t *testing.T) {
	good := []string{"sk-or-v1-0123456789abcdef0123456789abcdef", "  sk-proj-0123456789abcdefghij  "}
	bad := []string{"", "sk-", "hello", "sk-or-v1-abc def ghi jkl mno", "sk-or-v1-abc\ndef0123456789"}
	for _, key := range good {
		if !LooksLikeAPIKey(key) {
			t.Errorf("%q should pass the shape check", key)
		}
	}
	for _, key := range bad {
		if LooksLikeAPIKey(key) {
			t.Errorf("%q should fail the shape check", key)
		}
	}
}

func TestLoadKeylessOpensWhereLoadRefuses(t *testing.T) {
	t.Setenv(APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), APIKeyEnv) {
		t.Fatalf("Load with no key must refuse naming the variable, got %v", err)
	}
	settings, err := LoadKeyless()
	if err != nil {
		t.Fatalf("LoadKeyless: %v", err)
	}
	if settings.APIKey != "" || settings.Model == "" {
		t.Fatalf("keyless config must resolve everything but the key, got key %q model %q", settings.APIKey, settings.Model)
	}
}

func TestTheAPIKeyRowMasksReadsTheShellFirstAndWritesTheProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	row, ok := NewSettings(SettingsOptions{ProfileDir: dir}).Row(KeyAPIKey)
	if !ok {
		t.Fatal("the registry has no openrouter key row")
	}
	if !row.Secret {
		t.Fatal("the key row must be a secret")
	}
	if got := row.Value(); got != "not set" {
		t.Fatalf("an empty key reads %q, want `not set`", got)
	}
	if err := row.Apply("sk-or-v1-0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if got := PersistedAPIKey(dir); got != "sk-or-v1-0123456789abcdef" {
		t.Fatalf("the row wrote %q", got)
	}
	if got := row.Value(); strings.Contains(got, "0123456789") || !strings.HasSuffix(got, "cdef") {
		t.Fatalf("the row must read masked with a four-character tail, got %q", got)
	}
	// The mask read back and saved unedited changes nothing.
	if err := row.Apply(row.Value()); err != nil {
		t.Fatal(err)
	}
	if got := PersistedAPIKey(dir); got != "sk-or-v1-0123456789abcdef" {
		t.Fatalf("saving the mask overwrote the key with %q", got)
	}
	t.Setenv(APIKeyEnv, "sk-or-v1-shell")
	if _, pinned := row.PinnedBy(); !pinned {
		t.Fatal("the shell's key must pin the row")
	}
	if err := row.Apply("sk-or-v1-other"); err == nil {
		t.Fatal("a pinned row must refuse a write")
	}
}

// THE ALLOWED RULE NOBODY WROTE IS `all`, and narrowing it is read back.
func TestTheAllowedRuleDefaultsToAllAndReadsBack(t *testing.T) {
	dir := t.TempDir()
	if got := CrewAllowedAt(dir).String(); got != "all" {
		t.Fatalf("an untouched profile allows %q, want all", got)
	}
	if err := SetCrewAllowed(dir, "open"); err != nil {
		t.Fatal(err)
	}
	if got := CrewAllowedAt(dir).String(); got != "open" {
		t.Fatalf("a narrowed rule reads back %q", got)
	}
}
