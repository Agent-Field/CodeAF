package tui3

import (
	"strings"

	"charm.land/bubbletea/v2"
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
// The bare form is a three-row chooser. Each row keeps the comparison the old
// listing supplied: its sentence first and the four class models underneath.
//
// EVERY WRITE GOES THROUGH [config.ApplyCrew], the same function the panel's row
// writes through. A second writer here is how a command and a panel end up
// disagreeing about which crew is on.

// runCrew is /crew: the three presets with the current one marked, or one applied.
func (a *app) runCrew(arg string) {
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "" {
		a.closeLists()
		a.crewPick.start(config.CrewAt(a.profileDir))
		a.touch()
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
	a.applyCrew(arg)
}

// applyCrew is the ONE path from either /crew form to the profile write, the
// settings refresh and the person-facing summary.
func (a *app) applyCrew(preset string) {
	if err := config.ApplyCrew(a.profileDir, preset); err != nil {
		a.note("could not set the crew · " + err.Error())
		return
	}
	// The panel may be holding rows read before this write, so it is rebuilt if
	// it is open. Everything else is live: the crew source the session resolves
	// through re-reads on its next call (cmd/aforge's v3RolesSource).
	a.refreshSettings()
	// AND THE CONFIRMATION SAYS WHAT IT DID NOT CHANGE. This line is the whole of
	// the evidence a crew change leaves on the frame — the status line's model
	// readout is the CONVERSATION's model and the crew never touches it — so the
	// person who typed /crew to make aforge think harder reads four model names,
	// looks down at a bottom row that says exactly what it said before, and
	// concludes the command did nothing. The clause is on the note rather than in
	// [config.CrewSummary] because it is an answer to a question this MOMENT
	// raises: the same summary inside /crew's listing is being read by somebody
	// who is comparing three rows, not by somebody who just changed one.
	a.note(config.CrewSummary(a.profileDir) + " · the model you talk to is /model")
}

// crewWord is the crew as a page states it: the preset word or `custom`, then
// the three class names ([config.CrewClasses]). It is what /status prints and
// what [app.crewHint] shortens.
//
// THE EMPTINESS LAW: the crew is four rows of a PROFILE, so a door that opened
// without one has no crew to read and this is the empty string — every caller
// prints nothing rather than a word about a file nobody is writing.
func (a *app) crewWord() string {
	if strings.TrimSpace(a.profileDir) == "" {
		return ""
	}
	return config.CrewAt(a.profileDir) + " · " + config.CrewClasses(a.profileDir)
}

// crewHint is the crew in the fewest cells that still answer it — `crew max` —
// for the hint slot under the model picker (render.go's [app.hintWord]).
//
// It is the WORD and not the three class names, and it names no door. The slot
// is three cells wide in the sense that matters: every cell it takes is a cell
// the conversation's own name gives up at the other end of the legend
// (render.go's [app.legend]). And the slot's law is that it names only what the
// keyboard will do RIGHT NOW — while the picker is open every printable key
// goes into its filter box, so "/crew" printed there would be a door a person
// cannot walk through until they press esc. The word alone is a FACT, which is
// all this slot has to carry: somebody hunting the model they just changed the
// crew for reads that the crew is a different thing with a name of its own.
func (a *app) crewHint() string {
	if strings.TrimSpace(a.profileDir) == "" {
		return ""
	}
	return "crew " + config.CrewAt(a.profileDir)
}

// crewPicker is the fixed, bottom-anchored chooser opened by bare /crew. Its
// zero value is closed, like [picker], and its cursor is an index into
// [config.CrewPresets].
type crewPicker struct {
	open    bool
	cursor  int
	current string
}

func (p *crewPicker) start(current string) {
	*p = crewPicker{open: true, current: current}
	for i, preset := range config.CrewPresets {
		if preset == current {
			p.cursor = i
			return
		}
	}
}

func (p *crewPicker) close() { *p = crewPicker{} }

func (p *crewPicker) move(delta int) {
	p.cursor = (p.cursor + delta + len(config.CrewPresets)) % len(config.CrewPresets)
}

func (p *crewPicker) height() int {
	if !p.open {
		return 0
	}
	height := len(config.CrewPresets) * 2
	if p.current == config.CrewCustom {
		height++
	}
	return height
}

// rows uses the model picker's bottom-overlay row vocabulary, but always gives
// the models their own dim line: with three fixed choices, comparison matters
// more than fitting a fourth choice that does not exist.
func (p *crewPicker) rows(width, n int, pal palette, hover int, a *app) []string {
	if !p.open || n <= 0 {
		return nil
	}
	out := make([]string, 0, p.height())
	for i, preset := range config.CrewPresets {
		models, _ := config.CrewModels(preset)
		parts := make([]string, 0, len(roles.Tiers))
		for _, tier := range roles.Tiers {
			parts = append(parts, strings.TrimSpace(a.crewClassWord(tier))+" "+models[string(tier)])
		}
		// THE CREW IN FORCE AND THE CURSOR ARE TWO FACTS, AND A ROW CAN BE BOTH.
		// The crew a person is actually running is chosen and persistent, so it
		// takes THE GROUND LADDER's selected step; the cursor is where ↑/↓ has got
		// to, so it takes the cursor step, the same step the pointer takes.
		//
		// This list used to say both with the LEAD and lose one of them: the
		// current preset borrowed the pointer's own `·`, and because it was tested
		// first, the cursor's `›` disappeared the moment the cursor landed on the
		// preset already in force — which is the one row a person is most likely
		// to arrow onto, and the one moment they most need to know enter is aimed.
		oncursor, current := i == p.cursor, preset == p.current
		hovered := hover == len(out) || hover == len(out)+1
		lead := "  "
		switch {
		case oncursor:
			lead = pal.accent("› ")
		case hovered:
			lead = pal.accent("· ")
		}
		label := preset + " — " + config.CrewLine(preset)
		switch {
		case current:
			label = pal.accent(label)
		case oncursor:
			label = pal.ink(label)
		default:
			label = pal.dim(label)
		}
		head := lead + fit(label, width-2)
		// The models are the half of the row a person stopped on it to compare, so
		// they come up to ink wherever the row wears a ground: dim on a raised
		// ground is grey on grey.
		tail := "    " + fit(strings.Join(parts, " · "), width-4)
		if oncursor || hovered || current {
			tail = pal.ink(tail)
		} else {
			tail = pal.dim(tail)
		}
		switch {
		case current:
			head, tail = pal.selected(head, width), pal.selected(tail, width)
		case oncursor, hovered:
			head, tail = pal.cursor(head, width), pal.cursor(tail, width)
		}
		out = append(out, head, tail)
	}
	if p.current == config.CrewCustom {
		out = append(out, pal.dim("yours is none of the three — picking one puts all four back"))
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func (a *app) crewPickerKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "esc":
		a.crewPick.close()
	case "enter":
		preset := config.CrewPresets[a.crewPick.cursor]
		a.crewPick.close()
		a.applyCrew(preset)
	case "up", "ctrl+p":
		a.crewPick.move(-1)
	case "down", "ctrl+n":
		a.crewPick.move(1)
	}
	a.touch()
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
