package config

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/roles"
)

// THE CREW: five classes of model, answered as one word.
//
// The five tier rows are the honest shape of the decision — a class of call has
// a class of model, and a new call joins a class instead of growing a knob — and
// they are still five model ids somebody has to know. Nobody arrives at a
// settings sheet wanting to name five model ids. They arrive wanting to spend
// pennies, or wanting to spend what it takes. So there is one row above the five
// that takes that sentence and writes all of them.
//
// THREE PRESETS AND NO MORE. A fourth would be a fourth thing to explain, and
// the axis they move along has exactly three interesting points: everything
// cheap, one model thinking over cheap ones working, everything capable and
// asked to think.
//
// TWO FAMILIES BEHIND THE SAME THREE WORDS. Every preset exists twice: once in
// the all family, the same three words resolved over the whole catalog with
// closed and frontier models in it; and once in the open family, every seat an
// open-weight model, for the person who wants the three words to stay on that
// shelf whatever the catalog does around it. Which family the words draw from
// is one row, [KeyCrewSource], and all is what it reads when nobody has
// answered it. Both the derived reading and the write resolve through the row
// ([CrewSourceAt]), so the word on the sheet can never mean one family while
// the five values it summarizes were drawn from the other.
//
// THE PRESET IS DERIVED AND NEVER STORED. [CrewAt] reads the five live tier
// values and answers which preset they are, or "custom". A stored word would be
// a claim about five other rows that any one of them could falsify, and a sheet
// that said "balanced" over a hand-pinned mastermind would be lying in exactly
// the place somebody went to check. This is the one-source-of-truth law applied
// to a summary: a summary that can drift from what it summarizes is not a
// summary.
//
// THE BUILD WRITES NO WORD, AND READS ONE THAT IS THERE ([storedCrewWord]). Every
// profile this product shapes derives the preset from the rows; a run that wrote
// the word itself — a harness, a hand edit — named a budget, and a word on disk
// that nothing read is a crew word that reached nobody. The word is the budget
// when present, and the class rows under it are then that run's own pins.

// The preset words. They are the values [KeyCrew] takes, and they are strings
// on disk in the same sense every other choice row's words are — spelled here
// once, read by the row, the command and the manual.
const (
	CrewFrugal   = "frugal"
	CrewBalanced = "balanced"
	CrewMax      = "max"
	// CrewCustom is a READING and never a write. It is what the row says when
	// the five tier values are somebody's own arrangement rather than one of the
	// three, which is what happens the moment a person answers one tier row
	// directly. It is deliberately absent from [CrewPresets]: "set the crew to
	// custom" is not a sentence with a meaning — custom is what you get, not
	// what you ask for.
	CrewCustom = "custom"
)

// CrewPresets lists the words a person may WRITE, cheapest first. [CrewCustom]
// is not among them; see its own comment.
var CrewPresets = []string{CrewFrugal, CrewBalanced, CrewMax}

// DefaultCrew is what a profile nobody has touched reads. It is balanced because
// the five shipped tier defaults ARE the balanced row of the DEFAULT FAMILY —
// see [crewAllModels] — and that identity is asserted by a test rather than
// trusted.
const DefaultCrew = CrewBalanced

// The two families the preset words can draw from. They are the values
// [KeyCrewSource] takes, spelled here once and read by the row, the resolver
// and the manual.
const (
	// CrewSourceOpen is the open-weight family.
	CrewSourceOpen = "open"
	// CrewSourceAll is the whole catalog, closed and frontier models included,
	// and the family a profile that has answered nothing resolves.
	CrewSourceAll = "all"
)

// CrewSources lists them, open first, which is the order the row widens in: the
// narrower shelf, then the whole catalog. The default is the second of them,
// [DefaultCrewSource], because this list is about width and not about which one
// a profile starts on.
var CrewSources = []string{CrewSourceOpen, CrewSourceAll}

// DefaultCrewSource is all: the three words are read off the whole catalog
// unless the row says otherwise, and the five shipped tier defaults are that
// family's balanced row.
const DefaultCrewSource = CrewSourceAll

