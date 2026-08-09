package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// The settings registry is the one place a user-tunable knob is written down.
// Every row declares how it reads today, how it is applied, where it persists,
// and which environment variable pins it — so a new knob is a row here rather
// than a new lookup scattered through the tree, and the completeness test in
// settings_test.go fails the build until it is registered.
//
// Resolution is the budget rail's order, generalized: environment → the
// profile's config.json → the built-in default. The environment always wins,
// and a pinned row renders read-only rather than letting the surface fight the
// shell it was launched from.

// SettingKind decides how a row reads, edits, and validates.
type SettingKind int

const (
	// SettingModel opens the existing capability-filtered model picker.
	SettingModel SettingKind = iota
	SettingDollars
	SettingDuration
	SettingPercent
	SettingCount
	SettingBool
	SettingChoice
	SettingText
)

// Category names are the calm, plain-language groups the surface renders in
// this order.
const (
	CategoryModels    = "models"
	CategoryMoney     = "money & limits"
	CategoryRhythm    = "rhythm"
	CategoryLearning  = "learning"
	CategoryDocuments = "documents & vision"
	// CategorySharing groups what aforge puts of itself into work that leaves
	// the machine under the user's name. None of the other groups is about the
	// outside world, so attribution gets its own rather than hiding in one.
	CategorySharing    = "sharing"
	CategoryAppearance = "appearance"
)

// SettingCategories is the render order of the sheet.
var SettingCategories = []string{
	CategoryModels, CategoryMoney, CategoryRhythm,
	CategoryLearning, CategoryDocuments, CategorySharing, CategoryAppearance,
}

// Persisted keys are also the json field names in the profile's config.json.
// KeyDailyBudget keeps the name /budget default already writes.
const (
	KeyDailyBudget    = "daily_budget_usd"
	KeyPlanConsent    = "plan_consent_usd"
	KeyPracticeBudget = "practice_budget_usd"
	KeyPracticeIdle   = "practice_idle"
	KeyBriefAfter     = "brief_after"
	KeyTenureAfter    = "tenure_after"
	KeyDemandShare    = "practice_demand_pct"
	KeyProposeSkills  = "propose_new_skills"
	KeyDocumentEngine = "document_engine"
	KeyVisionModel    = "vision_model"
	KeyAttribution    = "attribution"
	KeySplitPct       = "split_pct"
)

// ModelSettingSlots is the palette's slot order, kept identical so the sheet
// and the models door read the same list in the same sequence.
var ModelSettingSlots = []string{"talk", "work", "plan", "voice", "image", "speech", "music", "video", "boost"}

// DocumentEngines are the four rungs AFORGE_DOC_ENGINE accepts.
var DocumentEngines = []string{"auto", "local", "free", "ocr"}

// OperatorEnvPins is the explicit allowlist of environment variables that are
// plumbing rather than settings: endpoints, credentials, profile roots, and
// planner internals. They are listed read-only in the sheet's environment
// footer and never become editable rows.
var OperatorEnvPins = []string{
	"AFORGE_BASE_URL",
	// AFORGE_HOME moves the graph, workspace, craft repository and resident
	// lease somewhere else in one word. It is plumbing rather than a setting
	// for the plainest reason there is: it decides which store the sheet
	// itself is being read out of.
	"AFORGE_HOME",
	"AFORGE_SITE_URL",
	"AFORGE_SITE_NAME",
	"AFORGE_PROFILE_DIR",
	"AFORGE_MODELS",
	"AFORGE_REASONING",
	"AFORGE_EXEC_REASONING",
	"AFORGE_SPINE_SAMPLES",
	"AFORGE_MAX_DEPTH",
	"AFORGE_NODE_BUDGET",
	"AFORGE_SKILL_DIR",
	"AFORGE_SKILLS_BIN",
	"AFORGE_RTK",
	"AFORGE_RTK_BIN",
	"AFORGE_PREAUTHORIZE_SPEND",
	// AFORGE_SWEPRO is not a setting anybody would ever want to turn on: it
	// tells the binary, before it has read anything else, that this process
	// is not aforge at all but the vendored swe engine (cmd/aforge/swepro.go).
	// The subharness sets it on the children it spawns; a user who set it
	// would simply lose their own program.
	"AFORGE_SWEPRO",
}

