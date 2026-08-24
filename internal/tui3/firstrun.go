package tui3

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// THE FIRST-RUN SETUP: the first minute of aforge for somebody with nothing
// configured, and the last time they see it.
//
// A fresh install used to open on an empty chat and the first thing the product
// said was a provider error. Now the door lets that launch open with no key
// (cmd/aforge's chatv3.go) and this screen asks for the three facts a first day
// needs, one at a time: the key every model call rides, the crew of models
// aforge uses on its own behalf, and the daily ceiling on what it may spend.
// Under a minute; enter accepts each default; esc skips the whole thing.
//
// Four rules, and each is a thing the person is protected from:
//
//   - IT SHOWS ONCE, EVER. A marker in the profile says it was shown
//     (internal/config's firstrun.go), and skipping counts as shown — a screen
//     that came back would be a screen a person has to dismiss twice.
//   - IT ASKS ONLY WHAT IS MISSING. A key in the shell, a crew already chosen,
//     a ceiling already written — each drops its step. A person who has some of
//     it configured sees only the rest, and one who has all of it sees nothing.
//   - IT STEALS NO KEYSTROKE FROM A CONVERSATION. It opens only on an empty,
//     un-resumed conversation the door said was a person at a terminal
//     ([Options.Setup]), before anything has been typed. Once it is gone
//     nothing brings it back, so no key ever lands here by surprise.
//   - IT SPENDS NOTHING. The key is checked for shape, never against the
//     network; the setup runs before a person has agreed to spend anything.
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
	// crew is the chooser the crew step draws — the same three rows /crew
	// draws, from the same type (crew.go), so a person meets one picture of
	// the crew and not two.
	crew crewPicker
	// refusal is the one line the screen says under the box when enter was
	// pressed on something it will not write. Any other key clears it.
	refusal string
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

// openSetup decides, once, whether this launch gets the screen. It is called
// from [newApp] after everything else on the first frame has been decided, so
// it can see what it is opening over.
//
// The door's own conditions are folded into [Options.Setup] (a TTY, no --once,
// no --host, no named session, no picker). What is left to check here is the
// profile — has it been shown, is anything missing — and the conversation: a
// resumed one, or one with anything in it, is a person coming back to work and
// not a person arriving.
func (a *app) openSetup(allowed bool) {
	if !allowed || a.resumed || len(a.entries) > 0 || a.hosted() {
		return
	}
	dir := strings.TrimSpace(a.profileDir)
	if dir == "" {
		// A door that opened without a profile has nowhere to write an answer,
		// and a setup whose enter lands nothing would be a form that lies.
		return
	}
	if !config.SetupSeenAt(dir).IsZero() {
		return
	}
	steps := setupStepsFor(dir)
	if len(steps) == 0 {
		// Nothing to ask. The marker is still written, so the next launch is one
		// read instead of three, and so a fact unset LATER — a key taken out of
		// the shell — is met by the settings row and not by a greeting.
		_ = config.MarkSetupSeen(dir, a.now())
		return
	}
	a.setup = setupFlow{open: true, steps: steps}
	a.setup.crew.start(config.CrewAt(dir))
	a.touch()
}

// setupNow is the step on screen.
func (s *setupFlow) step() setupStep { return s.steps[s.at] }

