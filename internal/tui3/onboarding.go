package tui3

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/fuzzy"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE CONTROLS SCREEN — the one screen between connecting and the first thing a
// person types.
//
// The setup used to ask three separate questions after the key: a crew chooser,
// then a rails screen with three money rows on it. Five answers, four of which a
// person who has never run the program cannot have an opinion about yet. This is
// the one screen that replaced them, and the rule that decided its contents is
// written in docs/design/onboarding/DESIGN.md: a control belongs here when it is
// NECESSARY TO WORK, MATERIALLY CHANGES THE FIRST EXPERIENCE, and CAN BE
// UNDERSTOOD BEFORE THE PERSON HAS USED codeaf. Three pass — the day's limit
// (money is consequential and an amount can be chosen with no knowledge of the
// engine), the chat model, and the crew — and everything else is a setting or a
// thing to be taught at the moment it happens.
//
// SIX LAWS HOLD THIS SCREEN TOGETHER.
//
//   - EVERY VALUE ON IT IS THE RESOLVED ONE. Nothing here is a figure this file
//     knows: the limit, the model and the crew are read back through the settings
//     registry and internal/config, so a profile that already has a limit sees
//     THEIR limit and not the default, a crew nobody can name reads `Custom`
//     rather than a preset it is not, and an environment variable that owns a row
//     is named rather than quietly overwritten.
//   - THE DEFAULTS ARE ALREADY CHOSEN. Every row opens on the value that is in
//     force, so a person who wants none of this presses enter on `Start a
//     conversation` and has agreed to exactly what they were shown. That is the
//     whole difference between a form and an interrogation.
//   - IT WRITES ONLY WHAT IT WAS GIVEN. Leaving this screen writes the day's
//     limit, because the limit is the field it asked about; it writes a crew ONLY
//     where the person chose one here or where the profile had none at all. A
//     setup that applied a preset to somebody's hand-pinned tiers would destroy an
//     opinion in the act of asking for one ([app.commitSetupCrew]).
//   - CANCELLING CHOOSES NOTHING. Esc out of either chooser leaves the value that
//     was there before it opened. A cursor is not an answer.
//   - THE SCREEN IS THREE SENTENCES LONG. Each control carries one line about
//     what it does and nothing else; the caveats, the exact model ids and the five
//     crew seats are behind `?`, on the field a person is standing on. A first
//     screen that has to be read before it can be answered is a first screen most
//     people leave.
//   - IT SPENDS NOTHING. Opening a model list reads the catalog this process
//     already has; nothing on this screen sends a prompt or calls a model.
//
// The example on the right is labelled as an example. It is one request somebody
// could make and the kind of result it leads to, and it moves only when the
// person moves — a deliberate focus change or the arrow keys, never a timer and
// never a keystroke inside a field. No invented cost, no fabricated activity, no
// claim that anything has already run.

// setupControl is one row of the controls screen, in the order tab walks them.
// The day's limit comes first because it is the consequential one; the two model
// rows are grouped under it because they are one subject.
type setupControl int

const (
	controlLimit setupControl = iota
	controlChatModel
	controlCrew
	controlReview
	controlStart
)

// setupControlCount is how many rows tab walks. It is derived from the last
// control rather than written down, so adding one cannot leave the walk short.
const setupControlCount = int(controlStart) + 1

// ── what the screen says ────────────────────────────────────────────────────

// The heading and the line under it. The heading names the SUBJECT of the screen
// rather than the act ("setup", "configuration"), because the subject is what a
// person is deciding about and the act is already obvious from being here.
const (
	controlsTitle = "Models and spending"
	controlsLead  = "Keep these choices or change them."
)

// The three field labels, laid out in one column so their values line up.
const (
	controlLimitLabel = "Daily limit"
	controlModelLabel = "Chat model"
	controlCrewLabel  = "Work crew"
)

// controlLabelWidth is the column the three labels are laid out in. It is wide
// enough for the longest of them with a comfortable gap, and it is one constant
// because a value column that wandered between rows would be a column nobody can
// read down.
const controlLabelWidth = 19

// The one-line explanation under each field, and THERE IS ONLY ONE LINE. Each
// says what the value DOES, in the words the settings registry uses for the same
// row where it can. Everything else this screen could say about a control is
// behind `?` on that control.
const (
	controlLimitWord = "When " + product + "'s spending today reaches this amount, " +
		"new work waits until midnight or you raise it."
	controlModelWord = "The model you talk to in this conversation."
	controlCrewWord  = "Models used to plan, run, and check tasks."
)

// The detail behind `?`, on the field with the focus.
//
// THE LIMIT'S DETAIL REFUSES TO OVERPROMISE. It is a ceiling on what codeaf
// RECORDS spending, calls already in flight can carry the day a little past it,
// and the provider account has controls of its own that this number knows nothing
// about. A screen that said "you will never be billed more than this" would be
// the product making a promise it cannot keep with somebody else's money.
const (
	controlLimitDetail = "It counts spending " + product + " records here. Calls already " +
		"running can carry it a little past. Your provider account has its own controls."
	controlModelDetail = "It also handles this conversation's tool use. Changing it here is " +
		"the same choice /model makes, and it is kept for the next launch."
	controlCrewDetail = "Changing the crew leaves the chat model alone."
)

// The two spellings of the row under the fields. A fresh profile is told these
// are defaults, because they are; a profile that has written any of them down is
// NOT told that, because it would be false — and the review then shows the actual
// values either way.
// AND EACH HAS A SHORT SPELLING FOR A NARROW WINDOW. `Other settings use
// defaults · r…` is a row that has stopped saying anything; a shorter true
// sentence is always better than a longer one with its end cut off.
const (
	controlReviewDefaults      = "Other settings use defaults · review"
	controlReviewDefaultsShort = "Other settings · review"
	controlReviewYours         = "Review other settings"
	controlReviewYoursShort    = "Other settings"
)

// controlStartWord is the way out, and it is named for what happens next rather
// than for the form being finished. "Done" and "Save" describe the screen;
// this describes the person's day.
const controlStartWord = "Start a conversation"

// controlReviewRest is the line under the optional review: where these three
// live afterwards. It is the one door this screen offers onto everything it
// deliberately does not ask about.
const controlReviewRest = "/settings changes these and every other one."

// controlPinnedLead prefixes a value an environment variable owns, and
// controlFromLead a value one merely SEEDED. The two are different facts and the
// screen may not spell them the same way: a pinned row cannot be edited here at
// all, where a seeded one can and the choice outranks the variable on the next
// launch (internal/config's chatmodel.go states that law).
const (
	controlPinnedLead = "set by "
	controlFromLead   = "from "
)

// controlTaskPinLead is the line under the crew when a task-model override is in
// force. THE CREW ROW WOULD OTHERWISE BE A LIE BY OMISSION: a person who pinned
// `task.model` has taken the worker seat out of the preset's hands, and a screen
// offering to change "the models used to run tasks" without saying so would be
// selling them a dial that is disconnected.
const controlTaskPinLead = "Tasks are pinned to "

// ── the keyboard ────────────────────────────────────────────────────────────

// setupControlsKey is the controls screen's whole claim on the keyboard. It
// reports whether it took the key, which is every key while the screen is up:
// [app.setupKeyPress] has already decided that the setup owns the keyboard, and
// this decides what the key means here.
func (a *app) setupControlsKey(name, text string) bool {
	s := &a.setup
	switch name {
	case "esc":
		// ESC CLOSES WHAT IS OPEN BEFORE IT LEAVES THE SCREEN, AND CHOOSES
		// NOTHING ON THE WAY. A chooser standing under a row is the thing a
		// person pressing esc means, every time; the cursor inside it is
		// provisional until enter, so closing it drops the cursor and leaves the
		// value that was on the row before it opened. Only an esc with nothing
		// open is about the screen itself, and [app.setupControlsPress] handles
		// that one because leaving is the flow's business rather than this
		// screen's.
		if s.modelOpen || s.crewOpen {
			s.closeChoosers()
			return true
		}
		if s.detail {
			s.detail = false
			return true
		}
		if s.reviewOpen {
			s.reviewOpen = false
			return true
		}
		return false

	case "tab", "down", "ctrl+n":
		if s.modelOpen && name != "tab" {
			a.moveSetupModel(1)
			return true
		}
		if s.crewOpen && name != "tab" {
			s.crewAt = moveCursor(s.crewAt, 1, len(config.CrewPresets))
			return true
		}
		s.focusControl(a, 1)
		return true

	case "shift+tab", "up", "ctrl+p":
		if s.modelOpen && name != "shift+tab" {
			a.moveSetupModel(-1)
			return true
		}
		if s.crewOpen && name != "shift+tab" {
			s.crewAt = moveCursor(s.crewAt, -1, len(config.CrewPresets))
			return true
		}
		s.focusControl(a, -1)
		return true

	case "left", "right":
		// THE EXAMPLES ARE BROWSED AND NEVER CYCLED. No clock on this screen moves
		// between them; these two keys are the only way the right-hand panel
		// changes other than following the focus, and they change nothing about
		// the profile. Each such move plays the panel's own one-shot
		// demonstration once and then it is still — a browse is a deliberate act,
		// which is exactly the condition that motion here is allowed under. Inside
		// the model list they are not free — the filter box has the keyboard — so
		// the list keeps them and does nothing.
		if s.modelOpen {
			return true
		}
		delta := 1
		if name == "left" {
			delta = -1
		}
		s.example = moveCursor(s.example, delta, len(setupExamples))
		// One of the two deliberate acts the demonstration plays for.
		a.restartSetupDemo()
		return true

	case "backspace":
		// Editing an amount or a model search is typing, and typing settles the
		// panel for the reason the character branch at the foot of this function
		// gives.
		a.settleSetupDemo()
		if s.modelOpen {
			a.filterSetupModels(dropLast(s.modelFind))
			return true
		}
		if s.control == controlLimit && !s.anyOpen() {
			s.limitTyped = true
			s.limitText = dropLast(s.limitText)
		}
		return true

	case "ctrl+u":
		a.settleSetupDemo()
		if s.modelOpen {
			a.filterSetupModels("")
			return true
		}
		if s.control == controlLimit && !s.anyOpen() {
			s.limitText, s.limitTyped = "", true
		}
		return true
	}
	// Everything else that carries a character is typing. Inside the model list
	// it narrows the list, which is the only way a catalog of two hundred models
	// is reachable from a form; on the limit row it is the amount.
	if text == "" {
		return true
	}
	// AND TYPING SETTLES THE PANEL. A person who has started answering the form
	// is not watching the illustration, and an illustration still assembling
	// itself beside a field somebody is typing into is motion with no reader.
	a.settleSetupDemo()
	// EXCEPT THE ONE CHARACTER THAT IS A QUESTION. `?` on a field opens its detail
	// and takes it away again — the detail is asked for and never offered, so the
	// screen a person arrives at is three sentences long however much they read on
	// the way. It is read off the key's TEXT rather than as a named key, because a
	// terminal reporting `?` as shift-and-something would otherwise have typed it
	// into the amount box.
	if text == "?" && !s.anyOpen() {
		s.detail = !s.detail
		return true
	}
	if s.modelOpen {
		a.filterSetupModels(s.modelFind + text)
		return true
	}
	if s.control == controlLimit && !s.crewOpen {
		s.limitText += text
		s.limitTyped = true
	}
	return true
}