// Defaults the registry owns beyond the ones config.go already declares.
const (
	// DefaultPlanConsentUSD is where ambition stops being cheap. Below it a
	// plan simply runs, because asking about a two-dollar errand is the nagging
	// nobody wants; above it the user is quoted a count and a price and gets to
	// say no first. It is deliberately far under the daily rail: the rail is a
	// stop after the fact, and this is the moment before.
	DefaultPlanConsentUSD = 3.0

	// DefaultTenureAfter is the clean-firing count a standing charter needs
	// before it earns tenure.
	DefaultTenureAfter = 3

	// DefaultPracticeDemandPct splits self-directed practice between measured
	// demand and open curiosity. The learning loops read it; the surface writes
	// it now so the preference exists before the loop that honors it lands.
	DefaultPracticeDemandPct = 70

	// DefaultProposeSkills lets the resident offer new skills it believes it
	// should learn. Same contract as the share above: persisted now, read by
	// the loops that follow.
	DefaultProposeSkills = true

	// DefaultAttribution signs by default, because the signature is provenance:
	// work the user did not type should be readable as such by whoever reads
	// the history later. One row turns it off.
	DefaultAttribution = true

	// The divider clamps so neither pane can be set into uselessness. The TUI
	// reads these so the drag, the [ ] nudge, and the sheet agree.
	DefaultSplitPct = 80
	MinSplitPct     = 25
	MaxSplitPct     = 85
)

// Setting is one row: what it is called, what it reads now, and what happens
// when the user changes it.
type Setting struct {
	Key      string
	Category string
	Label    string
	Hint     string
	Kind     SettingKind

	// Slot is the model role a SettingModel row fronts.
	Slot string

	// Choices lists the accepted values of a SettingChoice row.
	Choices []string

	// Env pins the row from the environment: while it is set the value is
	// read-only and the surface says which variable owns it.
	Env string

	// EnvDefault only seeds a value the user has never chosen — the model
	// slots work this way, so a pinned default never freezes the row.
	EnvDefault string

	// PrefsField names the chat-prefs json field this row fronts when the
	// value lives beside the graph rather than in the profile config.
	PrefsField string

	// EmptyLabel reads for a text row whose value is unset.
	EmptyLabel string

	read  func() string
	write func(string) error
}

// Value is the row's current reading, already formatted for display.
func (s Setting) Value() string {
	if s.read == nil {
		return ""
	}
	value := s.read()
	if value == "" && s.EmptyLabel != "" {
		return s.EmptyLabel
	}
	return value
}

// PinnedBy names the environment variable holding this row read-only.
func (s Setting) PinnedBy() (string, bool) {
	if s.Env == "" {
		return "", false
	}
	if strings.TrimSpace(os.Getenv(s.Env)) == "" {
		return "", false
	}
	return s.Env, true
}

// Apply validates, persists, and lands the live effect. A pinned row refuses
// calmly rather than writing a value the environment would keep overriding.
func (s Setting) Apply(raw string) error {
	if name, pinned := s.PinnedBy(); pinned {
		return fmt.Errorf("%s is set by %s", s.Label, name)
	}
	if s.write == nil {
		return fmt.Errorf("%s cannot be changed here", s.Label)
	}
	return s.write(raw)
}

// SettingGroup is one rendered category.
type SettingGroup struct {
	Title string
	Rows  []Setting
}

// SettingsOptions supplies the live seams the registry cannot reach on its
// own: the running model slots and the surface's own divider preference.
type SettingsOptions struct {
	ProfileDir string

	// ModelValue and SetModel front the model slots. They stay in the chat
	// prefs file; the registry does not migrate them.
	ModelValue func(slot string) string
	SetModel   func(slot, slug string) error

	// SplitPct and SaveSplitPct front the chat/task divider.
	SplitPct     func() int
	SaveSplitPct func(pct int)

	// Applied fires after a row is successfully written, so a process holding
	// its own copy of a value can honor the change without waiting for a
	// relaunch.
	Applied func(key string)
}

// Settings is the built registry.
type Settings struct {
	options SettingsOptions
	rows    []Setting
}

// NewSettings builds the registry against one profile directory and whatever
// live seams the caller can supply. Missing seams make a row read-only rather
// than absent, so the sheet always shows the complete surface.
func NewSettings(options SettingsOptions) *Settings {
	registry := &Settings{options: options}
	registry.rows = registry.build()
	for index := range registry.rows {
		registry.rows[index].write = registry.announce(registry.rows[index].Key, registry.rows[index].write)
	}
	return registry
}

