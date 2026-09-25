package tui3

import (
	"errors"
	"io/fs"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
)

// THE FIRST-RUN SETUP, AND THE MODEL DOOR THAT MAY COME BACK.
//
// A fresh install used to open on an empty chat and the first thing the product
// said was a provider error. Now the door lets that launch open with no key
// (cmd/codeaf's chatv3.go) and this screen asks for what a first day needs, in
// TWO steps: the key every model call rides, and then one screen of controls —
// the day's spending limit, the model you talk to, and the crew codeaf works
// with. Under a minute; every control opens on the value already in force; the
// way out is `Start a conversation`.
//
// THE SECOND STEP IS ONE SCREEN AND NOT THREE QUESTIONS. It used to be a crew
// chooser followed by a rails screen carrying three money rows, which is five
// answers from somebody who has not yet run the program once — and four of them
// about limits they cannot have an opinion on yet. The rule that decided what
// survived is in docs/design/onboarding/DESIGN.md, and onboarding.go holds the
// screen itself. The rails this no longer asks about — the per-plan question and
// the conversation ceiling — keep the defaults docs/LIMITS.md states and are
// changed with /budget, beside the spending page that explains them.
//
// Four rules, and each is a thing the person is protected from:
//
//   - THE CONTROLS SHOW ONCE. A marker in the profile says they were shown
//     (internal/config's firstrun.go), and skipping counts as shown. The key is
//     different because it is not a preference: with no key the default model
//     provider cannot work, so its one-step connection returns on a later local
//     interactive launch until it is answered.
//   - IT ASKS ONLY WHAT IS MISSING. A key in the shell, a crew already chosen,
//     a ceiling already written — each drops its step. A person who has some of
//     it configured sees only the rest, and one who has all of it sees nothing.
//   - IT STEALS NO KEYSTROKE FROM A CONVERSATION. The first-run questions open
//     only before anything has been typed. A returning key door may stand over
//     an existing conversation, but an attempted send opens it before the draft
//     is cleared, so connecting and pressing enter again sends the same words.
//   - IT SPENDS NOTHING. A pasted key is checked only for shape. The browser
//     exchange creates a key but makes no model call, so no prompt is sent and
//     no model charge can be made during setup.
//
// IT PRECEDES THE WELCOME BOX. The box is what an empty conversation shows; this
// is what it shows before that, and the box's arrival animation starts fresh the
// moment this screen goes ([app.endSetup]).
//
// Every write goes through the settings registry rows — the key row, the crew
// row, the daily budget row — so what this screen lands in the profile is
// byte-for-byte what /settings, /crew and a hand edit would have landed, and
// changing any of it later is those three doors.

// setupStep is one of the two questions.
type setupStep int

const (
	setupKey setupStep = iota
	setupControls
)

// setupFlow is the screen's whole state. The zero value is a surface that never
// had one, which is every launch but the first.
type setupFlow struct {
	open bool
	// steps is the questions still worth asking, in order; at is the index of
	// the one on screen.
	steps []setupStep
	at    int
	// text is what has been typed into the key box. It is the raw string and
	// nothing else: the key is masked at draw time and checked for shape on
	// enter.
	text string
	// The controls screen's own state, which onboarding.go owns entirely.
	//
	//   - control is the row with the focus, and detail whether `?` has been
	//     pressed on it. The detail belongs to the field and goes when the focus
	//     does, so the screen a person arrives at is three sentences long however
	//     much they read on the way.
	//   - limitText and limitTyped are the day's ceiling as it is being edited,
	//     with limitTyped telling an untouched field — which writes back the
	//     figure it drew — from one somebody has cleared on purpose.
	//   - modelOpen, modelAt, modelTop and modelFind are the model list: a
	//     viewport and a filter over the WHOLE catalog rather than a truncation
	//     of it, because a form with five rows must still reach two hundred
	//     models.
	//   - crewOpen and crewAt are the crew chooser's cursor, which is
	//     PROVISIONAL; crewPick is the preset a person actually accepted with
	//     enter, and is empty until they do. That is what makes esc out of the
	//     chooser choose nothing, and what stops `Start a conversation` writing a
	//     preset over somebody's hand-pinned tiers.
	//   - reviewOpen is the optional reading of the settings this screen
	//     deliberately does not ask about, example is which illustration the
	//     right-hand column is showing, and seeded says the screen has already
	//     been read from the profile once — so coming back from the step behind
	//     it does not throw away what was typed.
	control    setupControl
	detail     bool
	limitText  string
	limitTyped bool
	modelOpen  bool
	modelAt    int
	modelTop   int
	modelFind  string
	crewOpen   bool
	crewAt     int
	crewPick   string
	crewSource string
	reviewOpen bool
	example    int
	seeded     bool
	// The example panel's one-shot demonstration (onboarding.go): demoAt is
	// which beat it has reached, demoGen stamps the beats so one left over from
	// a previous example is dropped, and demoTicking says a beat is already in
	// flight so a second clock cannot be started beside the first.
	demoAt      int
	demoGen     int
	demoTicking bool
	// refusal is the one line the screen says under the box when enter was
	// pressed on something it will not write. Any other key clears it.
	refusal string
	// auth is the default provider's browser trip. Starting covers the short
	// interval before its listener is handed back; flow and link cover the wait
	// after that. id names the attempt so a late answer after esc is dropped.
	authStarting bool
	authFlow     OpenRouterFlow
	authLink     string
	authID       uint64
	// skipped says esc ended it, which is the difference between a profile
	// that was answered and one that was declined — the note at the end reads
	// the profile rather than this, but the marker is written either way.
	skipped bool
}

