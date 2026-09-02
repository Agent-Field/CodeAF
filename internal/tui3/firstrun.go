package tui3

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// THE FIRST-RUN SETUP, AND THE MODEL DOOR THAT MAY COME BACK.
//
// A fresh install used to open on an empty chat and the first thing the product
// said was a provider error. Now the door lets that launch open with no key
// (cmd/aforge's chatv3.go) and this screen asks for the three facts a first day
// needs, one at a time: the key every model call rides, the crew of models
// aforge uses on its own behalf, and THE RAILS — what aforge may spend, per day,
// per plan and per conversation. Under a minute; enter accepts each default; esc
// skips the whole thing.
//
// THE RAILS STEP ASKS THREE ROWS AND NOT ONE. It asked the day's ceiling alone
// for four waves, and the other rails were then discovered when they tripped —
// which is the worst possible moment to meet a limit for the first time
// (docs/design/spending/DESIGN.md). Three is the count a new person can answer:
// the day (the bill), the plan (the question aforge will ask), and this
// conversation (the window in front of them). The rest start where
// docs/LIMITS.md says and are changed later with /budget.
//
// Four rules, and each is a thing the person is protected from:
//
//   - THE CREW AND RAILS SHOW ONCE. A marker in the profile says they were shown
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

// setupStep is one of the three questions.
type setupStep int

const (
	setupKey setupStep = iota
	setupCrew
	setupBudget
)

// setupRails is the three rows the budget step asks, in the order it asks them,
// and each is a REGISTRY ROW rather than a number this screen knows: what this
// screen lands in the profile is byte-for-byte what /settings and /budget land,
// because it is the same writer.
var setupRails = []struct {
	key   string
	label string
}{
	{config.KeyDailyBudget, "per day"},
	{config.KeyPlanConsent, "per plan"},
	{config.KeySpendRail, "per conversation"},
}