func (s *Settings) announce(key string, write func(string) error) func(string) error {
	if write == nil {
		return nil
	}
	return func(raw string) error {
		if err := write(raw); err != nil {
			return err
		}
		if s.options.Applied != nil {
			s.options.Applied(key)
		}
		return nil
	}
}

// Rows returns every row in category order.
func (s *Settings) Rows() []Setting {
	return append([]Setting(nil), s.rows...)
}

// Row finds one row by key.
func (s *Settings) Row(key string) (Setting, bool) {
	for _, row := range s.rows {
		if row.Key == key {
			return row, true
		}
	}
	return Setting{}, false
}

// Groups returns the rows grouped for rendering, skipping empty categories.
func (s *Settings) Groups() []SettingGroup {
	groups := make([]SettingGroup, 0, len(SettingCategories))
	for _, title := range SettingCategories {
		group := SettingGroup{Title: title}
		for _, row := range s.rows {
			if row.Category == title {
				group.Rows = append(group.Rows, row)
			}
		}
		if len(group.Rows) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

// EnvironmentPins lists the operator plumbing currently set, for the sheet's
// read-only footer.
func (s *Settings) EnvironmentPins() []string {
	set := make([]string, 0, len(OperatorEnvPins))
	for _, name := range OperatorEnvPins {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			set = append(set, name)
		}
	}
	return set
}

// ModelSettingKey is the registry key fronting one model slot.
func ModelSettingKey(slot string) string { return "model." + slot }

func (s *Settings) build() []Setting {
	dir := s.options.ProfileDir
	rows := make([]Setting, 0, len(ModelSettingSlots)+10)

	for _, slot := range ModelSettingSlots {
		rows = append(rows, s.modelRow(slot))
	}

	rows = append(rows,
		Setting{
			Key: KeyDailyBudget, Category: CategoryMoney, Kind: SettingDollars,
			Label: "daily budget", Env: "AFORGE_DAILY_BUDGET",
			Hint: "what aforge may spend on your work in a day. 0 removes the rail. " +
				"A change lands at the next rail check.",
			read:  func() string { return formatDollars(resolvedDollars(DailyBudgetUSDAt(dir))) },
			write: func(raw string) error { return writeDollars(dir, KeyDailyBudget, raw) },
		},
		Setting{
			Key: KeyPlanConsent, Category: CategoryMoney, Kind: SettingDollars,
			Label: "ask before spending", Env: "AFORGE_PLAN_CONSENT",
			Hint: "when a planned job is estimated to cost more than this, aforge quotes " +
				"the step count and the price and waits for your go-ahead. 0 never asks.",
			read:  func() string { return formatDollars(resolvedDollars(PlanConsentUSDAt(dir))) },
			write: func(raw string) error { return writeDollars(dir, KeyPlanConsent, raw) },
		},
		Setting{
			Key: KeyPracticeBudget, Category: CategoryMoney, Kind: SettingDollars,
			Label: "practice budget", Env: "AFORGE_PRACTICE_BUDGET",
			Hint: "the slice of the day reserved for aforge practicing on itself. " +
				"A change lands the next time aforge starts.",
			read:  func() string { return formatDollars(resolvedDollars(PracticeBudgetUSDAt(dir))) },
			write: func(raw string) error { return writeDollars(dir, KeyPracticeBudget, raw) },
		},
		Setting{
			Key: KeyPracticeIdle, Category: CategoryMoney, Kind: SettingDuration,
			Label: "quiet before practice", Env: "AFORGE_PRACTICE_IDLE",
			Hint:  "how long the room stays quiet before aforge starts practicing.",
			read:  func() string { return formatDuration(resolvedDuration(PracticeIdleAt(dir))) },
			write: func(raw string) error { return writeDuration(dir, KeyPracticeIdle, raw) },
		},
		Setting{
			Key: KeyBriefAfter, Category: CategoryRhythm, Kind: SettingDuration,
			Label: "arrival brief after", Env: "AFORGE_BRIEF_AFTER",
			Hint:  "how long you have to be away before aforge greets you with a summary. 0 always briefs.",
			read:  func() string { return formatDuration(resolvedDuration(BriefAfterAt(dir))) },
			write: func(raw string) error { return writeDuration(dir, KeyBriefAfter, raw) },
		},
		Setting{
			Key: KeyTenureAfter, Category: CategoryRhythm, Kind: SettingCount,
			Label: "tenure after", Env: "AFORGE_TENURE_AFTER",
			Hint:  "how many clean firings a standing charter needs before it earns tenure.",
			read:  func() string { return strconv.Itoa(TenureAfterAt(dir)) },
			write: func(raw string) error { return writeTenure(dir, raw) },
		},
		Setting{
			Key: KeyDemandShare, Category: CategoryLearning, Kind: SettingPercent,
			Label: "demand vs curiosity",
			Hint:  "this much of practice follows measured demand; the rest follows open curiosity.",
			read:  func() string { return formatPercent(PracticeDemandPctAt(dir)) },
			write: func(raw string) error { return writePercent(dir, KeyDemandShare, raw, 0, 100) },
		},
		Setting{
			Key: KeyProposeSkills, Category: CategoryLearning, Kind: SettingBool,
			Label: "propose new skills",
			Hint:  "let aforge offer skills it believes it should learn.",
			read:  func() string { return formatBool(ProposeSkillsAt(dir)) },
			write: func(raw string) error { return writeBool(dir, KeyProposeSkills, raw) },
		},
		Setting{
			Key: KeyDocumentEngine, Category: CategoryDocuments, Kind: SettingChoice,
			Label: "document engine", Env: "AFORGE_DOC_ENGINE", Choices: DocumentEngines,
			Hint: "which rung reads your documents. auto walks local, then free, then paid OCR. " +
				"A change lands the next time aforge starts.",
			read:  func() string { return resolvedEngine(DocumentEngineAt(dir)) },
			write: func(raw string) error { return writeChoice(dir, KeyDocumentEngine, raw, DocumentEngines) },
		},
		Setting{
			Key: KeyVisionModel, Category: CategoryDocuments, Kind: SettingText,
			Label: "vision model", Env: "AFORGE_VISION_MODEL", EmptyLabel: "automatic",
			Hint: "the model that looks at images. Leave it blank and aforge picks one that can see. " +
				"A change lands the next time aforge starts.",
			read:  func() string { return VisionModelAt(dir) },
			write: func(raw string) error { return writeText(dir, KeyVisionModel, raw) },
		},
		Setting{
			Key: KeyAttribution, Category: CategorySharing, Kind: SettingBool,
			Label: "attribution", Env: "AFORGE_ATTRIBUTION",
			Hint: "signs commits and PRs aforge writes for you — one trailer, one footer line. " +
				"A change lands on the next job.",
			read:  func() string { return formatBool(AttributionAt(dir)) },
			write: func(raw string) error { return writeBool(dir, KeyAttribution, raw) },
		},
		s.splitRow(),
	)
	return rows
}

func (s *Settings) modelRow(slot string) Setting {
	options := s.options
	row := Setting{
		Key: ModelSettingKey(slot), Category: CategoryModels, Kind: SettingModel,
		Label: slot, Slot: slot, PrefsField: modelPrefsField(slot),
		Hint: modelSlotHint(slot),
	}
	if name, ok := modelSlotEnvDefault(slot); ok {
		row.EnvDefault = name
	}
	row.read = func() string {
		if options.ModelValue == nil {
			return ""
		}
		return strings.TrimSpace(options.ModelValue(slot))
	}
	row.write = func(slug string) error {
		if options.SetModel == nil {
			return fmt.Errorf("model switching is unavailable here")
		}
		return options.SetModel(slot, strings.TrimSpace(slug))
	}
	return row
}

func (s *Settings) splitRow() Setting {
	options := s.options
	row := Setting{
		Key: KeySplitPct, Category: CategoryAppearance, Kind: SettingPercent,
		Label: "chat width", PrefsField: KeySplitPct,
		Hint: fmt.Sprintf("the chat pane's share of the frame while the task rail is open (%d–%d). "+
			"[ and ] nudge it too.", MinSplitPct, MaxSplitPct),
	}
	row.read = func() string {
		return formatPercent(ClampSplitPct(readSplit(options)))
	}
	row.write = func(raw string) error {
		value, err := parsePercent(raw, MinSplitPct, MaxSplitPct)
		if err != nil {
			return err
		}
		if options.SaveSplitPct == nil {
			return fmt.Errorf("chat width is unavailable here")
		}
		options.SaveSplitPct(value)
		return nil
	}
	return row
}

func readSplit(options SettingsOptions) int {
	if options.SplitPct == nil {
		return DefaultSplitPct
	}
	if pct := options.SplitPct(); pct != 0 {
		return pct
	}
	return DefaultSplitPct
}

// ClampSplitPct keeps the divider inside the usable band. Zero stays "unset"
// and resolves to the default at layout time.
func ClampSplitPct(pct int) int {
	if pct == 0 {
		return 0
	}
	return max(MinSplitPct, min(MaxSplitPct, pct))
}

func modelPrefsField(slot string) string {
	switch slot {
	case "talk":
		return "chat_model"
	case "work":
		return "task_model"
	default:
		return slot + "_model"
	}
}

func modelSlotEnvDefault(slot string) (string, bool) {
	switch slot {
	case "talk", "work":
		return "AFORGE_MODEL", true
	case "plan":
		return "AFORGE_PLAN_MODEL", true
	case "voice":
		return "AFORGE_VOICE_MODEL", true
	case "image":
		return "AFORGE_IMAGE_MODEL", true
	case "speech":
		return "AFORGE_SPEECH_MODEL", true
	case "music":
		return "AFORGE_MUSIC_MODEL", true
	case "video":
		return "AFORGE_VIDEO_MODEL", true
	default:
		return "", false
	}
}

func modelSlotHint(slot string) string {
	switch slot {
	case "talk":
		return "the model that answers you here. It changes on your next message."
	case "work":
		return "the model that does the work. It changes on the next job."
	case "plan":
		return "the model that plans and reviews the work. Empty follows the work model."
	case "boost":
		return "the heavier model ctrl+b reaches for. Empty follows the work model."
	case "voice":
		return "the model that hears you when you speak."
	case "image":
		return "the model that draws."
	case "speech":
		return "the model that speaks."
	case "music":
		return "the model that composes."
	case "video":
		return "the model that films."
	}
	return ""
}

// Persisted resolution — environment, then the profile's config.json, then the
// built-in default. Malformed persisted values fall back to the default rather
// than stopping a launch; a malformed environment value is the operator's own
// explicit instruction and still errors.

// PlanConsentUSDAt resolves the estimate above which a plan asks first.
func PlanConsentUSDAt(profileDir string) (float64, error) {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_PLAN_CONSENT")); raw != "" {
		return validateDailyBudgetValue(raw, "AFORGE_PLAN_CONSENT")
	}
	if value, ok := persistedFloat(profileDir, KeyPlanConsent); ok && value >= 0 {
		return value, nil
	}
	return DefaultPlanConsentUSD, nil
}

