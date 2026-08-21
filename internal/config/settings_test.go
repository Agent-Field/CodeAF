package config

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
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
			// .claude holds other branches' worktrees; their pins register
			// in their own settings.go, and reading them here fails this
			// branch for a variable it cannot see.
			if entry.Name() == ".git" || entry.Name() == ".claude" {
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
	for _, slot := range ModelSlots() {
		row, ok := rows.Row(ModelSettingKey(slot.Slot))
		if !ok || row.Kind != SettingModel || row.Slot != slot.Slot {
			t.Fatalf("model slot %q is missing from the registry", slot.Slot)
		}
		if !slot.Held {
			// A role nothing holds a client for reads as what it follows —
			// never as the engine's answer for a word it does not know.
			if row.Value() != "follows "+slot.Follows {
				t.Fatalf("unheld role %q reads %q", slot.Slot, row.Value())
			}
			continue
		}
		if slot.Role == "" {
			// A CAPABILITY SLOT IS READ OUT OF THE PROFILE, not out of a
			// running engine (docs/MULTIMODAL.md Decision 5): its value is a
			// choice written down here, and an untouched profile has written
			// nothing, which reads as the word the resolver will act on.
			if row.Value() != "automatic" {
				t.Fatalf("capability slot %q reads %q in an untouched profile", slot.Slot, row.Value())
			}
			continue
		}
		if row.Value() != slot.Slot+"/model" {
			t.Fatalf("model row %q reads %q", slot.Slot, row.Value())
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
		KeyDocumentEngine: "free",
		KeyVisionModel:    "seer/vision", KeyAttribution: "off",
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
		"AFORGE_DOC_ENGINE", "AFORGE_VISION_MODEL", "AFORGE_ATTRIBUTION",
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
	if loaded.Attribution {
		t.Fatal("attribution switched off in the sheet did not reach Load")
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
		KeyTenureAfter:    "many",
		KeyDocumentEngine: "tesseract",
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

// Attribution is on until someone says otherwise, and the row is the only way
// to say otherwise short of the shell — which still wins.
func TestAttributionDefaultsOnPersistsAndHonorsItsEnvironmentPin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AFORGE_ATTRIBUTION", "")
	rows := registry(t, dir)
	row, ok := rows.Row(KeyAttribution)
	if !ok {
		t.Fatal("attribution is not registered")
	}
	if row.Category != CategoryInterface || row.Kind != SettingBool || row.Label != "attribution" {
		t.Fatalf("attribution row = %+v", row)
	}
	if row.Value() != "on" || !AttributionAt(dir) {
		t.Fatalf("attribution does not default on: %q", row.Value())
	}
	if err := row.Apply("off"); err != nil {
		t.Fatal(err)
	}
	if AttributionAt(dir) {
		t.Fatal("off did not persist")
	}
	reread, _ := registry(t, dir).Row(KeyAttribution)
	if reread.Value() != "off" {
		t.Fatalf("the reread row lost the persisted choice: %q", reread.Value())
	}

	t.Setenv("AFORGE_ATTRIBUTION", "on")
	if !AttributionAt(dir) {
		t.Fatal("the environment lost to the persisted file")
	}
	pinned, _ := registry(t, dir).Row(KeyAttribution)
	name, isPinned := pinned.PinnedBy()
	if !isPinned || name != "AFORGE_ATTRIBUTION" {
		t.Fatalf("attribution did not report its pin: %q", name)
	}
	if err := pinned.Apply("off"); err == nil || !strings.Contains(err.Error(), name) {
		t.Fatalf("a pinned attribution accepted an edit: %v", err)
	}

	// A hand-typed pin that means nothing reads as the default rather than
	// stopping a launch over a signature.
	t.Setenv("AFORGE_ATTRIBUTION", "sure")
	if !AttributionAt(dir) {
		t.Fatal("a malformed pin did not fall back to the default")
	}
}

// Linear mode is off until someone says otherwise — the opposite default from
// attribution, so the pin and the persisted file are both exercised in the
// direction that actually turns something on.
func TestLinearModeDefaultsOffPersistsAndHonorsItsEnvironmentPin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AFORGE_CHAT_LINEAR", "")
	rows := registry(t, dir)
	row, ok := rows.Row(KeyLinearMode)
	if !ok {
		t.Fatal("linear mode is not registered")
	}
	if row.Category != CategoryInterface || row.Kind != SettingBool || row.Label != "linear mode" {
		t.Fatalf("linear mode row = %+v", row)
	}
	if row.Value() != "off" || LinearModeAt(dir) {
		t.Fatalf("linear mode does not default off: %q", row.Value())
	}
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
	if !LinearModeAt(dir) {
		t.Fatal("on did not persist")
	}
	reread, _ := registry(t, dir).Row(KeyLinearMode)
	if reread.Value() != "on" {
		t.Fatalf("the reread row lost the persisted choice: %q", reread.Value())
	}

	t.Setenv("AFORGE_CHAT_LINEAR", "off")
	if LinearModeAt(dir) {
		t.Fatal("the environment lost to the persisted file")
	}
	pinned, _ := registry(t, dir).Row(KeyLinearMode)
	name, isPinned := pinned.PinnedBy()
	if !isPinned || name != "AFORGE_CHAT_LINEAR" {
		t.Fatalf("linear mode did not report its pin: %q", name)
	}
	if err := pinned.Apply("on"); err == nil || !strings.Contains(err.Error(), name) {
		t.Fatalf("a pinned linear mode accepted an edit: %v", err)
	}

	// A hand-typed pin that means nothing reads as the default rather than
	// stopping a launch over a rendering preference.
	t.Setenv("AFORGE_CHAT_LINEAR", "sure")
	if LinearModeAt(dir) {
		t.Fatal("a malformed pin did not fall back to the default")
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

// And the other half of that row: a caller with no divider to move does not get
// a divider row. It used to get one that read a plausible percentage, took a
// new one and answered "chat width is unavailable here" — a control whose only
// behaviour was to refuse, which is the shape this codebase leaves OFF rather
// than shipping broken. The v3 chat is that caller: its roster is a fixed
// column, not a share of the frame.
func TestTheDividerRowIsAbsentWithoutSomewhereToSaveIt(t *testing.T) {
	rows := NewSettings(SettingsOptions{
		ProfileDir: t.TempDir(),
		// The read seam alone, which is what a surface that can only DRAW a
		// divider would hand over.
		SplitPct: func() int { return 60 },
	})
	if _, ok := rows.Row(KeySplitPct); ok {
		t.Fatal("a surface that cannot save the divider was still given the row")
	}
	for _, row := range rows.Rows() {
		if row.Key == KeySplitPct {
			t.Fatal("the divider reached the sheet through Rows()")
		}
	}

	// With the seam it is back, and it is back WHERE IT WAS: at the head of the
	// interface group, ahead of the nerd-font row it has always sat above.
	full := NewSettings(SettingsOptions{
		ProfileDir:   t.TempDir(),
		SplitPct:     func() int { return 60 },
		SaveSplitPct: func(int) {},
	})
	divider, nerd := -1, -1
	for index, row := range full.Rows() {
		switch row.Key {
		case KeySplitPct:
			divider = index
		case KeyNerdFont:
			nerd = index
		}
	}
	if divider < 0 {
		t.Fatal("a surface that CAN save the divider was not given the row")
	}
	if divider > nerd {
		t.Fatalf("the divider moved: it is row %d and nerd font is row %d", divider, nerd)
	}
}

// The nerd-font tier is ON until someone says otherwise, which is the opposite
// default from linear mode — so this exercises the pin and the persisted file
// in the direction that actually turns something OFF, and proves the row that
// carries the opt-out really carries it (12.7 E.3, F.11).
func TestNerdFontDefaultsOnPersistsAndHonorsItsEnvironmentPin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AFORGE_NERD_FONT", "")
	rows := registry(t, dir)
	row, ok := rows.Row(KeyNerdFont)
	if !ok {
		t.Fatal("the nerd font row is not registered")
	}
	if row.Category != CategoryInterface || row.Kind != SettingBool || row.Label != "nerd font" {
		t.Fatalf("nerd font row = %+v", row)
	}
	if row.Value() != "on" || !NerdFontAt(dir) {
		t.Fatalf("the tier does not default on: %q", row.Value())
	}
	if err := row.Apply("off"); err != nil {
		t.Fatal(err)
	}
	if NerdFontAt(dir) {
		t.Fatal("off did not persist")
	}
	reread, _ := registry(t, dir).Row(KeyNerdFont)
	if reread.Value() != "off" {
		t.Fatalf("the reread row lost the persisted choice: %q", reread.Value())
	}

	t.Setenv("AFORGE_NERD_FONT", "on")
	if !NerdFontAt(dir) {
		t.Fatal("the environment lost to the persisted file")
	}
	pinned, _ := registry(t, dir).Row(KeyNerdFont)
	name, isPinned := pinned.PinnedBy()
	if !isPinned || name != "AFORGE_NERD_FONT" {
		t.Fatalf("the nerd font row did not report its pin: %q", name)
	}
	if err := pinned.Apply("off"); err == nil || !strings.Contains(err.Error(), name) {
		t.Fatalf("a pinned row accepted an edit: %v", err)
	}

	// A hand-typed pin that means nothing reads as the default rather than
	// stopping a launch over which characters get drawn.
	t.Setenv("AFORGE_NERD_FONT", "sure")
	if !NerdFontAt(dir) {
		t.Fatal("a malformed pin did not fall back to the default")
	}

	// And the second answer the launcher needs: WHO chose. A value nobody can
	// parse is not a choice, which is what lets the terminal veto run only
	// where no human has spoken (12.7 E.1).
	if _, source := NerdFontChosenAt(dir); source != NerdFontSourceNone {
		t.Errorf("an unparseable pin was reported as a choice by %q", source)
	}
	t.Setenv("AFORGE_NERD_FONT", "off")
	if value, source := NerdFontChosenAt(dir); value || source != NerdFontSourceEnv {
		t.Errorf("the pin did not name itself: %v, %q", value, source)
	}
	t.Setenv("AFORGE_NERD_FONT", "")
	if value, source := NerdFontChosenAt(dir); value || source != NerdFontSourcePersisted {
		t.Errorf("the persisted row did not name itself: %v, %q", value, source)
	}
	if _, source := NerdFontChosenAt(t.TempDir()); source != NerdFontSourceNone {
		t.Errorf("an untouched profile reported a chooser: %q", source)
	}
}

// ── the web-search rows ─────────────────────────────────────────────────────

// The provider row is a choice with a working default: a person who has never
// opened the sheet searches, and a value nobody can parse still searches.
func TestSearchProviderDefaultsToAutoAndRefusesAPlugItDoesNotKnow(t *testing.T) {
	dir := t.TempDir()
	rows := registry(t, dir)
	row, ok := rows.Row(KeySearchProvider)
	if !ok {
		t.Fatal("the search provider is not registered")
	}
	if row.Category != CategoryModels || row.Kind != SettingChoice || row.Label != "searching" {
		t.Fatalf("search provider row = %+v", row)
	}
	if row.Value() != SearchProviderAuto || SearchProviderAt(dir) != SearchProviderAuto {
		t.Fatalf("the search provider does not default to auto: %q", row.Value())
	}
	if _, pinned := row.PinnedBy(); pinned {
		t.Fatal("the provider row is pinned by an environment variable; it is a preference, not a secret")
	}

	if err := row.Apply("exa"); err != nil {
		t.Fatal(err)
	}
	if SearchProviderAt(dir) != "exa" {
		t.Fatalf("the pin did not persist: %q", SearchProviderAt(dir))
	}
	reread, _ := registry(t, dir).Row(KeySearchProvider)
	if reread.Value() != "exa" {
		t.Fatalf("the reread row lost the persisted pin: %q", reread.Value())
	}
	if err := reread.Apply("kagi"); err == nil {
		t.Fatal("the row accepted a plug this build does not have")
	}
	if SearchProviderAt(dir) != "exa" {
		t.Fatalf("a refused edit still moved the row: %q", SearchProviderAt(dir))
	}

	// A value written by hand — an older build's plug, a typo — reads as auto
	// rather than as an error. Search must not be takeable away by a stale row.
	if err := writeProfileValue(dir, KeySearchProvider, "yahoo!"); err != nil {
		t.Fatal(err)
	}
	if SearchProviderAt(dir) != SearchProviderAuto {
		t.Fatalf("a stale pin did not fall back to auto: %q", SearchProviderAt(dir))
	}
}

// Both credentials: optional, masked when they read, and pinned by the
// vendors' own environment variables.
func TestTheSearchKeysAreOptionalMaskedAndEnvironmentPinned(t *testing.T) {
	for _, credential := range []struct {
		key   string
		env   string
		label string
		read  func(string) string
	}{
		{KeyExaKey, "EXA_API_KEY", "exa key", ExaKeyAt},
		{KeyJinaKey, "JINA_API_KEY", "jina key", JinaKeyAt},
	} {
		t.Run(credential.key, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv(credential.env, "")
			rows := registry(t, dir)
			row, ok := rows.Row(credential.key)
			if !ok {
				t.Fatalf("%s is not registered", credential.label)
			}
			if row.Category != CategoryModels || row.Kind != SettingText || !row.Secret {
				t.Fatalf("%s row = %+v, want a secret text row", credential.label, row)
			}
			if row.Label != credential.label {
				t.Fatalf("row label = %q, want %q", row.Label, credential.label)
			}

			// Unset is a working configuration and reads as one: the empty
			// label, never a row of bullets standing in for nothing.
			if row.Value() != "not set" || credential.read(dir) != "" {
				t.Fatalf("an unset key reads %q", row.Value())
			}

			if err := row.Apply("secret-key-abcdefgh1234"); err != nil {
				t.Fatal(err)
			}
			if got := credential.read(dir); got != "secret-key-abcdefgh1234" {
				t.Fatalf("the key did not persist whole: %q", got)
			}
			reread, _ := registry(t, dir).Row(credential.key)
			masked := reread.Value()
			if strings.Contains(masked, "secret-key") {
				t.Fatalf("the row rendered the key: %q", masked)
			}
			if !strings.HasSuffix(masked, "1234") || !strings.HasPrefix(masked, "••••") {
				t.Fatalf("the row did not mask to bullets plus a tail: %q", masked)
			}

			// The edit that must not destroy anything: open the row, save what
			// it displayed, keep the key.
			if err := reread.Apply(masked); err != nil {
				t.Fatal(err)
			}
			if got := credential.read(dir); got != "secret-key-abcdefgh1234" {
				t.Fatalf("saving the mask overwrote the key: %q", got)
			}

			// Clearing is still possible, and is the only thing that clears.
			if err := reread.Apply(""); err != nil {
				t.Fatal(err)
			}
			if got := credential.read(dir); got != "" {
				t.Fatalf("an emptied row kept the key: %q", got)
			}

			// The vendor's variable wins, is named, and holds the row.
			t.Setenv(credential.env, "from-the-shell-wxyz")
			if got := credential.read(dir); got != "from-the-shell-wxyz" {
				t.Fatalf("the environment did not win: %q", got)
			}
			pinned, _ := registry(t, dir).Row(credential.key)
			name, isPinned := pinned.PinnedBy()
			if !isPinned || name != credential.env {
				t.Fatalf("%s did not report its pin: %q", credential.label, name)
			}
			if value := pinned.Value(); !strings.HasSuffix(value, "wxyz") || strings.Contains(value, "from-the-shell") {
				t.Fatalf("the pinned row rendered unmasked: %q", value)
			}
			if err := pinned.Apply("another-key"); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("a pinned key accepted an edit: %v", err)
			}
		})
	}
}

// The mask shows enough to tell two keys apart and never enough to use, and it
// never reports a length.
func TestMaskCredentialHidesTheKeyAndItsLength(t *testing.T) {
	short := maskCredential("sk-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	long := maskCredential("sk-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbcccc")
	if len([]rune(short)) != len([]rune(long)) {
		t.Fatalf("the mask leaks the key's length: %q vs %q", short, long)
	}
	if short == long {
		t.Fatalf("two different keys mask identically: %q", short)
	}
	if maskCredential("") != "" {
		t.Fatalf("an unset value masked to %q, want nothing", maskCredential(""))
	}
	// Too short to be a key: show none of it rather than most of it.
	if got := maskCredential("abc"); strings.Contains(got, "abc") {
		t.Fatalf("a short value was rendered: %q", got)
	}
}

// The three throttle rows read their defaults, take a person's answer, and hand
// it back to the accessor the session door calls — which is the whole of what a
// settings row has to do.
// EVERY TIER ROW SHIPS POINTED AT A MODEL, and the two answers a person can give
// one are different from each other: never touching it is this build's own
// choice, emptying it on purpose is "follow the conversation".
//
// The reflex row was the first written this way, for the reason its key states.
// The other three joined it when the crew landed (crew.go), because a whole crew
// following the conversation means the most expensive model in the build
// answering the cheapest questions in it.
func TestEveryTierShipsWithAModelAndCanStillBeCleared(t *testing.T) {
	dir := t.TempDir()
	row := mustRow(t, registry(t, dir), KeyTierReflexModel)

	if got := TierModelAt(dir, ModelTierReflex); got != DefaultReflexModel {
		t.Fatalf("an untouched profile resolves the reflex tier to %q, want %q", got, DefaultReflexModel)
	}
	if got := row.Value(); got != DefaultReflexModel {
		t.Fatalf("the reflex row reads %q in an untouched profile, want %q", got, DefaultReflexModel)
	}
	// And so do its three neighbours, each with the model this build chose for
	// that class of work.
	for _, c := range []struct{ tier, want string }{
		{ModelTierLow, DefaultLowModel},
		{ModelTierHigh, DefaultHighModel},
		{ModelTierMastermind, DefaultMastermindModel},
	} {
		if got := TierModelAt(dir, c.tier); got != c.want {
			t.Fatalf("the %s tier resolves %q in an untouched profile, want %q", c.tier, got, c.want)
		}
	}

	if err := row.Apply("vendor/tiny"); err != nil {
		t.Fatal(err)
	}
	if got := TierModelAt(dir, ModelTierReflex); got != "vendor/tiny" {
		t.Fatalf("the reflex tier resolves %q after a person wrote vendor/tiny", got)
	}
	if got := mustRow(t, registry(t, dir), KeyTierReflexModel).Value(); got != "vendor/tiny" {
		t.Fatalf("the reflex row reads %q on the next launch", got)
	}

	// Cleared is an ANSWER: the row goes back to its empty label and the tier
	// falls to internal/roles' floor, the model the person is talking to.
	if err := row.Apply(""); err != nil {
		t.Fatalf("clearing the reflex row: %v", err)
	}
	if got := TierModelAt(dir, ModelTierReflex); got != "" {
		t.Fatalf("a cleared reflex row resolves %q, want nothing", got)
	}
	if got := mustRow(t, registry(t, dir), KeyTierReflexModel).Value(); got != "follows the conversation" {
		t.Fatalf("a cleared reflex row reads %q, want its empty label", got)
	}
}

func TestTheTaskThrottleRowsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rows := registry(t, dir)

	// NO LIMIT IS THE DEFAULT, and the row says it in words rather than in a
	// zero: "0 tasks at once" reads like a switch that is off.
	parallel, ok := rows.Row(KeyTaskParallel)
	if !ok {
		t.Fatal("the parallel row is not registered")
	}
	if got := parallel.Value(); got != "no limit" {
		t.Fatalf("unset task.parallel reads %q, want its empty label", got)
	}
	if TaskParallelAt(dir) != DefaultTaskParallel {
		t.Fatalf("unset task.parallel resolves %d, want %d", TaskParallelAt(dir), DefaultTaskParallel)
	}
	if err := parallel.Apply("3"); err != nil {
		t.Fatal(err)
	}
	if got := TaskParallelAt(dir); got != 3 {
		t.Fatalf("task.parallel resolves %d after a person wrote 3", got)
	}
	if got := mustRow(t, registry(t, dir), KeyTaskParallel).Value(); got != "3" {
		t.Fatalf("task.parallel reads %q after a person wrote 3", got)
	}
	// And clearing it is asking for the empty label back, not a parse error.
	if err := parallel.Apply(""); err != nil {
		t.Fatalf("clearing task.parallel: %v", err)
	}
	if TaskParallelAt(dir) != 0 {
		t.Fatalf("a cleared task.parallel resolves %d, want no limit", TaskParallelAt(dir))
	}

	// The load row is a figure and not a count: 1.5 must survive being written.
	load := mustRow(t, rows, KeyTaskMaxLoad)
	if got := load.Value(); got != "1.5" {
		t.Fatalf("unset task.max_load reads %q, want 1.5", got)
	}
	if err := load.Apply("2.25"); err != nil {
		t.Fatal(err)
	}
	if got := TaskMaxLoadAt(dir); got != 2.25 {
		t.Fatalf("task.max_load resolves %v after a person wrote 2.25", got)
	}
	if err := load.Apply("busy"); err == nil {
		t.Fatal("task.max_load accepted a word")
	}
	if err := load.Apply("0"); err != nil || TaskMaxLoadAt(dir) != 0 {
		t.Fatalf("turning the load check off: %v, %v", err, TaskMaxLoadAt(dir))
	}

	memory := mustRow(t, rows, KeyTaskMinFreeMB)
	if got := memory.Value(); got != "1536" {
		t.Fatalf("unset task.min_free_mb reads %q", got)
	}
	if err := memory.Apply("512"); err != nil {
		t.Fatal(err)
	}
	if got := TaskMinFreeMBAt(dir); got != 512 {
		t.Fatalf("task.min_free_mb resolves %d after a person wrote 512", got)
	}
	if err := memory.Apply("-1"); err == nil {
		t.Fatal("task.min_free_mb accepted a negative floor")
	}
}

func mustRow(t *testing.T, rows *Settings, key string) Setting {
	t.Helper()
	row, ok := rows.Row(key)
	if !ok {
		t.Fatalf("%s is not registered", key)
	}
	return row
}

// ── background checks ───────────────────────────────────────────────────────

// THE ROW READS THE MACHINE AND NEVER THE FILE. What it shows is derived from
// the timer's own definition on disk, so a person who removed the agent by hand
// is told `off` in the one place they went to check — and turning the row is
// what installs and removes it.
func TestTheBackgroundChecksRowReadsTheTimerAndTurnsIt(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	program := filepath.Join(t.TempDir(), "aforge")
	if err := os.WriteFile(program, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	timer, err := standing.NewWatch(standing.WatchOptions{
		Platform: "darwin", HomeDir: home, Executable: program, UID: 501,
		Runner: quietRunner{},
	})
	if err != nil {
		t.Fatalf("NewWatch: %v", err)
	}
	registry := NewSettings(SettingsOptions{ProfileDir: dir, BackgroundChecks: timer})
	row, ok := registry.Row(KeyStandingBackground)
	if !ok {
		t.Fatal("a machine with a timer has no background checks row")
	}
	if row.Category != CategoryPractice || row.Kind != SettingChoice {
		t.Fatalf("row = %+v", row)
	}
	// Nothing installed yet, so the row says so however the file reads.
	if row.Value() != BackgroundOff {
		t.Fatalf("an uninstalled timer reads %q", row.Value())
	}
	if err := row.Apply(BackgroundOn); err != nil {
		t.Fatalf("turning it on: %v", err)
	}
	if row.Value() != BackgroundOn {
		t.Fatalf("an installed timer reads %q", row.Value())
	}
	if BackgroundChecksAt(dir) != BackgroundOn {
		t.Fatalf("the intent on disk = %q", BackgroundChecksAt(dir))
	}
	if err := row.Apply(BackgroundOff); err != nil {
		t.Fatalf("turning it off: %v", err)
	}
	if row.Value() != BackgroundOff {
		t.Fatalf("a removed timer reads %q", row.Value())
	}
	if BackgroundChecksWantedAt(dir) {
		t.Fatal("the launch repair would put back a timer the person turned off")
	}
	// AND THE HINT NAMES THE THING IT INSTALLS. "aforge installs a launchd
	// agent" is a sentence nobody can check.
	for _, want := range []string{standing.DarwinTickLabel, standing.LinuxTickTimer, standing.IntervalWords()} {
		if !strings.Contains(row.Hint, want) {
			t.Fatalf("the hint does not name %q: %s", want, row.Hint)
		}
	}
	// And a model may turn it: this is a preference, not a rail on the model.
	if !row.SelfService() {
		t.Fatal("the chat cannot turn off the background checks somebody asked it to")
	}
}

// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. On a machine with no
// timer to install there is nothing for this switch to switch, so the sheet has
// no row rather than a row that reads nothing and refuses every write.
func TestTheBackgroundChecksRowIsAbsentWithNoTimer(t *testing.T) {
	registry := NewSettings(SettingsOptions{ProfileDir: t.TempDir()})
	if _, ok := registry.Row(KeyStandingBackground); ok {
		t.Fatal("a machine with no timer offered a switch for one")
	}
	for _, row := range registry.Rows() {
		if row.Key == KeyStandingBackground {
			t.Fatal("the row is in the sheet after all")
		}
	}
}

// A REPOSITORY MAY NOT TURN THIS ON. Installing a timer is a change to
// somebody's machine, and a checked-in file that could make one is a clone
// arranging to run a program on every laptop it lands on.
func TestBackgroundChecksAreProfileOnly(t *testing.T) {
	for _, key := range ProjectKeys {
		if key == KeyStandingBackground {
			t.Fatal("a repository can install a timer on the reader's machine")
		}
	}
}

// quietRunner is this machine's scheduler, stood in for. Nothing in these tests
// goes near launchctl.
type quietRunner struct{}

func (quietRunner) Run(context.Context, string, ...string) error { return nil }