// ── where the seats are picked from ────────────────────────────────────────

// THE THIRD ROW THE CREW WORDS ARE ANSWERED THROUGH. The crew row says how
// much to spend and the family row says which shelf those budgets name; the
// pick row says where the models for that money come from when a tier row
// does not hold a model id of its own:
//
//   - `table` — the rows this build measured and shipped ([crewModels] and
//     [crewAllModels]), which is what an unwritten seat has always read;
//   - `catalog` — the same three budgets recomputed off the catalog's own
//     published prices and scores, on every read, with no measurement of
//     anybody's own runs in it ([AutoPickWith] with no prior);
//   - `learn` — the catalog computation plus the Model Pool's measurements
//     and the person's own judged runs, carried as a quality prior
//     ([autoPrior]).
//
// THE DEFAULT IS THE TABLE because the table is what a profile has always
// read: an unwritten seat names the preset's own row, and nothing about a
// profile that has answered nothing moves until somebody answers a row. The
// other two words are an opt-in to a read that keeps moving — a seat that
// follows the catalog follows it whether or not the shipped rows do — and a
// person has to say so.
//
// A PICK NEVER OVERRIDES A MODEL ID. The row answers for the seats a person
// did not name, and the seats they did — written by hand, or by a preset —
// keep their ids until the crew is picked again, except that a row holding
// the preset's own table value is the preset answering, not a person pinning
// one model by id. The rule is [pickedSeat]'s to apply and the manual's to
// state.
const (
	// CrewPickTable is the measured rows this build ships, and the default.
	CrewPickTable = "table"
	// CrewPickCatalog is the catalog's own published figures, with nothing
	// measured on top.
	CrewPickCatalog = "catalog"
	// CrewPickLearn is the catalog computation plus the Model Pool's
	// measurements and the person's own judged runs.
	CrewPickLearn = "learn"
)

// CrewPicks lists the words a person may WRITE, narrowest first: the shipped
// table, then the catalog on its own, then the catalog with what runs
// measured. A pick word is not a preset and names no budget — the crew row
// above it still does that.
var CrewPicks = []string{CrewPickTable, CrewPickCatalog, CrewPickLearn}

// DefaultCrewPick is the table: the rows this build measured are where an
// unwritten seat's model comes from until somebody answers the row.
const DefaultCrewPick = CrewPickTable

// knownCrewPick folds a word and says whether it is one of the picks this
// build knows. It is the ONE place the fold is spelled: [SetCrewPick] and the
// ladder's [pickedSeat] both go through it, so a pick added to [CrewPicks] is
// accepted everywhere at once. A reader folds a word it does not know to the
// default pick; a writer refuses it.
func knownCrewPick(pick string) (string, bool) {
	pick = strings.ToLower(strings.TrimSpace(pick))
	for _, known := range CrewPicks {
		if pick == known {
			return known, true
		}
	}
	return "", false
}

// normalCrewPick folds a pick word to one of the three this build knows,
// reading a word it does not know as the default pick.
func normalCrewPick(pick string) string {
	if known, ok := knownCrewPick(pick); ok {
		return known
	}
	return DefaultCrewPick
}

// CrewPickAt is where the seats are picked from on this profile,
// [DefaultCrewPick] when the row is absent. A word this build does not know
// reads as the default pick, silently, the way a retired choice reads
// everywhere else on this sheet.
func CrewPickAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyCrewPick); ok {
		return normalCrewPick(value)
	}
	return DefaultCrewPick
}

// AnyTierAutoAt answers whether any of the five tier rows reads `auto` on this
// profile — written directly, or reaching the word through an older row
// ([crewRow]) — which is the second way a seat resolution is computed from the
// catalog's rows, beside the pick row ([CrewPickAt]). A door that resolves
// seats asks both before it resolves, because both answers are computed from
// the rows the process already holds, and a door that asks before they land
// reads the family's table row over a profile that never chose it.
func AnyTierAutoAt(profileDir string) bool {
	for _, tier := range ModelTiers {
		if model, _, _, _ := crewRow(profileDir, tier); IsAuto(model) {
			return true
		}
	}
	return false
}

