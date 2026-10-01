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
// UNDERSTOOD BEFORE THE PERSON HAS USED codeaf. Two pass — the day's limit
// (money is consequential and an amount can be chosen with no knowledge of the
// engine) and the chat model — and everything else is a setting or a thing to
// be taught at the moment it happens. THE CREW USED TO BE THE THIRD, as a
// choice between three preset words; since 2026-09-24 a task's crew is picked
// per task and asks nothing up front, so it left this screen (/crew is where
// its pins, allowed models and cap live).
//
// SIX LAWS HOLD THIS SCREEN TOGETHER.
//
//   - EVERY VALUE ON IT IS THE RESOLVED ONE. Nothing here is a figure this file
//     knows: the limit and the model are read back through the settings
//     registry and internal/config, so a profile that already has a limit sees
//     THEIR limit and not the default, and an environment variable that owns a
//     row is named rather than quietly overwritten.
//   - THE DEFAULTS ARE ALREADY CHOSEN. Every row opens on the value that is in
//     force, so a person who wants none of this presses enter on `Start a
//     conversation` and has agreed to exactly what they were shown. That is the
//     whole difference between a form and an interrogation.
//   - IT WRITES ONLY WHAT IT WAS GIVEN. Leaving this screen writes the day's
//     limit, because the limit is the field it asked about, and nothing else.
//   - CANCELLING CHOOSES NOTHING. Esc out of the model list leaves the value that
//     was there before it opened. A cursor is not an answer.
//   - THE SCREEN IS THREE SENTENCES LONG. Each control carries one line about
//     what it does and nothing else; the caveats and the exact model ids are
//     behind `?`, on the field a person is standing on. A first
//     screen that has to be read before it can be answered is a first screen most
//     people leave.
//   - IT SPENDS NOTHING. Opening a model list reads the catalog this process
//     already has; nothing on this screen sends a prompt or calls a model.
//
// The example under the form is labelled as an example. It is one request
// somebody could make and the kind of result it leads to, and it moves only when
// the person moves — a deliberate focus change or the arrow keys, never a timer
// and never a keystroke inside a field. No invented cost, no fabricated
// activity, no claim that anything has already run.

// setupControl is one row of the controls screen, in the order tab walks them.
// The day's limit comes first because it is the consequential one; the two model
// rows are grouped under it because they are one subject.
type setupControl int

const (
	controlLimit setupControl = iota
	controlChatModel
	controlStart
)

// setupControlCount is how many rows tab walks. It is derived from the last
// control rather than written down, so adding one cannot leave the walk short.
const setupControlCount = int(controlStart) + 1

// ── what the screen says ────────────────────────────────────────────────────

// The heading. It is ONE LINE, and it names what the rows under it are —
// settings, the basic ones — because the keys line stands directly under it
// and a second sentence of lead-in between the two was a sentence nobody read
// on their way to the first row. (It was `Models and spending` over `Keep
// these choices or change them.` until 2026-10-01.)
const controlsTitle = "Basic settings"

// The three field labels, laid out in one column so their values line up.
const (
	controlLimitLabel = "Daily limit"
	controlModelLabel = "Chat model"
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
	controlLimitWord = "When all " + product + " spends today reaches this amount, " +
		"new work waits until midnight or you raise it. /budget changes it later."
	controlModelWord = "The model you talk to in this conversation. /model changes it later."
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
		"running can carry it a little past. Your provider account has its own controls. " +
		"Task crews also have a daily cap of their own, set in /crew."
	controlModelDetail = "It also handles this conversation's tool use. Changing it here is " +
		"the same choice /model makes, and it is kept for the next launch." +
		" Tasks get their own crew, picked per task · /crew shows it."
)

// controlStartWord is the way out, and it is named for what happens next rather
// than for the form being finished. "Done" and "Save" describe the screen;
// this describes the person's day.
const controlStartWord = "Start a conversation"

// The note under the way out: where everything this screen does not ask about
// lives. It is one dim sentence with the command in it painted as the
// composer's chip, so the one word a person can act on is the one word that
// stands out. IT REPLACED A ROW. Until 2026-10-01 `Other settings use defaults
// · review` stood between the chat model and the way out and opened three
// read-only rows; a row that shows settings and lets nobody change them was a
// door painted on a wall, and people pressed on it.
const (
	controlSettingsNoteLead    = "Everything else is in "
	controlSettingsNoteCommand = "/settings"
)

// controlPinnedLead prefixes a value an environment variable owns, and
// controlFromLead a value one merely SEEDED. The two are different facts and the
// screen may not spell them the same way: a pinned row cannot be edited here at
// all, where a seeded one can and the choice outranks the variable on the next
// launch (internal/config's chatmodel.go states that law).
const (
	controlPinnedLead = "set by "
	controlFromLead   = "from "
)

