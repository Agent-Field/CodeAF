package config

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// THE CREW: four classes of model, answered as one word.
//
// The four tier rows are the honest shape of the decision — a class of call has
// a class of model, and a new call joins a class instead of growing a knob — and
// they are still four model ids somebody has to know. Nobody arrives at a
// settings sheet wanting to name four model ids. They arrive wanting to spend
// pennies, or wanting to spend what it takes. So there is one row above the four
// that takes that sentence and writes all of them.
//
// THREE PRESETS AND NO MORE. A fourth would be a fourth thing to explain, and
// the axis they move along has exactly three interesting points: everything
// cheap, one model thinking over cheap ones working, everything capable and
// asked to think. Every model in every preset is open-source, which is the
// property that makes a shipped default defensible — nobody's default crew
// should be a bet on one vendor's pricing.
//
// THE PRESET IS DERIVED AND NEVER STORED. [CrewAt] reads the four live tier
// values and answers which preset they are, or "custom". A stored word would be
// a claim about four other rows that any one of them could falsify, and a sheet
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
	// the four tier values are somebody's own arrangement rather than one of the
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
// the four shipped tier defaults ARE the balanced row — see [crewModels] — and
// that identity is asserted by a test rather than trusted.
const DefaultCrew = CrewBalanced

// crewModels is the whole table: one row per preset, one model per class.
//
// The mastermind column is the only one carrying a level, and it is the column
// the presets actually differ in — frugal never reaches for the thinking model
// at all, balanced asks it to think a little, max asks it to think hard. The
// other three columns move by one step each, which is what makes the three
// presets read as one dial rather than three unrelated opinions.
var crewModels = map[string]map[string]string{
	CrewFrugal: {
		ModelTierReflex:     "mistralai/mistral-nemo",
		ModelTierLow:        "deepseek/deepseek-v4-flash",
		ModelTierHigh:       "qwen/qwen3.8-27b",
		ModelTierMastermind: "qwen/qwen3.8-27b",
	},
	CrewBalanced: {
		ModelTierReflex:     "mistralai/mistral-nemo",
		ModelTierLow:        "deepseek/deepseek-v4-flash",
		ModelTierHigh:       "qwen/qwen3.8-27b",
		ModelTierMastermind: "moonshotai/kimi-k3:low",
	},
	CrewMax: {
		ModelTierReflex:     "mistralai/mistral-nemo",
		ModelTierLow:        "deepseek/deepseek-v4-pro",
		ModelTierHigh:       "moonshotai/kimi-k3",
		ModelTierMastermind: "moonshotai/kimi-k3:high",
	},
}

// crewLines is the one line each preset says about itself. It is what /crew
// prints beside each option and what the settings chooser shows under it — the
// same words in both places, because they are one sentence about one thing.
var crewLines = map[string]string{
	CrewFrugal:   "qwen handles careful work · pennies a day",
	CrewBalanced: "kimi-k3 thinks, qwen checks",
	CrewMax:      "kimi-k3 everywhere, thinks longer",
}

// CrewLine is one preset's own line, empty for a word that is not a preset.
func CrewLine(preset string) string {
	return crewLines[strings.ToLower(strings.TrimSpace(preset))]
}

// CrewModels is the four models one preset would set, by tier word. It returns
// a copy, because a caller printing the table must not be able to edit it.
func CrewModels(preset string) (map[string]string, bool) {
	row, ok := crewModels[strings.ToLower(strings.TrimSpace(preset))]
	if !ok {
		return nil, false
	}
	out := make(map[string]string, len(row))
	for tier, model := range row {
		out[tier] = model
	}
	return out, true
}

// CrewAt is the crew as the four live tier values make it: the preset they are,
// or [CrewCustom].
//
// It reads through [TierModelAt], so a profile that has never been touched reads
// the shipped defaults and therefore reads [DefaultCrew] — the four defaults are
// the balanced row and nothing here needs to know that separately. A tier a
// person cleared on purpose reads empty, matches no preset, and turns the answer
// to custom, which is the truth: "one of these follows the conversation" is not
// any of the three.
func CrewAt(profileDir string) string {
	live := make(map[string]string, len(ModelTiers))
	for _, tier := range ModelTiers {
		live[tier] = TierModelAt(profileDir, tier)
	}
	for _, preset := range CrewPresets {
		if sameCrew(live, crewModels[preset]) {
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

// ApplyCrew writes all four tier rows from one preset, IN ONE FILE WRITE.
//
// The four keys land together or not at all. Four separate writes would leave a
// window — one process crash, one full disk — in which two classes belong to the
// old crew and two to the new, and the crew row would read "custom" about a
// state nobody chose. It is also the only shape in which a reader that happens
// to be resolving a role while somebody presses enter cannot see half a crew.
func ApplyCrew(profileDir, preset string) error {
	preset = strings.ToLower(strings.TrimSpace(preset))
	models, ok := crewModels[preset]
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

// CrewSummary is the one line a crew change confirms itself with:
//
//	crew → balanced · brain kimi-k3:low · hands deepseek-v4-flash · checks qwen3.8-27b
//
// The three names are the classes a person actually asked about — what thinks,
// what works, what checks — and the reflex model is deliberately absent: it is
// the same near-free model in all three presets, so naming it would be a fourth
// fact that never varies. The ids are shortened to their base names because the
// vendor prefix is the half nobody reads twice.
func CrewSummary(profileDir string) string {
	return "crew → " + CrewAt(profileDir) + " · " + CrewClasses(profileDir)
}

// CrewClasses is the three class names alone:
//
//	brain kimi-k3:low · hands deepseek-v4-flash · checks qwen3.8-27b
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
//	kimi-k3:low, deepseek-v4-flash, qwen3.8-27b
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
		shortModel(TierModelAt(profileDir, ModelTierLow)),
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

// writeTierModel is the writer all four tier rows share: validate the notation,
// then persist. It is one function rather than four closures so a fifth tier
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
