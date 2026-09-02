package tui3

import (
	"strings"

	"charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// /crew — THE FIVE MODELS AFORGE WORKS WITH, ANSWERED IN ONE WORD.
//
// The settings panel has the same five rows and a crew row above them, and this
// command exists anyway for the reason /model exists beside the model slot: the
// panel is where you go to READ a decision, and a command is where you go to
// CHANGE one you have already made up your mind about. Somebody whose planner is
// not thinking hard enough types `/crew max` and gets one line back; the same
// person in the panel opens a sheet, walks a tab bar, finds a row and presses a
// key three times.
//
// The bare form is the FIVE-SEAT READING: the model the person talks to on a
// line of its own, then the three presets as a chooser. Each preset row keeps
// the comparison the old listing supplied — its sentence first and the four
// class models underneath — and the seat above them is the one thing on the
// list no key can move ([crewPicker.rows] says why it is there anyway).
//
// EVERY WRITE GOES THROUGH [config.ApplyCrew], the same function the panel's row
// writes through. A second writer here is how a command and a panel end up
// disagreeing about which crew is on.

// runCrew is /crew: the three presets with the current one marked, or one applied.
func (a *app) runCrew(arg string) {
	if a.hosted() {
		a.note(a.remoteProfileWord("the crew"))
		return
	}
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "" {
		a.closeLists()
		a.crewPick.start(config.CrewAt(a.profileDir), a.crewInheritedLine())
		a.touch()
		return
	}
	if _, ok := config.CrewModels(arg); !ok {
		// AN UNKNOWN WORD CHANGES NOTHING AND SHOWS THE THREE. A refusal that
		// only said "no" would leave a person guessing at a word they were one
		// letter away from, and the listing is the answer to the question they
		// were really asking.
		// The listing is a two-column page — a class word, then the model it would
		// set — so its right-hand column is what THE PAYLOAD RULE lifts, read back
		// off the text this call just built (payload.go's [columnFacts]).
		listing := a.crewListing()
		a.noteFacts("/crew "+arg+" · not one of the three\n\n"+listing,
			columnFacts(listing, false)...)
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
	// AND THE CONFIRMATION NAMES WHAT IT DID NOT CHANGE, BY ITS ID. The status
	// line's model readout is the CONVERSATION's model and the crew never touches
	// it — so the person who typed /crew to make aforge think harder reads three
	// model names, looks down at a bottom row that says exactly what it said
	// before, and concludes the command did nothing. The clause used to say only
	// that "the model you talk to is /model", and a person who had not yet
	// learned the two dials read that as a hint about a command rather than as a
	// fact about their session; naming the model they are still on, in the same
	// spelling the status line draws it, makes the unchanged thing something they
	// can see is unchanged. The clause is on the note rather than in
	// [config.CrewSummary] because it is an answer to a question this MOMENT
	// raises: the same summary inside /status is being read by somebody who is
	// reading a page, not by somebody who just changed one dial of two.
	//
	// AND THE FOUR MODELS ARE THE PART THIS LINE IS FOR (payload.go). The prose
	// around them — `crew →`, the preset word the person has just typed, the three
	// role words, `you are still talking to` — stays in the dim tier every note
	// wears, and the ids step up, because "which models am I on now" is the whole
	// question and it used to be answered at exactly the weight of the sentence
	// carrying it. /model is a door, so it wears the chip a door wears.
	facts := config.CrewClassModels(a.profileDir)
	if id := a.talkingTo(); id != "" {
		facts = append(facts, id)
	}
	a.noteFacts(config.CrewSummary(a.profileDir)+" · "+a.crewUnchangedClause(), facts...)
}

// crewUnchangedClause is the tail of every crew confirmation: the model the
// conversation is still on, and the one command that moves it.
//
//	you are still talking to deepseek-v4-flash — /model changes that
//
// THE EMPTINESS LAW has a sentence-shaped edge here. A session that has not been
// told its model yet has nothing to name, and "you are still talking to" with a
// gap after it would be a line that looks cut; the clause says the fact without
// the id instead, which is still true and still points at the right door.
func (a *app) crewUnchangedClause() string {
	if id := a.talkingTo(); id != "" {
		return "you are still talking to " + id + " — /model changes that"
	}
	return "the model you talk to is untouched — /model changes that"
}

// talkingTo is the conversation's model spelled the way the status line's model
// segment spells it: the basename, with the reasoning level riding on it when
// one is set (render.go's [app.identityParts], view.go's [app.statusRow]).
//
// It reads a.model and NOT whatever the status row is naming at the moment,
// because inside a task room the row names the node's model, and the crew
// confirmation is a fact about the conversation whichever page is open. A person
// checking the clause against the foot of the frame from outside a room finds
// the same spelling; from inside one they find the node's, which the room's own
// lead word already says is a different thing.
func (a *app) talkingTo() string {
	id := modelBase(a.model)
	if id == "" {
		return ""
	}
	if level := a.reasoningFor(a.model); level != "" {
		id += ":" + level
	}
	return id
}

// crewWord is the crew as a page states it: the preset word or `custom`, then
// the three class names ([config.CrewClasses]). It is what /status prints and
// what [app.crewHint] shortens.
//
// THE EMPTINESS LAW: a window with no crew to read prints nothing rather than a
// word about a file nobody is writing — and the window with no crew to read is
// the HOSTED one ([app.crewReading]), never the ordinary launch this used to
// silence.
func (a *app) crewWord() string {
	crew, ok := a.crewReading()
	if !ok {
		return ""
	}
	return crew.word
}

// crewSegment is the crew in the fewest cells that still answer it — `crew max`,
// or `crew custom` over four rows the person arranged themselves. It is the
// status line's crew segment (render.go's [app.telemetry]) and the word the
// model picker's hint slot borrows ([app.crewHint]).
//
// ONE SOURCE FOR THE WORD. The status line, the picker's hint, /status's crew
// line and the settings row all answer "which crew" through [config.CrewAt],
// which derives the preset from the four live class rows rather than reading a
// stored word — so none of them can say `max` over a mastermind somebody pinned
// out of it, and none of them can disagree with the others.
//
// THE EMPTINESS LAW: a window with no four rows to read has no segment, and a
// segment with no text is a segment the row never draws ([app.telemetry] skips
// it). That window is the hosted one — see [app.crewReading] for why it is not
// the empty profile directory this guard used to ask about.
func (a *app) crewSegment() string {
	crew, ok := a.crewReading()
	if !ok {
		return ""
	}
	return crew.segment
}

// ── the one reading of the four rows ────────────────────────────────────────

// crewReading is the profile's crew as this surface last read it: the preset
// word [config.CrewAt] derives from the live rows, the three class names beside
// it, and the settings generation the pair was read at.
type crewReading struct {
	// word and segment are the two readings BUILT AT THE READING and not at the
	// draw. The status line asks for the segment on every frame, and a surface
	// that joined `"crew " + preset` there would put an allocation on the frame
	// clock for a sentence that cannot change between two settings writes
	// (inputsmooth_test.go's allocation law).
	word    string
	segment string
	preset  string
	classes string
	// dir is the profile the pair was read from, so a surface handed a
	// different one answers from that one and not from a snapshot of the last.
	dir string
	// generation is [config.SettingsGeneration] as of the reading, and taken
	// separates "read at generation zero" from "never read".
	generation uint64
	taken      bool
}

// crewReading is THE ONE READING every crew surface answers from — the status
// line's segment, the model picker's hint, /status's crew line and the welcome
// box's clause — and it reports false for a window that has no crew of its own.
//
// AN EMPTY PROFILE DIRECTORY IS THE NORMAL CASE, NOT THE ABSENT CASE, AND
// ABSENCE IS A HOSTED WINDOW. [config.ProfileDir] is AFORGE_PROFILE_DIR, which
// almost nobody exports, and every reader in internal/config resolves the empty
// string to this process's own profile in the state root
// ([config.ProfilePath]) — so the guard these surfaces used to carry was true on
// very nearly every launch, and the crew segment the status line exists to show
// was drawn only for the handful of people who had exported that variable
// (#315). The real absence is a CONNECTION: over --host the crew lives on the
// far machine and this window's profile is the laptop's, which is the refusal
// [app.runCrew] already opens with and the fact [app.workSeat] guards on.
//
// AND IT IS READ AT THE DOOR AND NOT AT THE DRAW. Answering the status line
// straight from the profile would put SEVEN config file reads on the frame
// clock — four for the preset, three for the classes — for four rows that change
// a few times a year, which is the cost [crewPicker.inherited] refuses for the
// same reason. The snapshot is invalidated by the ONE counter every persisted
// write bumps ([config.SettingsGeneration]), so /crew, the settings row and the
// `change_setting` tool all move it and none of them needs to know this cache
// exists; what the counter does not see — a config file edited by another
// process — lands on the next launch, exactly as it does for the role source
// the tasks resolve through (cmd/aforge's v3Crew).
func (a *app) crewReading() (crewReading, bool) {
	if a.hosted() {
		return crewReading{}, false
	}
	if generation := config.SettingsGeneration(); !a.crew.taken || a.crew.generation != generation || a.crew.dir != a.profileDir {
		preset, classes := config.CrewAt(a.profileDir), config.CrewClasses(a.profileDir)
		a.crew = crewReading{
			word:       preset + " · " + classes,
			segment:    "crew " + preset,
			preset:     preset,
			classes:    classes,
			dir:        a.profileDir,
			generation: generation,
			taken:      true,
		}
	}
	return a.crew, true
}

// crewHint is [app.crewSegment] for the hint slot under the model picker
// (render.go's [app.hintWord]).
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
func (a *app) crewHint() string { return a.crewSegment() }

// ── the work seat, and the one line about it ────────────────────────────────
//
// A CREW OLDER THAN THE WORK SEAT FILLS IT ANYWAY, AND THE CONVERSATION SAYS SO
// ONCE.
//
// The worker row arrived after the other four (#278), so a crew chosen before it
// exists on disk as four rows with no worker among them. Headless doors read
// that shape through the ladder and print one line about it (#311); this surface
// read the row, found nothing, and handed every task to the build's own worker
// model without a word — on the surface where most tasks are started, and where
// there is no `models:` line for a person to notice a word on (#312).
//
// So the seat is resolved through the SAME ladder (config.TierSeatAt) and the
// line is the SAME string (config.Seat.Notice), and it is said in the two places
// a person is already looking: in the thread, the moment work actually starts on
// that seat, and on the /crew sheet, which is where somebody goes to check.

// workSeat is this conversation's work seat, resolved the way the role map that
// runs its tasks resolves it (cmd/aforge's v3Crew).
//
// AN EMPTY profileDir IS THE ORDINARY PROFILE AND NOT THE ABSENCE OF ONE.
// [config.ProfileDir] is AFORGE_PROFILE_DIR, which almost nobody sets, and every
// reader in internal/config takes the empty string to mean "this process's own
// profile" — so a guard on the field would silence the line on precisely the
// launch it was written for. The absence is a CONNECTION: over --host the crew
// lives on the far machine and this window's profile is the laptop's, which is
// the same refusal [app.runCrew] opens with.
func (a *app) workSeat() config.Seat {
	if a.hosted() {
		return config.Seat{}
	}
	return config.TierSeatAt(a.profileDir, config.ModelTierWorker)
}

// sayWorkSeat puts the one line in the thread, ONCE PER SESSION, and is called
// where work actually starts on the seat (task.go's [app.taskUpdate]).
//
// THE MOMENT IS THE FIRST RUNNING NODE, and not the launch. A person who never
// hands anything off never meets this seat, and a line at boot about a model
// nothing has used yet is a notice competing with the greeting for a fact that
// may never become true. A person who hands off twenty things meets it once.
//
// THE QUESTION IS ASKED ONCE TOO. The flag is set whether or not there was
// anything to say, because the answer cannot change under a session in a way
// that owes a person a line: picking a crew ends the substitution, and clearing
// the row is somebody saying "follow the conversation" on purpose. That also
// keeps the profile off the path of every task update, which is where a file
// read has no business being.
//
// IT IS NOT ONE OF THE HINTS (notice.go). Those age out after a few sessions and
// go quiet when a person turns hints off, which is right for a tip about a key
// and wrong for a receipt about which model is spending their money: this is
// true until they answer it, and it is said every session until they do.
func (a *app) sayWorkSeat() {
	if a.workSeatSaid {
		return
	}
	a.workSeatSaid = true
	if notice := a.workSeat().Notice(); notice != "" {
		a.note(notice)
	}
}

// crewPicker is the fixed, bottom-anchored chooser opened by bare /crew. Its
// zero value is closed, like [picker], and its cursor is an index into
// [config.CrewPresets].
type crewPicker struct {
	open    bool
	cursor  int
	current string
	// inherited is the line naming the seat this profile has no row for, or
	// empty when every row was written. IT IS READ AT THE DOOR AND NOT AT THE
	// DRAW: the rows are built every frame and the answer is three lines of a
	// file, so a picker that asked the profile per frame would put a disk read
	// on the frame clock for a fact that cannot move while the list is up.
	inherited string
}

func (p *crewPicker) start(current, inherited string) {
	*p = crewPicker{open: true, current: current, inherited: inherited}
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

// The chooser's fixed lines around the three presets, spelled once so the
// height and the rows cannot count them differently.
const (
	// crewScopeLine is the header: what the presets below move, and what they
	// do not. It is the one sentence the whole surface exists to make plain.
	crewScopeLine = "the five models aforge uses on its own behalf — not the one you chat with"
	// crewSeatLead is seat one's label. It is a plain phrase and not a class word,
	// because the class words are what the presets change and this seat is not.
	crewSeatLead = "you talk to"
	// crewPinLine is the closing note: the door to moving one seat on its own,
	// which this chooser deliberately does not offer.
	crewPinLine = "each of the five can be pinned on its own in /settings → Providers"
	// crewCustomLine is the fourth reading, said as a fact about where the person
	// is rather than as a fourth row they could pick.
	crewCustomLine = "yours is none of the three — picking one puts all five back"
	// crewInheritedLead and crewInheritedTail wrap the row a profile older than
	// a seat never wrote:
	//
	//	your work seat is inherited from small work — picking one writes it
	//
	// It is said as a fact about a ROW on this sheet, which is what this list is
	// for, while the thread's line ([config.Seat.Notice], said once when work
	// starts) is the receipt. `inherited` is the same word the headless models
	// line prints beside the model, so a person who has seen one surface
	// recognises the other.
	crewInheritedLead = "your "
	crewInheritedMid  = " seat is inherited from "
	crewInheritedTail = " — picking one writes it"
	// crewFrameRows is how many of the chooser's rows are not preset rows: the
	// scope line, seat one, and the closing note.
	crewFrameRows = 3
)

func (p *crewPicker) height() int {
	if !p.open {
		return 0
	}
	height := crewFrameRows + len(config.CrewPresets)*2
	if p.current == config.CrewCustom {
		height++
	}
	if p.inherited != "" {
		height++
	}
	return height
}

// rows uses the model picker's bottom-overlay row vocabulary, but always gives
// the models their own dim line: with three fixed choices, comparison matters
// more than fitting a fourth choice that does not exist.
//
// THE CHOOSER IS THE FIVE-SEAT READING. aforge runs five model seats — the one
// you talk to, then reflex, small work, careful work and mastermind — and until
// this wave the chooser showed four of them and said nothing about the fifth,
// which is the one seat a person can see on the frame and the one `/crew` never
// moves. So the list opens with a header saying what the presets below change
// and what they do not, then seat one on a line of its own — labelled, with no
// lead and no ground, so it cannot be mistaken for a row enter would apply —
// then the three presets exactly as before, and a closing note pointing at the
// settings row where any one of the five can be pinned by itself. Per-seat
// picking is NOT built in here: settings already owns it, and a chooser that
// both applied presets and moved single seats would be two controls wearing one
// set of keys.
func (p *crewPicker) rows(width, n int, pal palette, hover int, a *app) []string {
	if !p.open || n <= 0 {
		return nil
	}
	out := make([]string, 0, p.height())
	out = append(out, pal.dim(fit(crewScopeLine, width)))
	// Seat one wears the data hue on its id and the dim tier on its label, which
	// is THE PAYLOAD RULE's own split (payload.go): the id is the answer, the
	// label is the question. It takes neither the cursor's `›` nor a ground,
	// because nothing on this list can move it.
	if id := a.talkingTo(); id != "" {
		out = append(out, "  "+pal.dim(crewSeatLead+" · ")+pal.data(fit(id, width-2-len(crewSeatLead)-3)))
	} else {
		// A session with no model yet has no seat one to show, and a labelled gap
		// would be a claim about a model nobody has named. The line is kept, empty,
		// so the chooser's height stays what [crewPicker.height] promised.
		out = append(out, "")
	}
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
		out = append(out, pal.dim(fit(crewCustomLine, width)))
	}
	// AND THE ROW NOBODY WROTE, said last among the readings and before the door
	// out: it is the one fact on this list that is about the person's own five
	// rows rather than about the three on offer, and the answer to it is the
	// same enter every row above it takes.
	if p.inherited != "" {
		out = append(out, pal.dim(fit(p.inherited, width)))
	}
	out = append(out, pal.dim(fit(crewPinLine, width)))
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// crewInheritedLine is the sheet's own sentence about a seat this profile has no
// row for, and empty for every profile that wrote all five.
//
// ONE READING OF THE SEAT FEEDS BOTH SURFACES. The words differ because the
// places do — the thread is told what is running right now, the sheet is told
// which of its rows is not yours — and the FACT is one call to one ladder
// ([app.workSeat]), so neither surface can name a row the other does not.
func (a *app) crewInheritedLine() string {
	seat := a.workSeat()
	if seat.Source != config.SeatInherited {
		return ""
	}
	return crewInheritedLead + string(seat.Role) + crewInheritedMid + seat.FromWords() + crewInheritedTail
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
			"\n/crew balanced puts all five back")
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
	if !a.at(pageSettings) || a.sheet.registry == nil {
		return
	}
	a.sheet.rows = a.sheet.registry.Rows()
	a.sheet.build()
}