// controlTaskPinLead is the line under the chat model when a task-model override is in
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
		if s.modelOpen {
			s.closeChoosers()
			return true
		}
		if s.detail {
			s.detail = false
			return true
		}
		return false

	case "tab", "down", "ctrl+n":
		if s.modelOpen && name != "tab" {
			a.moveSetupModel(1)
			return true
		}
		s.focusControl(a, 1)
		return true

	case "shift+tab", "up", "ctrl+p":
		if s.modelOpen && name != "shift+tab" {
			a.moveSetupModel(-1)
			return true
		}
		s.focusControl(a, -1)
		return true

	case "left", "right":
		// THE EXAMPLES ARE BROWSED WITH THESE TWO KEYS AND TURN BY THEMSELVES
		// OTHERWISE ([app.setupTurnAt]). They change nothing about the profile,
		// and they are the panel's own keys and nothing else's: the focus never
		// moves the example (it did until 2026-10-01, which read as the panel
		// jumping about under a person walking the rows). Inside the model list
		// they are not free — the filter box has the keyboard — so the list
		// keeps them and does nothing.
		if s.modelOpen {
			return true
		}
		delta := 1
		if name == "left" {
			delta = -1
		}
		a.turnSetupExample(delta)
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
	if s.control == controlLimit && !s.anyOpen() {
		s.limitText += text
		s.limitTyped = true
	}
	return true
}