// dropLast takes one rune off the end of what has been typed.
func dropLast(text string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

// anyOpen reports whether a chooser is standing under a row.
func (s *setupFlow) anyOpen() bool { return s.modelOpen || s.crewOpen }

// closeChoosers puts both away and drops what was provisional in them: the model
// list's filter and cursor, and the crew's cursor. Neither has written anything —
// that is what makes esc safe here.
func (s *setupFlow) closeChoosers() {
	s.modelOpen, s.crewOpen = false, false
	s.modelFind = ""
	s.modelTop = 0
}

// focusControl walks the rows, closing whatever was open on the way. THE
// EXAMPLE FOLLOWS THE FOCUS because a focus change is a deliberate act — which
// is the whole rule the right-hand column is built on (docs/design/onboarding).
func (s *setupFlow) focusControl(a *app, delta int) {
	s.closeChoosers()
	// The detail belongs to the field it was asked about and goes with it.
	s.detail = false
	s.control = setupControl(moveCursor(int(s.control), delta, setupControlCount))
	was := s.example
	s.example = exampleForControl(s.control)
	s.refusal = ""
	// The other deliberate act. Two rows share an example — the model row and
	// `Start a conversation` both stand beside the first — and walking between
	// them does not replay it: the panel would restart under a person who never
	// changed what it was showing.
	if s.example != was {
		a.restartSetupDemo()
	}
	a.touch()
}

// moveSetupModel walks the model list and carries the viewport with the cursor,
// so a list of two hundred is walkable through the five rows this form has for
// it. A list with nothing in it does nothing at all: a cursor moving over an
// empty list lands out of range the moment one arrives.
func (a *app) moveSetupModel(delta int) {
	s := &a.setup
	models := a.setupModelChoices()
	if len(models) == 0 {
		return
	}
	s.modelAt = moveCursor(s.modelAt, delta, len(models))
	s.modelTop = scrollTo(s.modelTop, s.modelAt, len(models), setupModelSlots)
}

// scrollTo keeps a cursor inside a window of n rows, moving the window by the
// least it can. It wraps with the cursor: walking off the bottom of a list puts
// the window back at the top, which is where the cursor went.
func scrollTo(top, at, count, slots int) int {
	if count <= slots {
		return 0
	}
	if at < top {
		top = at
	}
	if at >= top+slots {
		top = at - slots + 1
	}
	return clampIndex(top, count-slots+1)
}

// filterSetupModels narrows the list and re-aims the cursor. THE MODEL IN USE
// KEEPS THE CURSOR WHERE IT IS STILL ON THE LIST, so narrowing and then clearing
// the filter cannot walk somebody onto a model they never chose.
func (a *app) filterSetupModels(find string) {
	s := &a.setup
	s.modelFind = find
	models := a.setupModelChoices()
	s.modelAt = 0
	for i, model := range models {
		if model.ID == a.model {
			s.modelAt = i
			break
		}
	}
	s.modelTop = scrollTo(0, s.modelAt, len(models), setupModelSlots)
}

// setupControlsEnter is enter on the focused row. It reports whether the whole
// screen is finished, which is true only for `Start a conversation`.
func (a *app) setupControlsEnter() bool {
	s := &a.setup
	switch s.control {
	case controlLimit:
		// Enter on the limit is "yes, and move on": the figure is checked here so
		// a refusal lands beside the field rather than four keystrokes later on
		// the way out, and then the focus walks down.
		if !a.commitSetupLimit() {
			return false
		}
		s.focusControl(a, 1)
		return false

	case controlChatModel:
		if s.modelOpen {
			models := a.setupModelChoices()
			if s.modelAt >= 0 && s.modelAt < len(models) {
				a.takeSetupModel(models[s.modelAt].ID)
			}
			s.closeChoosers()
			return false
		}
		// A ROW THE ENVIRONMENT OWNS DOES NOT OPEN A LIST. Offering a choice that
		// [Setting.Apply] would refuse is a form that lies; the registry's own
		// sentence says who owns the row instead.
		if row, ok := a.registry().Row(config.ModelSettingKey(talkSlot)); ok {
			if name, pinned := row.PinnedBy(); pinned {
				s.refusal = controlModelLabel + " is set by " + name
				return false
			}
		}
		s.modelOpen = true
		a.filterSetupModels("")
		return false

	case controlCrew:
		if s.crewOpen {
			// THE CURSOR BECOMES THE ANSWER HERE AND NOWHERE ELSE. Until this
			// line the crew on the row is whatever the profile reads.
			s.crewPick = config.CrewPresets[clampIndex(s.crewAt, len(config.CrewPresets))]
			s.closeChoosers()
			return false
		}
		s.crewOpen = true
		s.crewAt = a.setupCrewCursor()
		return false

	case controlReview:
		s.reviewOpen = !s.reviewOpen
		return false
	}
	// `Start a conversation` — everything the screen holds is written down, and
	// a row that refused keeps the screen up with its refusal on it.
	return a.commitSetupControls()
}

// clampIndex keeps a cursor inside a list that may have changed under it.
func clampIndex(at, count int) int {
	if count <= 0 {
		return 0
	}
	if at < 0 {
		return 0
	}
	if at >= count {
		return count - 1
	}
	return at
}

// ── what it writes ──────────────────────────────────────────────────────────

// commitSetupLimit writes the day's ceiling through the registry row, and
// reports whether it landed. An untouched field writes back exactly what it drew
// — which is what makes enter on this screen agree with the figure on it.
//
// A ROW THE ENVIRONMENT OWNS IS NOT WRITTEN AT ALL. [Setting.Apply] would refuse
// it in the registry's own words, and refusing a person for not editing a field
// this screen told them it could not edit would be the form arguing with itself.
func (a *app) commitSetupLimit() bool {
	s := &a.setup
	row, ok := a.registry().Row(config.KeyDailyBudget)
	if !ok {
		return true
	}
	if _, pinned := row.PinnedBy(); pinned {
		return true
	}
	raw := strings.TrimSpace(s.limitText)
	if !s.limitTyped || raw == "" {
		raw = a.setupLimitReading()
	}
	if err := row.Apply(raw); err != nil {
		s.refusal = setupSaid(err, setupSaveFailedWord)
		return false
	}
	s.limitText, s.limitTyped = "", false
	a.refreshSettings()
	return true
}

// commitSetupCrew writes a crew, and MOSTLY DOES NOT.
//
// THE SETUP MAY NOT PAPER OVER AN OPINION. internal/config says why in its own
// words: [config.CrewConfigured] is true when ANY of the tier rows, or the
// family row above them, is in the profile, "because a person who pinned one tier
// by hand, or chose a family, has an opinion the setup must not paper over with a
// preset". So a preset is written on exactly two
// roads — the person chose one in the chooser on this screen, or the profile had
// no crew of its own at all and the row they were shown is the shipped default,
// which is the case the first-run flow exists for.
//
// The cost of getting this wrong was a real one: a profile with hand-pinned tiers
// and no daily limit opens this screen, and the old code applied
// `CrewPresets[0]` — Frugal — to it on the way out, silently, because the cursor
// started at zero.
func (a *app) commitSetupCrew() bool {
	s := &a.setup
	preset := s.crewPick
	if preset == "" {
		if config.CrewConfigured(a.profileDir) {
			return true
		}
		preset = config.CrewAt(a.profileDir)
		if config.CrewLine(preset) == "" {
			// Not one of the three, on a profile with nothing written down. There
			// is no preset to record and nothing to preserve, so nothing is done.
			return true
		}
	}
	row, ok := a.registry().Row(config.KeyCrew)
	if !ok {
		return true
	}
	if err := row.Apply(preset); err != nil {
		s.refusal = setupSaid(err, setupSaveFailedWord)
		return false
	}
	a.refreshSettings()
	return true
}

// commitSetupControls is `Start a conversation`. It reports whether the screen is
// finished.
//
// THE CHAT MODEL IS ALREADY WRITTEN BY THE TIME THIS RUNS, and deliberately: a
// model is taken at the moment it is chosen in the list, through the registry row
// every other model change goes through, so what this screen leaves behind is the
// live conversation on that model rather than a promise to switch later. A person
// who opened the list and pressed esc chose nothing and nothing was written.
func (a *app) commitSetupControls() bool {
	if !a.commitSetupLimit() {
		return false
	}
	return a.commitSetupCrew()
}

// takeSetupModel puts the conversation on a model chosen here, THROUGH THE
// REGISTRY ROW and not around it.
//
// The row's writer is the surface's one model road (settings.go's SetModel seam →
// [app.switchModel]), so what this screen lands is what /model and the settings
// sheet land: the live agent, the reasoning dial, the context window and the
// profile's own record of the choice. Going straight to [app.switchModel] would
// have skipped the row's own precedence — the environment pin above all of it —
// which is the one thing a form must not do quietly.
func (a *app) takeSetupModel(id string) {
	s := &a.setup
	id = strings.TrimSpace(id)
	if id == "" || id == a.model {
		return
	}
	row, ok := a.registry().Row(config.ModelSettingKey(talkSlot))
	if !ok {
		return
	}
	if err := row.Apply(id); err != nil {
		s.refusal = setupSaid(err, setupModelFailedWord)
		return
	}
	// AND THE PROFILE IS ASKED WHETHER IT ACTUALLY KEPT IT. The write's live half
	// cannot fail visibly and its disk half drops its error on purpose
	// (palette.go's [app.rememberModel] says why), so the one honest check is to
	// read the choice back. A surface with no way to remember says nothing: that
	// is the hosted window, and it is not a fault.
	if a.saveModel != nil && config.ChatModelAt(a.profileDir) != id {
		s.refusal = setupModelUnsavedWord
	}
}

// setupModelFailedWord is a model the row would not take — an environment pin,
// a slot this surface cannot write. It is the fallback under [setupSaid], which
// keeps the registry's own sentence wherever there is one.
const setupModelFailedWord = "could not change the model here"

// setupModelUnsavedWord is the model that changed for this conversation and did
// not reach the profile. It says both halves, because the half that worked is the
// one the person is looking at.
const setupModelUnsavedWord = "this conversation is on it, but it could not be saved for next time"

// setupModelChoices is the chat-model list this screen offers: the same catalog,
// filtered by the same chat law, that /model's picker offers, narrowed by
// whatever has been typed.
//
// THE MODEL IN USE IS ALWAYS ON THE LIST, even when the catalog has never heard
// of it — a fresh launch with no key has no catalog at all, and a chooser whose
// first row was some other model would turn "let me look" into an accidental
// switch the moment somebody pressed enter. So it is put at the head when the
// catalog does not carry it, and the cursor opens on it either way.
func (a *app) setupModelChoices() []Model {
	models := a.modelList()
	if current := strings.TrimSpace(a.model); current != "" {
		found := false
		for _, model := range models {
			if model.ID == current {
				found = true
				break
			}
		}
		if !found {
			models = append([]Model{{ID: current}}, models...)
		}
	}
	find := strings.TrimSpace(a.setup.modelFind)
	if find == "" {
		return models
	}
	// THE SAME MATCHER /model USES ([picker.rank], internal/fuzzy), so what a
	// person types finds the same models in setup as it does there. A plain
	// substring test found nothing for `ds v4`, which /model answers with
	// deepseek/deepseek-v4-flash: the query is tokens, every one must match, and
	// the best alignment ranks first, ties keeping the catalog's order (#1321).
	// The words are folded and made into terms exactly as the picker makes them
	// ([fuzzyTerms]), so a capital typed here means what it means there.
	terms := fuzzyTerms(strings.Fields(strings.ToLower(find)))
	type hit struct {
		model Model
		score int
	}
	hits := make([]hit, 0, len(models))
	for _, model := range models {
		// A person types what they can SEE, so the friendly name is searched
		// beside the id: "sonnet" and "Claude Sonnet" reach the same row.
		if score, ok := fuzzy.ScoreFields([]string{model.ID, modelWord(model.ID)}, terms); ok {
			hits = append(hits, hit{model: model, score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]Model, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.model)
	}
	return out
}

// setupModelSlots is how many models the list SHOWS AT ONCE, and it is a height
// budget rather than a limit on the catalog: the cursor scrolls the rest past it
// and typing narrows them. Five is what a twenty-four-row window has to spare
// under a field that still needs the primary action beneath it.
const setupModelSlots = 5

// setupCrewCursor is where the crew chooser opens: on the preset in force, so
// enter confirms rather than changes. A crew that is nobody's preset opens on the
// default, because there is no row to confirm and the default is the one this
// screen would otherwise be recommending.
func (a *app) setupCrewCursor() int {
	current := a.setupCrewReading()
	for i, preset := range config.CrewPresets {
		if preset == current {
			return i
		}
	}
	for i, preset := range config.CrewPresets {
		if preset == config.DefaultCrew {
			return i
		}
	}
	return 0
}

// setupLimitReading is the day's ceiling as this screen draws it and as it writes
// it back: the registry row's own reading, which is `$500`, or `no limit` for a
// profile that has taken the rail off.
func (a *app) setupLimitReading() string {
	row, ok := a.registry().Row(config.KeyDailyBudget)
	if !ok {
		return ""
	}
	if value := strings.TrimSpace(row.Value()); value != "" {
		return value
	}
	return config.NoLimitWord
}

// setupCrewReading is the crew this screen is showing: what was chosen here, or
// what the profile's five tier rows actually make — which may be
// [config.CrewCustom], and says so rather than naming a preset it is not.
func (a *app) setupCrewReading() string {
	if a.setup.crewPick != "" {
		return a.setup.crewPick
	}
	return config.CrewAt(a.profileDir)
}

// startSetupControls seeds the screen from the profile, ONCE. Every value on it
// is the resolved one at the moment the screen is first reached — and it is not
// re-read afterwards, because a form that reseeded on the way back from the step
// behind it would throw away the amount somebody had typed and the crew they had
// picked before they went to look at something (the design's own back/return
// rule, docs/design/onboarding/DESIGN.md).
func (a *app) startSetupControls() {
	s := &a.setup
	if s.seeded {
		return
	}
	s.seeded = true
	s.control = controlLimit
	s.limitText, s.limitTyped = "", false
	s.closeChoosers()
	s.reviewOpen, s.detail = false, false
	s.crewPick = ""
	// The family is read HERE, arriving on the screen, and not in the draw: this
	// screen's rows are drawn every frame and a profile read belongs at a door.
	s.crewSource = config.CrewSourceAt(a.profileDir)
	s.crewAt = a.setupCrewCursor()
	s.example = exampleForControl(controlLimit)
	// ARRIVING ON THE SCREEN IS THE FIRST OF THE TWO DELIBERATE ACTS, so the
	// panel plays once here. Coming BACK from the step behind this one does not
	// reach this line at all — the seeded guard above returns first — which is
	// the same rule that keeps a half-typed amount: returning to a screen is not
	// arriving at it.
	a.restartSetupDemo()
}

// ── the examples on the right ───────────────────────────────────────────────

// setupExample is one illustration: a request somebody could make, and the KIND
// of result it leads to. Nothing in it is a measurement, a price, or a claim that
// any of it has happened.
type setupExample struct {
	// title names the subject in the person's terms, not the product's.
	title string
	// ask is the request, drawn in quotation marks so it cannot be mistaken for
	// something the screen is reporting. IT IS A CONCRETE ONE: "work on this
	// while I carry on" demonstrates nothing, because it describes the mechanism
	// rather than a job — a person reading it learns that tasks exist and not
	// what they are for.
	ask string
	// leads are the kinds of thing that come back. They are shapes of a result
	// and never a result: "a short account in the conversation" is true of every
	// run of that request, where "found 41 files" would be a lie about a run that
	// has not happened.
	leads []string
}

// setupExamples are the four, in the order ←/→ walks them. Each one is tied to a
// control by [exampleForControl] except the last, which belongs to no field and
// is reachable by browsing — the design's own point that the column is an
// invitation rather than a caption.
var setupExamples = []setupExample{
	{
		title: "Understand an unfamiliar project",
		ask:   "What is in this folder, and where would I start?",
		leads: []string{
			"A read of the files that matter",
			"A short account in the conversation",
			"A next step you can ask for",
		},
	},
	{
		// THE ONE EXAMPLE THAT SPELLS A COMMAND, and it spells one that exists and
		// does what the three lines under it say. `/task <brief>` is commands.go's
		// own row — "start work you can walk away from" — and the manual's account
		// of it is exact about the shape: the words after the command are turned
		// into a fuller brief, one worker starts, and nothing is asked of the
		// person. So the lines below say a brief, work, and a page to read, and
		// they deliberately do NOT say "a plan you approve": `/task` asks nothing,
		// and a first screen that promised a question would be selling a gate this
		// road does not have.
		title: "Hand off something longer",
		ask:   "/task Fix the failing tests and explain the changes.",
		leads: []string{
			"A brief written out from your words",
			"Work you can watch or walk away from",
			"A result on its own page to read",
		},
	},
	{
		title: "Follow the work and its cost",
		ask:   "What has this cost me so far today?",
		leads: []string{
			"Activity in the conversation",
			"Usage as work runs",
			"Spending details when you need them",
		},
	},
	{
		title: "Compare the options",
		ask:   "Should this cache live in memory or on disk?",
		leads: []string{
			"Each option looked at in turn",
			"The evidence behind each one",
			"A recommendation you can argue with",
		},
	},
}

// exampleForControl is which example accompanies which field, and it is the
// design's own mapping: the money row is accompanied by following the work and
// its cost, the crew by handing something off, and the model row — with the
// connection that precedes it — by understanding a project, which is the first
// thing most people actually type.
func exampleForControl(control setupControl) int {
	switch control {
	case controlLimit:
		return 2
	case controlCrew:
		return 1
	}
	return 0
}

// The two labels that keep the column honest. They are the whole reason a person
// does not read the right-hand side as a report about their own machine.
const (
	exampleAskLabel  = "Example request"
	exampleLeadLabel = "What it leads to"
)

// ── model names a person can read ───────────────────────────────────────────

// modelWord is a catalog id as a NAME: `deepseek/deepseek-v4-flash` reads
// `DeepSeek V4 Flash`.
//
// The form shows names and the detail shows ids, which is the right way round for
// a first screen: `z-ai/glm-5.3-flash` is an address, and an address is what you
// need when you are typing one into a config file — not when you are deciding
// which of two models to talk to. The exact id is one keystroke away on `?` and
// under the cursor in the list, so nothing is hidden, only ranked.
//
// THE VENDOR TABLE IS SPELLINGS AND NOT TRANSLATIONS. Everything not in it is
// title-cased from the id's own words, so a model that shipped this morning gets
// a readable name rather than nothing; the table exists only for the handful of
// names whose own owners capitalise them in a way no rule derives.
var modelWords = map[string]string{
	"ai":         "AI",
	"deepseek":   "DeepSeek",
	"glm":        "GLM",
	"gpt":        "GPT",
	"k2":         "K2",
	"k3":         "K3",
	"kimi":       "Kimi",
	"llama":      "Llama",
	"moonshotai": "Moonshot",
	"openai":     "OpenAI",
	"qwen":       "Qwen",
	"r1":         "R1",
	"xai":        "xAI",
	"z":          "Z",
}

// modelWord is the name; the id it was made from is [Model.ID] and stays
// available everywhere this is drawn.
func modelWord(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	// The vendor prefix goes: it is the half nobody reads twice, and the same
	// judgement internal/config's shortModel already makes about crew ids.
	name := id
	if at := strings.LastIndexByte(name, '/'); at >= 0 {
		name = name[at+1:]
	}
	// A reasoning level rides the id and is NOT part of the model's name — it is
	// what somebody asked for — so it is kept on the end untouched.
	level := ""
	if at := strings.IndexByte(name, ':'); at >= 0 {
		name, level = name[:at], name[at:]
	}
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if said, ok := modelWords[strings.ToLower(part)]; ok {
			out = append(out, said)
			continue
		}
		out = append(out, titleWord(part))
	}
	if len(out) == 0 {
		return id
	}
	return strings.Join(out, " ") + level
}

// titleWord capitalises one word of a model id, and LEAVES A WORD THAT ALREADY
// HAS CAPITALS ALONE: a vendor who shipped `MiniMax` spelled it that way on
// purpose, and lower-casing it to re-capitalise it would lose the second one.
func titleWord(word string) string {
	if word == "" {
		return ""
	}
	if strings.ToLower(word) != word {
		return word
	}
	runes := []rune(word)
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}

// presetWord is a crew preset as a label — `balanced` reads `Balanced` — and it
// covers [config.CrewCustom] too, which is a reading rather than a preset and
// still has to be spelled on the row.
func presetWord(preset string) string { return titleWord(strings.TrimSpace(preset)) }

// ── the drawing ─────────────────────────────────────────────────────────────

// The composition, in cells. At 120×24 it is a 54-cell form, an eight-cell gap
// and a 36-cell example, centred as one 98-cell object with eleven cells of
// margin either side. Under [setupWideCols] the example is not drawn at all —
// stacking it under the form would put promotional content between a person and
// the thing they came here to do.
const (
	setupFormWidth = 54
	setupShowGap   = 8
	setupShowWidth = 36
	// setupWideCols is the width at which the pair fits with the three-cell
	// margins the design asks for, rounded to the round number the design study
	// states.
	setupWideCols = 112
	// setupNarrowForm is the form's width when it stands alone, which is a
	// comfortable measure for a sentence and no wider.
	setupNarrowForm = 64
	// setupMargin is the least the composition is ever inset from the frame.
	setupMargin = 3
)

// setupControlsFrame draws the controls screen: the header row, one blank row,
// then the form, with the example column beside it on a wide window.
//
// IT IS TOP-ANCHORED where the key step is centred, and the two are different on
// purpose. The key step is one question in the middle of an empty screen; this is
// a form with five rows, a primary action and a keyboard legend, and a form that
// floated up and down as its rows opened and closed would move the thing a person
// is aiming at every time they pressed a key.
func (a *app) setupControlsFrame(width, height int) ([]string, int, int) {
	wide := width >= setupWideCols
	form := min(width-2*setupMargin, setupNarrowForm)
	pair := form
	if wide {
		form = setupFormWidth
		pair = setupFormWidth + setupShowGap + setupShowWidth
	}
	if form < 20 {
		form = max(width-2, 1)
		pair = form
	}
	lead := max((width-pair)/2, 0)
	pad := strings.Repeat(" ", lead)

	// The header: the product on the left and where this is in the flow on the
	// right, both dim. It is one row and it is the only chrome the screen has.
	head := a.setupControlsHead(pair)

	// THE LEGEND IS MEASURED AGAINST THE WHOLE COMPOSITION AND NOT THE FORM. It
	// is the only row that has nothing beside it — the example column stops well
	// above the foot — so budgeting it at the form's fifty-four cells cut
	// `←→ examples` off the one screen the arrows exist on.
	sheet := a.setupControlsForm(form)
	// The rows the window cannot have are given up WHOLE BLOCK AT A TIME, in the
	// order the form itself ranked them. A wrapped sentence cut off in the middle
	// is worse than a sentence that is not there. Three rows are spoken for
	// before the form gets any: the header, the blank under it, and the legend.
	body, caretRow := sheet.trim(max(height-3, 1))

	var show []string
	if wide {
		show = a.setupShowcase(setupShowWidth, len(body))
	}

	lines := make([]string, 0, height)
	lines = append(lines, pad+head, "")
	for i, line := range body {
		row := pad + line
		if i < len(show) && show[i] != "" {
			// The example is placed at a fixed column so its own left edge is
			// straight down the screen: a column whose rows started wherever the
			// form's row happened to end would not read as a column at all.
			row = pad + padTo(line, setupFormWidth+setupShowGap) + show[i]
		}
		lines = append(lines, row)
	}
	// THE KEYBOARD GUIDANCE FOLLOWS THE FORM WITH ONE BLANK ROW UNDER IT, and the
	// window's own empty rows fall below that. It is measured against the WHOLE
	// composition rather than the form, because it is the only row the example
	// column never stands beside — budgeting it at the form's fifty-four cells cut
	// `←→ examples` off the one screen the arrows exist on
	// ([app.setupControlsKeys] then cuts by whole clauses, never mid-word).
	//
	// It is not pinned to the last row of the window. A legend nailed to the foot
	// of a twenty-four-row frame under an eighteen-row form leaves a hole in the
	// middle of the composition, and a hole reads as a screen that stopped.
	if len(lines) < height {
		lines = append(lines, pad+a.pal.dim(a.setupControlsKeys(pair)))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	caretY := caretRow + 2
	a.caret = caretRow >= 0 && caretY < height
	if !a.caret {
		return lines, 0, 0
	}
	return lines, lead + sheet.caretX, caretY
}

// padTo pads a painted row out to a column, measuring the plain text under the
// paint so an escape sequence is never counted as a cell.
func padTo(text string, width int) string {
	if gap := width - ansi.StringWidth(ansi.Strip(text)); gap > 0 {
		return text + strings.Repeat(" ", gap)
	}
	return text
}

// setupControlsHead is the one chrome row: the product, and where this is in the
// flow. The count is the flow's own ([setupTitle] is the same fact said the same
// way on the key step), and a flow with one step says no count at all.
func (a *app) setupControlsHead(width int) string {
	pal := a.pal
	right := ""
	if len(a.setup.steps) > 1 {
		right = "setup · " + itoa(a.setup.at+1) + " of " + itoa(len(a.setup.steps))
	}
	left := product
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return pal.dim(fit(left, width))
	}
	return pal.dim(left) + strings.Repeat(" ", gap) + pal.dim(right)
}

// controlsSheet is the form under construction: its rows, which droppable block
// each row belongs to, and the blocks in the order the screen gives them up.
//
// IT IS BLOCKS AND NOT ROWS, and that is the whole of what this type is for. The
// key step gives up single soft ROWS from the bottom, which is right for a screen
// whose soft rows are one paragraph — and wrong here, where a forty-column window
// dropped four lines of a five-line explanation and left the first line of a
// sentence hanging under the field. A wrapped sentence is one object: it goes
// whole or it stays whole.
type controlsSheet struct {
	rows []string
	// block is the block id each row belongs to, or zero for a row that is never
	// given up: a field's value, an open chooser, the primary action, the legend.
	block []int
	// ranked is every droppable block with how willingly it goes.
	ranked  []controlsRank
	next    int
	caretAt int
	caretX  int
}

// controlsRank pairs a block with how willingly it goes.
type controlsRank struct{ rank, id int }

// The ranks, lowest given up first. The order is a judgement about what a person
// standing in front of a short window still needs: the blank rows go before any
// words at all, then the explanations of the fields they are NOT on, and the
// explanation of the field they ARE on is the last thing to go, because it is the
// sentence they are reading. The detail is not ranked at all — it is only ever on
// screen because somebody pressed `?` for it.
const (
	rankSpacer = iota + 1
	rankOtherWords
	rankReviewRest
	rankLead
	rankFocusedWords
	rankDetail
)

func (f *controlsSheet) add(lines ...string) {
	for _, line := range lines {
		f.rows, f.block = append(f.rows, line), append(f.block, 0)
	}
}

// soft adds a block that a short window may give up whole.
func (f *controlsSheet) soft(rank int, lines ...string) {
	if len(lines) == 0 {
		return
	}
	f.next++
	id := f.next
	f.ranked = append(f.ranked, controlsRank{rank: rank, id: id})
	for _, line := range lines {
		f.rows, f.block = append(f.rows, line), append(f.block, id)
	}
}

// trim gives the window back the rows it does not have, block by block, and
// carries the caret with it.
func (f *controlsSheet) trim(height int) ([]string, int) {
	if len(f.rows) <= height {
		return f.rows, f.caretAt
	}
	over := len(f.rows) - height
	// The blocks are ordered by rank, and within one rank the LOWEST ON THE
	// SCREEN goes first — a form gives up its foot before its head, because the
	// rows nearest the top are the ones a person has already started reading.
	order := append([]controlsRank(nil), f.ranked...)
	for i := 1; i < len(order); i++ {
		for j := i; j > 0; j-- {
			if order[j].rank > order[j-1].rank ||
				(order[j].rank == order[j-1].rank && order[j].id < order[j-1].id) {
				break
			}
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	gone := make(map[int]bool, len(order))
	for _, block := range order {
		if over <= 0 {
			break
		}
		gone[block.id] = true
		for _, id := range f.block {
			if id == block.id {
				over--
			}
		}
	}
	out := make([]string, 0, len(f.rows))
	caret := f.caretAt
	for at, line := range f.rows {
		if gone[f.block[at]] {
			if f.caretAt >= 0 && at < f.caretAt {
				caret--
			}
			continue
		}
		out = append(out, line)
	}
	if len(out) > height {
		// EVERY BLOCK HAS GONE AND IT IS STILL TOO TALL, which is a window shorter
		// than the fields themselves. What is left is taken off the HEAD: of the
		// heading, the fields, the action and the legend, the heading is the one a
		// person can do without, and the legend at the foot is the one they cannot.
		cut := len(out) - height
		out = out[cut:]
		caret -= cut
	}
	return out, caret
}

// setupControlsForm builds the form: its rows, the blocks a short window gives
// up, and where the caret sits. The legend is NOT one of its rows — it belongs to
// the frame, at the foot of the window ([app.setupControlsFrame]).
func (a *app) setupControlsForm(width int) *controlsSheet {
	pal := a.pal
	s := &a.setup
	f := &controlsSheet{caretAt: -1}

	f.add(pal.bold(pal.ink(fit(controlsTitle, width))))
	f.soft(rankLead, pal.muted(fit(controlsLead, width)))
	f.soft(rankSpacer, "")

	// ── the day's limit ──
	limitRow, limitCaret := a.setupLimitRow(width)
	if s.control == controlLimit {
		f.caretAt, f.caretX = len(f.rows), limitCaret
	}
	f.add(limitRow)
	a.addControlWords(f, width, controlLimit, controlLimitWord, controlLimitDetail)
	f.soft(rankSpacer, "")

	// ── the chat model ──
	f.add(a.setupFieldRow(width, controlChatModel, controlModelLabel, modelWord(a.model), a.setupModelSource()))
	a.addControlWords(f, width, controlChatModel, controlModelWord, a.setupModelDetail())
	if s.modelOpen {
		f.add(a.setupModelRows(width)...)
	}
	f.soft(rankSpacer, "")

	// ── the crew ──
	f.add(a.setupFieldRow(width, controlCrew, controlCrewLabel, presetWord(a.setupCrewReading()), ""))
	a.addControlWords(f, width, controlCrew, controlCrewWord, a.setupCrewDetail())
	if line := a.setupTaskPinRow(width); line != "" {
		// A TASK-MODEL OVERRIDE IS NOT DETAIL. It is a fact that contradicts the
		// row above it, so it is on the screen whether or not anybody asked.
		f.add(line)
	}
	if s.crewOpen {
		f.add(a.setupCrewRows(width)...)
	}
	f.soft(rankSpacer, "")

	// ── the optional review ──
	f.add(a.setupReviewRow(width))
	if s.reviewOpen {
		rows, rest := a.setupReviewRows(width)
		f.add(rows...)
		f.soft(rankReviewRest, rest...)
	}
	f.soft(rankSpacer, "")

	// ── the way out ──
	f.add(a.setupStartRow(width))
	if s.refusal != "" {
		// A REFUSAL IS NEVER GIVEN UP. It is the one row on this screen that is
		// about something that just went wrong, and a window too short to show it
		// would leave a person pressing enter at a form that does nothing.
		for _, line := range wrap(s.refusal, width) {
			f.add(pal.warn(line))
		}
	}
	f.soft(rankSpacer, "")
	return f
}

// addControlWords adds a field's one-line explanation, and — only where `?` asked
// for it on the field with the focus — the detail under it.
func (a *app) addControlWords(f *controlsSheet, width int, control setupControl, word, detail string) {
	pal := a.pal
	indent := strings.Repeat(" ", 4)
	paint := func(text string, ink func(string) string) []string {
		lines := wrap(text, width-4)
		for i, line := range lines {
			lines[i] = indent + ink(line)
		}
		return lines
	}
	focused := a.setup.control == control
	rank := rankOtherWords
	if focused {
		rank = rankFocusedWords
	}
	// THE SENTENCE UNDER THE ACTIVE FIELD IS READ AND THE OTHERS ARE SCANNED, so
	// they are not painted the same. The focused one is at narration weight,
	// which is the tone this surface reads prose in; the other two drop to dim.
	// Everything on this screen was dim once and the whole form arrived at the
	// same faint weight — with the result that the only thing the eye found was
	// the example beside it, which is the one thing on the frame that is not the
	// decision. Dim is for what is beside the point, and the explanation of the
	// field somebody is standing on is the point.
	ink := pal.dim
	if focused {
		ink = pal.narr
	}
	f.soft(rank, paint(word, ink)...)
	if !focused || !a.setup.detail {
		return
	}
	// A DETAIL MAY BE TWO STATEMENTS AND THEN IT IS TWO PARAGRAPHS. The crew's is
	// the seats it is made of and then what a crew change does not touch, and
	// joining those with a `·` read as one sentence that had lost its way.
	for _, para := range strings.Split(detail, "\n") {
		if strings.TrimSpace(para) == "" {
			continue
		}
		f.soft(rankDetail, paint(para, pal.muted)...)
	}
}

// setupControlLead is the mark in front of a row: the cursor's own `›`, the same
// one every list on this surface draws, and two blanks for a row that has not
// got it. One selection accent on the screen, which is the design's whole rule
// about hierarchy here.
func (a *app) setupControlLead(control setupControl) string {
	if a.setup.control == control {
		return a.pal.accent(setupLead)
	}
	return "  "
}

// setupFieldRow is one label-and-value row of the form: the label in its column,
// the value in bold, and — where a row has one — a dim word for where the value
// came from.
func (a *app) setupFieldRow(width int, control setupControl, label, value, source string) string {
	pal := a.pal
	name := padTo(label, controlLabelWidth)
	if a.setup.control == control {
		name = pal.ink(name)
	} else {
		// A LABEL IS NEVER DECORATION. `Daily limit` is what tells a person what
		// the figure beside it means, and a form whose three labels were all at
		// telemetry weight was a form nobody could scan. Dim on this screen is
		// kept for where a value came from and for the keyboard legend — the
		// metadata around the decision, not the decision.
		name = pal.muted(name)
	}
	if value == "" {
		// THE EMPTINESS LAW. A value nobody has resolved yet draws as nothing at
		// all rather than as a placeholder claiming one.
		return a.setupControlLead(control) + name
	}
	room := max(width-2-controlLabelWidth, 1)
	if source != "" {
		room = max(room-ansi.StringWidth(source)-2, 1)
	}
	row := a.setupControlLead(control) + name + pal.bold(pal.ink(fit(value, room)))
	if source != "" {
		row += pal.dim("  " + source)
	}
	return row
}

// setupModelSource is where the conversation's model came from, said only where
// it is not simply this person's own saved choice.
//
// THE TWO ENVIRONMENT CASES ARE DIFFERENT AND ARE SPELLED DIFFERENTLY. A row the
// registry has PINNED cannot be edited here and says `set by`; CODEAF_MODEL is
// carried by the talk slot as an [Setting.EnvDefault], which only SEEDS a value
// nobody has chosen and is outranked by a choice made here — so it says `from`,
// and the list still opens (internal/config's chatmodel.go states that law).
func (a *app) setupModelSource() string {
	row, ok := a.registry().Row(config.ModelSettingKey(talkSlot))
	if !ok {
		return ""
	}
	if name, pinned := row.PinnedBy(); pinned {
		return controlPinnedLead + name
	}
	if row.EnvDefault == "" || config.ChatModelAt(a.profileDir) != "" {
		return ""
	}
	if strings.TrimSpace(env.Value(row.EnvDefault)) == "" {
		return ""
	}
	return controlFromLead + row.EnvDefault
}

// setupModelDetail is what `?` says about the chat model: its exact id, because
// the row shows a name, and then the sentence about what else the model does.
func (a *app) setupModelDetail() string {
	if id := strings.TrimSpace(a.model); id != "" {
		return id + " · " + controlModelDetail
	}
	return controlModelDetail
}

// setupCrewDetail is what `?` says about the crew: the three seats a person
// actually asks about, read off the profile's own live rows through
// internal/config's one summary, and then what a crew change does not touch.
//
// It is the LIVE reading and not the preset's table, which is the whole point on
// a profile whose crew is nobody's preset: `Custom` on the row, and the models it
// is actually made of underneath it.
func (a *app) setupCrewDetail() string {
	seats := ""
	if a.setup.crewPick != "" {
		// A PRESET TAKEN IN THE CHOOSER IS NOT ON DISK YET, so the seats come off
		// the preset's own table rather than off the profile — otherwise `?` on a
		// crew somebody has just chosen would describe the crew they replaced.
		// The three roles are spelled the way internal/config spells them for the
		// live reading below, because a person opening this twice must not be
		// shown one sentence in two shapes.
		seats = crewPickSeats(a.setup.crewSource, a.setup.crewPick)
	}
	if seats == "" {
		seats = strings.TrimSpace(config.CrewClasses(a.profileDir))
	}
	if seats == "" {
		return controlCrewDetail
	}
	return seats + "\n" + controlCrewDetail
}

// crewPickSeats is one preset's three named seats — `brain … · hands … · checks
// …` — built from [config.CrewModels] so a preset chosen a moment ago describes
// itself rather than the profile it has not been written to yet. It answers ""
// for a word that is not a preset, which is the emptiness law: there is no crew
// to describe and no placeholder that would be true.
func crewPickSeats(source, preset string) string {
	models, ok := config.CrewModelsForSource(source, preset)
	if !ok {
		return ""
	}
	short := func(tier string) string {
		id := strings.TrimSpace(models[tier])
		if at := strings.LastIndex(id, "/"); at >= 0 {
			id = id[at+1:]
		}
		return id
	}
	return "brain " + short(config.ModelTierMastermind) +
		" · hands " + short(config.ModelTierWorker) +
		" · checks " + short(config.ModelTierHigh)
}

// setupTaskPinRow is the line under the crew when `task.model` is pinned, and ""
// — the emptiness law — when it is not.
func (a *app) setupTaskPinRow(width int) string {
	row, ok := a.registry().Row(config.KeyTaskModel)
	if !ok {
		return ""
	}
	pinned := strings.TrimSpace(row.Value())
	if pinned == "" || pinned == row.EmptyLabel {
		return ""
	}
	return strings.Repeat(" ", 4) +
		a.pal.dim(fit(controlTaskPinLead+modelWord(pinned)+" · /settings changes that", width-4))
}

// setupLimitRow is the day's ceiling: the value being typed where somebody is
// typing one, the resolved reading where nobody is, and the variable's name
// where the environment owns the row. It answers the caret's column as well,
// because the caret belongs at the end of what has been typed.
func (a *app) setupLimitRow(width int) (string, int) {
	pal := a.pal
	s := &a.setup
	if row, ok := a.registry().Row(config.KeyDailyBudget); ok {
		if pin, pinned := row.PinnedBy(); pinned {
			return a.setupFieldRow(width, controlLimit, controlLimitLabel,
				a.setupLimitReading(), controlPinnedLead+pin), -1
		}
	}
	shown := a.setupLimitReading()
	if s.limitTyped {
		shown = s.limitText
	}
	room := max(width-2-controlLabelWidth, 1)
	shown = fit(shown, room)
	name := padTo(controlLimitLabel, controlLabelWidth)
	if s.control == controlLimit {
		name = pal.ink(name)
	} else {
		name = pal.dim(name)
	}
	at := ansi.StringWidth(setupLead) + controlLabelWidth + ansi.StringWidth(shown)
	return a.setupControlLead(controlLimit) + name + pal.bold(pal.ink(shown)), at
}

// setupModelRows is the list standing under the chat-model row: five rows of the
// catalog with the cursor's exact id under it, and a count that says how much
// more there is and how to reach it. A machine with no catalog at all still shows
// the model in use, so enter confirms rather than changes.
func (a *app) setupModelRows(width int) []string {
	pal := a.pal
	s := &a.setup
	models := a.setupModelChoices()
	if len(models) == 0 {
		if strings.TrimSpace(s.modelFind) != "" {
			return []string{strings.Repeat(" ", 4) + pal.dim(fit(setupNoMatchWord, width-4))}
		}
		return []string{strings.Repeat(" ", 4) + pal.dim(fit(setupNoCatalogWord, width-4))}
	}
	top := clampIndex(s.modelTop, max(len(models)-setupModelSlots+1, 1))
	out := make([]string, 0, setupModelSlots+2)
	for i := top; i < len(models) && i < top+setupModelSlots; i++ {
		name := modelWord(models[i].ID)
		if i == s.modelAt {
			out = append(out, "  "+pal.accent(setupLead)+pal.bold(pal.ink(fit(name, width-6))))
			// THE EXACT ID, UNDER THE ONE ROW IT IS ABOUT. The list reads as names
			// and the address is still on the screen for whoever needs it.
			out = append(out, strings.Repeat(" ", 6)+pal.dim(fit(models[i].ID, width-6)))
			continue
		}
		out = append(out, "    "+pal.dim(fit(name, width-6)))
	}
	out = append(out, strings.Repeat(" ", 4)+pal.dim(fit(a.setupModelCountWord(len(models)), width-4)))
	return out
}

// setupModelCountWord is the line under the list: where the cursor is in the
// whole of it, and — because a form cannot show two hundred rows — that typing
// narrows it. A filter that is already typed is shown instead of the invitation.
func (a *app) setupModelCountWord(count int) string {
	at := clampIndex(a.setup.modelAt, count) + 1
	where := itoa(at) + " of " + itoa(count)
	if find := strings.TrimSpace(a.setup.modelFind); find != "" {
		return where + " · matching " + find
	}
	return where + " · type to narrow"
}

// setupNoCatalogWord is what the list says where there is no catalog to choose
// from — no key yet, no cache, nothing fetched — and no model in use either. It
// names the door rather than the absence, because the absence is not something a
// person can act on.
const setupNoCatalogWord = "no model list on this machine yet · /model finds one once you are connected"

// setupNoMatchWord is a filter that matched nothing.
const setupNoMatchWord = "nothing matches · backspace widens it"

// crewChoiceWords is what each preset says about itself ON THIS SCREEN, and it
// is deliberately NOT [config.CrewLine].
//
// The registry's line is written for somebody who already has the vocabulary:
// `glm-flash works, checks and thinks · pennies a day` names one model three
// times and makes a claim about money. Both halves are wrong here. Nobody meeting codeaf for the
// first time can rank `glm-flash` against `kimi-k3`, and — the harder rule —
// THIS SCREEN MAKES NO CLAIM ABOUT WHAT ANYTHING COSTS. "Pennies a day" is a
// forecast about somebody else's usage on somebody else's pricing, said on the
// one screen that also asks them to name a daily ceiling, and a product that
// guesses at a person's bill next to a field where they set their own limit has
// put a number in their head that it cannot stand behind.
//
// So each line is about THE CHOICE — how much model is put on the work — in the
// comparative terms the three presets actually differ by. The five model ids are
// one keystroke away on `?`, which is where an address belongs.
var crewChoiceWords = map[string]string{
	config.CrewFrugal:   "The lightest models that can do the work.",
	config.CrewBalanced: "A stronger model plans and checks; lighter ones do the work.",
	config.CrewMax:      "The strongest models on the work and the thinking.",
}

// crewChoiceWord is one preset's line here, falling back to the registry's own
// for a preset this table has not been taught — which is the one-source rule
// applied to a gap: a new preset gets a real sentence rather than a blank row,
// and the blank row is what would have shipped if this map were the only source.
func crewChoiceWord(preset string) string {
	if word := crewChoiceWords[strings.ToLower(strings.TrimSpace(preset))]; word != "" {
		return word
	}
	// The fallback is for a preset this screen has not been taught, and it is the
	// default family's line: setupCrewRows runs inside the draw, so reading the profile
	// here would put a profile read on the frame clock, and the seats this screen
	// shows already take the family from a value read at the door.
	return config.CrewLine(preset)
}

// setupCrewRows is the chooser under the crew row: the three presets, each with
// its own compact sentence WHOLE — a comparison a person cannot finish reading is
// not a comparison — and a word about a crew that is none of the three.
func (a *app) setupCrewRows(width int) []string {
	pal := a.pal
	out := make([]string, 0, len(config.CrewPresets)*2+1)
	at := clampIndex(a.setup.crewAt, len(config.CrewPresets))
	for i, preset := range config.CrewPresets {
		lead, name := "    ", pal.dim(presetWord(preset))
		if i == at {
			lead, name = "  "+pal.accent(setupLead), pal.bold(pal.ink(presetWord(preset)))
		}
		out = append(out, lead+name)
		// The sentence is wrapped rather than cut, on its own line, so the three
		// read as three comparable things at any width this screen has.
		for _, line := range wrap(crewChoiceWord(preset), width-6) {
			out = append(out, strings.Repeat(" ", 6)+pal.muted(line))
		}
	}
	if a.setupCrewReading() == config.CrewCustom {
		// crew.go's own sentence for this state, said in the one place a person
		// can act on it.
		out = append(out, strings.Repeat(" ", 4)+pal.dim(fit(crewCustomLine, width-4)))
	}
	return out
}

// setupReviewRow is the row under the fields. Its wording depends on the profile
// and not on a guess: a profile that has written any of the three down is NOT
// told they are defaults.
func (a *app) setupReviewRow(width int) string {
	pal := a.pal
	long, short := controlReviewDefaults, controlReviewDefaultsShort
	if a.setupOtherSettingsWritten() {
		long, short = controlReviewYours, controlReviewYoursShort
	}
	word := long
	if ansi.StringWidth(long) > width-2 {
		word = short
	}
	if a.setup.control == controlReview {
		return a.setupControlLead(controlReview) + pal.ink(fit(word, width-2))
	}
	return a.setupControlLead(controlReview) + pal.dim(fit(word, width-2))
}

// setupOtherSettingsWritten reports whether this profile has chosen any of the
// three the review shows. It reads the registry's own record of what is written
// down ([config.Settings.PersistedKeys]) rather than comparing values to
// defaults, because a person who deliberately set a row to its default has still
// chosen it.
func (a *app) setupOtherSettingsWritten() bool {
	written := a.registry().PersistedKeys()
	for _, key := range setupReviewKeys {
		for _, have := range written {
			if have == key {
				return true
			}
		}
	}
	return false
}

// setupReviewKeys are the three the optional review shows, in the order it shows
// them. They are the three the design deliberately does NOT make into questions:
// memory is on and useful before anybody has an opinion, permissions cannot be
// configured before a person has seen a tool ask for something, and the countdown
// belongs beside the actual countdown.
var setupReviewKeys = []string{
	config.KeyMemoryEnabled,
	config.KeyToolApprovalMode,
	config.KeyTaskAutoApprove,
}

// setupReviewRows is the review itself: three rows read straight off the
// registry, so what it shows is what /settings shows and never a claim that a
// configured profile is on defaults. It answers the rows and the closing line
// separately, because the line is the one part a short window may give up.
func (a *app) setupReviewRows(width int) ([]string, []string) {
	pal := a.pal
	out := make([]string, 0, len(setupReviewKeys))
	// The label column is measured against the labels themselves rather than
	// borrowed from the fields above: `ask before running` is eighteen cells and
	// ran straight into its own value at the field column's seventeen, which is
	// how `ask before runningprompt` reached a screen.
	names := 0
	for _, key := range setupReviewKeys {
		if row, ok := a.registry().Row(key); ok {
			names = max(names, ansi.StringWidth(row.Label)+2)
		}
	}
	for _, key := range setupReviewKeys {
		row, ok := a.registry().Row(key)
		if !ok {
			continue
		}
		out = append(out, strings.Repeat(" ", 4)+pal.dim(padTo(row.Label, names))+
			pal.ink(fit(row.Reading(), max(width-4-names, 1))))
	}
	rest := []string{strings.Repeat(" ", 4) + pal.dim(fit(controlReviewRest, width-4))}
	return out, rest
}

// setupStartRow is the primary action, with the key that takes it on the right.
// It is a row of the same form rather than a bright panel: the accent on this
// screen belongs to whatever the person is standing on, and an action that
// glowed whether or not it had the focus would be two things competing to be the
// obvious one.
func (a *app) setupStartRow(width int) string {
	pal := a.pal
	word := controlStartWord
	if a.setup.control == controlStart {
		word = pal.bold(pal.ink(word))
	} else {
		word = pal.dim(word)
	}
	row := a.setupControlLead(controlStart) + word
	const cap = "enter"
	if width > ansi.StringWidth(controlStartWord)+len(cap)+6 {
		row = padTo(row, width-len(cap)) + pal.dim(cap)
	}
	return row
}

// setupControlsKeys is the legend at the foot: what the keys do RIGHT HERE, in
// the order a person needs them, and CUT BY WHOLE CLAUSES.
//
// A legend that ends `enter…` has taught nobody anything — it is the one row on
// the screen whose whole job is to be readable at any width, so the clauses are
// added in order of how badly they are needed and the first one that does not fit
// ends the line. What survives at forty columns is what enter does and how to
// leave.
func (a *app) setupControlsKeys(width int) string {
	s := &a.setup
	var parts []string
	if s.anyOpen() {
		// THE WAY OUT IS SECOND AND NOT LAST. At forty columns a legend has room
		// for about two clauses, and of everything a chooser could teach, "this
		// key leaves without choosing" is the one a person cannot guess.
		parts = []string{"enter takes it", "esc cancels", "↑↓ choose"}
		if s.modelOpen {
			parts = append(parts, "type to narrow")
		}
	} else {
		// THE TWO KEYS THAT DRIVE THE FORM COME FIRST, and `tab moves` is the
		// second of them. At forty columns the legend has room for two clauses,
		// and a person who has been told only what enter does and how to go back
		// has been told everything except how to reach the other four rows —
		// which is the one thing this screen cannot be completed without. The way
		// out is third and appears from sixty columns up.
		switch s.control {
		case controlLimit:
			parts = []string{"enter goes on", "tab moves", a.setupBackWord(), "type an amount or none"}
		case controlChatModel, controlCrew:
			parts = []string{"enter opens the list", "tab moves", a.setupBackWord()}
		case controlReview:
			parts = []string{"enter shows them", "tab moves", a.setupBackWord()}
		case controlStart:
			parts = []string{"enter starts", "tab moves", a.setupBackWord()}
		}
		if s.control <= controlCrew {
			parts = append(parts, "? detail")
		}
		if w, _ := a.size(); w >= setupWideCols {
			parts = append(parts, "←→ examples")
		}
	}
	return clausesWithin(parts, width)
}

// clausesWithin joins as many whole clauses as fit, in the order they are given.
// It never cuts one in half, which is the whole reason it is not [fit].
func clausesWithin(parts []string, width int) string {
	out := ""
	for _, part := range parts {
		next := part
		if out != "" {
			next = out + legendJoin + part
		}
		if ansi.StringWidth(next) > width {
			break
		}
		out = next
	}
	return out
}

// setupBackWord is what esc does from this screen, said honestly: a screen with
// something before it goes back to it, and a screen that is the whole of the
// setup skips the setup — which is what esc has always done here and what the
// note left behind names the doors for ([app.endSetup]).
func (a *app) setupBackWord() string {
	if a.setup.at > 0 {
		return "esc back"
	}
	return setupSkipKeysWord
}

// ── the demonstration panel on the right ────────────────────────────────────
//
// A FRAME THAT IS THERE TO SAY "NOT YOURS".
//
// Nothing about the form on the left has an edge. This panel is framed, and the
// reason is the one thing a frame is actually good at: it separates a thing
// from its surroundings. Unframed, the right-hand column read as a SECOND
// COLUMN OF THE FORM — more instructions, in the same voice, about the fields
// on the left. A frame with a label on its top edge cannot be read that way.
// Everything inside it is an illustration, and the frame is what says so before
// a word is read.
//
// It is drawn by the one frame (frame.go), which is also what a question hangs
// in and what the two sheets raised over the page wear — this panel's header
// used to call it "the one border on this surface" while two others shipped
// beside it, each with its own copy of the corner pieces.

// The panel's own words.
//
// THE TITLE IS THE WHOLE HONESTY MECHANISM and it is two facts joined: this is
// an EXAMPLE, and it is about WHAT YOU CAN DO. Neither half survives alone —
// "example" with nothing after it invites the question "an example of what?",
// and "what you can do" with nothing before it is a claim about this machine.
//
// showcaseHonestWord is the second guard, at the foot, in the plainest words
// available: a person who has watched three lines appear in order has watched
// something that LOOKS like a run, and the panel says outright that it was not
// one. Neither line is decoration and neither is dropped before the leads are.
const (
	showcaseTitleWord  = "Example · what you can do"
	showcaseHonestWord = "An illustration. Nothing here has run."
)

// showcaseCaret is the block that trails the request while it is being typed
// out. It is a DRAWN caret and not the terminal's — the real one belongs to the
// amount field on the left and never leaves it — so it has an ascii floor of its
// own like every other glyph on this surface.
const (
	showcaseCaret      = "▌"
	showcaseCaretASCII = "_"
)

// setupShowcase draws the right-hand column: a framed panel holding one example
// request and what it leads to, with where in the four it is on the bottom edge.
//
// It is drawn LOWER CONTRAST than the form beside it — nothing in it is at ink
// weight except the request being typed — because it is an illustration standing
// beside the thing a person came here to do, and the active control on the left
// is the anchor.
//
// The panel is a FIXED HEIGHT for a given example: the rows the leads will land
// in are drawn empty before they arrive, so the frame that is on screen at the
// first beat is the same size as the frame at the last. A box that grew three
// rows while somebody was reading the form would be motion in the corner of
// their eye that means nothing.
func (a *app) setupShowcase(width, height int) []string {
	pal := a.pal
	if len(setupExamples) == 0 || height <= 0 || width < 20 {
		return nil
	}
	at := clampIndex(a.setup.example, len(setupExamples))
	example := setupExamples[at]
	inner := frameInner(width) - 2 // one cell of air inside each edge

	body := a.showcaseBody(example, inner)
	// The frame comes off the top of what the window has left, whole rows at a
	// time, and the leads go before the title does: a panel with no title is
	// still labelled by its own top edge, where a panel with no request is a
	// panel about nothing.
	for len(body)+2 > height && len(body) > 0 {
		body = body[:len(body)-1]
	}
	if len(body)+2 > height {
		return nil
	}

	rows := make([]string, 0, len(body))
	for _, line := range body {
		rows = append(rows, " "+padTo(line, inner)+" ")
	}
	block, _ := framed{
		title:     pal.dim(showcaseTitle(pal)),
		keysAside: pal.dim(setupShowcaseCount(at, len(setupExamples))),
	}.draw(pal, width, rows)

	// showcaseTop is where the panel begins: level with the first field, which is
	// three rows into the form (heading, lead, blank). A short window has dropped
	// those, and the panel comes up with them rather than floating.
	showcaseTop := min(3, max(height-len(block), 0))
	out := make([]string, height)
	for i, line := range block {
		if showcaseTop+i >= height {
			break
		}
		out[showcaseTop+i] = line
	}
	return out
}

// showcaseBody is what stands inside the frame, at the inner width, with the
// demonstration at whatever beat it has reached.
func (a *app) showcaseBody(example setupExample, inner int) []string {
	pal := a.pal
	caret := showcaseCaret
	if pal.ascii {
		caret = showcaseCaretASCII
	}
	rows := make([]string, 0, 16)
	rows = append(rows, pal.muted(fit(example.title, inner)), "")

	// THE REQUEST, TYPED. It is drawn behind this surface's own `you` marker,
	// which is the mark the transcript opens a person's own line with — so what
	// the panel is showing is unmistakably a thing somebody TYPED, in the box,
	// rather than a thing the screen is reporting.
	typed, typing := a.showcaseTyped(example)
	full := wrap(example.ask, inner-2)
	shown := wrap(typed, inner-2)
	// The marker opens the request and the wrapped remainder lines up under it.
	// A `›` on every row would read as three separate requests.
	lead := func(row int) string {
		if row == 0 {
			return pal.dim(pal.youGlyph())
		}
		return strings.Repeat(" ", ansi.StringWidth(pal.youGlyph()))
	}
	for i := range full {
		switch {
		case i < len(shown)-1:
			rows = append(rows, lead(i)+pal.ink(shown[i]))
		case i == len(shown)-1:
			line := pal.ink(shown[i])
			if typing {
				line += pal.accent(caret)
			}
			rows = append(rows, lead(i)+line)
		default:
			// The row the rest of the request will land in, held open.
			rows = append(rows, "")
		}
	}
	rows = append(rows, "")

	// WHAT IT LEADS TO, one line at a time, each a SHAPE of a result and never a
	// result: "work you can walk away from" is true of every run of that request,
	// where "41 files read" would be a claim about a run that has not happened.
	revealed := a.showcaseRevealed(example)
	for i, word := range example.leads {
		lines := wrap(word, inner-2)
		for j, line := range lines {
			if i >= revealed {
				rows = append(rows, "")
				continue
			}
			mark := "  "
			if j == 0 {
				mark = glyphIdle + " "
			}
			rows = append(rows, pal.dim(mark+line))
		}
	}
	rows = append(rows, "")
	for _, line := range wrap(showcaseHonestWord, inner) {
		rows = append(rows, pal.dim(line))
	}
	return rows
}

// showcaseTitle is the panel's label, written into its top edge by the frame,
// behind the one mark on it. A label on an edge is a label that cannot be
// mistaken for content.
func showcaseTitle(pal palette) string {
	// THE MARK IS THIS SURFACE'S OWN GLYPH FOR "NOTHING IS TURNING"
	// (tokens.GQueued, the empty circle that is deliberately not a spinner), which
	// is exactly what this panel is. Borrowing it rather than inventing a shape
	// keeps one vocabulary, and it means the one glyph on the frame agrees with
	// the sentence at its foot.
	return pal.glyph(tokens.GQueued) + " " + showcaseTitleWord
}

// setupShowcaseCount is the position line on the panel's bottom edge —
// `←  3 / 4  →`. It is drawn whether or not the arrows have been pressed,
// because a control nobody can see is a control nobody has.
func setupShowcaseCount(at, count int) string {
	return "←  " + itoa(at+1) + " / " + itoa(count) + "  →"
}

// ── the one-shot demonstration ──────────────────────────────────────────────
//
// THE PANEL PLAYS ONCE, ON A DELIBERATE ACT, AND THEN IT IS STILL.
//
// The request types itself out and the three lines under it arrive in order,
// which is the whole of it: about a second and a third, six beats of typing and
// one per line. It is armed by exactly two things — arriving on this screen, and
// a person moving the focus or browsing the examples with ←/→. Nothing else
// starts it, nothing repeats it, and there is no clock anywhere on this screen
// that switches examples by itself: a carousel on a setup screen is motion
// competing with the decision a person is trying to make.
//
// AND ANY OTHER KEY SETTLES IT AT ONCE. Typing an amount, narrowing the model
// list, opening a chooser — every one of those jumps the panel straight to its
// finished state rather than freezing it half-drawn, because a half-drawn panel
// beside a field somebody is typing into is a thing that looks broken. This is
// the "pause the moment a person types" rule as a settle, which is the version
// of it that leaves a screen you can read.
//
// THE SCREEN-READER TIER NEVER ANIMATES. Under [app.linear] the panel is built
// finished on the first frame and no tick is ever armed, so what is read aloud
// is one static illustration and not the same three lines announced again as
// each arrives (styles.go's account of the linear tier).
//
// THE FORM IS UNTOUCHED BY ALL OF IT. A beat advances one integer on the flow
// and repaints; it moves no focus, writes nothing, clears no pending amount and
// never takes the caret, which stays in the field on the left the whole time.

// setupDemoMsg is one beat of the demonstration, stamped with the generation
// that asked for it so a beat left over from a previous example is dropped
// rather than driving the current one.
type setupDemoMsg struct{ gen int }

const (
	// setupDemoTypeBeats is how many beats the request takes to type itself out.
	// Six is enough that it reads as typing rather than as a paste, and few
	// enough that the whole demonstration is over before somebody who ignored it
	// has finished reading the field they are standing on.
	setupDemoTypeBeats = 6
	// setupDemoBeat is the interval between beats.
	setupDemoBeat = 150 * time.Millisecond
)

// setupDemoLast is the beat at which the current example is fully drawn.
func (a *app) setupDemoLast() int {
	at := clampIndex(a.setup.example, len(setupExamples))
	if len(setupExamples) == 0 {
		return 0
	}
	return setupDemoTypeBeats + len(setupExamples[at].leads) - 1
}

// showcaseTyped is how much of the request has been typed at this beat, and
// whether it is still being typed — which is what puts the drawn caret at the
// end of it.
func (a *app) showcaseTyped(example setupExample) (string, bool) {
	runes := []rune(example.ask)
	if a.setup.demoAt >= setupDemoTypeBeats-1 {
		return example.ask, false
	}
	shown := len(runes) * (a.setup.demoAt + 1) / setupDemoTypeBeats
	return string(runes[:shown]), true
}

// showcaseRevealed is how many of the lines under the request have arrived.
func (a *app) showcaseRevealed(example setupExample) int {
	if a.setup.demoAt < setupDemoTypeBeats {
		return 0
	}
	return min(a.setup.demoAt-setupDemoTypeBeats+1, len(example.leads))
}

// restartSetupDemo plays the panel from the top. It is called on the two
// deliberate acts and nowhere else.
func (a *app) restartSetupDemo() {
	s := &a.setup
	s.demoGen++
	s.demoTicking = false
	if a.linear {
		s.demoAt = a.setupDemoLast()
		return
	}
	s.demoAt = 0
}

// settleSetupDemo puts the panel straight into its finished state and stops the
// clock. Any key that is not one of the two deliberate acts lands here.
func (a *app) settleSetupDemo() {
	s := &a.setup
	if last := a.setupDemoLast(); s.demoAt < last {
		s.demoGen++
		s.demoTicking = false
		s.demoAt = last
	}
}

// setupDemoCmd arms the next beat, and answers nil in every state where there
// should not be one: off this screen, already finished, already ticking, or in
// the screen-reader tier. It is the ONE place a beat is asked for, so a second
// clock cannot be started beside the first.
func (a *app) setupDemoCmd() tea.Cmd {
	s := &a.setup
	if !s.open || len(s.steps) == 0 || s.step() != setupControls || a.linear {
		return nil
	}
	if s.demoTicking || s.demoAt >= a.setupDemoLast() {
		return nil
	}
	s.demoTicking = true
	gen := s.demoGen
	return surfaceTick(setupDemoBeat, func(time.Time) tea.Msg { return setupDemoMsg{gen: gen} })
}

// setupDemoBeatAt is one beat arriving. A beat from a previous generation is
// dropped whole: it neither advances the panel nor arms another.
func (a *app) setupDemoBeatAt(gen int) tea.Cmd {
	s := &a.setup
	if gen != s.demoGen {
		return nil
	}
	s.demoTicking = false
	if !s.open || len(s.steps) == 0 || s.step() != setupControls {
		return nil
	}
	if s.demoAt >= a.setupDemoLast() {
		return nil
	}
	s.demoAt++
	a.touch()
	return a.setupDemoCmd()
}

// sortStrings is the one small thing the crew detail needs and nothing else here
// does. It is written out rather than reached for because the list is five long
// and the sort is a stable, obvious one.
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
