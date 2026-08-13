package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/store"
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

// Category names are the four faint lowercase words the sheet may announce a
// section with (15). There were seven, and five of them were labels doing
// structure's job: `rhythm`, `documents & vision` and `sharing` each announced
// two rows or one, which is a header naming a mechanism rather than a section a
// reader could otherwise not place. Deleting them is 15's own test — the rows
// still read, because position and spacing already said what the word said.
const (
	// CategoryModels is what runs the work: one row per role the router has,
	// then the capability models beside them.
	CategoryModels = "models"
	// CategorySpending is every dollar the product will spend without asking.
	CategorySpending = "spending"
	// CategoryPractice is what aforge does with its own time, and what it
	// remembers of yours.
	CategoryPractice = "memory & practice"
	// CategoryInterface is how the surface draws itself, and how it signs the
	// work that leaves the machine.
	CategoryInterface = "interface"
)

// The old spellings, kept as aliases so a surface that still names one keeps
// compiling while it is being ported. They are the same four words; nothing
// resolves to a group that no longer exists.
const (
	CategoryMoney      = CategorySpending
	CategoryLearning   = CategoryPractice
	CategoryAppearance = CategoryInterface
)

// SettingCategories is the render order of the sheet.
var SettingCategories = []string{
	CategoryModels, CategorySpending, CategoryPractice, CategoryInterface,
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
	KeyDocumentEngine = "document_engine"
	KeyVisionModel    = "vision_model"
	KeyAttribution    = "attribution"
	KeySplitPct       = "split_pct"
	KeyLinearMode     = "linear_mode"
	KeyNerdFont       = "nerd_font"

	// The two context-law knobs. Fill is how much of a model's window any
	// agent may use before compaction fires; the reserve is the room every
	// call keeps for its answer and its reasoning.
	KeyContextFill       = "context_fill_pct"
	KeyCompletionReserve = "completion_reserve"
)

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
	// The two dials on the context-window law (internal/ctxbudget): how full a
	// window may get before it compacts, and how much room every call keeps for
	// its answer and its reasoning. They are plumbing for the same reason the
	// node budget is — numbers the engine spends, not preferences the product
	// has an opinion about — and the place they are actually turned is a
	// headless campaign, where `aforge do --context-fill / --completion-reserve`
	// sets them for one run and docs/HEADLESS.md is their documentation.
	"AFORGE_CONTEXT_FILL_PCT",
	"AFORGE_COMPLETION_RESERVE",
	"AFORGE_SKILL_DIR",
	"AFORGE_SKILLS_BIN",
	"AFORGE_RTK",
	"AFORGE_RTK_BIN",
	"AFORGE_PREAUTHORIZE_SPEND",
	// AFORGE_SWE_MAX_COST is the dollar ceiling one coding-pipeline leaf may
	// spend inside the vendored engine. It is plumbing rather than a setting
	// for the same reason the node budget is: it is a number handed to a
	// subprocess, not a preference the product has an opinion about, and the
	// preference that governs spending is the daily rail.
	"AFORGE_SWE_MAX_COST",
	// AFORGE_SWEPRO is not a setting anybody would ever want to turn on: it
	// tells the binary, before it has read anything else, that this process
	// is not aforge at all but the vendored swe engine (cmd/aforge/swepro.go).
	// The subharness sets it on the children it spawns; a user who set it
	// would simply lose their own program.
	"AFORGE_SWEPRO",
	// AFORGE_CHAT_V2 selects the chat surface being built beside the current
	// one, and with it the resident's room-addressing policy. It is plumbing
	// for the same reason AFORGE_SWEPRO is — it decides which program the
	// binary is before anything reads a preference — and it is temporary
	// besides: it disappears one wave after the new surface becomes the
	// default, which is exactly the lifetime a persisted setting must not have.
	"AFORGE_CHAT_V2",
	// AFORGE_CHAT_TRACE turns on the v2 chat surface's journal-versus-screen
	// trace (internal/tui2/chat/engine.go), which writes into the chat.log the
	// entry point already opens. It is plumbing rather than a setting for the
	// reason the whole list exists: it changes nothing about the product, only
	// how much the build says about itself while a fault is being chased, and a
	// preference the sheet offered to persist would be a preference for a
	// noisier log forever. The one line that reports an actual divergence is not
	// behind it — that one is always on, because a condition that eats replies
	// does not get to wait for an operator to opt in.
	"AFORGE_CHAT_TRACE",
	// AFORGE_GROWTH_GATE is the growth governor's rollback switch
	// (internal/resident/grow.go): set to 0 and the governor keeps its three
	// free checks and never asks the paid satisfaction question. It is
	// plumbing for the reason AFORGE_CHAT_V2 is — a wave's escape hatch, not a
	// preference — and it has the same lifetime: it disappears once the gate
	// has proven itself, which is exactly the lifetime a persisted setting
	// must not have.
	"AFORGE_GROWTH_GATE",
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

	// DefaultAttribution signs by default, because the signature is provenance:
	// work the user did not type should be readable as such by whoever reads
	// the history later. One row turns it off.
	DefaultAttribution = true

	// The divider clamps so neither pane can be set into uselessness. The TUI
	// reads these so the drag, the [ ] nudge, and the sheet agree.
	DefaultSplitPct = 80
	MinSplitPct     = 25
	MaxSplitPct     = 85

	// DefaultLinearMode leaves the full v2 surface running: most people want
	// the motion and the layout, so the accessible single-column rendering
	// (10.1.5) is a door someone walks through on purpose, not a default they
	// have to walk back out of.
	DefaultLinearMode = false

	// DefaultNerdFont draws the v2 chrome with Nerd Font icons, because that is
	// what the user asked the surface to look like (12.7). Default-on is only
	// defensible because turning it off costs nothing: the plain tier is not a
	// degradation but the designed floor — same segments, same order, same
	// tints, same widths, asserted by a parity gate rather than hoped for — so
	// a user whose font is not patched loses one keystroke and no layout.
	DefaultNerdFont = true
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

	read    func() string
	write   func(string) error
	receipt func() string
}