// PracticeBudgetUSDAt resolves the daily self-practice carve-out.
func PracticeBudgetUSDAt(profileDir string) (float64, error) {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_PRACTICE_BUDGET")); raw != "" {
		return validateDailyBudgetValue(raw, "AFORGE_PRACTICE_BUDGET")
	}
	if value, ok := persistedFloat(profileDir, KeyPracticeBudget); ok && value >= 0 {
		return value, nil
	}
	return DefaultPracticeBudgetUSD, nil
}

// PracticeIdleAt resolves the quiet period before self-practice.
func PracticeIdleAt(profileDir string) (time.Duration, error) {
	return durationAt(profileDir, "AFORGE_PRACTICE_IDLE", KeyPracticeIdle, DefaultPracticeIdle)
}

// BriefAfterAt resolves the absence that earns an arrival brief.
func BriefAfterAt(profileDir string) (time.Duration, error) {
	return durationAt(profileDir, "AFORGE_BRIEF_AFTER", KeyBriefAfter, DefaultBriefAfter)
}

func durationAt(profileDir, envName, key string, fallback time.Duration) (time.Duration, error) {
	if raw := strings.TrimSpace(os.Getenv(envName)); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value < 0 {
			return 0, fmt.Errorf("%s: want a non-negative duration, got %q", envName, raw)
		}
		return value, nil
	}
	if value, ok := persistedDuration(profileDir, key); ok {
		return value, nil
	}
	return fallback, nil
}