// setupSteps is which of the two questions this conversation still needs
// answered, in the order they are asked. The provider question uses the same
// answer as enter, so setup cannot ask for a default-service key a connected
// service carrying this conversation does not need. The controls keep using
// internal/config's own predicates, so the screen cannot ask for a crew /crew
// would already report.
//
// THE CONTROLS SCREEN IS ONE STEP AND SO IT IS ASKED AS ONE. It carries three
// controls and it opens when EITHER of the two persisted ones is still unwritten
// — a profile that has a crew and no limit is shown both, with the crew already
// on the value it chose, because a screen that dropped the row a person had
// answered would read as a different screen every time it opened. The chat model
// is not in the condition: it resolves from the build and from CODEAF_MODEL until
// somebody chooses, so a profile is never MISSING one.
func (a *app) setupSteps() []setupStep {
	steps := make([]setupStep, 0, 2)
	if a.defaultProviderNeeded() {
		steps = append(steps, setupKey)
	}
	if !config.CrewConfigured(a.profileDir) || !config.DailyBudgetConfigured(a.profileDir) {
		steps = append(steps, setupControls)
	}
	return steps
}

// openSetup decides whether this launch gets the screen. The first-run half is
// still once-only and limited to an empty new conversation. The provider half
// is a prerequisite rather than a greeting: on a local interactive launch
// using the default OpenRouter endpoint, no key opens the one-step connection
// even when the profile has met setup before or the conversation was resumed.
//
// AN EMPTY PROFILE DIRECTORY IS THE NORMAL CASE, NOT THE ABSENT CASE, AND
// ABSENCE IS A HOSTED WINDOW. This function used to return on an empty
// [app.profileDir], reasoning that a door opened without a profile has nowhere
// to write an answer — and the reasoning was sound about a fact that is not
// true. [config.ProfileDir] is CODEAF_PROFILE_DIR, which almost nobody exports,
// so the empty string is what very nearly EVERY launch hands this surface, and
// internal/config has always resolved it to this process's own profile in the
// state root ([config.ProfilePath]). The guard therefore closed the front door
// on the ordinary launch and opened it only on the rare one: a fresh install
// with no key, started the normal way, was never shown the screen that connects
// a provider, and every person who tested it already had a key (#322).
//
// THE HOSTED WINDOW IS THE ONE THAT KEEPS ITS EARLY RETURN, and it is the whole
// of what the old guard was reaching for. What this screen writes — a key, a
// crew, three spending rails, the marker saying it was shown — lands in the
// profile of the machine the AGENT is on, and over --host that machine is not
// this one. A form here would write this laptop's answers about somebody else's
// session, so a connection is asked nothing.
func (a *app) openSetup(allowed bool) {
	if a.hosted() {
		return
	}
	dir := a.profileDir
	providerMissing := a.defaultProviderNeeded()
	firstRun := allowed && !a.resumed && len(a.entries) == 0 && config.SetupSeenAt(dir).IsZero()
	if !providerMissing && !firstRun {
		return
	}
	steps := make([]setupStep, 0, 2)
	if providerMissing {
		steps = append(steps, setupKey)
	}
	if firstRun {
		for _, step := range a.setupSteps() {
			if step == setupKey && providerMissing {
				continue
			}
			steps = append(steps, step)
		}
	}
	if len(steps) == 0 {
		// Nothing to ask. The marker is still written, so the next launch is one
		// read instead of three. A key taken out of the shell later is not one of
		// these preference questions: the provider prerequisite above catches it.
		_ = config.MarkSetupSeen(dir, a.now())
		return
	}
	a.setup = setupFlow{open: true, steps: steps}
	// The controls are seeded from the profile rather than from zero values, so
	// every row on that screen opens on the value that is actually in force
	// (onboarding.go's [app.startSetupControls]).
	a.startSetupControls()
	a.touch()
}

// setupNow is the step on screen.
func (s *setupFlow) step() setupStep { return s.steps[s.at] }