// SetCrewPick writes the pick row ALONE, in one file write. The word is
// refused the way every choice row refuses one, so a typo cannot land a pick
// nothing reads. It writes no tier row: the pick says where seats are read
// from, and the seats keep the ids on disk until the crew is picked again.
func SetCrewPick(profileDir, pick string) error {
	known, ok := knownCrewPick(pick)
	if !ok {
		return fmt.Errorf("pick one of: %s", strings.Join(CrewPicks, ", "))
	}
	return writeProfileValue(profileDir, KeyCrewPick, known)
}

// crewModels is the open-weight table: one row per preset, one model per class.
//
// THE WORKER COLUMN IS THE DIAL. It holds glm-5.3-flash through balanced, and
// max is the preset that takes it to glm-5.3, because it is the seat that pays
// most of a task's bill, and a preset that moved every other seat while leaving
// it alone would change everything about a task except its cost. The careful
// column always sees images (the vision role rides it) and is a second vendor
// from balanced upward; frugal keeps worker and careful on the same
// glm-5.3-flash, because at that bill the open-weight front has no second
// vendor to take the careful seat. The reflex and low columns never vary: they
// are the same near-free models in all three presets, and a column that never
// varies is not a dial.
//
// HOW THE IDS WERE READ OFF, on 2026-09-16 and seat by seat. Every open-weight
// row of the catalog was placed on two axes: the expected bill that seat's own
// call shape runs up, built from the catalog's published prompt, completion and
// cache-read prices, against that seat's quality, taken from its published
// intelligence, coding and agentic indexes. THE CALL SHAPE IS PART OF THE
// PRICE. The worker and the careful seats were costed as LONG CACHED LOOPS — a
// large prompt read back turn after turn, so the cache-read price carries most
// of the weight — and the mastermind as ONE-SHOT CALLS, where the prompt is
// paid in full each time and there are few of them. Each preset then takes, for
// each seat, a point on the pareto front of that plot at the bill it is willing
// to run: nothing on the front costs less at the same quality, and nothing at
// the same bill scores higher.
//
// That is why the columns do not climb together. Under one-shot pricing
// glm-5.3-flash is on the front at frugal's bill and glm-5.3 is the next point
// above it, so the mastermind column reads flash, glm-5.3, glm-5.3; under
// long-loop pricing the same plot puts kimi-k3 on the careful seat from
// balanced upward, which is also the second vendor that seat has to be.
//
// No closed model is here: the open family is the shelf that stands on the
// open-weight rows alone, and the `all` family is where a closed model goes.
var crewModels = map[string]map[string]string{
	CrewFrugal: {
		ModelTierReflex:     "mistralai/mistral-nemo",
		ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
		ModelTierWorker:     "z-ai/glm-5.3-flash",
		ModelTierHigh:       "z-ai/glm-5.3-flash",
		ModelTierMastermind: "z-ai/glm-5.3-flash",
	},
	CrewBalanced: {
		ModelTierReflex:     "mistralai/mistral-nemo",
		ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
		ModelTierWorker:     "z-ai/glm-5.3-flash",
		ModelTierHigh:       "moonshotai/kimi-k3",
		ModelTierMastermind: "z-ai/glm-5.3",
	},
	CrewMax: {
		ModelTierReflex:     "mistralai/mistral-nemo",
		ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
		ModelTierWorker:     "z-ai/glm-5.3",
		ModelTierHigh:       "moonshotai/kimi-k3",
		ModelTierMastermind: "z-ai/glm-5.3",
	},
}