// TenureAfterAt resolves the clean-firing count that earns a charter tenure.
func TenureAfterAt(profileDir string) int {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_TENURE_AFTER")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			return value
		}
		return DefaultTenureAfter
	}
	if value, ok := persistedInt(profileDir, KeyTenureAfter); ok && value > 0 {
		return value
	}
	return DefaultTenureAfter
}

// PracticeDemandPctAt resolves how much practice follows measured demand.
func PracticeDemandPctAt(profileDir string) int {
	if value, ok := persistedInt(profileDir, KeyDemandShare); ok && value >= 0 && value <= 100 {
		return value
	}
	return DefaultPracticeDemandPct
}

// ProposeSkillsAt resolves whether the resident may offer new skills.
func ProposeSkillsAt(profileDir string) bool {
	if value, ok := persistedBool(profileDir, KeyProposeSkills); ok {
		return value
	}
	return DefaultProposeSkills
}

// AttributionAt resolves whether aforge signs the git work it does for the
// user. A malformed pin reads as the default rather than refusing a launch over
// a signature.
func AttributionAt(profileDir string) bool {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_ATTRIBUTION")); raw != "" {
		if value, err := parseBool(raw); err == nil {
			return value
		}
		return DefaultAttribution
	}
	if value, ok := persistedBool(profileDir, KeyAttribution); ok {
		return value
	}
	return DefaultAttribution
}