// endSetup puts this showing away: the marker is written, whatever state the
// screen held is dropped, and the welcome box — decided before this screen and
// held behind it — starts its arrival from the first frame. The first-run
// questions are gone for good; a still-missing provider may open its key-only
// form later.
//
// AND THE NOTES THIS SCREEN MAY LEAVE. A person who skipped with no key gets
// one dim line naming the next direct road; a profile with a key says nothing.
// On a Mac a SECOND line follows
// it about the option key, for the reason written over it below — so a test
// that means the key's line asks for the note that names it rather than for the
// last note on the pile.
func (a *app) endSetup(skipped bool) tea.Cmd {
	if !a.setup.open {
		return nil
	}
	dir := strings.TrimSpace(a.profileDir)
	_ = config.MarkSetupSeen(dir, a.now())
	a.cancelSetupAuth()
	// THE QUESTIONS THIS ESC WALKED PAST GET A DOOR. `setup_seen_at` is stamped
	// whichever way this screen ended and only the key-only form ever reopens,
	// so the crew and the day's limit are retired here — silently, until this
	// line. It names the step ON SCREEN and the ones under it, because esc left
	// that one unanswered too, and nothing a person already answered.
	var later []string
	if skipped && a.setup.at < len(a.setup.steps) {
		for _, step := range a.setup.steps[a.setup.at:] {
			if word := setupStepLater(step); word != "" {
				later = append(later, word)
			}
		}
	}
	a.setup = setupFlow{skipped: skipped}
	if len(later) > 0 {
		a.noteFacts(setupLaterWord + " · " + strings.Join(later, " · "))
	}
	if a.defaultProviderNeeded() {
		a.noteFacts(setupNoKeyConnectWord, "enter", config.APIKeyEnv)
	} else if a.routerConnect == nil && !config.APIKeyConfigured(dir) && !a.connectedServiceCarriesModel() {
		// This is the older surface's settings pointer, not a second send gate.
		// The local chat door always wires the browser seam above.
		a.noteFacts(setupNoKeyWord, "/settings", config.APIKeyEnv)
	}
	// AND THE ONE LINE A MAC IS OWED BEFORE IT COSTS ANYBODY ANYTHING. The places
	// are built on `alt+` chords — drawn `opt+` on a Mac — and most macOS
	// terminals send Option as an accent-composing key until a setting is turned
	// on, so the first minute is
	// where that is worth saying, while a person is being told how the program
	// works rather than after a chord has silently typed `¡` into their sentence.
	//
	// IT IS WRITTEN AS A CONDITION AND NOT AS A DIAGNOSIS. Nothing has been
	// pressed yet, so nothing here knows which way the profile is set; what this
	// can honestly say is what the keys are and what to do if they type a
	// character instead. The places' own note says it the other way round, after
	// the character has actually arrived (chords.go's [app.chordNote]).
	if words := a.chords.chordSetupWords(); words != "" {
		a.note(words)
	}
	a.touch()
	if a.welcome.open {
		a.welcome.step = 0
		return a.wake()
	}
	return nil
}

// setupNoKeyWord is the older, non-browser setup seam's key line. Local chat
// launches wire the browser road and use [setupNoKeyConnectWord]; a surface
// without that road can still point at the settings row, unless a connected
// service already carries the conversation and the emptiness law says nothing.
const setupNoKeyWord = "no openrouter key yet · paste one into /settings, or export " + config.APIKeyEnv

// setupSkipKeysWord is what esc does, said the same way on every step of the
// flow. It is a constant because it was SIX spellings of one key and one of them
// disagreed with the other five: the browser-connect step said `esc not now`,
// which reads as a promise that the question comes back, and esc on any step
// stamps `setup_seen_at` and the crew and budget questions never open again
// ([app.endSetup]).
const setupSkipKeysWord = "esc skips setup"

// setupLaterWord leads the line [app.endSetup] leaves behind when esc walked
// past a question. The doors follow it, and only the doors onto questions this
// person was NOT asked — a line naming a question somebody just answered would
// be the screen arguing with them.
const setupLaterWord = "still yours to set"

// setupStepLater is the door onto ONE question esc walked past, said as the
// thing a person would do rather than as the name of a step. The key step has
// none: a conversation that still needs the default provider says so in
// [setupNoKeyConnectWord] or [setupNoKeyWord] already, and one carried by a
// connected service owes no line about OpenRouter at all.
//
// THE CONTROLS SCREEN NAMES ITS THREE DOORS AND NOT ITS OWN NAME. "the controls
// screen" is a thing a person cannot go back to; /budget, /model and /crew are
// three things they can type, and between them they are every choice that screen
// was going to offer.
func setupStepLater(step setupStep) string {
	if step == setupControls {
		return "/budget sets what " + product + " may spend · /model and /crew pick the models"
	}
	return ""
}

// setupNoKeyConnectWord is the local default-provider form. It points at the
// next ordinary act rather than at a buried settings row: the draft is kept,
// and enter brings the browser connection back before anything is submitted.
const setupNoKeyConnectWord = "openrouter is not connected · enter on your message connects in a browser, or export " + config.APIKeyEnv

// ── the keyboard ────────────────────────────────────────────────────────────

