package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func registry(t *testing.T, dir string) *Settings {
	t.Helper()
	return NewSettings(SettingsOptions{
		ProfileDir: dir,
		ModelValue: func(slot string) string { return slot + "/model" },
		SetModel:   func(string, string) error { return nil },
		SplitPct:   func() int { return 0 },
	})
}

// The registry is the completeness gate: a user-tunable environment pin added
// anywhere in the tree has to arrive as a row here, or land on the explicit
// operator-plumbing allowlist. Until then this test fails the build.
func TestRegistryCoversEveryUserFacingEnvironmentPin(t *testing.T) {
	registered := map[string]bool{}
	for _, row := range registry(t, t.TempDir()).Rows() {
		if row.Env != "" {
			registered[row.Env] = true
		}
		if row.EnvDefault != "" {
			registered[row.EnvDefault] = true
		}
	}
	for _, name := range OperatorEnvPins {
		registered[name] = true
	}

	pattern := regexp.MustCompile(`AFORGE_[A-Z0-9_]+`)
	root := repositoryRoot(t)
	seen := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, name := range pattern.FindAllString(string(raw), -1) {
			if _, ok := seen[name]; !ok {
				seen[name] = path
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) < 10 {
		t.Fatalf("the environment scan found only %d pins; it is not reading the tree", len(seen))
	}
	for name, path := range seen {
		if strings.HasPrefix(name, "AFORGE_TEST_") || registered[name] {
			continue
		}
		t.Fatalf("%s (%s) is neither a settings row nor operator plumbing — register it in settings.go",
			name, path)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the config package")
		}
		dir = parent
	}
}

func TestRegistryGroupsEveryCategoryAndEveryModelSlot(t *testing.T) {
	rows := registry(t, t.TempDir())
	groups := rows.Groups()
	if len(groups) != len(SettingCategories) {
		t.Fatalf("groups = %d, want %d", len(groups), len(SettingCategories))
	}
	for index, group := range groups {
		if group.Title != SettingCategories[index] {
			t.Fatalf("group %d = %q, want %q", index, group.Title, SettingCategories[index])
		}
		if len(group.Rows) == 0 {
			t.Fatalf("category %q is empty", group.Title)
		}
	}
	for _, slot := range ModelSettingSlots {
		row, ok := rows.Row(ModelSettingKey(slot))
		if !ok || row.Kind != SettingModel || row.Slot != slot {
			t.Fatalf("model slot %q is missing from the registry", slot)
		}
		if row.Value() != slot+"/model" {
			t.Fatalf("model row %q reads %q", slot, row.Value())
		}
	}
	seen := map[string]bool{}
	for _, row := range rows.Rows() {
		if seen[row.Key] {
			t.Fatalf("duplicate settings key %q", row.Key)
		}
		seen[row.Key] = true
		if row.Label == "" || row.Category == "" || row.Hint == "" {
			t.Fatalf("row %q is missing plain language: %+v", row.Key, row)
		}
	}
}

