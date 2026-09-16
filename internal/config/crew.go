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
// the open family, every seat an open-weight model, which is the property that
// makes a shipped default defensible, because nobody's default crew should be
// a bet on one vendor's pricing; and once in the all family, the same three
// words resolved over the whole catalog with closed and frontier models in it,
// for the person who has chosen to spend what those cost. Which family the
// words draw from is one row, [KeyCrewSource], and open is what it reads when
// nobody has answered it: the all family is a thing a person asks for, never
// one they get by saying nothing. Both the derived reading and the write
// resolve through the row ([CrewSourceAt]), so the word on the sheet can never
// mean one family while the five values it summarizes were drawn from the
// other.
//
// THE PRESET IS DERIVED AND NEVER STORED. [CrewAt] reads the five live tier
// values and answers which preset they are, or "custom". A stored word would be
// a claim about five other rows that any one of them could falsify, and a sheet
// that said "balanced" over a hand-pinned mastermind would be lying in exactly
// the place somebody went to check. This is the one-source-of-truth law applied
// to a summary: a summary that can drift from what it summarizes is not a
// summary.

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
// the five shipped tier defaults ARE the balanced row — see [crewModels] — and
// that identity is asserted by a test rather than trusted.
const DefaultCrew = CrewBalanced

// The two families the preset words can draw from. They are the values
// [KeyCrewSource] takes, spelled here once and read by the row, the resolver
// and the manual.
const (
	// CrewSourceOpen is the open-weight family and the shipped default.
	CrewSourceOpen = "open"
	// CrewSourceAll is the whole catalog, closed and frontier models included.
	CrewSourceAll = "all"
)

// CrewSources lists them, open first, which is also the default and the order
// the row widens in.
var CrewSources = []string{CrewSourceOpen, CrewSourceAll}

// DefaultCrewSource is open, for the same reason the shipped crew is open
// weights: a family nobody chose is the one this build can defend.
const DefaultCrewSource = CrewSourceOpen

// crewModels is the whole table: one row per preset, one model per class.
//
// THE WORKER COLUMN IS THE DIAL. It climbs the open-weight pareto front one step
// per preset — deepseek-v4-flash, glm-5.3-flash, glm-5.3 — because it is the
// seat that pays most of a task's bill, and a preset that moved every other seat
// while leaving it alone would change everything about a task except its cost.
// The mastermind column buys a bigger planning model without choosing its
// generation behavior. The careful column is always a DIFFERENT VENDOR from
// the worker and always sees images (the vision role rides it). The reflex and
// low columns never vary: they are the same
// near-free models in all three presets, and a column that never varies is not
// a dial.
//
// Every id is an open-weight model, picked off the catalog's own published
// scores against blended price on 2026-09-01 (settings.go's DefaultWorkerModel
// says how); the closed models that are cheaper on their own vendor's platform
// than through the router are deliberately not here.
var crewModels = map[string]map[string]string{
	CrewFrugal: {
		ModelTierReflex:     "mistralai/mistral-nemo",
		ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
		ModelTierWorker:     "deepseek/deepseek-v4-flash-0731",
		ModelTierHigh:       "z-ai/glm-5.3-flash",
		ModelTierMastermind: "z-ai/glm-5.3",
	},
	CrewBalanced: {
		ModelTierReflex:     "mistralai/mistral-nemo",
		ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
		ModelTierWorker:     "z-ai/glm-5.3-flash",
		ModelTierHigh:       "moonshotai/kimi-k3",
		ModelTierMastermind: "moonshotai/kimi-k3",
	},
	CrewMax: {
		ModelTierReflex:     "mistralai/mistral-nemo",
		ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
		ModelTierWorker:     "z-ai/glm-5.3",
		ModelTierHigh:       "moonshotai/kimi-k3",
		ModelTierMastermind: "moonshotai/kimi-k3",
	},
}

// crewAllModels is the same three presets answered from the whole catalog
// rather than its open-weight shelf, which is what the `all` family of
// [KeyCrewSource] draws from. The careful column is the same law here as
// there: a DIFFERENT VENDOR from the worker in every preset, and the reflex
// and low columns still never vary. The ids are locked the way the open ones
// were, off the catalog's own published scores against blended price, and
// closed models live here and only here: the open table is the shipped
// default and stays byte-for-byte what it was.
//
// TWO COLUMNS SPEND DIFFERENTLY HERE. The worker column is [crewModels]'s dial
// but moves once, not per preset: gpt-5.6-sol holds frugal and balanced, and
// only max buys claude-fable-5.1, because on the catalog the coding ceiling is
// the expensive step and the two steps below it are the same model. What
// frugal-to-balanced buys here is the careful seat (gemini-3.8-flash to
// claude-opus-5) and a smarter mastermind, not a dearer worker. And max's
// mastermind sits on the worker's own model on purpose: fable-5.1 is the
// catalog's ceiling, so there is no better planner to buy above it and the top
// seat adds generation behavior, not a bigger model.
var crewAllModels = map[string]map[string]string{
	CrewFrugal: {
		ModelTierReflex:     "google/gemini-2.5-flash",
		ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
		ModelTierWorker:     "openai/gpt-5.6-sol",
		ModelTierHigh:       "google/gemini-3.8-flash",
		ModelTierMastermind: "anthropic/claude-opus-5",
	},
	CrewBalanced: {
		ModelTierReflex:     "google/gemini-2.5-flash",
		ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
		ModelTierWorker:     "openai/gpt-5.6-sol",
		ModelTierHigh:       "anthropic/claude-opus-5",
		ModelTierMastermind: "anthropic/claude-fable-5.1",
	},
	CrewMax: {
		ModelTierReflex:     "google/gemini-2.5-flash",
		ModelTierLow:        "deepseek/deepseek-v4-flash-0731",
		ModelTierWorker:     "anthropic/claude-fable-5.1",
		ModelTierHigh:       "openai/gpt-6-astra",
		ModelTierMastermind: "anthropic/claude-fable-5.1",
	},
}