// DocumentEngineAt resolves the document-reading rung.
func DocumentEngineAt(profileDir string) (string, error) {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_DOC_ENGINE")); raw != "" {
		engine := strings.ToLower(raw)
		if !knownDocumentEngine(engine) {
			return "", fmt.Errorf("AFORGE_DOC_ENGINE: unknown engine %q (auto, local, free, ocr)", engine)
		}
		return engine, nil
	}
	if value, ok := persistedString(profileDir, KeyDocumentEngine); ok {
		if engine := strings.ToLower(strings.TrimSpace(value)); knownDocumentEngine(engine) {
			return engine, nil
		}
	}
	return DefaultDocumentEngine, nil
}

// VisionModelAt resolves the image-inspection proxy slot. Empty means resolve
// from the live catalog at use.
func VisionModelAt(profileDir string) string {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_VISION_MODEL")); raw != "" {
		return raw
	}
	if value, ok := persistedString(profileDir, KeyVisionModel); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// InstallPersistedEnv exports the persisted value of every knob whose only
// reader is the process environment, so a choice made in the sheet survives a
// relaunch without a second lookup path. A variable the user actually set is
// never overwritten — the environment still wins.
func InstallPersistedEnv(profileDir string) {
	if strings.TrimSpace(os.Getenv("AFORGE_TENURE_AFTER")) != "" {
		return
	}
	if value, ok := persistedInt(profileDir, KeyTenureAfter); ok && value > 0 {
		_ = os.Setenv("AFORGE_TENURE_AFTER", strconv.Itoa(value))
	}
}

func knownDocumentEngine(engine string) bool {
	for _, candidate := range DocumentEngines {
		if candidate == engine {
			return true
		}
	}
	return false
}

// Writers. Each validates in plain language, then persists atomically through
// the same profile config.json the budget rail already writes.

func writeDollars(profileDir, key, raw string) error {
	value, err := parseDollars(raw)
	if err != nil {
		return err
	}
	return writeProfileValue(profileDir, key, value)
}

func writeDuration(profileDir, key, raw string) error {
	value, err := parseDuration(raw)
	if err != nil {
		return err
	}
	return writeProfileValue(profileDir, key, formatDuration(value))
}

func writePercent(profileDir, key, raw string, low, high int) error {
	value, err := parsePercent(raw, low, high)
	if err != nil {
		return err
	}
	return writeProfileValue(profileDir, key, value)
}

func writeBool(profileDir, key, raw string) error {
	value, err := parseBool(raw)
	if err != nil {
		return err
	}
	return writeProfileValue(profileDir, key, value)
}

func writeChoice(profileDir, key, raw string, choices []string) error {
	value := strings.ToLower(strings.TrimSpace(raw))
	for _, choice := range choices {
		if choice == value {
			return writeProfileValue(profileDir, key, value)
		}
	}
	return fmt.Errorf("pick one of: %s", strings.Join(choices, ", "))
}

func writeText(profileDir, key, raw string) error {
	return writeProfileValue(profileDir, key, strings.TrimSpace(raw))
}