// wrapCursor walks a ring of count rows by delta, coming round at both ends —
// the examples' walk, where [moveCursor]'s clamp is a list's.
func wrapCursor(cursor, delta, count int) int {
	if count <= 0 {
		return 0
	}
	return ((cursor+delta)%count + count) % count
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
func (s *setupFlow) anyOpen() bool { return s.modelOpen }

// closeChoosers puts both away and drops what was provisional in them: the model
// list's filter and cursor. It has written nothing —
// that is what makes esc safe here.
func (s *setupFlow) closeChoosers() {
	s.modelOpen = false
	s.modelFind = ""
	s.modelTop = 0
}

// focusControl walks the rows, closing whatever was open on the way. THE
// EXAMPLE DOES NOT FOLLOW THE FOCUS: it turns on its own clock and on ←/→, and
// nothing a person does to the rows moves it ([app.setupTurnAt]).
func (s *setupFlow) focusControl(a *app, delta int) {
	s.closeChoosers()
	// The detail belongs to the field it was asked about and goes with it.
	s.detail = false
	s.control = setupControl(moveCursor(int(s.control), delta, setupControlCount))
	s.refusal = ""
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
		s.answered[controlLimit] = true
		s.focusControl(a, 1)
		return false

	case controlChatModel:
		if s.modelOpen {
			models := a.setupModelChoices()
			if s.modelAt >= 0 && s.modelAt < len(models) {
				a.takeSetupModel(models[s.modelAt].ID)
			}
			s.closeChoosers()
			// AND THEN ENTER GOES ON, as it does on the limit. A model taken
			// from the list is this row answered; an earlier build left the
			// focus on it, so the next enter opened the list again and a person
			// pressing enter to get through the form never got past this row.
			// A refusal keeps the focus here so it can be read.
			if s.refusal == "" {
				s.answered[controlChatModel] = true
				s.focusControl(a, 1)
			}
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
	}
	// `Start a conversation` — everything the screen holds is written down, and
	// a row that refused keeps the screen up with its refusal on it.
	return a.commitSetupControls()
}

// ── the pointer ─────────────────────────────────────────────────────────────

// setupDoors is what the last frame of the controls screen drew for the pointer:
// a door per body row, and where the body stands in the window. It is written by
// [app.setupControlsFrame] and read by [app.setupPress], so a press is answered
// against the rows a person can see rather than against a form rebuilt from
// state that may have moved under them.
type setupDoors struct {
	rows []setupDoor
	// top is the frame row the first body row is on; left and width are the
	// form's columns, so a press on the example column beside the form — which
	// is an illustration — selects nothing.
	top, left, width int
}

// setupPress is a left press on the controls screen, answered the way the keys
// would have answered it. It reports whether the screen is finished.
//
// THE SCREEN USED TO SWALLOW EVERY PRESS, on the argument that the setup was
// three keystrokes. It is a form now, with rows that look like rows and a list
// that looks like a list, and the first thing a person who sees a list does is
// click on it. So a press on a control is a tab to it, and a press on a row that
// enter would act on — the model field, the way out, or one model of
// the open list — is that enter. The limit row is only focused, because what a
// press on an amount means is "I want to type here". A press anywhere else on
// the screen — a sentence, a blank, the example — does nothing, which is what a
// press on words should do.
func (a *app) setupPress(x, y int) bool {
	s := &a.setup
	if s.step() != setupControls {
		return false
	}
	d := s.doors
	row := y - d.top
	if row < 0 || row >= len(d.rows) || x < d.left || x >= d.left+d.width {
		return false
	}
	door := d.rows[row]
	switch door.kind {
	case doorModel:
		models := a.setupModelChoices()
		if door.model < 0 || door.model >= len(models) {
			return false
		}
		s.modelAt = door.model
		a.touch()
		return a.setupControlsEnter()
	case doorControl:
		if s.control != door.control {
			// A tab to the row, which closes whatever was open on the way and
			// moves the example with it ([setupFlow.focusControl]).
			s.focusControl(a, int(door.control)-int(s.control))
		} else if s.modelOpen && door.control == controlChatModel {
			// A press on the field whose list is open puts the list away and
			// chooses nothing, which is esc's rule: a cursor is not an answer.
			s.closeChoosers()
			a.touch()
			return false
		}
		if door.control == controlLimit {
			a.touch()
			return false
		}
		a.touch()
		return a.setupControlsEnter()
	}
	return false
}

// setupWheel is the wheel over the controls screen. It turns the open model
// list — the one thing on the screen that scrolls — and nothing else.
func (a *app) setupWheel(down bool) {
	s := &a.setup
	if s.step() != setupControls || !s.modelOpen {
		return
	}
	delta := -1
	if down {
		delta = 1
	}
	a.moveSetupModel(delta)
	a.touch()
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

// commitSetupControls is `Start a conversation`. It reports whether the screen is
// finished.
//
// THE CHAT MODEL IS ALREADY WRITTEN BY THE TIME THIS RUNS, and deliberately: a
// model is taken at the moment it is chosen in the list, through the registry row
// every other model change goes through, so what this screen leaves behind is the
// live conversation on that model rather than a promise to switch later. A person
// who opened the list and pressed esc chose nothing and nothing was written.
func (a *app) commitSetupControls() bool {
	return a.commitSetupLimit()
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
	if a.setupFreeOnly() {
		// ONLY THE FREE ROWS WHILE THE ACCOUNT READS LOW. The warning under the
		// message box says some models may not be available; on this screen,
		// where a person who has never run the program is choosing by name, a
		// list of three hundred paid models they cannot use is a list they will
		// pick the wrong row from (five new people did, 2026-09-30). The same
		// test the warning uses decides a row ([app.paidCreditModel]): a `:free`
		// id, a catalog row priced at zero, or a model another service serves.
		free := make([]Model, 0, len(models))
		for _, model := range models {
			if !a.paidCreditModel(model.ID, models) {
				free = append(free, model)
			}
		}
		models = free
	}
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

// setupFreeOnly reports whether the model list is cut to free rows: the default
// provider's account is known low, by the same reading the warning under the
// message box follows ([app.refreshCreditWarnings]). It is read live rather than
// at seed time because the balance is read AFTER the key lands, on its own
// goroutine, and usually answers while this screen is already up.
func (a *app) setupFreeOnly() bool {
	return a.readCredits != nil && a.creditsLow && !a.creditsExpired
}

// setupKeyExpired reports whether the default provider refused the key as
// expired at the last read. The list is NOT cut for it — the free rows fail on
// the same key — but the line under the field says what is wrong and where the
// fix is.
func (a *app) setupKeyExpired() bool {
	return a.readCredits != nil && a.creditsExpired
}

// setupModelSlots is how many models the list SHOWS AT ONCE, and it is a height
// budget rather than a limit on the catalog: the cursor scrolls the rest past it
// and typing narrows them. Five is what a twenty-four-row window has to spare
// under a field that still needs the primary action beneath it.
const setupModelSlots = 5

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
	s.detail = false
	// The panel opens on the first example and turns from there.
	s.example = 0
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

// setupExamples are the five, in the order the panel turns through them and
// ←/→ walk them. None is tied to a row: the panel is an invitation rather than
// a caption, and it turns on its own clock ([app.setupTurnAt]).
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
	{
		// THE OTHER EXAMPLE THAT SPELLS A COMMAND. `/senior-dev <brief>` hands the
		// whole brief to the autonomous coding agent codeaf carries, and the
		// manual's account of it (senior-dev.md) is what the three lines are held
		// to: it writes the brief down word for word, works in a private copy on
		// a branch of its own, and hands back a change it has built and tested on
		// a frozen tree. Nothing here says "approved" or "merged" — it submits,
		// and what happens to the branch is the person's.
		title: "Hand off complex coding tasks",
		ask:   "/senior-dev Add retries with backoff to the HTTP client, with tests.",
		leads: []string{
			"Your brief written down word for word",
			"Work on a branch of its own, step by step",
			"A change built and tested, handed back",
		},
	},
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

// ── the drawing ─────────────────────────────────────────────────────────────

// The composition, in cells: ONE COLUMN. The example panel stands at the top,
// at the composition's full width, so the request and the three lines it leads
// to each stand on one row instead of wrapping inside a thirty-six-cell box;
// two blank rows under it; then the keyboard legend with the form directly
// under it, because the legend is about the form and the two read as one
// object when nothing stands between them. Until 2026-10-01 the example was a
// second column to the right of the form, drawn only from 112 columns up and
// level with the first field; on a tall window that left the whole lower half
// of the screen empty while the panel squeezed its sentences three ways, and
// the first move — under the form — put the legend between the form and the
// panel, where it read as a caption for the wrong one. The panel takes the rows
// the form leaves, WHOLE OR NOT AT ALL: a window with no rows to spare draws
// the legend and the form alone, from the top.
const (
	// setupFormWidth is the form's width, a comfortable measure for a sentence
	// and no wider.
	setupFormWidth = 64
	// setupShowcaseWidth is the example panel's width on a window that has it —
	// the old pair's own width, which the two columns used to share — and
	// setupShowcaseMinWidth is the least a panel is drawn at, which is the width
	// the design gave the old column.
	setupShowcaseWidth    = 92
	setupShowcaseMinWidth = 36
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
	s := &a.setup
	form := min(width-2*setupMargin, setupFormWidth)
	if form < 20 {
		form = max(width-2, 1)
	}
	show := min(width-2*setupMargin, setupShowcaseWidth)
	if show < setupShowcaseMinWidth {
		show = 0
	}
	// The composition is centred on the wider of the two, and the form keeps to
	// its own measure inside it: a sentence set ninety cells wide is a sentence
	// nobody reads to the end, where a panel that wide is a panel whose lines
	// do not wrap.
	pair := max(form, show)
	lead := max((width-pair)/2, 0)
	pad := strings.Repeat(" ", lead)

	// The header: the product on the left and where this is in the flow on the
	// right, both dim. It is one row and it is the only chrome the screen has.
	head := a.setupControlsHead(pair)

	// THE LEGEND IS MEASURED AGAINST THE WHOLE COMPOSITION AND NOT THE FORM. It
	// is the only row that has nothing beside it — the example column stops well
	// above the foot — so budgeting it at the form's fifty-four cells cut
	// `←→ examples` off the one screen the arrows exist on.
	sheet := a.setupControlsForm(form, pair)
	// The rows the window cannot have are given up WHOLE BLOCK AT A TIME, in the
	// order the form itself ranked them. A wrapped sentence cut off in the middle
	// is worse than a sentence that is not there. Two rows are spoken for before
	// the form gets any: the header and the blank under it.
	body, doors, caretRow := sheet.trim(max(height-2, 1))
	// THE DOORS ARE KEPT WITH THE FRAME THAT DREW THEM, so a press reads the
	// rows that are actually on the screen: body row i is frame row top+i,
	// where top is settled below once the panel and the legend are placed, and
	// it spans the form's own columns.
	s.doors = setupDoors{rows: doors, left: lead, width: form}

	// THE EXAMPLE TAKES THE ROWS THE FORM LEAVES, and only the whole of it.
	// Two rows are spoken for around the form — the header and the blank under
	// it — and the panel needs its own rows plus the two blank ones under it; a
	// panel that does not fit whole in what is left is not drawn, because half
	// a panel is a panel whose request has lost what it leads to.
	var block []string
	if show > 0 {
		block = a.setupShowcaseBlock(show, height-2-len(body)-setupShowcaseGap)
	}

	lines := make([]string, 0, height)
	lines = append(lines, pad+head, "")
	for _, line := range block {
		lines = append(lines, pad+line)
	}
	for i := 0; len(block) > 0 && i < setupShowcaseGap; i++ {
		lines = append(lines, "")
	}
	// The form begins here — its heading, the keys line under it, then the
	// rows: the row a press is measured from, and the caret's.
	top := len(lines)
	s.doors.top = top
	for _, line := range body {
		lines = append(lines, pad+line)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	// THE ACCOUNT'S WARNING RIDES THE FOOT, whole or not at all, and only on a
	// foot the form left empty: a window the form fills to its last row keeps
	// that row, because a form row is a thing a person acts on and the warning
	// is read again under the message box a moment later.
	if warning := a.setupFootWarning(); warning != "" && height > 0 && lines[height-1] == "" &&
		ansi.StringWidth(warning)+3 <= width {
		lines[height-1] = withCreditWarning("", 0, width, warning, a.pal)
	}
	caretY := caretRow + top
	a.caret = caretRow >= 0 && caretY < height
	if !a.caret {
		return lines, 0, 0
	}
	return lines, lead + sheet.caretX, caretY
}

// setupShowcaseGap is the blank rows between the example panel and the legend:
// enough that the panel reads as its own object above the form, and the legend
// as the form's.
const setupShowcaseGap = 2

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
	// door is what each row is a door onto for the pointer, row for row with
	// rows: the control a press on it focuses, or the model it chooses. A row
	// that is only words carries the zero value, which is a row a press lands on
	// and nothing happens.
	door []setupDoor
	// ranked is every droppable block with how willingly it goes.
	ranked  []controlsRank
	next    int
	caretAt int
	caretX  int
}

// setupDoor is what one row of the form means to a press. A row is one of three
// things: nothing (a sentence, a blank, the count under the list), a control
// (its label-and-value row, or the primary action), or one model of the open
// list — in which case model is its index into [app.setupModelChoices].
//
// IT IS RECORDED WHILE THE FORM IS BUILT AND NOT RECOMPUTED FROM A CLICK,
// because the form is not a fixed shape: a short window gives up whole blocks
// ([controlsSheet.trim]) and an open list or review adds rows under a field, so
// the only thing that knows which control is on row nine is the pass that put it
// there. The frame keeps the doors it drew ([setupFlow.doors]) and a press reads
// them back — the same memo-at-draw-time rule every other page's press obeys.
type setupDoor struct {
	control setupControl
	model   int
	kind    setupDoorKind
}

// setupDoorKind is which of the three a row is.
type setupDoorKind int

const (
	doorNone setupDoorKind = iota
	doorControl
	doorModel
)

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
	// rankNote is the sentence under the way out, which a short window gives
	// up right after the blank rows: it points somewhere else, and a person on
	// a short window has the rest of the form to read first.
	rankNote
	rankOtherWords
	rankFocusedWords
	rankDetail
)

func (f *controlsSheet) add(lines ...string) {
	for _, line := range lines {
		f.rows, f.block, f.door = append(f.rows, line), append(f.block, 0), append(f.door, setupDoor{})
	}
}

// open adds rows that are never given up AND are a door for the pointer: a
// control's own row, or one model of the list.
func (f *controlsSheet) open(door setupDoor, lines ...string) {
	for _, line := range lines {
		f.rows, f.block, f.door = append(f.rows, line), append(f.block, 0), append(f.door, door)
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
		f.rows, f.block, f.door = append(f.rows, line), append(f.block, id), append(f.door, setupDoor{})
	}
}

// trim gives the window back the rows it does not have, block by block, and
// carries the caret and the doors with it.
func (f *controlsSheet) trim(height int) ([]string, []setupDoor, int) {
	if len(f.rows) <= height {
		return f.rows, f.door, f.caretAt
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
	doors := make([]setupDoor, 0, len(f.rows))
	caret := f.caretAt
	for at, line := range f.rows {
		if gone[f.block[at]] {
			if f.caretAt >= 0 && at < f.caretAt {
				caret--
			}
			continue
		}
		out = append(out, line)
		doors = append(doors, f.door[at])
	}
	if len(out) > height {
		// EVERY BLOCK HAS GONE AND IT IS STILL TOO TALL, which is a window shorter
		// than the fields themselves. What is left is taken off the HEAD: of the
		// heading, the fields, the action and the legend, the heading is the one a
		// person can do without, and the legend at the foot is the one they cannot.
		cut := len(out) - height
		out = out[cut:]
		doors = doors[cut:]
		caret -= cut
	}
	return out, doors, caret
}

// setupControlsForm builds the form: its rows, the blocks a short window gives
// up, and where the caret sits. THE KEYS LINE IS ITS SECOND ROW, directly under
// the heading, so what the keys do is read with the rows they do it to; it is
// measured against the whole composition (legend) rather than the form's own
// width, because it is one line of clauses rather than a sentence, and
// [app.setupControlsKeys] cuts it by whole clauses, never mid-word. Neither row
// is ever given up.
func (a *app) setupControlsForm(width, legend int) *controlsSheet {
	pal := a.pal
	s := &a.setup
	f := &controlsSheet{caretAt: -1}

	f.add(pal.bold(pal.ink(fit(controlsTitle, width))))
	f.add(pal.dim(a.setupControlsKeys(legend)))
	f.soft(rankSpacer, "")

	// ── the day's limit ──
	limitRow, limitCaret := a.setupLimitRow(width)
	if s.control == controlLimit {
		f.caretAt, f.caretX = len(f.rows), limitCaret
	}
	f.open(setupDoor{kind: doorControl, control: controlLimit}, limitRow)
	a.addControlWords(f, width, controlLimit, controlLimitWord, controlLimitDetail)
	f.soft(rankSpacer, "")

	// ── the chat model ──
	f.open(setupDoor{kind: doorControl, control: controlChatModel},
		a.setupFieldRow(width, controlChatModel, controlModelLabel, modelWord(a.model), a.setupModelSource()))
	a.addControlWords(f, width, controlChatModel, controlModelWord, a.setupModelDetail())
	if s.modelOpen {
		rows, doors := a.setupModelRows(width)
		for i, row := range rows {
			f.open(doors[i], row)
		}
	}
	f.soft(rankSpacer, "")

	if line := a.setupTaskPinRow(width); line != "" {
		// A TASK-MODEL OVERRIDE IS NOT DETAIL. It is a fact that contradicts the
		// row above it, so it is on the screen whether or not anybody asked.
		f.add(line)
	}
	f.soft(rankSpacer, "")

	// ── the way out ──
	f.open(setupDoor{kind: doorControl, control: controlStart}, a.setupStartRow(width))
	if s.refusal != "" {
		// A REFUSAL IS NEVER GIVEN UP. It is the one row on this screen that is
		// about something that just went wrong, and a window too short to show it
		// would leave a person pressing enter at a form that does nothing.
		for _, line := range wrap(s.refusal, width) {
			f.add(pal.warn(line))
		}
	}
	f.soft(rankSpacer, "")
	f.soft(rankNote, a.setupSettingsNote(width))
	return f
}

// setupSettingsNote is the dim sentence under the way out, with the command in
// it wearing the chip the message box paints a recognised command with — the
// same paint, so the word reads as something to type rather than as prose.
func (a *app) setupSettingsNote(width int) string {
	pal := a.pal
	lead := fit(controlSettingsNoteLead, max(width-2-len(controlSettingsNoteCommand), 1))
	return "  " + pal.dim(lead) + pal.chip(controlSettingsNoteCommand)
}

// addControlWords adds a field's one-line explanation, and — only where `?` asked
// for it on the field with the focus — the detail under it.
func (a *app) addControlWords(f *controlsSheet, width int, control setupControl, word, detail string) {
	pal := a.pal
	indent := strings.Repeat(" ", 4)
	// A COMMAND IN THE SENTENCE WEARS ITS CHIP — `/budget`, `/model` — the
	// paint the message box gives a recognised command, so the one word a
	// person can act on later is the one word that stands out in a dim line.
	paint := func(text string, ink func(string) string) []string {
		lines := wrap(text, width-4)
		for i, line := range lines {
			lines[i] = indent + paintCommandSpans(line, recognizedCommandSpans([]rune(line), true), pal, ink)
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

// setupLabelInk is the one rule for how a row's name is painted, on every row
// of the form: the ACCENT while the focus is on it, DIM once enter has answered
// it, and the body INK until then. A name is never decoration — it is what tells
// a person what the figure beside it means — so the three states are the three
// facts about it a person needs at a glance: this one, done, still to do.
func (a *app) setupLabelInk(control setupControl) func(string) string {
	s := &a.setup
	switch {
	case s.control == control:
		return a.pal.accent
	case s.answered[control]:
		return a.pal.dim
	}
	return a.pal.ink
}

// setupFieldRow is one label-and-value row of the form: the label in its column,
// the value in bold, and — where a row has one — a dim word for where the value
// came from.
func (a *app) setupFieldRow(width int, control setupControl, label, value, source string) string {
	pal := a.pal
	name := a.setupLabelInk(control)(padTo(label, controlLabelWidth))
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

// setupTaskPinRow is the line under the chat model when `task.model` is pinned, and ""
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
	name := a.setupLabelInk(controlLimit)(padTo(controlLimitLabel, controlLabelWidth))
	at := ansi.StringWidth(setupLead) + controlLabelWidth + ansi.StringWidth(shown)
	return a.setupControlLead(controlLimit) + name + pal.bold(pal.ink(shown)), at
}

// setupModelRows is the list standing under the chat-model row: five rows of the
// catalog, EACH ITS EXACT ID AND NOTHING ELSE, and a count that says how much
// more there is and how to reach it. A machine with no catalog at all still shows
// the model in use, so enter confirms rather than changes.
//
// IT IS A FLAT LIST OF IDS. Until 2026-10-01 each row was the catalog's friendly
// name (`Qwen3.8 27b:free`) with the exact id drawn under the cursor's row only,
// which read as a heading with a subheading and made the one row a person could
// act on two rows tall. The id is the name a person pastes, types after /model
// and sees in the catalog; the friendly name is still what the filter searches
// ([app.setupModelChoices]) and what the field above shows.
//
// It answers the rows and, row for row, what each is a door onto for a press:
// a model's row chooses that model, and the count line chooses nothing.
func (a *app) setupModelRows(width int) ([]string, []setupDoor) {
	pal := a.pal
	s := &a.setup
	models := a.setupModelChoices()
	if len(models) == 0 {
		if strings.TrimSpace(s.modelFind) != "" {
			return []string{strings.Repeat(" ", 4) + pal.dim(fit(setupNoMatchWord, width-4))}, []setupDoor{{}}
		}
		return []string{strings.Repeat(" ", 4) + pal.dim(fit(setupNoCatalogWord, width-4))}, []setupDoor{{}}
	}
	top := clampIndex(s.modelTop, max(len(models)-setupModelSlots+1, 1))
	out := make([]string, 0, setupModelSlots+2)
	doors := make([]setupDoor, 0, setupModelSlots+2)
	for i := top; i < len(models) && i < top+setupModelSlots; i++ {
		door := setupDoor{kind: doorModel, control: controlChatModel, model: i}
		if i == s.modelAt {
			out = append(out, "  "+pal.accent(setupLead)+pal.bold(pal.ink(fit(models[i].ID, width-6))))
			doors = append(doors, door)
			continue
		}
		out = append(out, "    "+pal.dim(fit(models[i].ID, width-6)))
		doors = append(doors, door)
	}
	out = append(out, strings.Repeat(" ", 4)+pal.dim(fit(a.setupModelCountWord(len(models)), width-4)))
	doors = append(doors, setupDoor{})
	return out, doors
}

// setupModelCountWord is the line under the list: where the cursor is in the
// whole of it, and — because a form cannot show two hundred rows — that typing
// narrows it. A filter that is already typed is shown instead of the invitation.
func (a *app) setupModelCountWord(count int) string {
	at := clampIndex(a.setup.modelAt, count) + 1
	where := itoa(at) + " of " + itoa(count)
	if a.setupFreeOnly() {
		where += " · " + setupFreeOnlyWord
	}
	if find := strings.TrimSpace(a.setup.modelFind); find != "" {
		return where + " · matching " + find
	}
	return where + " · type to narrow"
}

// setupFreeOnlyWord is the count line's word for a list cut to free rows, and
// setupLowCreditsWord is the dim line under the chat model that says why. The
// line names the account rather than the list, because the account is the fact
// a person can act on, and it ends on where the rest went so the cut does not
// read as a catalog that failed to load.
const (
	setupFreeOnlyWord   = "free only"
	setupLowCreditsWord = "Your OpenRouter account is low on credits · the list shows free models only"
	setupExpiredKeyWord = "Your OpenRouter key has expired · esc to paste a new one from openrouter.ai/settings/keys"
)

// setupFootWarning is the account's one-line warning for this screen — the key
// expired, or the balance low and the list cut — and "", the emptiness law,
// when the account gives no cause. It is drawn on the frame's LAST ROW, right
// aligned, in the warning colour: where the same warning stands on the keys
// row under a conversation's message box ([withCreditWarning]), so a person
// meets it in the one place it will keep appearing. Until 2026-10-01 it stood
// under the chat-model row, wrapped, where it read as part of the form.
func (a *app) setupFootWarning() string {
	switch {
	case a.setupKeyExpired():
		return setupExpiredKeyWord
	case a.setupFreeOnly():
		return setupLowCreditsWord
	}
	return ""
}

// setupNoCatalogWord is what the list says where there is no catalog to choose
// from — no key yet, no cache, nothing fetched — and no model in use either. It
// names the door rather than the absence, because the absence is not something a
// person can act on.
const setupNoCatalogWord = "no model list on this machine yet · /model finds one once you are connected"

// setupNoMatchWord is a filter that matched nothing.
const setupNoMatchWord = "nothing matches · backspace widens it"

// setupStartRow is the primary action. It is a row of the same form rather than
// a bright panel: the accent on this screen belongs to whatever the person is
// standing on, and an action that glowed whether or not it had the focus would
// be two things competing to be the obvious one. The key that takes it is NOT
// written at its right any more: the keys line under the heading already says
// `enter starts` when the focus is here, and a second `enter` on the row was
// the same fact twice.
func (a *app) setupStartRow(width int) string {
	word := a.setupLabelInk(controlStart)(controlStartWord)
	if a.setup.control == controlStart {
		word = a.pal.bold(word)
	}
	return a.setupControlLead(controlStart) + word
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
		parts = []string{"enter takes it", "esc cancel", "↑↓ choose"}
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
		// THE ROWS ARE WALKED WITH THE ARROWS, AND THE LEGEND SAYS SO. Tab
		// walks them too, and used to be the word here; it was one more key to
		// learn on a screen whose list a person already walks with ↑↓.
		switch s.control {
		case controlLimit:
			parts = []string{"enter sets the limit", setupMovesWord, a.setupBackWord(), "type an amount or none"}
		case controlChatModel:
			parts = []string{"enter opens the list", setupMovesWord, a.setupBackWord()}
		case controlStart:
			parts = []string{"enter starts", setupMovesWord, a.setupBackWord()}
		}
		if s.control <= controlChatModel {
			parts = append(parts, "? detail")
		}
		// THE ARROWS ARE NOT NAMED HERE. The example panel's own bottom edge
		// carries `←  3 / 5  →`, which is the one place a control for it
		// belongs, and a clause about it on the form's keys line was a clause
		// about a different object.
	}
	return clausesWithin(parts, width)
}

// setupMovesWord is the legend's clause for walking the rows. It is one
// constant because the tmux suite waits for it to know the form is up
// (internal/e2e's tmux_test.go), and a respelling has to be one edit.
const setupMovesWord = "↑↓ moves"

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
	showcaseTitleWord = "Example"
)

// showcaseCaret is the block that trails the request while it is being typed
// out. It is a DRAWN caret and not the terminal's — the real one belongs to the
// amount field on the left and never leaves it — so it has an ascii floor of its
// own like every other glyph on this surface.
const (
	showcaseCaret      = "▌"
	showcaseCaretASCII = "_"
)

// setupShowcaseBlock is the example panel as a framed block at the given width,
// or nil when the window cannot hold the whole of it in maxHeight rows. It is
// whole or nothing: the line at its foot — that nothing in it has run — is the
// sentence that keeps the panel from being read as a report about this machine,
// and a panel trimmed from the bottom would lose exactly that line first.
func (a *app) setupShowcaseBlock(width, maxHeight int) []string {
	pal := a.pal
	if len(setupExamples) == 0 || width < setupShowcaseMinWidth {
		return nil
	}
	at := clampIndex(a.setup.example, len(setupExamples))
	example := setupExamples[at]
	inner := frameInner(width) - 2 // one cell of air inside each edge

	body := a.showcaseBody(example, inner)
	if len(body)+2 > maxHeight {
		return nil
	}
	rows := make([]string, 0, len(body))
	for _, line := range body {
		rows = append(rows, " "+padTo(line, inner)+" ")
	}
	block, _ := framed{
		title:     showcaseTitle(pal, example),
		keysAside: pal.dim(setupShowcaseCount(at, len(setupExamples))),
	}.draw(pal, width, rows)
	return block
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
	// The example's title is on the panel's top edge ([showcaseTitle]), so the
	// body opens straight on the request.

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
	// A COMMAND IN THE REQUEST WEARS ITS CHIP, the same chip the composer draws
	// over a recognised command (slashchip.go's [paintCommands]), so the panel
	// shows `/senior-dev` the way the box will show it when it is typed: as a
	// word the program knows. While the request is still typing itself out the
	// word is painted the moment it is whole, and plain before that, exactly as
	// it is under a person's own fingers.
	for i := range full {
		switch {
		case i < len(shown)-1:
			rows = append(rows, lead(i)+paintCommands(shown[i], pal, pal.ink, i == 0))
		case i == len(shown)-1:
			line := paintCommands(shown[i], pal, pal.ink, i == 0)
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
	// THERE IS NO LINE AT THE FOOT SAYING NOTHING HERE HAS RUN. Until
	// 2026-10-01 one stood there, dim; the label on the top edge — `Example`
	// — says the same thing once, and the panel is now above a form that has
	// not been answered yet, where nothing could have run.
	return rows
}

// showcaseTitle is the panel's label, written into its top edge by the frame,
// behind the one mark on it: the word that says what the panel IS, dim, and
// then the example's own title at the panel's reading weight. A label on an edge
// is a label that cannot be mistaken for content, and a title on the edge is a
// row the body does not have to spend.
func showcaseTitle(pal palette, example setupExample) string {
	// THE MARK IS THIS SURFACE'S OWN GLYPH FOR "NOTHING IS TURNING"
	// (tokens.GQueued, the empty circle that is deliberately not a spinner), which
	// is exactly what this panel is. Borrowing it rather than inventing a shape
	// keeps one vocabulary, and it means the one glyph on the frame agrees with
	// the sentence at its foot.
	return pal.dim(pal.glyph(tokens.GQueued)+" "+showcaseTitleWord+" · ") + pal.muted(example.title)
}

// setupShowcaseCount is the position line on the panel's bottom edge —
// `←  3 / 4  →`. It is drawn whether or not the arrows have been pressed,
// because a control nobody can see is a control nobody has.
func setupShowcaseCount(at, count int) string {
	return "←  " + itoa(at+1) + " / " + itoa(count) + "  →"
}

// ── the demonstration, and the clock that turns the examples ────────────────
//
// THE PANEL PLAYS EACH EXAMPLE ONCE AS IT ARRIVES, AND THE EXAMPLES TURN.
//
// The request types itself out and the three lines under it arrive in order,
// which is the whole of the demonstration: about a second and a third, six
// beats of typing and one per line. It plays when an example arrives — on
// reaching this screen, on ←/→, and on the turn — and then it is still.
//
// THE TURN is the second clock. Left alone, the panel shows the next example
// every [setupTurnEvery], round and round, so a person reading the form sees
// all five without touching anything; ←/→ browse them by hand. ANY KEY HOLDS
// THE CLOCK for a full [setupTurnEvery] from that key, so the panel never
// turns under a person who is typing an amount or walking the rows, and
// browsing by hand is not raced by the clock. Until 2026-10-01 the design
// refused a carousel here and moved the example with the focus instead; new
// people read that as the panel jumping about as they walked the rows, and
// the owner asked for the clock.
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

// restartSetupDemo plays the panel from the top: on arriving, on ←/→, and on
// the turn.
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
// demonstration's clock. Any key that is not ←/→ lands here.
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

// setupTurnEvery is how long the panel holds one example before showing the
// next, and how long any key holds the clock.
const setupTurnEvery = 3 * time.Second

// setupTurnMsg is the turn's clock arriving, stamped with the generation that
// armed it so a tick left over from before a key is dropped.
type setupTurnMsg struct{ gen int }

// turnSetupExample moves the panel by delta, round the ring, and plays the
// example that arrives. It is the one way the example changes — ←/→ and the
// clock both come through it.
func (a *app) turnSetupExample(delta int) {
	s := &a.setup
	// THE EXAMPLES GO ROUND. `→` on the last one is the first again, so a
	// person browsing them never hits a wall they cannot see the reason for;
	// the count on the panel's edge says where they are.
	s.example = wrapCursor(s.example, delta, len(setupExamples))
	a.restartSetupDemo()
	a.touch()
}

// setupTurnCmd arms the turn's clock, and answers nil in every state where
// there should not be one: off this screen, already armed, or in the
// screen-reader tier, where a panel that changed by itself would be the same
// illustration announced over and over. It is the ONE place the clock is
// armed, so a second cannot be started beside the first.
func (a *app) setupTurnCmd() tea.Cmd {
	s := &a.setup
	if !s.open || len(s.steps) == 0 || s.step() != setupControls || a.linear || s.turnTicking {
		return nil
	}
	s.turnTicking = true
	gen := s.turnGen
	return surfaceTick(setupTurnEvery, func(time.Time) tea.Msg { return setupTurnMsg{gen: gen} })
}

// holdSetupTurn is any key on this screen: the clock in flight is retired and
// a fresh one armed, so the next turn is a full [setupTurnEvery] after the key.
func (a *app) holdSetupTurn() tea.Cmd {
	s := &a.setup
	s.turnGen++
	s.turnTicking = false
	return a.setupTurnCmd()
}

// setupTurnAt is the clock arriving. A tick from a retired generation is
// dropped whole, and so is one that finds the screen gone or stepped back to
// the key; otherwise the next example arrives, plays, and the clock is armed
// again.
func (a *app) setupTurnAt(gen int) tea.Cmd {
	s := &a.setup
	if gen != s.turnGen {
		return nil
	}
	s.turnTicking = false
	if !s.open || len(s.steps) == 0 || s.step() != setupControls {
		return nil
	}
	a.turnSetupExample(1)
	return tea.Batch(a.setupDemoCmd(), a.setupTurnCmd())
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