// Receipt is the dim fact that belongs beside this row's value — today's spend
// beside the day's ceiling, the price tier beside a model. It is a receipt and
// never a second value: it is derived from a live read, it is never editable,
// and a row with nothing true to add returns the empty string rather than a
// placeholder (13, and 10.2.8's rule against inventing a reading).
func (s Setting) Receipt() string {
	if s.receipt == nil {
		return ""
	}
	return strings.TrimSpace(s.receipt())
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

	// RoleModel answers what a router role is bound to right now, for the roles
	// the engine holds no client for. Nil is the honest state of this build —
	// nothing resolves verify or scribe yet — and those rows then read as the
	// role they follow instead of as a guess.
	RoleModel func(role string) (string, bool)

	// SpentTodayUSD is the day's spend, for the receipt beside the day's
	// ceiling. The bool separates "spent nothing" from "nobody counted"
	// (10.2.8); nil leaves the receipt off rather than printing $0.00.
	SpentTodayUSD func() (float64, bool)

	// ModelCost is what the provider table knows about one model's price. It is
	// a hint beside a model row and never a filter: an unpriced model is the
	// offline case, not a bad model. [ModelCostHint] is the derivation the
	// wiring lane hands in.
	ModelCost func(slug string) string

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

// build is the whole sheet, in the order it is read.
//
// Within a group the order is how often a person touches a row, not the order
// the rows were written or the order they happen to persist in: the day's
// ceiling before the ask-first threshold before the practice carve-out, the
// conversation model before the two nobody has ever changed. Between groups the
// order is [SettingCategories]. Nothing here announces either — 15 — the
// sequence is the sequence.
func (s *Settings) build() []Setting {
	dir := s.options.ProfileDir
	slots := ModelSlots()
	rows := make([]Setting, 0, len(slots)+10)

	for _, slot := range slots {
		rows = append(rows, s.modelRow(slot))
	}

	rows = append(rows,
		// The two rows that pick what reads a thing rather than what runs it.
		// They sit with the models because that is the question they answer.
		Setting{
			Key: KeyVisionModel, Category: CategoryModels, Kind: SettingText,
			Label: "looking", Env: "AFORGE_VISION_MODEL", EmptyLabel: "automatic",
			Hint: "the model that looks at images. Leave it blank and aforge picks one that can see. " +
				"A change lands the next time aforge starts.",
			read:  func() string { return VisionModelAt(dir) },
			write: func(raw string) error { return writeText(dir, KeyVisionModel, raw) },
		},
		Setting{
			Key: KeyDocumentEngine, Category: CategoryModels, Kind: SettingChoice,
			Label: "reading", Env: "AFORGE_DOC_ENGINE", Choices: DocumentEngines,
			Hint: "which rung reads your documents. auto walks local, then free, then paid OCR. " +
				"A change lands the next time aforge starts.",
			read:  func() string { return resolvedEngine(DocumentEngineAt(dir)) },
			write: func(raw string) error { return writeChoice(dir, KeyDocumentEngine, raw, DocumentEngines) },
		},

		Setting{
			Key: KeyDailyBudget, Category: CategorySpending, Kind: SettingDollars,
			Label: "daily budget", Env: "AFORGE_DAILY_BUDGET",
			Hint: "what aforge may spend on your work in a day. 0 removes the rail. " +
				"A change lands at the next rail check.",
			read:    func() string { return formatDollars(resolvedDollars(DailyBudgetUSDAt(dir))) },
			write:   func(raw string) error { return writeDollars(dir, KeyDailyBudget, raw) },
			receipt: s.spentTodayReceipt,
		},
		Setting{
			Key: KeyPlanConsent, Category: CategorySpending, Kind: SettingDollars,
			Label: "ask before spending", Env: "AFORGE_PLAN_CONSENT",
			Hint: "when a planned job is estimated to cost more than this, aforge quotes " +
				"the step count and the price and waits for your go-ahead. 0 never asks.",
			read:  func() string { return formatDollars(resolvedDollars(PlanConsentUSDAt(dir))) },
			write: func(raw string) error { return writeDollars(dir, KeyPlanConsent, raw) },
		},
		Setting{
			Key: KeyPracticeBudget, Category: CategorySpending, Kind: SettingDollars,
			Label: "practice budget", Env: "AFORGE_PRACTICE_BUDGET",
			Hint: "the slice of the day reserved for aforge practicing on itself. " +
				"A change lands the next time aforge starts.",
			read:  func() string { return formatDollars(resolvedDollars(PracticeBudgetUSDAt(dir))) },
			write: func(raw string) error { return writeDollars(dir, KeyPracticeBudget, raw) },
		},

		Setting{
			Key: KeyPracticeIdle, Category: CategoryPractice, Kind: SettingDuration,
			Label: "quiet before practice", Env: "AFORGE_PRACTICE_IDLE",
			Hint:  "how long the room stays quiet before aforge starts practicing.",
			read:  func() string { return formatDuration(resolvedDuration(PracticeIdleAt(dir))) },
			write: func(raw string) error { return writeDuration(dir, KeyPracticeIdle, raw) },
		},
		Setting{
			Key: KeyBriefAfter, Category: CategoryPractice, Kind: SettingDuration,
			Label: "arrival brief after", Env: "AFORGE_BRIEF_AFTER",
			Hint:  "how long you have to be away before aforge greets you with a summary. 0 always briefs.",
			read:  func() string { return formatDuration(resolvedDuration(BriefAfterAt(dir))) },
			write: func(raw string) error { return writeDuration(dir, KeyBriefAfter, raw) },
		},
		Setting{
			Key: KeyContextFill, Category: CategoryModels, Kind: SettingCount,
			Label: "context fill", Env: "AFORGE_CONTEXT_FILL_PCT",
			Hint: "how much of a model's context window aforge fills before it starts " +
				"compacting, as a percent. Higher packs more in; the rest stays as thinking " +
				"and answer room. A change lands on the next call.",
			read:  func() string { return strconv.Itoa(ContextFillAt(dir)) },
			write: func(raw string) error { return writeContextFill(dir, raw) },
		},
		Setting{
			Key: KeyCompletionReserve, Category: CategoryModels, Kind: SettingCount,
			Label: "answer room", Env: "AFORGE_COMPLETION_RESERVE",
			Hint: "tokens every call keeps free for its answer and its reasoning. " +
				"Generous costs nothing on turns that do not use it; small produces empty " +
				"replies from a model that thinks past it. A change lands on the next call.",
			read:  func() string { return strconv.Itoa(CompletionReserveAt(dir)) },
			write: func(raw string) error { return writeCompletionReserve(dir, raw) },
		},
		Setting{
			Key: KeyTenureAfter, Category: CategoryPractice, Kind: SettingCount,
			Label: "tenure after", Env: "AFORGE_TENURE_AFTER",
			Hint:  "how many clean firings a standing charter needs before it earns tenure.",
			read:  func() string { return strconv.Itoa(TenureAfterAt(dir)) },
			write: func(raw string) error { return writeTenure(dir, raw) },
		},

		s.splitRow(),
		Setting{
			Key: KeyNerdFont, Category: CategoryInterface, Kind: SettingBool,
			Label: "nerd font", Env: "AFORGE_NERD_FONT",
			Hint: "draw the v2 chrome with Nerd Font icons instead of the plain glyphs. " +
				"Turn it off if icons show as boxes — nothing moves, the same marks are drawn " +
				"as plain characters. Patched fonts work best in their Mono variant. " +
				"A change lands the next time aforge starts.",
			read:  func() string { return formatBool(NerdFontAt(dir)) },
			write: func(raw string) error { return writeBool(dir, KeyNerdFont, raw) },
		},
		Setting{
			Key: KeyLinearMode, Category: CategoryInterface, Kind: SettingBool,
			Label: "linear mode", Env: "AFORGE_CHAT_LINEAR",
			Hint: "single column, no motion, no spinners — the accessible rendering (10.1.5) " +
				"in the v2 chat surface. A change lands the next time aforge starts.",
			read:  func() string { return formatBool(LinearModeAt(dir)) },
			write: func(raw string) error { return writeBool(dir, KeyLinearMode, raw) },
		},
		Setting{
			Key: KeyAttribution, Category: CategoryInterface, Kind: SettingBool,
			Label: "attribution", Env: "AFORGE_ATTRIBUTION",
			Hint: "signs commits and PRs aforge writes for you — one trailer, one footer line. " +
				"A change lands on the next job.",
			read:  func() string { return formatBool(AttributionAt(dir)) },
			write: func(raw string) error { return writeBool(dir, KeyAttribution, raw) },
		},
	)
	return rows
}

// spentTodayReceipt is the day's spend beside the day's ceiling (13). Nil seam
// or an uncounted day renders nothing at all rather than $0.00, which would be
// a claim nobody made.
func (s *Settings) spentTodayReceipt() string {
	if s.options.SpentTodayUSD == nil {
		return ""
	}
	spent, counted := s.options.SpentTodayUSD()
	if !counted {
		return ""
	}
	return formatDollars(spent) + " today"
}

func (s *Settings) modelRow(slot ModelSlot) Setting {
	options := s.options
	row := Setting{
		Key: ModelSettingKey(slot.Slot), Category: CategoryModels, Kind: SettingModel,
		Label: slot.Label, Slot: slot.Slot,
		Hint: modelSlotHint(slot),
	}
	if slot.Held {
		// Only a slot the engine holds lives in the chat prefs file. A role
		// bound in the roles table and resolved nowhere has no prefs field to
		// name, and claiming one would send the surface looking for provenance
		// in a file that has never heard of it.
		row.PrefsField = modelPrefsField(slot.Slot)
	}
	if slot.Follows != "" {
		row.EmptyLabel = "follows " + slot.Follows
	}
	if name, ok := modelSlotEnvDefault(slot.Slot); ok {
		row.EnvDefault = name
	}
	row.read = func() string { return modelSlotReading(options, slot) }
	row.write = func(slug string) error {
		if options.SetModel == nil {
			return fmt.Errorf("model switching is unavailable here")
		}
		return options.SetModel(slot.Slot, strings.TrimSpace(slug))
	}
	row.receipt = func() string {
		if options.ModelCost == nil {
			return ""
		}
		return options.ModelCost(modelSlotReading(options, slot))
	}
	return row
}

// modelSlotReading asks the ONE source that can answer for this slot. A role
// the engine holds no client for is asked of the roles table if the wiring lane
// supplied one, and of nothing otherwise — never of the engine, whose slot
// lookup would hand back the conversation model for a word it does not know
// (internal/command's CurrentModel falls through), which is a wrong answer
// wearing a confident face.
func modelSlotReading(options SettingsOptions, slot ModelSlot) string {
	if slot.Held {
		if options.ModelValue == nil {
			return ""
		}
		return strings.TrimSpace(options.ModelValue(slot.Slot))
	}
	if options.RoleModel == nil {
		return ""
	}
	value, bound := options.RoleModel(string(slot.Role))
	if !bound {
		return ""
	}
	return strings.TrimSpace(value)
}

func (s *Settings) splitRow() Setting {
	options := s.options
	row := Setting{
		Key: KeySplitPct, Category: CategoryInterface, Kind: SettingPercent,
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

// modelSlotHint is what the row says about itself, in the product's words. The
// two roles nothing resolves yet say so — 5.20 rule 3: a row that cannot change
// anything today must not read as though it can.
func modelSlotHint(slot ModelSlot) string {
	switch slot.Role {
	case store.RoleOrchestrate:
		return "the model that answers you here. It changes on your next message."
	case store.RoleWork:
		return "the model that does the work. It changes on the next job."
	case store.RolePlan:
		return "the model that plans and reviews the work. Empty follows the work model."
	case store.RoleVerify:
		return "the model that checks the work — gates, judges, second opinions. " +
			"Nothing reads this binding yet; it is written down and waiting."
	case store.RoleScribe:
		return "the model that writes the short things — titles, labels, summaries. " +
			"Nothing reads this binding yet; it is written down and waiting."
	}
	switch slot.Slot {
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

// The two resolvers that used to sit here — practice_demand_pct and
// propose_new_skills — are gone with their rows. Nothing in this build read
// either one: the value was resolved, copied onto a Config field, and never
// looked at again, so the sheet was offering a person a dial wired to nothing.
// 5.20 is that an affordance which does nothing must not be offered as though
// it does, and the fix for a knob with no reader is to delete the knob, not to
// leave it turning. When the learning loop that wants them lands it brings its
// own rows, and the completeness gate will make sure of it.

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

// LinearModeAt resolves whether the v2 chat surface renders in the accessible
// single-column mode (10.1.5): one column, no motion, no spinners. A
// malformed pin reads as the default rather than refusing a launch over a
// rendering preference.
func LinearModeAt(profileDir string) bool {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_CHAT_LINEAR")); raw != "" {
		if value, err := parseBool(raw); err == nil {
			return value
		}
		return DefaultLinearMode
	}
	if value, ok := persistedBool(profileDir, KeyLinearMode); ok {
		return value
	}
	return DefaultLinearMode
}

// NerdFontAt resolves whether the v2 chat surface draws its chrome with Nerd
// Font icons (12.7). It is shaped exactly like [LinearModeAt], including the
// forgiveness: a malformed pin reads as the default rather than refusing a
// launch over a rendering preference.
//
// This is only the persisted layer of the answer. The command line outranks it,
// and two things outrank everything: linear mode forces the plain tier — a
// screen reader reads a private-use codepoint as nothing or as garbage, and a
// tier that made the accessible mode less accessible would be the affordance
// lying — and a terminal that cannot draw private use at all vetoes it. See
// cmd/aforge/chatv2_nerdfont.go, which is where those four layers meet.
func NerdFontAt(profileDir string) bool {
	value, _ := NerdFontChosenAt(profileDir)
	return value
}

// The sources [NerdFontChosenAt] can name, and the empty string it returns when
// nobody has chosen at all.
const (
	NerdFontSourceNone      = ""
	NerdFontSourceEnv       = "AFORGE_NERD_FONT"
	NerdFontSourcePersisted = KeyNerdFont
)

// NerdFontChosenAt is [NerdFontAt] that also says WHO chose, so a launcher can
// tell a decision from a default. It matters for exactly one reason: the
// terminal veto (tokens.DetectGlyphSet) sits BELOW a human's choice and above
// the built-in default, and a resolver that could not tell the two apart would
// either override a user or never veto anything.
//
// The source is empty when nobody chose — including when the pin is set to
// something unparseable, because a value nobody can read is not a choice, and
// it is not a reason to refuse a launch either: it reads as the default,
// exactly where [LinearModeAt] stops.
func NerdFontChosenAt(profileDir string) (bool, string) {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_NERD_FONT")); raw != "" {
		if value, err := parseBool(raw); err == nil {
			return value, NerdFontSourceEnv
		}
		return DefaultNerdFont, NerdFontSourceNone
	}
	if value, ok := persistedBool(profileDir, KeyNerdFont); ok {
		return value, NerdFontSourcePersisted
	}
	return DefaultNerdFont, NerdFontSourceNone
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

// ContextFillAt resolves the fill law: environment pin, then the persisted
// row, then the package default. A malformed pin reads as the default.
func ContextFillAt(profileDir string) int {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_CONTEXT_FILL_PCT")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			return value
		}
		return ctxbudget.DefaultFillPercent
	}
	if value, ok := persistedInt(profileDir, KeyContextFill); ok && value > 0 {
		return value
	}
	return ctxbudget.DefaultFillPercent
}

// CompletionReserveAt resolves the answer-and-reasoning reserve the same way.
func CompletionReserveAt(profileDir string) int {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_COMPLETION_RESERVE")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			return value
		}
		return ctxbudget.DefaultCompletionReserveTokens
	}
	if value, ok := persistedInt(profileDir, KeyCompletionReserve); ok && value > 0 {
		return value
	}
	return ctxbudget.DefaultCompletionReserveTokens
}

// writeContextFill persists the fill percent and hands it to ctxbudget, so
// the change lands in this process as well as the next one.
func writeContextFill(profileDir, raw string) error {
	value, err := parseCount(raw)
	if err != nil {
		return err
	}
	if value < 10 || value > 90 {
		return fmt.Errorf("that needs to be between 10 and 90")
	}
	if err := writeProfileValue(profileDir, KeyContextFill, value); err != nil {
		return err
	}
	ctxbudget.Configure(value, CompletionReserveAt(profileDir))
	return nil
}

// writeCompletionReserve persists the reserve and hands it to ctxbudget.
func writeCompletionReserve(profileDir, raw string) error {
	value, err := parseCount(raw)
	if err != nil {
		return err
	}
	if value < 1024 {
		return fmt.Errorf("that needs to be at least 1024 tokens")
	}
	if err := writeProfileValue(profileDir, KeyCompletionReserve, value); err != nil {
		return err
	}
	ctxbudget.Configure(ContextFillAt(profileDir), value)
	return nil
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