// setupFlow is the screen's whole state. The zero value is a surface that never
// had one, which is every launch but the first.
type setupFlow struct {
	open bool
	// steps is the questions still worth asking, in order; at is the index of
	// the one on screen.
	steps []setupStep
	at    int
	// text is what has been typed into the key box or the budget box. It is
	// the raw string and nothing else: the key is masked at draw time, and the
	// budget is parsed by the row's own writer on enter.
	text string
	// rail is which of [setupRails] the budget step is on, and railText what has
	// been typed against each of the three. They are a slice and an index rather
	// than three steps because the design draws ONE screen with three rows on it
	// — a person answering "what may this spend" answers it once, looking at all
	// three figures together.
	rail     int
	railText [3]string
	// crew is the chooser the crew step draws — the same three rows /crew
	// draws, from the same type (crew.go), so a person meets one picture of
	// the crew and not two.
	crew crewPicker
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

// setupStepsFor is which of the three questions this profile still needs
// answered, in the order they are asked. Each predicate is internal/config's
// own, so the screen cannot ask for a key Load would have found or a crew /crew
// would already report.
func setupStepsFor(profileDir string) []setupStep {
	steps := make([]setupStep, 0, 3)
	if !config.APIKeyConfigured(profileDir) {
		steps = append(steps, setupKey)
	}
	if !config.CrewConfigured(profileDir) {
		steps = append(steps, setupCrew)
	}
	if !config.DailyBudgetConfigured(profileDir) {
		steps = append(steps, setupBudget)
	}
	return steps
}

// openSetup decides whether this launch gets the screen. The first-run half is
// still once-only and limited to an empty new conversation. The provider half
// is a prerequisite rather than a greeting: on a local interactive launch
// using the default OpenRouter endpoint, no key opens the one-step connection
// even when the profile has met setup before or the conversation was resumed.
func (a *app) openSetup(allowed bool) {
	if a.hosted() {
		return
	}
	dir := strings.TrimSpace(a.profileDir)
	if dir == "" {
		// A door that opened without a profile has nowhere to write an answer,
		// and a setup whose enter lands nothing would be a form that lies.
		return
	}
	providerMissing := a.routerConnect != nil && !config.APIKeyConfigured(dir)
	firstRun := allowed && !a.resumed && len(a.entries) == 0 && config.SetupSeenAt(dir).IsZero()
	if !providerMissing && !firstRun {
		return
	}
	steps := make([]setupStep, 0, 3)
	if providerMissing {
		steps = append(steps, setupKey)
	}
	if firstRun {
		for _, step := range setupStepsFor(dir) {
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
	// The inherited line is asked for here too, and on a genuine first run it is
	// empty because a profile with no rows has nothing to inherit from. It is
	// not hard-coded empty: this screen also opens on a profile that has rows
	// and no setup marker, and a person answering the crew question is exactly
	// the person the line is for (crew.go's [app.crewInheritedLine]).
	a.setup.crew.start(config.CrewAt(dir), a.crewInheritedLine())
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
	a.setup = setupFlow{skipped: skipped}
	if !config.APIKeyConfigured(dir) {
		word := setupNoKeyWord
		facts := []string{config.APIKeyEnv}
		if a.routerConnect != nil {
			word = setupNoKeyConnectWord
			facts = append([]string{"enter"}, facts...)
		} else {
			facts = append([]string{"/settings"}, facts...)
		}
		a.noteFacts(word, facts...)
	}
	// AND THE ONE LINE A MAC IS OWED BEFORE IT COSTS ANYBODY ANYTHING. Every
	// chord this surface binds is `⌥`, and most macOS terminals send Option as an
	// accent-composing key until a setting is turned on — so the first minute is
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

// setupNoKeyWord is what the empty conversation says after a setup that ended
// with no key. It names the row and the variable, and nothing else: the
// conversation can be typed into, and the refusal on the first turn will say the
// rest in its own words.
const setupNoKeyWord = "no openrouter key yet · paste one into /settings, or export " + config.APIKeyEnv

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
	switch name {
	case "esc":
		return a.endSetup(true), true
	case "enter":
		if s.step() == setupKey && strings.TrimSpace(s.text) == "" && a.routerConnect != nil {
			return a.beginOpenRouter(), true
		}
		if !a.setupCommit() {
			a.touch()
			return nil, true
		}
		return a.advanceSetup(), true
	case "up", "ctrl+p":
		if s.step() == setupCrew {
			s.crew.move(-1)
		}
		// AND ON THE RAILS SCREEN THE ARROWS WALK THE THREE ROWS, writing
		// nothing: a person who has just typed a figure into the second row and
		// wants to change the first should not have to finish the form to reach
		// it. Only enter writes.
		if s.step() == setupBudget && s.rail > 0 {
			s.rail--
			s.text = ""
		}
	case "down", "ctrl+n":
		if s.step() == setupCrew {
			s.crew.move(1)
		}
		if s.step() == setupBudget && s.rail+1 < len(setupRails) {
			s.rail++
			s.text = ""
		}
	case "backspace":
		if runes := []rune(s.text); len(runes) > 0 {
			s.text = string(runes[:len(runes)-1])
		}
	case "ctrl+u":
		s.text = ""
	default:
		if text := msg.Key().Text; text != "" && s.step() != setupCrew {
			s.text += text
		}
	}
	a.touch()
	return nil, true
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
	if a.setup.step() != setupCrew {
		a.setup.text += strings.TrimSpace(text)
		a.setup.refusal = ""
		a.touch()
	}
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
		a.setup.refusal = msg.err.Error()
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
		a.setup.refusal = err.Error() + " · open the link above"
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
		a.setup.refusal = msg.err.Error()
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
		a.setup.refusal = err.Error()
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
	registry := a.registry()
	switch s.step() {
	case setupKey:
		key := strings.TrimSpace(s.text)
		if key == "" {
			return true
		}
		if !config.LooksLikeAPIKey(key) {
			s.refusal = setupKeyShapeWord
			return false
		}
		row, ok := registry.Row(config.KeyAPIKey)
		if !ok {
			return true
		}
		if err := row.Apply(key); err != nil {
			s.refusal = err.Error()
			return false
		}
		return true
	case setupCrew:
		row, ok := registry.Row(config.KeyCrew)
		if !ok {
			return true
		}
		if err := row.Apply(config.CrewPresets[s.crew.cursor]); err != nil {
			s.refusal = err.Error()
			return false
		}
		a.refreshSettings()
		return true
	case setupBudget:
		// ENTER COMMITS THE ROW UNDER THE CURSOR AND WALKS TO THE NEXT, and on
		// the last one the step is answered. Blank keeps the default, which is
		// what the foot line says enter will do — a screen whose enter accepted
		// something other than the figure on it would be a form that lies.
		if !a.setupRail(s.rail) {
			return false
		}
		if s.rail+1 < len(setupRails) {
			s.rail++
			s.text = ""
			a.touch()
			return false
		}
		return true
	}
	return true
}

// setupRail writes one of the three rails, and reports whether it landed.
func (a *app) setupRail(at int) bool {
	s := &a.setup
	if at < 0 || at >= len(setupRails) {
		return true
	}
	row, ok := a.registry().Row(setupRails[at].key)
	if !ok {
		return true
	}
	raw := strings.TrimSpace(s.text)
	if raw == "" {
		raw = setupRailDefault(at)
	}
	if err := row.Apply(raw); err != nil {
		s.refusal = err.Error()
		return false
	}
	s.railText[at] = raw
	a.refreshSettings()
	return true
}

// setupKeyShapeWord is the one refusal the key step has of its own. Everything
// else it could say is the row's.
const setupKeyShapeWord = "not the shape of an openrouter key — they start with sk-or-"

// setupBudgetDefault is the ceiling enter accepts, spelled from the one
// constant every other reader of the rail resolves to ([config.DefaultDailyBudgetUSD]).
func setupBudgetDefault() string {
	return strconv.FormatFloat(config.DefaultDailyBudgetUSD, 'f', -1, 64)
}

// setupRailDefaults are the three figures enter accepts, each spelled from the
// one constant its own rail resolves to. A rail whose default is zero is spelled
// `none` — the word this screen offers in its header and the word every writer
// takes — rather than a `0` that reads as its own opposite.
func setupRailDefault(at int) string {
	switch at {
	case 1:
		return strconv.FormatFloat(config.DefaultPlanConsentUSD, 'f', -1, 64)
	case 2:
		if config.DefaultSpendRailUSD == 0 {
			return setupNoneWord
		}
		return strconv.FormatFloat(config.DefaultSpendRailUSD, 'f', -1, 64)
	}
	return setupBudgetDefault()
}

// setupNoneWord is the word the header offers and the word a default of zero is
// written with. It is one of [config]'s own accepted spellings, so what this
// screen writes is what a person could have typed.
const setupNoneWord = "none"

// setupRailWord is one row of the rails screen as it reads before it is
// answered: the default, in the words the Spending tab uses for it.
func (a *app) setupRailWord(at int) string {
	if at < 0 || at >= len(setupRails) {
		return ""
	}
	raw := setupRailDefault(at)
	if raw == setupNoneWord {
		return config.NoLimitWord
	}
	if row, ok := a.registry().Row(setupRails[at].key); ok {
		switch row.Key {
		case config.KeyPlanConsent:
			return "asks first above $" + raw
		}
	}
	return "$" + raw
}

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
	// The block is as wide as a sentence is comfortable to read, and narrower
	// on a window that has less: the same ceiling a note wraps at.
	inner := min(width-4, setupWidth)
	if inner < 20 {
		inner = max(width-2, 1)
	}
	lead := (width - inner) / 2
	if lead < 0 {
		lead = 0
	}
	pad := strings.Repeat(" ", lead)

	body := make([]string, 0, 24)
	caretRow, caretX := -1, 0
	add := func(line string) { body = append(body, line) }

	// The wordmark, at rest and muted: the same letterforms the welcome box
	// draws, so the screen after this one reads as the same place.
	for _, row := range wordmarkRows(pal.ascii) {
		add(pal.muted(row))
	}
	add("")
	add(pal.dim(setupTitle(s)))
	add("")

	switch s.step() {
	case setupKey:
		switch {
		case s.authStarting:
			add(pal.ink("connecting openrouter"))
			for _, line := range wrap(setupConnectStartingWord, inner) {
				add(pal.dim(line))
			}
		case s.authFlow != nil:
			add(pal.ink("finish connecting openrouter"))
			for _, line := range wrap(setupConnectWaitingWord, inner) {
				add(pal.dim(line))
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
				add(pal.dim(line))
			}
			add(pal.dim("or get a key at ") + pal.ink(linkify(setupKeyURL, setupKeyURL)))
			add("")
			caretRow = len(body)
			shown := maskTyped(s.text)
			caretX = len(setupLead) + ansi.StringWidth(shown)
			add(pal.accent(setupLead) + pal.ink(shown))
		}
	case setupCrew:
		add(pal.ink("the crew"))
		for _, line := range wrap(setupCrewWord, inner) {
			add(pal.dim(line))
		}
		add("")
		for _, line := range s.crew.rows(inner, s.crew.height(), pal, -1, a) {
			add(line)
		}
	case setupBudget:
		// THE RAILS SCREEN: three rows, the same three the Spending tab leads
		// with, in the same words, written through the same registry rows.
		add(pal.ink(setupRailsTitle))
		for _, line := range wrap(setupRailsWord, inner) {
			add(pal.dim(line))
		}
		add("")
		for at, rail := range setupRails {
			label := fit(rail.label, setupRailLabel)
			label += strings.Repeat(" ", max(setupRailLabel-ansi.StringWidth(label), 0))
			switch {
			case at < s.rail:
				// ANSWERED ROWS KEEP THEIR ANSWER ON THE SCREEN. A form that
				// scrolled its own answers away would be a form a person cannot
				// check before they finish it.
				add("  " + pal.dim(label) + " " + pal.muted(setupRailAnswer(s.railText[at])))
			case at > s.rail:
				add("  " + pal.dim(label) + " " + pal.dim(a.setupRailWord(at)))
			default:
				caretRow = len(body)
				if s.text == "" {
					// The default is drawn where the answer goes, dim, so what
					// enter accepts is on the screen and not in a sentence about it.
					add(pal.accent(setupLead) + pal.ink(label) + " " + pal.dim(a.setupRailWord(at)))
					caretX = len(setupLead) + ansi.StringWidth(label) + 1
				} else {
					typed := setupTyped(s.text)
					add(pal.accent(setupLead) + pal.ink(label) + " " + pal.ink(typed))
					caretX = len(setupLead) + ansi.StringWidth(label) + 1 + ansi.StringWidth(typed)
				}
			}
		}
		add("")
		for _, line := range wrap(setupRailsRest, inner) {
			add(pal.dim(line))
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

	// Centred vertically, and never past the top: a window shorter than the
	// block shows the head of it, which is where the question is.
	top := (height - len(body)) / 2
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
	if len(lines) > height {
		lines = lines[:height]
	}
	a.caret = caretRow >= 0
	return lines, lead + caretX, top + caretRow
}

// setupWidth is the block's ceiling in cells. Sixty-four is a sentence's width
// on this surface — the same figure a note stops wrapping at.
const setupWidth = 64

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

// The three questions' own sentences. Each is one calm line about what the
// answer does — the person's real question here is about money and about
// which model is which, and both are answered before anything is asked.
const (
	setupKeyWord = "aforge talks to models through openrouter, on your key and your card. " +
		"nothing is sent until you do."
	setupKeyURL      = "https://openrouter.ai/settings/keys"
	setupConnectWord = "sign in once in your browser. openrouter makes a key for this profile; " +
		"aforge stores it on this machine. no prompt is sent and no model is called."
	setupConnectStartingWord = "opening a private return address on this machine…"
	setupConnectWaitingWord  = "finish signing in in your browser. this page will continue when openrouter sends you back."
	// THE CREW STEP SAYS WHAT IT IS NOT. People conflate the crew with the model
	// they talk to, and /crew's own confirmation already has to say the same
	// thing after the fact (crew.go's applyCrew). Here it is said before.
	setupCrewWord = "these five are the models aforge uses on its own behalf — the work " +
		"inside every task, planning, checking, reading every turn. the model you talk to is a separate choice, " +
		"made with /model."
	// THE RAILS SCREEN'S OWN WORDS. `none` is offered in the header on purpose:
	// no limits is a choice a person should SEE, rather than a trick they learn
	// later from a `0` that reads as its own opposite.
	setupRailsTitle = "what may aforge spend?"
	setupRailsWord  = "enter keeps a default · type a number · none means no limit"
	setupRailsRest  = "the rest — a task, a standing run, aforge's own practice — start with " +
		"a small limit or none. change any of them later with /budget."
)

// setupRailLabel is the width the three row names are laid out in, so the
// figures beside them line up in one column.
const setupRailLabel = 17

// setupTyped is what is being typed, shown as money.
//
// THE DOLLAR SIGN IS DRAWN AND NOT TYPED, which is right for a figure and wrong
// for a word: `$none` is not an amount, and the screen's own header offers
// `none` as an answer. So the mark goes in front of a number and nowhere else.
func setupTyped(text string) string {
	if text == "" {
		return ""
	}
	if _, err := strconv.ParseFloat(text, 64); err != nil {
		return text
	}
	return "$" + text
}

// setupRailAnswer is an answered row, said back the way it was taken: `none`
// becomes the word the row itself reads.
func setupRailAnswer(raw string) string {
	if raw == "" || raw == setupNoneWord {
		return config.NoLimitWord
	}
	return "$" + raw
}

// setupBudgetWord is the daily ceiling's sentence, read off its own settings
// row rather than written a second time here: the row's hint is what /settings
// shows beside the number, and two sentences about one rail would drift.
func setupBudgetWord(registry *config.Settings) string {
	if row, ok := registry.Row(config.KeyDailyBudget); ok && strings.TrimSpace(row.Hint) != "" {
		return strings.ToLower(row.Hint[:1]) + row.Hint[1:]
	}
	return "what aforge may spend on your work in a day."
}

// setupKeysWord is the foot: what enter does RIGHT NOW, and that esc leaves.
// It names the default enter would take, because the default is the whole of
// what a person pressing enter is agreeing to.
func (a *app) setupKeysWord() string {
	s := &a.setup
	switch s.step() {
	case setupKey:
		if s.authStarting || s.authFlow != nil {
			return "esc cancels"
		}
		if strings.TrimSpace(s.text) == "" {
			if a.routerConnect != nil {
				return "enter connects in browser · paste a key · esc not now"
			}
			return "enter goes on without a key · esc skips setup"
		}
		return "enter saves it · esc skips setup"
	case setupCrew:
		return "↑↓ choose · enter takes " + config.CrewPresets[s.crew.cursor] + " · esc skips setup"
	case setupBudget:
		if strings.TrimSpace(s.text) == "" {
			return "enter keeps " + a.setupRailWord(s.rail) + " · esc skips setup"
		}
		return "enter sets " + setupTyped(strings.TrimSpace(s.text)) + " · esc skips setup"
	}
	return "esc skips setup"
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