// endSetup puts the screen away for good: the marker is written, whatever
// state the screen held is dropped, and the welcome box — decided before this
// screen and held behind it — starts its arrival from the first frame, which
// is what "precedes the box" means in practice.
//
// AND THE ONE NOTE THIS SCREEN MAY LEAVE. A person who skipped, or who pressed
// enter through the key step with nothing in it, is about to meet the first
// turn's refusal with no key behind it. One dim line pointing at the row that
// takes one is the whole of what is said; a profile with a key says nothing.
func (a *app) endSetup(skipped bool) tea.Cmd {
	if !a.setup.open {
		return nil
	}
	dir := strings.TrimSpace(a.profileDir)
	_ = config.MarkSetupSeen(dir, a.now())
	a.setup = setupFlow{skipped: skipped}
	if !config.APIKeyConfigured(dir) {
		a.noteFacts(setupNoKeyWord, "/settings")
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
	if name != "enter" {
		s.refusal = ""
	}
	switch name {
	case "esc":
		return a.endSetup(true), true
	case "enter":
		if !a.setupCommit() {
			a.touch()
			return nil, true
		}
		s.at++
		s.text = ""
		if s.at >= len(s.steps) {
			return a.endSetup(false), true
		}
		a.touch()
		return nil, true
	case "up", "ctrl+p":
		if s.step() == setupCrew {
			s.crew.move(-1)
		}
	case "down", "ctrl+n":
		if s.step() == setupCrew {
			s.crew.move(1)
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

// setupPaste is a paste while the screen is up — which, on the key step, is the
// ordinary way the key arrives. Whitespace around it is the terminal's; inside
// it is a paste that picked up a line break, and the shape check refuses that
// on enter rather than here, so a person sees what landed before it is judged.
func (a *app) setupPaste(text string) bool {
	if !a.setup.open {
		return false
	}
	if a.setup.step() != setupCrew {
		a.setup.text += strings.TrimSpace(text)
		a.setup.refusal = ""
		a.touch()
	}
	return true
}

// setupCommit is enter on the step on screen: the answer is written through
// its settings row, or the refusal is put under the box and the step stays.
// It reports whether the step is answered.
//
// A KEY STEP LEFT EMPTY IS A STEP SKIPPED, not a refusal. There is no default
// key to accept, and a person who has none yet is told where one goes by the
// note at the end rather than held here.
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
		row, ok := registry.Row(config.KeyDailyBudget)
		if !ok {
			return true
		}
		raw := strings.TrimSpace(s.text)
		if raw == "" {
			raw = setupBudgetDefault()
		}
		if err := row.Apply(raw); err != nil {
			s.refusal = err.Error()
			return false
		}
		a.refreshSettings()
		return true
	}
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
		add(pal.ink("your openrouter key"))
		for _, line := range wrap(setupKeyWord, inner) {
			add(pal.dim(line))
		}
		add(pal.dim("get one at ") + pal.ink(setupKeyURL))
		add("")
		caretRow = len(body)
		shown := maskTyped(s.text)
		caretX = len(setupLead) + ansi.StringWidth(shown)
		add(pal.accent(setupLead) + pal.ink(shown))
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
		add(pal.ink("a daily ceiling"))
		for _, line := range wrap(setupBudgetWord(a.registry()), inner) {
			add(pal.dim(line))
		}
		add("")
		caretRow = len(body)
		if s.text == "" {
			// The default is drawn where the answer goes, dim, so what enter
			// accepts is on the screen and not in a sentence about it.
			add(pal.accent(setupLead) + pal.dim("$"+setupBudgetDefault()))
			caretX = len(setupLead)
		} else {
			caretX = len(setupLead) + 1 + ansi.StringWidth(s.text)
			add(pal.accent(setupLead) + pal.ink("$"+s.text))
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
	setupKeyURL = "https://openrouter.ai/settings/keys"
	// THE CREW STEP SAYS WHAT IT IS NOT. People conflate the crew with the model
	// they talk to, and /crew's own confirmation already has to say the same
	// thing after the fact (crew.go's applyCrew). Here it is said before.
	setupCrewWord = "these four are the models aforge uses on its own behalf — planning, " +
		"checking, reading every turn. the model you talk to is a separate choice, " +
		"made with /model."
)

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
		if strings.TrimSpace(s.text) == "" {
			return "enter goes on without a key · esc skips setup"
		}
		return "enter saves it · esc skips setup"
	case setupCrew:
		return "↑↓ choose · enter takes " + config.CrewPresets[s.crew.cursor] + " · esc skips setup"
	case setupBudget:
		if strings.TrimSpace(s.text) == "" {
			return "enter keeps $" + setupBudgetDefault() + " · esc skips setup"
		}
		return "enter sets $" + strings.TrimSpace(s.text) + " · esc skips setup"
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