// crewAllModels is the same three presets answered from the whole catalog
// rather than its open-weight shelf, which is what the `all` family of
// [KeyCrewSource] draws from AND WHAT A PROFILE THAT HAS ANSWERED NOTHING
// RESOLVES ([DefaultCrewSource]). The careful column is a DIFFERENT VENDOR
// from the worker in every preset here — the open family's frugal row is the
// one standing exception — and the reflex and low columns still never vary.
// Closed models live here and only here.
//
// The ids come off the same plot [crewModels] describes, run on 2026-09-16 over
// every row of the catalog rather than the open-weight ones: expected task bill
// against seat quality, the bill built from the published prompt, completion
// and cache-read prices under each seat's own call shape — the worker and the
// careful seats as long cached loops, the mastermind as one-shot calls — and
// the quality from the published intelligence, coding and agentic indexes.
//
// THE WORKER STAYS ON glm-5.3-flash THROUGH BALANCED, and that is the whole
// shape of this table. The worker seat carries most of a task's tokens, so a
// step there multiplies through the entire bill while a step on the careful or
// the mastermind seat is paid a handful of times. The money therefore goes to
// the two low-volume seats first: frugal to balanced moves the careful seat to
// claude-fable-5.1 and the mastermind to claude-opus-5, and max moves the
// worker itself, with the careful seat staying on fable beside it.
var crewAllModels = map[string]map[string]string{
	CrewFrugal: {
		ModelTierReflex:     "google/gemini-2.5-flash",
		ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
		ModelTierWorker:     "z-ai/glm-5.3-flash",
		ModelTierHigh:       "qwen/qwen3.8-max-0902",
		ModelTierMastermind: "z-ai/glm-5.3-flash",
	},
	CrewBalanced: {
		ModelTierReflex:     "google/gemini-2.5-flash",
		ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
		ModelTierWorker:     "z-ai/glm-5.3-flash",
		ModelTierHigh:       "anthropic/claude-fable-5.1",
		ModelTierMastermind: "anthropic/claude-opus-5",
	},
	CrewMax: {
		ModelTierReflex:     "google/gemini-2.5-flash",
		ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
		ModelTierWorker:     "z-ai/glm-5.3",
		ModelTierHigh:       "anthropic/claude-fable-5.1",
		ModelTierMastermind: "anthropic/claude-opus-5",
	},
}

// crewTableFor is the family one source word names. It is total: `open` names
// the open-weight family, and every other reading (blank, misspelt, a word a
// later build retired) names the catalog-wide one, because an unreadable answer
// resolves the same family an absent answer does ([DefaultCrewSource]).
func crewTableFor(source string) map[string]map[string]string {
	if normalCrewSource(source) == CrewSourceOpen {
		return crewModels
	}
	return crewAllModels
}

// knownCrewSource folds a word and says whether it is one of the families this
// build knows. It is the ONE place the fold is spelled: [normalCrewSource],
// [ApplyCrewUnder] and [SetCrewSource] all go through it, so a family added to
// [CrewSources] is accepted everywhere at once. A reader folds a word it does
// not know to the default family; a writer refuses it.
func knownCrewSource(source string) (string, bool) {
	source = strings.ToLower(strings.TrimSpace(source))
	for _, known := range CrewSources {
		if source == known {
			return known, true
		}
	}
	return "", false
}

// normalCrewSource folds a source word to one of the two this build knows,
// reading a word it does not know as the default family.
func normalCrewSource(source string) string {
	if known, ok := knownCrewSource(source); ok {
		return known
	}
	return DefaultCrewSource
}

// CrewSourceAt is which family the preset words draw from on this profile,
// [DefaultCrewSource] when the row is absent. A word this build does not know
// reads as the default family, silently, the way a retired choice reads
// everywhere else on this sheet.
func CrewSourceAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyCrewSource); ok {
		return normalCrewSource(value)
	}
	return DefaultCrewSource
}