// setupKeyPress is the screen's whole claim on the keyboard, and it is the
// first thing [app.key] asks after the pointer handover — above every question
// and overlay — because while it is up there is nothing under it a key could
// mean anything to. ctrl+c is excepted in input.go, as it is for every modal
// on this surface: leaving is never modal.
//
// It reports whether it took the key, which is every key while it is open.
func (a *app) setupKeyPress(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.setup.open {
		return nil, false
	}
	s := &a.setup
	name := msg.String()
	if s.step() == setupKey && (s.authStarting || s.authFlow != nil) {
		if name == "esc" {
			a.cancelSetupAuth()
			s.refusal = setupConnectCancelledWord
			a.touch()
		}
		return nil, true
	}
	if name != "enter" {
		s.refusal = ""
	}
	// THE CONTROLS SCREEN OWNS ITS OWN KEYBOARD. It is a form with five rows, two
	// choosers and a browsable column, and none of that is the key box's
	// vocabulary — so it is routed whole rather than grown as arms on the switch
	// below (onboarding.go).
	if s.step() == setupControls {
		return a.setupControlsPress(name, msg.Key().Text)
	}
	switch name {
	case "esc":
		return a.endSetup(true), true
	case "enter":
		if strings.TrimSpace(s.text) == "" && a.routerConnect != nil {
			return a.beginOpenRouter(), true
		}
		if !a.setupCommit() {
			a.touch()
			return nil, true
		}
		return a.advanceSetup(), true
	case "backspace":
		if runes := []rune(s.text); len(runes) > 0 {
			s.text = string(runes[:len(runes)-1])
		}
	case "ctrl+u":
		s.text = ""
	default:
		if text := msg.Key().Text; text != "" {
			s.text += text
		}
	}
	a.touch()
	return nil, true
}

// setupControlsPress is the controls screen's half of [app.setupKeyPress]: the
// two keys the FLOW owns — enter on the way out and esc on the way back — and
// everything else handed to the screen itself.
//
// ESC IS TWO DIFFERENT ACTS AND IT IS HONEST ABOUT WHICH. With something open
// under a row it closes that; with a step before this one it goes back to it,
// which is what makes a browser sign-in something a person can return to; and on
// a flow where this screen is the whole of the setup it does what esc has always
// done here — stamps the marker and leaves, with the line naming the doors onto
// what it walked past ([app.endSetup]).
func (a *app) setupControlsPress(name, text string) (tea.Cmd, bool) {
	s := &a.setup
	switch name {
	case "enter":
		if a.setupControlsEnter() {
			return a.endSetup(false), true
		}
		a.touch()
		return nil, true
	case "esc":
		if a.setupControlsKey(name, text) {
			a.touch()
			return nil, true
		}
		if s.at > 0 {
			s.at--
			s.text = ""
			s.refusal = ""
			a.touch()
			return nil, true
		}
		return a.endSetup(true), true
	}
	a.setupControlsKey(name, text)
	a.touch()
	// AND THE EXAMPLE PANEL'S CLOCK IS ARMED FROM HERE, once, after the key has
	// been dealt with. [app.setupDemoCmd] answers nil in every state that should
	// not have a beat — finished, already ticking, off this screen, or the
	// screen-reader tier — so this line is safe on every key rather than only on
	// the two that start it (onboarding.go).
	return a.setupDemoCmd(), true
}

// advanceSetup moves past one answered step and closes the screen after the
// last. It is shared by a key pasted here and a key returning from the browser,
// so those two roads cannot disagree about which question follows.
func (a *app) advanceSetup() tea.Cmd {
	s := &a.setup
	s.at++
	s.text = ""
	s.refusal = ""
	if s.at >= len(s.steps) {
		return a.endSetup(false)
	}
	// The controls are re-seeded from the profile on arrival, so a key that has
	// just landed — and with it a catalog this process can now read — is what the
	// model row opens on rather than whatever was resolved before the connection.
	if s.step() == setupControls {
		a.startSetupControls()
		a.touch()
		return a.setupDemoCmd()
	}
	a.touch()
	return nil
}

// setupPaste is a paste while the screen is up — which, on the key step, is the
// ordinary way the key arrives. Whitespace around it is the terminal's; inside
// it is a paste that picked up a line break, and the shape check refuses that
// on enter rather than here, so a person sees what landed before it is judged.
func (a *app) setupPaste(text string) bool {
	if !a.setup.open {
		return false
	}
	if a.setup.authStarting || a.setup.authFlow != nil {
		return true
	}
	// A PASTE LANDS IN WHICHEVER BOX IS TAKING TEXT, and on the controls screen
	// that is the day's limit and only when it has the focus. A pasted line
	// arriving in a form whose focus is on a chooser would be text a person
	// cannot see and cannot delete.
	if a.setup.step() == setupControls {
		if a.setup.control == controlLimit && !a.setup.anyOpen() {
			a.setup.limitText += strings.TrimSpace(text)
			a.setup.limitTyped = true
			a.setup.refusal = ""
			a.touch()
		}
		return true
	}
	a.setup.text += strings.TrimSpace(text)
	a.setup.refusal = ""
	a.touch()
	return true
}