func TestSettingsResolveEnvironmentThenFileThenDefault(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"AFORGE_DAILY_BUDGET", "AFORGE_PRACTICE_BUDGET", "AFORGE_PRACTICE_IDLE",
		"AFORGE_BRIEF_AFTER", "AFORGE_TENURE_AFTER", "AFORGE_DOC_ENGINE", "AFORGE_VISION_MODEL",
	} {
		t.Setenv(name, "")
	}

	rows := registry(t, dir)
	defaults := map[string]string{
		KeyDailyBudget:    formatDollars(DefaultDailyBudgetUSD),
		KeyPracticeBudget: formatDollars(DefaultPracticeBudgetUSD),
		KeyPracticeIdle:   "20m",
		KeyBriefAfter:     "4h",
		KeyTenureAfter:    "3",
		KeyDemandShare:    "70%",
		KeyProposeSkills:  "on",
		KeyDocumentEngine: "auto",
		KeyVisionModel:    "automatic",
	}
	for key, want := range defaults {
		row, ok := rows.Row(key)
		if !ok {
			t.Fatalf("%s is not registered", key)
		}
		if got := row.Value(); got != want {
			t.Fatalf("%s default = %q, want %q", key, got, want)
		}
	}

	changes := map[string]string{
		KeyDailyBudget:    "35.50",
		KeyPracticeBudget: "$4",
		KeyPracticeIdle:   "45m",
		KeyBriefAfter:     "90m",
		KeyTenureAfter:    "5",
		KeyDemandShare:    "40%",
		KeyProposeSkills:  "off",
		KeyDocumentEngine: "local",
		KeyVisionModel:    "seer/vision",
	}
	for key, raw := range changes {
		row, _ := rows.Row(key)
		if err := row.Apply(raw); err != nil {
			t.Fatalf("apply %s=%q: %v", key, raw, err)
		}
	}
	reread := registry(t, dir)
	persisted := map[string]string{
		KeyDailyBudget:    "$35.5",
		KeyPracticeBudget: "$4",
		KeyPracticeIdle:   "45m",
		KeyBriefAfter:     "1h30m",
		KeyTenureAfter:    "5",
		KeyDemandShare:    "40%",
		KeyProposeSkills:  "off",
		KeyDocumentEngine: "local",
		KeyVisionModel:    "seer/vision",
	}
	for key, want := range persisted {
		row, _ := reread.Row(key)
		if got := row.Value(); got != want {
			t.Fatalf("%s persisted = %q, want %q", key, got, want)
		}
	}

	// One file, one writer: the budget rail and the new rows share it, and an
	// unrelated key already in the object survives every write.
	if err := WriteDailyBudgetUSD(dir, 12); err != nil {
		t.Fatal(err)
	}
	if got, err := DailyBudgetUSDAt(dir); err != nil || got != 12 {
		t.Fatalf("daily budget = %v err=%v", got, err)
	}
	if got, _ := reread.Row(KeyDocumentEngine); got.Value() != "local" {
		t.Fatalf("a budget write erased the document engine: %q", got.Value())
	}

	t.Setenv("AFORGE_DOC_ENGINE", "ocr")
	t.Setenv("AFORGE_TENURE_AFTER", "9")
	t.Setenv("AFORGE_VISION_MODEL", "pinned/vision")
	pinned := registry(t, dir)
	for key, want := range map[string]string{
		KeyDocumentEngine: "ocr", KeyTenureAfter: "9", KeyVisionModel: "pinned/vision",
	} {
		row, _ := pinned.Row(key)
		if got := row.Value(); got != want {
			t.Fatalf("%s under the environment = %q, want %q", key, got, want)
		}
		name, isPinned := row.PinnedBy()
		if !isPinned || name == "" {
			t.Fatalf("%s did not report its environment pin", key)
		}
		if err := row.Apply("auto"); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s accepted an edit while pinned: %v", key, err)
		}
	}
}