// storedCrewWord is the crew word a profile STORES on the crew row, when it
// names one of this build's presets.
//
// THIS BUILD NEVER WRITES IT. The crew row is derived from the five tier rows,
// not stored ([KeyCrew] argues it), so on every profile this product shapes the
// row is absent. But a run that writes ONE word into config.json instead of the
// five rows — a harness driven arm, a hand edit — is asking for a budget, and a
// word sitting on disk that nothing reads is a crew word that reached nobody.
// The word is read here, at the one place a preset is decided ([crewPresetUnder]
// and [crewStoredAt]), so a word that is present seats the crew it names whether
// the five rows were written or not.
//
// A word this build does not know names no preset and is read as no word at all,
// the way the family and pick rows fold a retired choice. The family is still
// [CrewSourceAt]'s: the word names a budget, not a shelf.
func storedCrewWord(profileDir string) (string, bool) {
	value, held := persistedString(profileDir, KeyCrew)
	if !held {
		return "", false
	}
	word := strings.ToLower(strings.TrimSpace(value))
	for _, preset := range CrewPresets {
		if word == preset {
			return preset, true
		}
	}
	return "", false
}

// CrewModelsForSource is the five models one preset would set under one
// family, by tier word, false for a word that is not a preset. It returns a
// copy for [CrewModels]'s reason.
func CrewModelsForSource(source, preset string) (map[string]string, bool) {
	row, ok := crewTableFor(source)[strings.ToLower(strings.TrimSpace(preset))]
	if !ok {
		return nil, false
	}
	out := make(map[string]string, len(row))
	for tier, model := range row {
		out[tier] = model
	}
	return out, true
}

// crewLines is the one line each preset says about itself, IN THE FAMILY IT
// DRAWS FROM. It is what /crew prints beside each option and what the settings
// chooser shows under it, so it must name the models the preset actually picks
// in the family on screen: a line naming open models above frontier ids is the
// contradiction the chooser exists to prevent.
var crewLines = map[string]map[string]string{
	CrewSourceOpen: {
		CrewFrugal:   "glm-flash works, checks and thinks · pennies a day",
		CrewBalanced: "glm-flash works, kimi-k3 checks, glm-5.3 thinks",
		CrewMax:      "glm-5.3 works and thinks, kimi-k3 checks",
	},
	CrewSourceAll: {
		CrewFrugal:   "glm-flash works and thinks, qwen-max checks",
		CrewBalanced: "glm-flash works, fable checks, opus thinks",
		CrewMax:      "glm-5.3 works, fable checks, opus thinks",
	},
}

// CrewLine is one preset's own line in the DEFAULT family, empty for a word
// that is not a preset. The family-aware spelling is [CrewLineFor].
func CrewLine(preset string) string {
	return CrewLineFor(DefaultCrewSource, preset)
}

// CrewLineFor is one preset's own line in one family, empty for a word that is
// not a preset. A family this build does not know reads as the default one, the
// way the row does.
func CrewLineFor(source, preset string) string {
	return crewLines[normalCrewSource(source)][strings.ToLower(strings.TrimSpace(preset))]
}

// CrewModels is the five models one preset would set in the DEFAULT family, by
// tier word: the family a profile nobody has touched reads, and the spelling
// the callers hold. The family-aware spelling is
// [CrewModelsForSource]. It returns a copy, because a caller printing the
// table must not be able to edit it.
func CrewModels(preset string) (map[string]string, bool) {
	return CrewModelsForSource(DefaultCrewSource, preset)
}

// CrewAt is the crew as the five live tier values make it: the preset they are,
// or [CrewCustom].
//
// It reads through [TierModelAt], so a profile that has never been touched reads
// the shipped defaults and therefore reads [DefaultCrew] — the five defaults are
// the balanced row and nothing here needs to know that separately. A tier a
// person cleared on purpose reads empty, matches no preset, and turns the answer
// to custom, which is the truth: "one of these follows the conversation" is not
// any of the three.
//
// The comparison runs against the family [CrewSourceAt] names, so the reading
// moves with the row and never behind it: flip the family and a crew the old
// family wrote matches nothing, which reads as custom and is true, because one
// family's five ids are not any preset of the other.
func CrewAt(profileDir string) string {
	family := CrewSourceAt(profileDir)
	if CrewPickAt(profileDir) != CrewPickTable {
		// WITH THE PICK OFF THE TABLE the three dial seats are computed ids the
		// preset tables do not hold, and comparing the live seats would read
		// custom over a crew the person chose. The word answers what the STORED
		// rows make instead — the budget the seats are computed at ([pickedSeat]
		// reads the same rows) — so the word on the sheet stays the decision it
		// summarizes while the ids underneath move with the catalog.
		return crewStoredAt(profileDir, family)
	}
	live := make(map[string]string, len(ModelTiers))
	for _, tier := range ModelTiers {
		live[tier] = tierSeatUnder(profileDir, family, tier).Model
	}
	if sameCrew(live, freeCrewModels) {
		return CrewFree
	}
	table := crewTableFor(family)
	for _, preset := range CrewPresets {
		if sameCrew(live, table[preset]) {
			return preset
		}
	}
	return CrewCustom
}

