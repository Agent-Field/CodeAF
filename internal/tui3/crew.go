package tui3

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// /crew — THE FOUR MODELS AFORGE WORKS WITH, ANSWERED IN ONE WORD.
//
// The settings panel has the same four rows and a crew row above them, and this
// command exists anyway for the reason /model exists beside the model slot: the
// panel is where you go to READ a decision, and a command is where you go to
// CHANGE one you have already made up your mind about. Somebody whose planner is
// not thinking hard enough types `/crew max` and gets one line back; the same
// person in the panel opens a sheet, walks a tab bar, finds a row and presses a
// key three times.
//
// The bare form is a LISTING AND NOT A PICKER. There are three answers, each is
// four model ids, and the thing a person actually wants to see before choosing is
// what the four would become — which is a table, not a list of rows to walk.
//
// EVERY WRITE GOES THROUGH [config.ApplyCrew], the same function the panel's row
// writes through. A second writer here is how a command and a panel end up
// disagreeing about which crew is on.

// runCrew is /crew: the three presets with the current one marked, or one applied.
func (a *app) runCrew(arg string) {
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "" {
		a.note(a.crewListing())
		return
	}
	if _, ok := config.CrewModels(arg); !ok {
		// AN UNKNOWN WORD CHANGES NOTHING AND SHOWS THE THREE. A refusal that
		// only said "no" would leave a person guessing at a word they were one
		// letter away from, and the listing is the answer to the question they
		// were really asking.
		a.note("/crew " + arg + " · not one of the three\n\n" + a.crewListing())
		return
	}
	if err := config.ApplyCrew(a.profileDir, arg); err != nil {
		a.note("could not set the crew · " + err.Error())
		return
	}
	// The panel may be holding rows read before this write, so it is rebuilt if
	// it is open. Everything else is live: the crew source the session resolves
	// through re-reads on its next call (cmd/aforge's v3RolesSource).
	a.refreshSettings()
	a.note(config.CrewSummary(a.profileDir))
}

// crewListing is the three presets, the current one marked, each with its own
// line and the four models it would set.
//
// The four are named by CLASS and not by tier word, because the classes are what
// the settings rows are called and a person reading this is being invited to open
// them. The marked row is marked with the same chip the model picker marks the
// model in use with, so "this is the one you are on" is one gesture across the
// surface.
func (a *app) crewListing() string {
	current := config.CrewAt(a.profileDir)
	var out strings.Builder
	for at, preset := range config.CrewPresets {
		if at > 0 {
			out.WriteString("\n")
		}
		lead := "  "
		if preset == current {
			lead = "· "
		}
		out.WriteString(lead + preset + " — " + config.CrewLine(preset) + "\n")
		models, _ := config.CrewModels(preset)
		for _, tier := range roles.Tiers {
			out.WriteString("    " + a.crewClassWord(tier) + "  " + models[string(tier)] + "\n")
		}
	}
	if current == config.CrewCustom {
		// THE FOURTH READING IS NOT A CHOICE and it is said last, as a fact about
		// where they are rather than as a fourth row they could pick. Naming the
		// four live models here would repeat the settings panel; naming the row
		// that made it custom is what they need to undo it.
		out.WriteString("\nyours is none of the three — " + config.CrewSummary(a.profileDir) +
			"\n/crew balanced puts all four back")
	}
	return strings.TrimRight(out.String(), "\n")
}

// crewClassWord is a class in the words its own settings row is labelled with,
// padded so the four ids line up. It reads the skin rather than spelling the
// words again, for [sheet.tierWord]'s reason: a person told "mastermind" here has
// to find a row called "mastermind" there.
func (a *app) crewClassWord(tier roles.Tier) string {
	word := string(tier)
	if meta, ok := settingUI[tierSettingKey(tier)]; ok && meta.label != "" {
		word = meta.label
	}
	for len(word) < crewClassWidth {
		word += " "
	}
	return word
}

// crewClassWidth is the column the class words are padded to — "careful work" is
// the longest of the four and this is its width. It is a constant rather than a
// measurement because the four words are fixed and a loop to find the longest of
// four literals is machinery for nothing.
const crewClassWidth = 12

// refreshSettings rebuilds an open settings panel from the registry. A command
// that wrote a row while the panel was open would otherwise leave the panel
// showing what it read when it opened.
func (a *app) refreshSettings() {
	if !a.sheet.open || a.sheet.registry == nil {
		return
	}
	a.sheet.rows = a.sheet.registry.Rows()
	a.sheet.build()
}