// crewTableFor is the family one source word names. It is total: `all` names
// the catalog-wide family, and every other reading (blank, misspelt, a word a
// later build retired) names the open one, because a family nobody asked for
// is the one this build can defend.
func crewTableFor(source string) map[string]map[string]string {
	if strings.EqualFold(strings.TrimSpace(source), CrewSourceAll) {
		return crewAllModels
	}
	return crewModels
}

// CrewSourceAt is which family the preset words draw from on this profile,
// [DefaultCrewSource] when the row is absent. A word this build does not know
// reads as open, silently, the way a retired choice reads everywhere else on
// this sheet.
func CrewSourceAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyCrewSource); ok {
		if strings.EqualFold(strings.TrimSpace(value), CrewSourceAll) {
			return CrewSourceAll
		}
	}
	return DefaultCrewSource
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

// crewLines is the one line each preset says about itself. It is what /crew
// prints beside each option and what the settings chooser shows under it — the
// same words in both places, because they are one sentence about one thing.
var crewLines = map[string]string{
	CrewFrugal:   "deepseek works, glm-5.3 thinks · pennies a day",
	CrewBalanced: "glm-flash works, kimi-k3 checks and thinks",
	CrewMax:      "glm-5.3 works, kimi-k3 thinks and checks",
}

// CrewLine is one preset's own line, empty for a word that is not a preset.
func CrewLine(preset string) string {
	return crewLines[strings.ToLower(strings.TrimSpace(preset))]
}

// CrewModels is the five models one preset would set in the OPEN family, by
// tier word: the family a profile nobody has touched reads, and the spelling
// every existing caller and page already holds. The family-aware spelling is
// [CrewModelsForSource]. It returns a copy, because a caller printing the
// table must not be able to edit it.
func CrewModels(preset string) (map[string]string, bool) {
	return CrewModelsForSource(CrewSourceOpen, preset)
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
// family wrote matches nothing, which reads as custom and is true, because
// five open ids are not any all-family preset either.
func CrewAt(profileDir string) string {
	live := make(map[string]string, len(ModelTiers))
	for _, tier := range ModelTiers {
		live[tier] = TierModelAt(profileDir, tier)
	}
	table := crewTableFor(CrewSourceAt(profileDir))
	for _, preset := range CrewPresets {
		if sameCrew(live, table[preset]) {
			return preset
		}
	}
	return CrewCustom
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
	preset = strings.ToLower(strings.TrimSpace(preset))
	models, ok := CrewModelsForSource(CrewSourceAt(profileDir), preset)
	if !ok {
		return fmt.Errorf("pick one of: %s", strings.Join(CrewPresets, ", "))
	}
	values := make(map[string]any, len(models))
	for tier, model := range models {
		values[tierKeyFor(tier)] = model
	}
	return writeProfileValues(profileDir, values)
}

// writeCrew is the row's writer: the same refusal wording every choice row uses,
// and then the atomic write.
func writeCrew(profileDir, raw string) error {
	return ApplyCrew(profileDir, raw)
}

// SetCrewSource writes the family row alone, in one file write, for the chooser
// that promises the family and the preset as ONE decision: it lands this row
// first and then applies the preset, so the resolution that turns the two into
// five ids reads the family this same enter chose and not yesterday's. The word
// is refused the way every choice row refuses one, so a typo cannot land a
// family nothing reads.
func SetCrewSource(profileDir, source string) error {
	source = strings.ToLower(strings.TrimSpace(source))
	if source != CrewSourceOpen && source != CrewSourceAll {
		return fmt.Errorf("pick one of: %s", strings.Join(CrewSources, ", "))
	}
	return writeProfileValue(profileDir, KeyCrewSource, source)
}

// CrewSummary is the one line a crew change confirms itself with:
//
//	crew → balanced · brain glm-5.3 · hands glm-5.3-flash · checks qwen3.8-27b
//
// The three names are the classes a person actually asked about — what thinks,
// what works, what checks — and HANDS IS THE WORKER: the seat that does the
// task, which is what everybody reading the word took it to mean back when it
// named the small-work tier. The reflex and small-work models are deliberately
// absent: they are the same near-free models in all three presets, so naming
// them would be facts that never vary. The ids are shortened to their base names
// because the vendor prefix is the half nobody reads twice.
func CrewSummary(profileDir string) string {
	return "crew → " + CrewAt(profileDir) + " · " + CrewClasses(profileDir)
}

// CrewClasses is the three class names alone:
//
//	brain glm-5.3 · hands glm-5.3-flash · checks qwen3.8-27b
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
//	glm-5.3, glm-5.3-flash, qwen3.8-27b
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