// The persisted layer is what Load reads on the next launch; the sheet writing
// a value has to be the same thing the process reads back.
func TestLoadReadsPersistedSettings(t *testing.T) {
	dir := t.TempDir()
	rows := registry(t, dir)
	for key, raw := range map[string]string{
		KeyPracticeBudget: "6", KeyPracticeIdle: "5m", KeyBriefAfter: "30m",
		KeyDemandShare: "25", KeyProposeSkills: "off", KeyDocumentEngine: "free",
		KeyVisionModel: "seer/vision",
	} {
		row, _ := rows.Row(key)
		if err := row.Apply(raw); err != nil {
			t.Fatalf("apply %s: %v", key, err)
		}
	}
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("AFORGE_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("AFORGE_PROFILE_DIR", dir)
	for _, name := range []string{
		"AFORGE_PRACTICE_BUDGET", "AFORGE_PRACTICE_IDLE", "AFORGE_BRIEF_AFTER",
		"AFORGE_DOC_ENGINE", "AFORGE_VISION_MODEL",
	} {
		t.Setenv(name, "")
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PracticeBudgetUSD != 6 || loaded.PracticeIdle != 5*time.Minute ||
		loaded.BriefAfter != 30*time.Minute || loaded.DocumentEngine != "free" ||
		loaded.VisionModel != "seer/vision" {
		t.Fatalf("persisted settings did not reach Load: %+v", loaded)
	}
	if loaded.PracticeDemandPct != 25 || loaded.ProposeSkills {
		t.Fatalf("learning dial = %d%% propose=%v", loaded.PracticeDemandPct, loaded.ProposeSkills)
	}

	// The environment still wins over everything written here.
	t.Setenv("AFORGE_DOC_ENGINE", "ocr")
	t.Setenv("AFORGE_BRIEF_AFTER", "2h")
	loaded, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DocumentEngine != "ocr" || loaded.BriefAfter != 2*time.Hour {
		t.Fatalf("environment lost to the persisted file: %+v", loaded)
	}
}

func TestSettingEditorsRefuseNonsenseInPlainLanguage(t *testing.T) {
	rows := registry(t, t.TempDir())
	for key, raw := range map[string]string{
		KeyDailyBudget:    "twenty dollars",
		KeyBriefAfter:     "soonish",
		KeyDemandShare:    "140",
		KeyTenureAfter:    "many",
		KeyDocumentEngine: "tesseract",
		KeyProposeSkills:  "maybe",
	} {
		row, ok := rows.Row(key)
		if !ok {
			t.Fatalf("%s is not registered", key)
		}
		err := row.Apply(raw)
		if err == nil {
			t.Fatalf("%s accepted %q", key, raw)
		}
		if message := err.Error(); message == "" || strings.Contains(message, "strconv") {
			t.Fatalf("%s error is not plain language: %v", key, err)
		}
	}
}

func TestTenurePersistsAndReachesTheProcessEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AFORGE_TENURE_AFTER", "")
	rows := registry(t, dir)
	row, _ := rows.Row(KeyTenureAfter)
	if err := row.Apply("6"); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("AFORGE_TENURE_AFTER"); got != "6" {
		t.Fatalf("tenure did not reach the running process: %q", got)
	}
	t.Setenv("AFORGE_TENURE_AFTER", "")
	if got := TenureAfterAt(dir); got != 6 {
		t.Fatalf("persisted tenure = %d", got)
	}
	InstallPersistedEnv(dir)
	if got := os.Getenv("AFORGE_TENURE_AFTER"); got != "6" {
		t.Fatalf("relaunch did not reinstall the persisted tenure: %q", got)
	}
	t.Setenv("AFORGE_TENURE_AFTER", "2")
	InstallPersistedEnv(dir)
	if got := os.Getenv("AFORGE_TENURE_AFTER"); got != "2" {
		t.Fatalf("a set environment was overwritten: %q", got)
	}
}

func TestSplitPercentClampsAndSavesThroughTheRegistry(t *testing.T) {
	saved := 0
	rows := NewSettings(SettingsOptions{
		ProfileDir:   t.TempDir(),
		SplitPct:     func() int { return saved },
		SaveSplitPct: func(pct int) { saved = pct },
	})
	row, ok := rows.Row(KeySplitPct)
	if !ok {
		t.Fatal("the divider is not registered")
	}
	if got := row.Value(); got != formatPercent(DefaultSplitPct) {
		t.Fatalf("unset divider reads %q", got)
	}
	if err := row.Apply("60"); err != nil || saved != 60 {
		t.Fatalf("apply 60 saved=%d err=%v", saved, err)
	}
	if err := row.Apply("95"); err == nil {
		t.Fatal("the divider accepted a share outside its band")
	}
}