// writeTenure persists the count and exports it, because the standing watch
// reads the variable at each check: the change lands in this process too.
func writeTenure(profileDir, raw string) error {
	value, err := parseCount(raw)
	if err != nil {
		return err
	}
	if value <= 0 {
		return fmt.Errorf("that needs to be at least 1")
	}
	if err := writeProfileValue(profileDir, KeyTenureAfter, value); err != nil {
		return err
	}
	_ = os.Setenv("AFORGE_TENURE_AFTER", strconv.Itoa(value))
	return nil
}

// Parsers. Their errors are the words the row shows under itself, so they read
// like a person talking rather than a validator.

func parseDollars(raw string) (float64, error) {
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "$"))
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("that's not a dollar amount")
	}
	return value, nil
}

func parseDuration(raw string) (time.Duration, error) {
	text := strings.TrimSpace(raw)
	if text == "0" {
		return 0, nil
	}
	value, err := time.ParseDuration(text)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("that's not a length of time — try 20m or 4h")
	}
	return value, nil
}

func parsePercent(raw string, low, high int) (int, error) {
	text := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), "%"))
	value, err := strconv.Atoi(text)
	if err != nil || value < low || value > high {
		return 0, fmt.Errorf("that's not a percentage between %d and %d", low, high)
	}
	return value, nil
}

func parseCount(raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return 0, fmt.Errorf("that's not a whole number")
	}
	return value, nil
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	}
	return false, fmt.Errorf("that's not on or off")
}

// Formatters.

func formatDollars(value float64) string {
	return "$" + strconv.FormatFloat(value, 'f', -1, 64)
}

func formatPercent(value int) string { return strconv.Itoa(value) + "%" }

func formatBool(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

// formatDuration writes the shortest honest form: 4h, 20m, 1h30m.
func formatDuration(value time.Duration) string {
	if value <= 0 {
		return "0"
	}
	var text strings.Builder
	if hours := int(value / time.Hour); hours > 0 {
		fmt.Fprintf(&text, "%dh", hours)
	}
	if minutes := int(value % time.Hour / time.Minute); minutes > 0 {
		fmt.Fprintf(&text, "%dm", minutes)
	}
	if seconds := int(value % time.Minute / time.Second); seconds > 0 {
		fmt.Fprintf(&text, "%ds", seconds)
	}
	if text.Len() == 0 {
		return value.String()
	}
	return text.String()
}

// A row still has to read while the environment holds an unparsable value.
// The sheet shows the pin and refuses the edit; the reading falls back rather
// than blanking.

func resolvedDollars(value float64, err error) float64 {
	if err != nil {
		return 0
	}
	return value
}

func resolvedDuration(value time.Duration, err error) time.Duration {
	if err != nil {
		return 0
	}
	return value
}

func resolvedEngine(value string, err error) string {
	if err != nil {
		return DefaultDocumentEngine
	}
	return value
}

// Typed reads of the profile config. A malformed entry reports "absent" so a
// hand-edited file degrades to the default instead of refusing to start.

func persistedFloat(profileDir, key string) (float64, bool) {
	encoded, ok := persistedValue(profileDir, key)
	if !ok {
		return 0, false
	}
	var value float64
	if err := json.Unmarshal(encoded, &value); err != nil {
		return 0, false
	}
	return value, true
}

func persistedInt(profileDir, key string) (int, bool) {
	encoded, ok := persistedValue(profileDir, key)
	if !ok {
		return 0, false
	}
	var value int
	if err := json.Unmarshal(encoded, &value); err != nil {
		return 0, false
	}
	return value, true
}

func persistedBool(profileDir, key string) (bool, bool) {
	encoded, ok := persistedValue(profileDir, key)
	if !ok {
		return false, false
	}
	var value bool
	if err := json.Unmarshal(encoded, &value); err != nil {
		return false, false
	}
	return value, true
}

func persistedString(profileDir, key string) (string, bool) {
	encoded, ok := persistedValue(profileDir, key)
	if !ok {
		return "", false
	}
	var value string
	if err := json.Unmarshal(encoded, &value); err != nil {
		return "", false
	}
	return value, true
}

func persistedDuration(profileDir, key string) (time.Duration, bool) {
	text, ok := persistedString(profileDir, key)
	if !ok {
		return 0, false
	}
	value, err := parseDuration(text)
	if err != nil {
		return 0, false
	}
	return value, true
}

func persistedValue(profileDir, key string) (json.RawMessage, bool) {
	values, err := readProfileConfig(profileDir)
	if err != nil {
		return nil, false
	}
	encoded, ok := values[key]
	return encoded, ok
}