// beginOpenRouter starts the default provider's local browser connection off
// the update loop. The screen moves first, so even the small wait for a
// loopback listener has words on it rather than looking like a swallowed enter.
func (a *app) beginOpenRouter() tea.Cmd {
	if a.routerConnect == nil {
		return nil
	}
	a.authSerial++
	id := a.authSerial
	a.setup.authID = id
	a.setup.authStarting = true
	a.setup.authLink = ""
	a.setup.refusal = ""
	a.touch()
	connect, ctx := a.routerConnect, a.ctx
	return func() tea.Msg {
		flow, err := connect(ctx)
		return openRouterFlowMsg{id: id, flow: flow, err: err}
	}
}

// adoptOpenRouterFlow opens the address only after the listener behind it is
// standing, then waits off-loop for the browser to return. A stale attempt is
// cancelled immediately: esc owns the fact that the person left it.
func (a *app) adoptOpenRouterFlow(msg openRouterFlowMsg) tea.Cmd {
	if !a.setup.open || a.setup.step() != setupKey || a.setup.authID != msg.id {
		if msg.flow != nil {
			msg.flow.Cancel()
		}
		return nil
	}
	a.setup.authStarting = false
	if msg.err != nil {
		a.setup.refusal = setupConnectFailedWord
		a.setup.authID = 0
		a.touch()
		return nil
	}
	if msg.flow == nil {
		a.setup.refusal = "openrouter did not start a browser connection"
		a.setup.authID = 0
		a.touch()
		return nil
	}
	a.setup.authFlow = msg.flow
	a.setup.authLink = strings.TrimSpace(msg.flow.URL())
	if a.setup.authLink == "" {
		msg.flow.Cancel()
		a.setup.authFlow = nil
		a.setup.authID = 0
		a.setup.refusal = "openrouter returned no browser address"
		a.touch()
		return nil
	}
	if err := processOpener(a.setup.authLink); err != nil {
		a.setup.refusal = setupBrowserWord
	}
	a.touch()
	flow, ctx := msg.flow, a.ctx
	return func() tea.Msg {
		key, err := flow.Wait(ctx)
		return openRouterKeyMsg{id: msg.id, key: key, err: err}
	}
}

// adoptOpenRouterKey lands the browser-created key through the very same
// settings row a paste uses, which gives it the same 0600 file and the same
// live handover to every conversation in this process.
func (a *app) adoptOpenRouterKey(msg openRouterKeyMsg) tea.Cmd {
	if !a.setup.open || a.setup.step() != setupKey || a.setup.authID != msg.id {
		return nil
	}
	a.setup.authStarting = false
	a.setup.authFlow = nil
	a.setup.authLink = ""
	a.setup.authID = 0
	if msg.err != nil {
		a.setup.refusal = setupSignInLostWord
		a.touch()
		return nil
	}
	key := strings.TrimSpace(msg.key)
	if !config.LooksLikeAPIKey(key) {
		a.setup.refusal = "openrouter returned no usable key"
		a.touch()
		return nil
	}
	row, ok := a.registry().Row(config.KeyAPIKey)
	if !ok {
		a.setup.refusal = "this profile has nowhere to save the openrouter key"
		a.touch()
		return nil
	}
	if err := row.Apply(key); err != nil {
		a.setup.refusal = setupSaid(err, setupSaveFailedWord)
		a.touch()
		return nil
	}
	return a.advanceSetup()
}

// cancelSetupAuth releases whichever half of the browser trip exists. Setting
// authID to zero also invalidates a Begin command still on its way back; its
// adoption closes the listener as soon as it arrives.
func (a *app) cancelSetupAuth() {
	if a.setup.authFlow != nil {
		a.setup.authFlow.Cancel()
	}
	a.setup.authStarting = false
	a.setup.authFlow = nil
	a.setup.authLink = ""
	a.setup.authID = 0
}

const setupConnectCancelledWord = "openrouter connection cancelled · enter tries again or paste a key"

// setupCommit is enter on the step on screen: the answer is written through
// its settings row, or the refusal is put under the box and the step stays.
// It reports whether the step is answered.
//
// A KEY STEP LEFT EMPTY IS SKIPPED only when no browser connection is wired —
// the custom-endpoint and test path. On the local default provider, keypress
// routing catches that enter first and begins OpenRouter instead.
func (a *app) setupCommit() bool {
	s := &a.setup
	key := strings.TrimSpace(s.text)
	if key == "" {
		return true
	}
	if !config.LooksLikeAPIKey(key) {
		s.refusal = setupKeyShapeWord
		return false
	}
	row, ok := a.registry().Row(config.KeyAPIKey)
	if !ok {
		return true
	}
	if err := row.Apply(key); err != nil {
		s.refusal = setupSaid(err, setupSaveFailedWord)
		return false
	}
	return true
}