// crewStoredAt is the crew the STORED five rows make, in the family given:
// the preset they are, or [CrewCustom]. It is [CrewAt]'s reading when the
// pick row takes the seats off the table, where the live comparison would
// compare computed ids.
//
// Each row is read through the ladder's own row reader ([crewRow]). A row the
// reader cannot answer because the key was NEVER HELD reads the family's
// default-preset id, which is what that tier runs until somebody writes it;
// a row CLEARED ON PURPOSE reads empty and matches nothing, because
// "follows the conversation" is not any of the three; a row that says auto is
// skipped, because auto is the one row with no opinion of its own — it runs
// at whatever budget the rows around it name ([crewPresetUnder]).
func crewStoredAt(profileDir, family string) string {
	// A STORED CREW WORD NAMES THE BUDGET OUTRIGHT. A run that wrote one word
	// instead of the five rows made its decision on the word, and the five rows
	// under it are that word's own table rows — so the word is the answer, and
	// the row comparison below is only for a profile whose budget is the rows.
	if word, ok := storedCrewWord(profileDir); ok {
		return word
	}
	defaults := crewTableFor(family)[DefaultCrew]
	stored := make(map[string]string, len(ModelTiers))
	for _, tier := range ModelTiers {
		value, _, source, cleared := crewRow(profileDir, tier)
		switch {
		case cleared:
			stored[tier] = ""
		case source == "":
			stored[tier] = strings.ToLower(strings.TrimSpace(defaults[tier]))
		case IsAuto(value):
			stored[tier] = AutoValue
		default:
			stored[tier] = strings.ToLower(strings.TrimSpace(value))
		}
	}
	table := crewTableFor(family)
	// THE DEFAULT PRESET WINS EVERY TIE, the law [crewPresetUnder] states: a
	// profile whose every row says auto is a crew with no opinion of its own,
	// and it reads balanced rather than whichever preset the loop met first.
	if crewStoredMatches(stored, table[DefaultCrew]) {
		return DefaultCrew
	}
	for _, preset := range CrewPresets {
		if preset != DefaultCrew && crewStoredMatches(stored, table[preset]) {
			return preset
		}
	}
	return CrewCustom
}

// crewStoredMatches compares a stored reading with one preset row, skipping
// the tiers whose row says auto.
func crewStoredMatches(stored, preset map[string]string) bool {
	for _, tier := range ModelTiers {
		if stored[tier] == AutoValue {
			continue
		}
		if stored[tier] != strings.ToLower(strings.TrimSpace(preset[tier])) {
			return false
		}
	}
	return true
}

// sameCrew compares two crews class by class, case-folded, because a model id is
// matched case-insensitively everywhere else on this surface.
func sameCrew(live, preset map[string]string) bool {
	for _, tier := range ModelTiers {
		if !strings.EqualFold(strings.TrimSpace(live[tier]), preset[tier]) {
			return false
		}
	}
	return true
}

// ApplyCrew writes all five tier rows from one preset, IN ONE FILE WRITE.
//
// The five keys land together or not at all. Five separate writes would leave a
// window — one process crash, one full disk — in which two classes belong to the
// old crew and two to the new, and the crew row would read "custom" about a
// state nobody chose. It is also the only shape in which a reader that happens
// to be resolving a role while somebody presses enter cannot see half a crew.
//
// The preset is resolved under the family [CrewSourceAt] names, so the row and
// the write cannot disagree about which table the word means: flip to `all`,
// press the crew again, and the five ids that land are the all-family ones.
func ApplyCrew(profileDir, preset string) error {
	// An empty family means KEEP THE ONE THE PROFILE HOLDS, so this path reads the
	// profile once inside ApplyCrewUnder rather than once here and again there, and
	// the write is exactly the five tiers. One resolve-and-write serves both callers
	// rather than two that can drift apart.
	return ApplyCrewUnder(profileDir, "", preset)
}

// ApplyCrewUnder writes a FAMILY AND A PRESET AS ONE DECISION, IN ONE FILE
// WRITE. The family row and the five tier rows are one state, so writing them
// apart leaves a window in which a reader sees the family set to `all` while the
// rows still hold open ids, which is the half-written crew [ApplyCrew] forbids, read as
// `custom` about a state nobody chose.
//
// The family row rides along ONLY WHEN IT CHANGES, so an enter that keeps the
// family does not pin a setting the person never answered: the five tier rows are
// written and the family row stays unanswered, free to follow a later default
// family.
func ApplyCrewUnder(profileDir, source, preset string) error {
	// An empty source keeps the family the profile holds, which is how [ApplyCrew]
	// asks for the five tiers alone. The profile is read ONCE here, so there is no
	// window between a check and the write in which the family could move.
	persisted := CrewSourceAt(profileDir)
	if strings.TrimSpace(source) == "" {
		source = persisted
	}
	known, ok := knownCrewSource(source)
	if !ok {
		return fmt.Errorf("pick one of: %s", strings.Join(CrewSources, ", "))
	}
	preset = strings.ToLower(strings.TrimSpace(preset))
	models, ok := CrewModelsForSource(known, preset)
	if !ok {
		return fmt.Errorf("pick one of: %s", strings.Join(CrewPresets, ", "))
	}
	values := make(map[string]any, len(models)+1)
	for tier, model := range models {
		values[tierKeyFor(tier)] = model
	}
	if known != persisted {
		values[KeyCrewSource] = known
	}
	return writeProfileValues(profileDir, values)
}

// writeCrew is the row's writer: the same refusal wording every choice row uses,
// and then the atomic write.
func writeCrew(profileDir, raw string) error {
	return ApplyCrew(profileDir, raw)
}

// SetCrewSource writes the family row ALONE, in one file write. The settings
// row is its caller; the chooser commits the family and the preset together
// through [ApplyCrewUnder]. The word is refused the way every choice row refuses
// one, so a typo cannot land a family nothing reads.
func SetCrewSource(profileDir, source string) error {
	known, ok := knownCrewSource(source)
	if !ok {
		return fmt.Errorf("pick one of: %s", strings.Join(CrewSources, ", "))
	}
	return writeProfileValue(profileDir, KeyCrewSource, known)
}

// CrewSummary is the one line a crew change confirms itself with:
//
//	crew → balanced · brain claude-opus-5 · hands glm-5.3-flash · checks claude-fable-5.1
//
// The three names are the classes a person actually asked about — what thinks,
// what works, what checks — and HANDS IS THE WORKER: the seat that does the
// task, which is what everybody reading the word took it to mean back when it
// named the small-work tier. The reflex and small-work models are deliberately
// absent: they are the same near-free models in all three presets, so naming
// them would be facts that never vary. The ids are shortened to their base names
// because the vendor prefix is the half nobody reads twice.
func CrewSummary(profileDir string) string {
	return crewSummaryWith(profileDir, "")
}

// CrewSummaryPick is the confirmation with the pick named when it is not the
// default one:
//
//	crew → balanced · learn · brain claude-opus-5 · hands glm-5.3-flash · checks claude-fable-5.1
//
// The pick rides the preset word because the two are one decision read at two
// heights — how much to spend, and where the models for that money come from
// — and a confirmation that said only `balanced` would drop the half the
// person just changed. At the default pick this is [CrewSummary] itself, so a
// profile nobody has taught the pick to confirms exactly as it always has.
func CrewSummaryPick(profileDir string) string {
	return crewSummaryWith(profileDir, CrewPickAt(profileDir))
}