// ── WHAT THIS SCREEN SAYS WHEN SOMETHING GOES WRONG ─────────────────────────
//
// THE FIRST SCREEN OF A FRESH INSTALL MAY NOT PRINT A GO ERROR. Seven of this
// setup's refusals were `err.Error()`, so the first sentence a new person could
// be shown — on the one screen where they have done nothing yet and something
// has already failed — was a wrapped chain like `write config daily_budget_usd:
// open /home/…/.codeaf/config.json: permission denied`. It names a function, a
// key, a path inside the program's own storage and an errno, and there is no act
// in it. Three lines away this same file already had the right shape twice
// ([setupKeyShapeWord], [setupNoKeyConnectWord]): the cause, and then what to do.
//
// THE THREE THAT ARE ALWAYS THE MACHINE'S are authored outright — nothing a
// browser trip can return is a sentence for a person — and the four that write
// through a settings row go through [setupSaid], which keeps the REGISTRY's own
// refusal and replaces the operating system's.
const (
	// setupConnectFailedWord is the browser sign-in that never started: the
	// listener, the flow, the round trip to openrouter.
	setupConnectFailedWord = "could not reach openrouter to start the sign-in — check the network, or paste a key instead"
	// setupSignInLostWord is the trip that started and did not come back —
	// closed tab, refused page, a connection that went away mid-flight.
	setupSignInLostWord = "the browser sign-in did not finish — enter tries again, or paste a key instead"
	// setupSaveFailedWord is the answer that could not be written down. It names
	// no path: the folder is codeaf's own, a person who needs its name asks
	// /status, and a permission on a directory is the one thing they can act on.
	setupSaveFailedWord = "could not save that — the folder codeaf keeps your settings in is not writable"
)

// setupSaid is the line under the box for an error a SETTINGS ROW handed back:
// the row's own words where it wrote them for a person, and the authored
// sentence where the operating system wrote them for a program.
//
// THE TEST IS STRUCTURAL AND NOT A GUESS AT THE PROSE. Every refusal the
// registry authors is a plain fmt.Errorf with nothing wrapped inside it —
// `that's not a dollar amount — a number, or none for no limit`,
// `pick one of: frugal, balanced, max`, `OpenRouter key is set by
// OPENROUTER_API_KEY` — while every failure that came off the disk is wrapped
// around the operating system's own error (internal/config's
// writeProfileValues wraps each one with %w). So an error that wraps another
// error is the machine talking, and an error that wraps nothing is a person's
// sentence this screen has no business rewriting. The two file errors are
// checked as well, for the writer that hands one back unwrapped.
//
// It is the settings panel's own bargain kept on this screen: that panel draws
// `item.row.Apply`'s refusal as it stands (settings.go's [app.applySetting]),
// because the rows are written to be read.
func setupSaid(err error, instead string) string {
	if err == nil {
		return ""
	}
	if errors.Unwrap(err) != nil || errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) {
		return instead
	}
	if said := strings.TrimSpace(err.Error()); said != "" {
		return said
	}
	return instead
}

// setupKeyShapeWord is the one refusal the key step has about the SHAPE of what
// was typed. Everything else it could say is either the row's or one of the
// four sentences above.
const setupKeyShapeWord = "not the shape of an openrouter key — they start with sk-or-"

// setupBudgetDefault is the ceiling a profile that has chosen nothing opens on,
// spelled from the one constant every other reader of the rail resolves to
// ([config.DefaultDailyBudgetUSD]). IT IS NOT A NEW DEFAULT AND THIS WAVE DID NOT
// MOVE IT: the onboarding design study drew $10 to keep its illustration short,
// and choosing a smaller backstop for new installs is a product decision nobody
// has made.
func setupBudgetDefault() string {
	return strconv.FormatFloat(config.DefaultDailyBudgetUSD, 'f', -1, 64)
}

// setupNoneWord is the word a limit of zero is written with. It is one of
// [config]'s own accepted spellings, so what this screen writes is what a person
// could have typed, and the row then READS `no limit` back.
const setupNoneWord = "none"

// ── the drawing ─────────────────────────────────────────────────────────────