// crewSummaryWith is the line both summaries are built from: the pick named
// between the preset and the three classes when one was given that is not the
// default, and never otherwise — a profile at the default pick confirms in
// the words it has always confirmed in.
func crewSummaryWith(profileDir, pick string) string {
	head := "crew → " + CrewAt(profileDir)
	if pick != "" && pick != CrewPickTable {
		head += " · " + pick
	}
	return head + " · " + CrewClasses(profileDir)
}

// CrewClasses is the three class names alone:
//
//	brain claude-opus-5 · hands glm-5.3-flash · checks claude-fable-5.1
//
// It is the tail of [CrewSummary] lifted out because a second surface prints the
// crew now — /status, where the word already has a label of its own and "crew →"
// in front of it would say the word twice. ONE SOURCE OF TRUTH: the three names,
// their order and their separator are spelled here once, so the confirmation a
// person reads after /crew and the line they read in /status cannot drift into
// naming the same four models differently.
func CrewClasses(profileDir string) string {
	ids := CrewClassModels(profileDir)
	return "brain " + ids[0] + " · hands " + ids[1] + " · checks " + ids[2]
}

// CrewClassModels is the three ids [CrewClasses] names, in that order and
// without the role words in front of them:
//
//	claude-opus-5, glm-5.3-flash, claude-fable-5.1
//
// It exists because a surface drawing the crew line has to be able to say which
// runs of it are the ANSWER — the ids a person typed /crew to change — and which
// are the labels around them (internal/tui3's payload.go). Reading them back out
// of the sentence would be a second parser for a string this file just built, so
// the sentence is built from this list instead and the two cannot disagree about
// how many models there are or which order they come in.
func CrewClassModels(profileDir string) []string {
	return []string{
		shortModel(TierModelAt(profileDir, ModelTierMastermind)),
		shortModel(TierModelAt(profileDir, ModelTierWorker)),
		shortModel(TierModelAt(profileDir, ModelTierHigh)),
	}
}

// shortModel is a model id without its vendor prefix, and the level kept. THE
// EMPTINESS LAW: a class that follows the conversation has no id to print, and
// says so in words rather than leaving a gap a reader has to interpret.
func shortModel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "the conversation"
	}
	if at := strings.LastIndex(value, "/"); at >= 0 {
		return value[at+1:]
	}
	return value
}

// ── the gate on a tier value ────────────────────────────────────────────────

// writeTierModel is the writer all five tier rows share: validate the notation,
// then persist. It is one function rather than five closures so a sixth tier
// cannot arrive with a gate somebody forgot to put on it.
func writeTierModel(profileDir, tier, raw string) error {
	raw = strings.TrimSpace(raw)
	if err := ValidateTierValue(raw); err != nil {
		return err
	}
	return writeProfileValue(profileDir, tierKeyFor(tier), raw)
}

// ValidateTierValue refuses every suffix except the three thinking levels. Tier
// rows own the `:<level>` notation, so accepting an unknown suffix here would
// silently turn a misspelling into a model id and defer a clear settings error
// until a provider call much later.
func ValidateTierValue(value string) error {
	value = strings.TrimSpace(value)
	at := strings.LastIndex(value, ":")
	if at <= 0 {
		return nil
	}
	suffix := strings.ToLower(strings.TrimSpace(value[at+1:]))
	if roles.ValidEffort(suffix) {
		return nil
	}
	return fmt.Errorf("%q is not a thinking level. Add %s to a model id, or leave the level off",
		suffix, strings.Join(quoted(roles.Efforts), ", "))
}

// quoted spells a list of words the way a refusal reads them: `low`, `medium`,
// `high` — in the list's own order, which is cheapest first.
func quoted(words []string) []string {
	out := make([]string, 0, len(words))
	for _, word := range words {
		out = append(out, "`"+word+"`")
	}
	return out
}