// setupFrame is the whole screen while the setup is up: one block, centred in
// the window, no border and no chrome. It takes the frame whole for the
// settings panel's reason — there is nothing under it worth showing around the
// edges — and it is decided FIRST in [app.frame], above every other fullscreen
// surface, because it is the one that may be open before any of them and must
// be the one a person sees.
func (a *app) setupFrame(width, height int) ([]string, int, int) {
	pal := a.pal
	s := &a.setup
	// THE CONTROLS SCREEN IS A DIFFERENT COMPOSITION AND SAYS SO HERE. It is a
	// form with a heading, five rows and a legend, laid out from the top with an
	// example column beside it on a wide window — where this block is one
	// question floating in the middle of an empty screen. Two shapes, one for
	// each kind of thing being asked (onboarding.go).
	if s.step() == setupControls {
		return a.setupControlsFrame(width, height)
	}
	// ── ONE RULE, ONE MEASURE, FOR THE TWO SCREENS THE WORDMARK IS DRAWN ON ───
	//
	// This block and the greeting that replaces it are the ONLY two screens that
	// draw the wordmark, and they used to be laid out by two different rules:
	// this one took an exact half of a sixty-four-cell measure of its own, the
	// greeting takes two fifths of the slack over [welcomeUnitWidth]. At 160x50
	// that put the setup's wordmark at row 18 column 49 and the identical
	// letterform, one keypress later, at row 17 column 43 — a six-column jump on
	// the one object that is supposed to say "this is still the same program".
	//
	// So the measure and the lift are the greeting's, named from its own
	// constants rather than copied: `welcomeUnitWidth` for the width the block is
	// centred on, and [welcomeAbove] for how much of the slack goes over it.
	// [setupWidth] is gone with the second rule it was the only user of.
	inner := min(width-4, welcomeUnitWidth)
	if inner < 20 {
		inner = max(width-2, 1)
	}
	lead := (width - inner) / 2
	if lead < 0 {
		lead = 0
	}
	pad := strings.Repeat(" ", lead)

	body := make([]string, 0, 24)
	// soft marks the rows this block CAN DO WITHOUT, row for row with body.
	//
	// A SHORT WINDOW GIVES UP ROWS FROM THE MIDDLE AND NEVER FROM THE FOOT. The
	// block used to be centred and then cut at `height`, so a twelve-row split
	// pane drew the wordmark, the question and four lines of prose and stopped:
	// no `›` box, no `enter connects in browser · paste a key · esc not now`,
	// and therefore no visible way off a screen that looked like an install that
	// had hung. The prose is what a person can be without; the box they type
	// into and the line naming the way out are not. It is the same law
	// [homeBands] keeps for a card — the bands go, the title stays.
	soft := make([]bool, 0, 24)
	caretRow, caretX := -1, 0
	add := func(line string) { body, soft = append(body, line), append(soft, false) }
	// addSoft adds a row that a window too short for the whole block gives up,
	// last one first.
	addSoft := func(line string) { body, soft = append(body, line), append(soft, true) }

	// The wordmark, at rest and muted: the same letterforms the welcome box
	// draws, so the screen after this one reads as the same place.
	for _, row := range wordmarkRows(pal.ascii) {
		addSoft(pal.muted(row))
	}
	addSoft("")
	add(pal.dim(setupTitle(s)))
	add("")

	{
		switch {
		case s.authStarting:
			add(pal.ink("connecting openrouter"))
			for _, line := range wrap(setupConnectStartingWord, inner) {
				addSoft(pal.dim(line))
			}
		case s.authFlow != nil:
			add(pal.ink("finish connecting openrouter"))
			for _, line := range wrap(setupConnectWaitingWord, inner) {
				addSoft(pal.dim(line))
			}
			if s.authLink != "" {
				add("")
				for _, line := range wrap(s.authLink, inner) {
					add(pal.dim(linkify(line, s.authLink)))
				}
			}
		default:
			heading := "your openrouter key"
			word := setupKeyWord
			if a.routerConnect != nil {
				heading = "connect openrouter"
				word = setupConnectWord
			}
			add(pal.ink(heading))
			for _, line := range wrap(word, inner) {
				addSoft(pal.dim(line))
			}
			add(pal.dim("or get a key at ") + pal.ink(linkify(setupKeyURL, setupKeyURL)))
			add("")
			caretRow = len(body)
			shown := maskTyped(s.text)
			caretX = len(setupLead) + ansi.StringWidth(shown)
			add(pal.accent(setupLead) + pal.ink(shown))
		}
	}
	if s.refusal != "" {
		for _, line := range wrap(s.refusal, inner) {
			add(pal.accent(line))
		}
	} else {
		add("")
	}
	add(pal.dim(a.setupKeysWord()))

	// A SHADE ABOVE THE MIDDLE, WHICH IS WHERE A CENTRED THING LOOKS CENTRED, and
	// it is the greeting's own arithmetic rather than a second copy of it
	// ([welcomeAbove] states why two fifths and not a half). Never past the top:
	// a window shorter than the block shows the head of it, which is where the
	// question is.
	// AND WHAT WILL NOT FIT IS GIVEN UP BEFORE THE BLOCK IS PLACED, out of its
	// middle, so that the two rows a person acts on are still on the screen.
	body, caretRow = setupTrim(body, soft, caretRow, height)
	top := welcomeAbove(len(body), height-len(body))
	if top < 0 {
		top = 0
	}
	lines := make([]string, 0, height)
	for i := 0; i < top; i++ {
		lines = append(lines, "")
	}
	for _, line := range body {
		lines = append(lines, pad+line)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	caretY := top + caretRow
	if over := len(lines) - height; over > 0 {
		// THE LAST RESORT TAKES THE HEAD AND NOT THE FOOT. Every soft row has
		// already gone and the block is still taller than the window, so what is
		// left is the question, the box and the keys — and of those three the
		// one a person can do without is the one at the top.
		lines = lines[over:]
		caretY -= over
	}
	a.caret = caretRow >= 0
	return lines, lead + caretX, caretY
}

// setupTrim gives the window back the rows it does not have, taking them from
// the block's MIDDLE — the prose and the wordmark, last one first — and never
// from its foot, where the box and the keys line are.
//
// The caret rides the trim: it is a row of this block and not a number about the
// screen, so a line dropped above it moves it up with everything else.
func setupTrim(body []string, soft []bool, caret, height int) ([]string, int) {
	over := len(body) - height
	if over <= 0 {
		return body, caret
	}
	drop := make(map[int]bool, over)
	for at := len(body) - 1; at >= 0 && over > 0; at-- {
		if !soft[at] || at == caret {
			continue
		}
		drop[at] = true
		over--
	}
	out := make([]string, 0, len(body))
	moved := caret
	for at, line := range body {
		if drop[at] {
			if caret >= 0 && at < caret {
				moved--
			}
			continue
		}
		out = append(out, line)
	}
	return out, moved
}

// THERE IS NO setupWidth ANY MORE. It was sixty-four — a sentence's comfortable
// width — and it was the second of the two measures that made the wordmark jump
// six columns between this screen and the greeting. [welcomeUnitWidth] is the
// one measure now, and [app.setupFrame] says why.

// setupLead is the mark in front of the box, the same one the cursor wears on
// every list here.
const setupLead = "› "

// setupTitle is the dim line over the question: where in the flow this is, in
// the fewest words. One question needs no count.
func setupTitle(s *setupFlow) string {
	if len(s.steps) <= 1 {
		return "setting up"
	}
	return "setting up · " + itoa(s.at+1) + " of " + itoa(len(s.steps))
}

// The connection step's own sentences. Each is one calm line about what the
// answer does — the person's real question here is who is billing them, and it
// is answered before anything is asked.
//
// AND EVERY ONE OF THEM NAMES THE PRODUCT FROM [product] AND NEVER FROM A
// LITERAL. The wordmark above this prose is drawn from that same
// constant, and when the two were spelled separately the first screen anybody
// ever sees said one name in the letterforms and a different one in the
// sentence three rows under them.
const (
	setupKeyWord = product + " talks to models on its default service through openrouter, on your key and your card. " +
		"nothing is sent until you do."
	setupKeyURL      = "https://openrouter.ai/settings/keys"
	setupConnectWord = "sign in once in your browser. openrouter makes the default service's key for this profile; " +
		product + " stores it on this machine. no prompt is sent and no model is called."
	setupConnectStartingWord = "opening a private return address on this machine…"
	setupConnectWaitingWord  = "finish signing in in your browser. this page will continue when openrouter sends you back."
)

// setupKeysWord is the foot: what enter does RIGHT NOW, and that esc leaves.
// It names the default enter would take, because the default is the whole of
// what a person pressing enter is agreeing to.
//
// THE CONTROLS SCREEN WRITES ITS OWN, because its keys change with the row a
// person is standing on and with whether a chooser is open under it
// (onboarding.go's [app.setupControlsKeys]).
func (a *app) setupKeysWord() string {
	s := &a.setup
	if s.step() == setupControls {
		width, _ := a.size()
		return a.setupControlsKeys(max(width-2*setupMargin, 1))
	}
	if s.authStarting || s.authFlow != nil {
		return "esc cancel"
	}
	if strings.TrimSpace(s.text) == "" {
		if a.routerConnect != nil {
			// `esc skips setup`, IN THE SAME WORDS AS EVERY OTHER BRANCH. It read
			// `esc not now` here alone, which is a promise about a later — and
			// what esc actually does is stamp `setup_seen_at` and retire the
			// controls screen for good ([app.endSetup]). The key is named for what
			// it does, and the note it leaves behind says where those choices live
			// afterwards.
			return "enter connects in browser · paste a key · " + setupSkipKeysWord
		}
		return "enter goes on without a key · " + setupSkipKeysWord
	}
	return "enter saves it · " + setupSkipKeysWord
}

// maskTyped is the key as it is being typed: one bullet per character and the
// last four in the clear, so the length grows as the paste lands and the tail
// says which key it was. It is the settings row's mask with the count kept —
// a person watching a box fill wants to see it fill.
func maskTyped(text string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}
	const tail = 4
	if len(runes) <= tail {
		return strings.Repeat("•", len(runes))
	}
	return strings.Repeat("•", len(runes)-tail) + string(runes[len(runes)-tail:])
}

// handAPIKey is the live half of a key write: the settings row landed one in
// the profile — from this screen or from /settings — and the running session is
// handed the same value so its next request rides it. It reads the key back
// through internal/config rather than trusting the text that was typed, because
// the environment still outranks the file and the session must get the one
// Load would.
func (a *app) handAPIKey() {
	if a.applyAPIKey == nil {
		return
	}
	key := config.APIKeyAt(a.profileDir)
	if key == "" {
		return
	}
	if err := a.applyAPIKey(key); err != nil {
		a.note("the key is saved but this conversation could not take it · " + err.Error())
	}
}
