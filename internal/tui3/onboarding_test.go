package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE CONTROLS SCREEN'S OWN TESTS (onboarding.go).
//
// Every one of these is about something the screen may or may not do to a
// person's profile, or about a value being the resolved one rather than a figure
// this package invented. None of them asserts the spelling of a sentence for its
// own sake: the words that ARE pinned here are pinned because a person acts on
// them — the two the review row chooses between, and the count under the model
// list.

// controlsApp is a surface standing on the controls screen, over a profile the
// seed has prepared first. It is [setupApp] walked one step past the key box.
func controlsApp(t *testing.T, seed func(dir string)) (*app, string) {
	t.Helper()
	a, dir, _ := setupApp(t, seed)
	a.pal = newPalette(tokens.ANSI256, false)
	a.width, a.height = 120, 24
	pressSetup(a, key("enter"))
	if !a.setup.open || a.setup.step() != setupControls {
		t.Fatalf("the fixture is not on the controls screen (open=%v)", a.setup.open)
	}
	return a, dir
}

// seedRow writes one settings row into a profile BEFORE the surface opens on it,
// through the registry itself — so a fixture cannot arrange a state the product
// could not have reached.
func seedRow(t *testing.T, dir, key, value string) {
	t.Helper()
	rows := config.NewSettings(config.SettingsOptions{ProfileDir: dir})
	row, ok := rows.Row(key)
	if !ok {
		t.Fatalf("the registry has no %s row", key)
	}
	if err := row.Apply(value); err != nil {
		t.Fatalf("seed %s = %q: %v", key, value, err)
	}
}

// walkToControl puts the focus on one control by pressing tab, which is the only
// way a person reaches it.
func walkToControl(t *testing.T, a *app, want setupControl) {
	t.Helper()
	for i := 0; i < setupControlCount; i++ {
		if a.setup.control == want {
			return
		}
		pressSetup(a, key("tab"))
	}
	t.Fatalf("tab never reached control %v", want)
}

// THE MODEL LIST IS THE WHOLE CATALOG AND NOT THE FIRST FIVE OF IT.
//
// The form shows five rows at a time. An earlier build TRUNCATED the catalog to
// those five, which had two costs: everything past the fifth model was
// unreachable from the setup, and a person whose own model was the ninth found
// the cursor on somebody else's — so enter, the key that should confirm, changed
// their model instead.
func TestTheModelListReachesTheWholeCatalogAndOpensOnTheModelInUse(t *testing.T) {
	a, _ := controlsApp(t, nil)
	catalog := make([]Model, 0, 12)
	for _, id := range []string{
		"a/alpha", "b/bravo", "c/charlie", "d/delta", "e/echo",
		"f/foxtrot", "g/golf", "h/hotel", "openai/gpt-4.1-mini", "i/india",
	} {
		catalog = append(catalog, Model{ID: id, ContextLength: 128000})
	}
	a.models = func() []Model { return catalog }

	walkToControl(t, a, controlChatModel)
	pressSetup(a, key("enter"))
	if !a.setup.modelOpen {
		t.Fatal("enter on the chat model row did not open the list")
	}
	// THE CURSOR OPENS ON THE MODEL IN USE even though it is the ninth row and
	// only five are drawn.
	choices := a.setupModelChoices()
	if len(choices) != len(catalog) {
		t.Fatalf("the list offers %d models, want the whole catalog's %d", len(choices), len(catalog))
	}
	if got := choices[a.setup.modelAt].ID; got != a.model {
		t.Fatalf("the cursor opened on %q, want the model in use %q", got, a.model)
	}
	// And enter on it confirms rather than changes.
	was := a.model
	pressSetup(a, key("enter"))
	if a.model != was {
		t.Fatalf("enter on the model in use switched to %q", a.model)
	}
	// The last row of the catalog is reachable by walking, and the count says
	// how far there is to go.
	pressSetup(a, key("enter"))
	for i := 0; i < len(catalog); i++ {
		if a.setupModelChoices()[a.setup.modelAt].ID == "i/india" {
			break
		}
		pressSetup(a, key("down"))
	}
	if got := a.setupModelChoices()[a.setup.modelAt].ID; got != "i/india" {
		t.Fatalf("walking the list stopped at %q, want the catalog's last row", got)
	}
	if screen := setupScreen(a); !strings.Contains(screen, "of "+itoa(len(catalog))) {
		t.Fatalf("the list must say how many there are; got:\n%s", screen)
	}
}

// TYPING NARROWS THE LIST, which is the other half of reaching a catalog of two
// hundred from a form with five rows on it — and it searches the readable name
// as well as the id, because a person types what they can see.
func TestTypingNarrowsTheModelListByNameAndById(t *testing.T) {
	a, _ := controlsApp(t, nil)
	a.models = func() []Model {
		return []Model{
			{ID: "deepseek/deepseek-v4-flash"},
			{ID: "anthropic/claude-sonnet-4.5"},
			{ID: "openai/gpt-4.1-mini"},
		}
	}
	walkToControl(t, a, controlChatModel)
	pressSetup(a, key("enter"))
	for _, letter := range []string{"s", "o", "n", "n", "e", "t"} {
		pressSetup(a, key(letter))
	}
	got := a.setupModelChoices()
	if len(got) != 1 || got[0].ID != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("filtering on a readable name left %v", got)
	}
	pressSetup(a, key("esc"))
	if a.setup.modelFind != "" || a.setup.modelOpen {
		t.Fatalf("esc left the filter %q and open=%v", a.setup.modelFind, a.setup.modelOpen)
	}
}

// SETUP FINDS WHAT /model FINDS. The first-run chooser matched the typed text
// as one substring, so `ds v4` found nothing there while /model's picker found
// deepseek/deepseek-v4-flash: the two lists read one catalog and must read one
// query the same way, which is the shared matcher's tokens (#1321).
func TestSetupsModelFilterIsTheModelPickersMatcher(t *testing.T) {
	a, _ := controlsApp(t, nil)
	a.models = func() []Model {
		return []Model{
			{ID: "anthropic/claude-sonnet-4.5"},
			{ID: "deepseek/deepseek-v4-flash"},
			{ID: "openai/gpt-4.1-mini"},
		}
	}
	walkToControl(t, a, controlChatModel)
	pressSetup(a, key("enter"))
	for _, letter := range []string{"d", "s", " ", "v", "4"} {
		pressSetup(a, key(letter))
	}
	got := a.setupModelChoices()
	if len(got) != 1 || got[0].ID != "deepseek/deepseek-v4-flash" {
		t.Fatalf("`ds v4` in setup left %v, want the one model /model finds for it", got)
	}
}

// THE MODEL IN USE IS CONFIRMABLE WITH NO CATALOG AT ALL. A fresh machine has
// fetched nothing; a list that was empty — or worse, that opened on some other
// model — would turn "let me look" into an accidental switch.
func TestTheModelInUseIsOnTheListWhenThereIsNoCatalog(t *testing.T) {
	a, _ := controlsApp(t, nil)
	a.models = func() []Model { return nil }
	walkToControl(t, a, controlChatModel)
	pressSetup(a, key("enter"))
	choices := a.setupModelChoices()
	if len(choices) == 0 || choices[a.setup.modelAt].ID != a.model {
		t.Fatalf("with no catalog the list offers %v, want the model in use", choices)
	}
	was := a.model
	pressSetup(a, key("enter"))
	if a.model != was {
		t.Fatalf("confirming with no catalog switched to %q", a.model)
	}
}

// A MODEL CHOSEN HERE GOES THROUGH THE SETTINGS ROW AND IS KEPT.
//
// It is the same road /model and the settings sheet take, so the live
// conversation and the profile agree — and the surface says so when the profile
// half did not land.
func TestChoosingAModelLandsLiveAndIsSavedForNextTime(t *testing.T) {
	a, dir := controlsApp(t, nil)
	a.models = func() []Model { return []Model{{ID: "z/zulu"}, {ID: "openai/gpt-4.1-mini"}} }
	saved := ""
	a.saveModel = func(id string) error {
		saved = id
		return config.WriteChatModel(dir, id)
	}
	walkToControl(t, a, controlChatModel)
	pressSetup(a, key("enter"), key("up"), key("enter"))
	if a.model != "z/zulu" {
		t.Fatalf("the conversation is on %q, want the model that was chosen", a.model)
	}
	if saved != "z/zulu" || config.ChatModelAt(dir) != "z/zulu" {
		t.Fatalf("the profile kept %q (seam saw %q), want the choice", config.ChatModelAt(dir), saved)
	}
	if a.setup.refusal != "" {
		t.Fatalf("a write that worked said %q", a.setup.refusal)
	}
}

// AND A CHOICE THE PROFILE COULD NOT KEEP SAYS BOTH HALVES. The live half works
// and the disk half did not, and a screen that reported neither would leave
// somebody to discover it at the next launch.
func TestAModelThatCouldNotBeSavedSaysSoWithoutClaimingItFailed(t *testing.T) {
	a, _ := controlsApp(t, nil)
	a.models = func() []Model { return []Model{{ID: "z/zulu"}, {ID: "openai/gpt-4.1-mini"}} }
	a.saveModel = func(string) error { return nil } // records nothing
	walkToControl(t, a, controlChatModel)
	pressSetup(a, key("enter"), key("up"), key("enter"))
	if a.model != "z/zulu" {
		t.Fatalf("the conversation is on %q, want the model that was chosen", a.model)
	}
	if a.setup.refusal != setupModelUnsavedWord {
		t.Fatalf("the screen said %q, want %q", a.setup.refusal, setupModelUnsavedWord)
	}
}

// NO LIMIT IS A FIRST-CLASS ANSWER, typed in the word the screen offers.
func TestNoneOnTheDailyLimitWritesNoLimit(t *testing.T) {
	a, dir := controlsApp(t, nil)
	for _, letter := range []string{"n", "o", "n", "e"} {
		pressSetup(a, key(letter))
	}
	walkToControl(t, a, controlStart)
	pressSetup(a, key("enter"))
	if a.setup.open {
		t.Fatalf("the setup did not finish: %s", a.setup.refusal)
	}
	if amount, err := config.DailyBudgetUSDAt(dir); err != nil || amount != 0 {
		t.Fatalf("the limit read back as %v (%v), want no limit", amount, err)
	}
}

// GOING BACK TO THE CONNECTION AND RETURNING KEEPS WHAT WAS TYPED.
//
// startSetupControls seeds the screen from the profile ONCE. A form that re-read
// itself on the way back would throw away the amount somebody had typed before
// they went to look at something.
func TestGoingBackToTheConnectionAndReturningKeepsTheFormsEdits(t *testing.T) {
	a, _ := controlsApp(t, nil)
	for _, letter := range []string{"4", "2"} {
		pressSetup(a, key(letter))
	}

	// esc with a step behind this one goes back rather than leaving.
	pressSetup(a, key("esc"))
	if !a.setup.open || a.setup.step() != setupKey {
		t.Fatalf("esc did not go back to the connection (open=%v step=%v)", a.setup.open, a.setup.step())
	}
	pressSetup(a, key("enter"))
	if a.setup.step() != setupControls {
		t.Fatal("the connection did not lead back to the controls")
	}
	if a.setup.limitText != "42" || !a.setup.limitTyped {
		t.Fatalf("the typed limit came back as %q (typed=%v)", a.setup.limitText, a.setup.limitTyped)
	}
}

// THE DETAIL IS ASKED FOR AND NEVER OFFERED. The screen a person arrives at is
// three sentences long; `?` adds one about the field with the focus and takes it
// away again, and moving the focus takes it away too.
func TestTheDetailIsBehindAQuestionMarkAndGoesWithTheFocus(t *testing.T) {
	a, _ := controlsApp(t, nil)
	// Read at eighty columns: the example column beside the form interleaves its
	// own words into the same rows, and a test about a sentence must not depend on
	// what was drawn next to it.
	a.width, a.height = 80, 24
	first := setupScreen(a)
	if strings.Contains(first, "Calls already running") {
		t.Fatalf("the limit's caveat is on the screen before anybody asked:\n%s", first)
	}
	pressSetup(a, key("?"))
	if !strings.Contains(setupScreen(a), "Calls already running") {
		t.Fatalf("? did not open the limit's detail:\n%s", setupScreen(a))
	}
	pressSetup(a, key("tab"))
	if strings.Contains(setupScreen(a), "Calls already running") {
		t.Fatalf("the detail followed the focus off its own field:\n%s", setupScreen(a))
	}
}

// A TASK-MODEL OVERRIDE IS ON THE SCREEN WITHOUT BEING ASKED FOR.
//
// It says the worker seat is out of the router's hands, so it is a fact rather
// than a detail, and the emptiness law keeps it off every screen where no
// override is set.
func TestATaskModelOverrideIsSaid(t *testing.T) {
	a, _ := controlsApp(t, nil)
	if strings.Contains(setupScreen(a), controlTaskPinLead) {
		t.Fatalf("a profile with no override drew a line about one:\n%s", setupScreen(a))
	}
	b, _ := controlsApp(t, func(dir string) {
		seedRow(t, dir, config.KeyTaskModel, "anthropic/claude-opus-5")
	})
	if screen := setupScreen(b); !strings.Contains(screen, controlTaskPinLead) {
		t.Fatalf("a pinned task model must be said; got:\n%s", screen)
	}
}

// THE REVIEW ROW NEVER CALLS SOMEBODY'S OWN SETTINGS DEFAULTS, and what it opens
// is read off the registry rather than written here twice.
func TestTheReviewRowTellsDefaultsFromChoices(t *testing.T) {
	a, _ := controlsApp(t, nil)
	if screen := setupScreen(a); !strings.Contains(screen, controlReviewDefaults) {
		t.Fatalf("a fresh profile must be told these are defaults; got:\n%s", screen)
	}
	b, dir := controlsApp(t, func(dir string) {
		seedRow(t, dir, config.KeyMemoryEnabled, "off")
	})
	screen := setupScreen(b)
	if strings.Contains(screen, controlReviewDefaults) {
		t.Fatalf("a configured profile was told its settings are defaults:\n%s", screen)
	}
	if !strings.Contains(screen, controlReviewYours) {
		t.Fatalf("a configured profile must be offered the review; got:\n%s", screen)
	}
	walkToControl(t, b, controlReview)
	pressSetup(b, key("enter"))
	// The value it shows is the registry's own reading of the row, which is what
	// /settings shows for the same profile.
	row, ok := b.registry().Row(config.KeyMemoryEnabled)
	if !ok {
		t.Fatal("the registry lost the memory row")
	}
	if got := row.Reading(); got != config.MemoryAt(dir) {
		t.Fatalf("the row reads %q and the profile says %q", got, config.MemoryAt(dir))
	}
	if !strings.Contains(setupScreen(b), row.Reading()) {
		t.Fatalf("the review must show the actual value %q; got:\n%s", row.Reading(), setupScreen(b))
	}
}

// THE SCREEN IS USABLE AT EVERY WIDTH THE DESIGN NAMES, and "usable" is three
// concrete things: the values are legible, the way out is on the frame, and the
// keyboard line is there and is not cut in the middle of a word.
func TestTheControlsScreenStaysUsableDownToFortyColumns(t *testing.T) {
	for _, size := range [][2]int{{120, 24}, {80, 24}, {60, 20}, {40, 16}} {
		a, _ := controlsApp(t, nil)
		a.width, a.height = size[0], size[1]
		frame, _, _ := a.frame()
		screen := plain(frame)
		where := itoa(size[0]) + "x" + itoa(size[1])
		for _, want := range []string{controlLimitLabel, controlModelLabel, controlStartWord} {
			if !strings.Contains(screen, want) {
				t.Fatalf("%s lost %q:\n%s", where, want, screen)
			}
		}
		// The legend is present and whole. `fit` marks a cut with the more glyph,
		// and a keyboard line that ends in one has taught nobody anything.
		legend := ""
		for _, line := range strings.Split(screen, "\n") {
			if strings.Contains(line, "enter goes on") {
				legend = strings.TrimSpace(line)
			}
		}
		if legend == "" {
			t.Fatalf("%s has no keyboard line:\n%s", where, screen)
		}
		if strings.Contains(legend, glyphMore) {
			t.Fatalf("%s cut its keyboard line mid-word: %q", where, legend)
		}
		// AND WHAT SURVIVES THE NARROWEST CUT IS THE PAIR THAT DRIVES THE FORM.
		// A legend teaching only what enter does and how to go back has taught
		// everything except how to reach the other four rows, which is the one
		// thing this screen cannot be completed without.
		if !strings.Contains(legend, "tab moves") {
			t.Fatalf("%s dropped `tab moves` from the keyboard line: %q", where, legend)
		}
		// AND NO SENTENCE IS LEFT HALF-DRAWN. Rows are given up whole block at a
		// time, so a wrapped explanation is either all there or not there at all.
		if strings.Contains(screen, controlLimitWord[:20]) &&
			!strings.Contains(strings.Join(strings.Fields(screen), " "), lastWords(controlLimitWord)) {
			t.Fatalf("%s drew the limit's explanation without its end:\n%s", where, screen)
		}
	}
}

// rawSetupFrame is the frame with its rows kept, which is what a claim about a
// BOX has to be made against: collapsing the whitespace out of it would take the
// frame apart.
func rawSetupFrame(a *app) string {
	frame, _, _ := a.frame()
	return plain(frame)
}

// showcasePanel is the example panel cut out of the frame, one row per line and
// each row the panel's own columns and nothing else.
//
// THE TWO COLUMNS SHARE EVERY ROW, so a claim about what the panel says cannot
// be made against the frame: the form's own sentence runs into it on the left of
// every line. Cutting the panel out first is what makes "the panel says X" a
// question with an answer.
func showcasePanel(a *app) []string {
	box := framePiecesOf(a.pal)
	rows := strings.Split(rawSetupFrame(a), "\n")
	top, left := -1, -1
	for i, row := range rows {
		at := strings.Index(row, showcaseTitleWord)
		if at < 0 {
			continue
		}
		top, left = i, strings.LastIndex(row[:at], box.tl+box.edge)
		break
	}
	if top < 0 || left < 0 {
		return nil
	}
	// The frame is measured in cells and indexed here in bytes: `╭` is three of
	// them, so the byte offset the search answered becomes a rune offset before
	// any row is cut with it.
	left = len([]rune(rows[top][:left]))
	cut := func(row string) string {
		runes := []rune(row)
		if left >= len(runes) {
			return ""
		}
		return string(runes[left:min(left+setupShowWidth, len(runes))])
	}
	// THE TOP EDGE IS TAKEN BEFORE THE LOOP AND THE LOOP ENDS ON THE BOTTOM ONE.
	// In the ascii tier every corner is the same `+`, so a walk that stopped at
	// "a row beginning with the bottom-left corner" would stop on the top edge
	// and answer a one-row box.
	out := []string{cut(rows[top])}
	for _, row := range rows[top+1:] {
		edge := cut(row)
		if !strings.HasPrefix(edge, box.side) && !strings.HasPrefix(edge, box.bl) {
			break
		}
		out = append(out, edge)
		if strings.HasPrefix(edge, box.bl) {
			break
		}
	}
	return out
}

// panelWords is the panel as one line of words, which is how a sentence that
// wrapped inside a thirty-two-cell box is asserted.
func panelWords(a *app) string {
	box := framePiecesOf(a.pal)
	rows := showcasePanel(a)
	said := make([]string, 0, len(rows))
	for _, row := range rows {
		// The two side edges come off before the words are joined, or a sentence
		// that wrapped inside the box reads with a `│` in the middle of it.
		runes := []rune(row)
		if len(runes) < 2 {
			continue
		}
		said = append(said, strings.Trim(string(runes[1:len(runes)-1]), box.edge))
	}
	return strings.Join(strings.Fields(strings.Join(said, " ")), " ")
}

// lastWords is the tail of a sentence, which is what a block-wise trim
// guarantees is on the screen whenever the head of it is.
func lastWords(sentence string) string {
	fields := strings.Fields(sentence)
	if len(fields) < 4 {
		return sentence
	}
	return strings.Join(fields[len(fields)-4:], " ")
}

// THE EXAMPLE PANEL IS AN ILLUSTRATION AND SAYS SO, it follows a deliberate
// focus change, and it is not drawn at all where there is no room for it.
func TestTheExampleColumnIsLabelledFollowsTheFocusAndHidesWhenNarrow(t *testing.T) {
	a, _ := controlsApp(t, nil)
	screen := setupScreen(a)
	// The two sentences that keep the panel from being read as a report about
	// this machine: the label on its top edge, and the line at its foot.
	if !strings.Contains(screen, showcaseTitleWord) {
		t.Fatalf("the example panel must be labelled as one; got:\n%s", screen)
	}
	a.settleSetupDemo()
	if words := panelWords(a); !strings.Contains(words, showcaseHonestWord) {
		t.Fatalf("the example panel must say nothing in it has run; got:\n%s", words)
	}
	if want := setupExamples[exampleForControl(controlLimit)].title; !strings.Contains(screen, want) {
		t.Fatalf("the limit's example is %q; got:\n%s", want, screen)
	}
	// A deliberate focus change moves it, and nothing else does: a frame drawn
	// again with no key pressed is the same frame. (The demonstration inside the
	// panel is driven by beats that ARRIVE, never by drawing — settling it first
	// is what makes this a question about the example and not about the clock.)
	a.settleSetupDemo()
	screen = setupScreen(a)
	if again := setupScreen(a); again != screen {
		t.Fatal("the example moved between two frames with no keypress")
	}
	walkToControl(t, a, controlReview)
	if want := setupExamples[exampleForControl(controlReview)].title; !strings.Contains(setupScreen(a), want) {
		t.Fatalf("the review row's example is %q; got:\n%s", want, setupScreen(a))
	}
	// ←/→ browse without touching anything.
	limit := a.setup.limitText
	pressSetup(a, key("right"))
	if a.setup.example == exampleForControl(controlReview) {
		t.Fatal("→ did not move the example")
	}
	if a.setup.limitText != limit {
		t.Fatal("browsing the examples changed a control")
	}
	// And below the width the pair needs, the column is gone entirely.
	a.width, a.height = setupWideCols-1, 24
	narrow, _, _ := a.frame()
	if strings.Contains(plain(narrow), showcaseTitleWord) {
		t.Fatalf("the example panel was drawn under %d columns:\n%s", setupWideCols, plain(narrow))
	}
}

// THE PANEL IS FRAMED, AND THE FRAME IS THE POINT.
//
// The right-hand side is framed so it cannot be read as a second column of the
// form — by the one frame (frame.go) every framed thing on this surface wears. So
// the frame is asserted: all four corners, on every row of the panel, at the
// width the design gives it — and the terminal that cannot be trusted with box
// drawing gets the frame's own ASCII run instead, two plain rules and no sides.
func TestTheExamplePanelIsFramedAndFallsBackToAscii(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		a, _ := controlsApp(t, nil)
		a.pal = newPalette(tokens.ANSI256, ascii)
		a.settleSetupDemo()
		box := framePiecesOf(a.pal)
		panel := showcasePanel(a)
		if len(panel) < 8 {
			t.Fatalf("ascii=%v: no panel on the frame:\n%s", ascii, rawSetupFrame(a))
		}
		head, foot := panel[0], panel[len(panel)-1]
		if !strings.HasPrefix(head, box.tl) || !strings.HasSuffix(head, box.tr) {
			t.Fatalf("ascii=%v: the panel has no top edge: %q", ascii, head)
		}
		if !strings.HasPrefix(foot, box.bl) || !strings.HasSuffix(foot, box.br) {
			t.Fatalf("ascii=%v: the panel has no bottom edge: %q", ascii, foot)
		}
		for _, row := range panel[1 : len(panel)-1] {
			if !strings.HasPrefix(row, box.side) || !strings.HasSuffix(row, box.side) {
				t.Fatalf("ascii=%v: the panel leaks out of its frame: %q", ascii, row)
			}
		}
		// EVERY ROW IS THE SAME WIDTH, which is the difference between a box and
		// four characters that happen to be near each other.
		for _, row := range panel {
			if got := len([]rune(row)); got != setupShowWidth {
				t.Fatalf("ascii=%v: a panel row is %d cells, want %d: %q",
					ascii, got, setupShowWidth, row)
			}
		}
	}
}

// THE DEMONSTRATION PLAYS ONCE, ON A DELIBERATE ACT, AND NOTHING ELSE STARTS IT.
//
// This is the whole motion contract of the screen, and every clause of it is
// something a person would notice if it broke: a panel that replayed on every
// keystroke, or looped, or restarted while somebody was typing an amount, is a
// screen with something moving in the corner of the eye for no reason.
func TestTheExampleDemonstrationPlaysOnceOnADeliberateAct(t *testing.T) {
	a, _ := controlsApp(t, nil)
	// Arriving on the screen is the first deliberate act, and it leaves the panel
	// at its first beat with a beat armed.
	if a.setup.demoAt != 0 {
		t.Fatalf("the panel did not start from the top; demoAt = %d", a.setup.demoAt)
	}
	if a.setupDemoCmd() == nil && !a.setup.demoTicking {
		t.Fatal("arriving on the controls screen armed no beat")
	}
	// Beats advance it and then it stops. The last one arms nothing.
	gen := a.setup.demoGen
	last := a.setupDemoLast()
	for i := 0; i < last; i++ {
		if cmd := a.setupDemoBeatAt(gen); cmd == nil && a.setup.demoAt < last {
			t.Fatalf("the demonstration stopped at beat %d of %d", a.setup.demoAt, last)
		}
	}
	if a.setup.demoAt != last {
		t.Fatalf("the demonstration reached beat %d, want %d", a.setup.demoAt, last)
	}
	if cmd := a.setupDemoBeatAt(gen); cmd != nil {
		t.Fatal("the demonstration asked for another beat after its last — it loops")
	}
	// AND TYPING SETTLES IT RATHER THAN LEAVING IT HALF-DRAWN. Restart it, type
	// one character into the amount, and it is finished.
	a.restartSetupDemo()
	if a.setup.demoAt != 0 {
		t.Fatal("browsing did not replay the panel")
	}
	pressSetup(a, key("2"))
	if a.setup.demoAt != a.setupDemoLast() {
		t.Fatalf("typing left the panel mid-play at beat %d", a.setup.demoAt)
	}
	// A BEAT FROM A PREVIOUS GENERATION IS DROPPED WHOLE. It is what stops the
	// clock of an example somebody has browsed away from driving the one in front
	// of them.
	a.restartSetupDemo()
	stale := a.setup.demoGen - 1
	at := a.setup.demoAt
	if cmd := a.setupDemoBeatAt(stale); cmd != nil || a.setup.demoAt != at {
		t.Fatal("a beat from a retired generation moved the panel")
	}
}

// AND THE SCREEN-READER TIER NEVER ANIMATES AT ALL. What is read aloud is one
// finished illustration, not three lines announced again as each arrives.
func TestTheExampleDemonstrationDoesNotAnimateInTheLinearTier(t *testing.T) {
	a, _ := controlsApp(t, nil)
	a.linear = true
	a.restartSetupDemo()
	if a.setup.demoAt != a.setupDemoLast() {
		t.Fatalf("the linear tier started an animation; demoAt = %d", a.setup.demoAt)
	}
	if cmd := a.setupDemoCmd(); cmd != nil {
		t.Fatal("the linear tier armed a beat")
	}
	// And the whole illustration is there to be read on the first frame.
	words := panelWords(a)
	for _, word := range setupExamples[clampIndex(a.setup.example, len(setupExamples))].leads {
		if !strings.Contains(words, word) {
			t.Fatalf("the linear tier left %q off the first frame:\n%s", word, words)
		}
	}
}

// MODEL NAMES ARE READ AS NAMES AND THE ID IS STILL AVAILABLE. The form is a
// place to decide between two models, not to type an address into.
func TestModelNamesReadAsNamesAndKeepTheirLevel(t *testing.T) {
	for _, c := range []struct{ id, want string }{
		{"deepseek/deepseek-v4-flash", "DeepSeek V4 Flash"},
		{"openai/gpt-4.1-mini", "GPT 4.1 Mini"},
		{"z-ai/glm-5.3:high", "GLM 5.3:high"},
		{"moonshotai/kimi-k3", "Kimi K3"},
		{"", ""},
	} {
		if got := modelWord(c.id); got != c.want {
			t.Errorf("modelWord(%q) = %q, want %q", c.id, got, c.want)
		}
	}
	a, _ := controlsApp(t, nil)
	a.models = func() []Model { return []Model{{ID: "deepseek/deepseek-v4-flash"}} }
	walkToControl(t, a, controlChatModel)
	// The model in use heads the list when the catalog does not carry it, so the
	// cursor walks one row to reach the catalog's own.
	pressSetup(a, key("enter"), key("down"))
	screen := setupScreen(a)
	if !strings.Contains(screen, "DeepSeek V4 Flash") {
		t.Fatalf("the list must read as names; got:\n%s", screen)
	}
	if !strings.Contains(screen, "deepseek/deepseek-v4-flash") {
		t.Fatalf("the row under the cursor must still show its exact id; got:\n%s", screen)
	}
}

// NOTHING ON THIS SCREEN CALLS A MODEL. The whole first run is free, and the
// one seam that could spend — the agent — is asked for nothing but its name.
func TestTheControlsScreenSendsNoPrompt(t *testing.T) {
	a, _ := controlsApp(t, nil)
	agent, ok := a.agent.(*fakeAgent)
	if !ok {
		t.Skip("this fixture's agent cannot be asked what it was sent")
	}
	a.models = func() []Model { return []Model{{ID: "z/zulu"}, {ID: "openai/gpt-4.1-mini"}} }
	walkToControl(t, a, controlChatModel)
	pressSetup(a, key("enter"), key("up"), key("enter"))
	walkToControl(t, a, controlStart)
	pressSetup(a, key("enter"))
	if len(agent.sent) != 0 {
		t.Fatalf("the setup sent %v", agent.sent)
	}
}

// ── the first conversation (welcome.go) ─────────────────────────────────────

// firstChatApp is a surface standing on the first conversation: the setup taken
// as it stands, and the greeting behind it revealed.
func firstChatApp(t *testing.T) *app {
	t.Helper()
	a, _ := controlsApp(t, nil)
	walkToControl(t, a, controlStart)
	pressSetup(a, key("enter"))
	if a.setup.open {
		t.Fatalf("the setup did not finish: %s", a.setup.refusal)
	}
	if !a.welcome.open || !a.welcome.first {
		t.Fatalf("the first conversation's greeting is not up (open=%v first=%v)",
			a.welcome.open, a.welcome.first)
	}
	a.welcome.step = welcomeFrames
	a.touch()
	return a
}

// welcomeScreen is the frame as words, wrapped and rejoined, for the same reason
// [setupScreen] is.
func welcomeScreen(a *app) string {
	frame, _, _ := a.frame()
	return strings.Join(strings.Fields(plain(frame)), " ")
}

// THE FIRST CONVERSATION SAYS WHAT THE BOX IS FOR, AND WHERE IT IS STANDING.
func TestTheFirstConversationAsksWhatYouWouldLikeToWorkOn(t *testing.T) {
	a := firstChatApp(t)
	screen := welcomeScreen(a)
	for _, want := range []string{welcomeFirstTitle, welcomeFirstWord} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the first conversation must say %q; got:\n%s", want, screen)
		}
	}
	for _, starter := range welcomeStarters {
		if !strings.Contains(screen, starter.word) {
			t.Fatalf("the starting point %q is not on the screen:\n%s", starter.word, screen)
		}
	}
	// THE REAL FOLDER, and only where there is one. It is the workspace this
	// conversation is standing in, not a claim about what is in it.
	if !strings.Contains(screen, "lab") {
		t.Fatalf("the working folder must be on the first conversation; got:\n%s", screen)
	}
}

// ONLY THE SELECTED STARTING POINT IS EXPLAINED.
func TestOnlyTheSelectedStartingPointGetsAHelperLine(t *testing.T) {
	a := firstChatApp(t)
	for _, starter := range welcomeStarters {
		if strings.Contains(welcomeScreen(a), starter.helper) {
			t.Fatalf("a helper line was drawn before anything was selected: %q", starter.helper)
		}
	}
	drive(t, a, key("down"))
	if a.welcome.starter != 0 {
		t.Fatalf("↓ selected %d, want the first starting point", a.welcome.starter)
	}
	screen := welcomeScreen(a)
	if !strings.Contains(screen, welcomeStarters[0].helper) {
		t.Fatalf("the selected starting point must be explained; got:\n%s", screen)
	}
	if strings.Contains(screen, welcomeStarters[1].helper) {
		t.Fatalf("an unselected starting point was explained:\n%s", screen)
	}
}

// A STARTING POINT FILLS THE BOX AND SENDS NOTHING.
func TestAStartingPointFillsTheBoxWithoutSendingIt(t *testing.T) {
	a := firstChatApp(t)
	agent, ok := a.agent.(*fakeAgent)
	if !ok {
		t.Skip("this fixture's agent cannot be asked what it was sent")
	}
	drive(t, a, key("down"), key("enter"))
	if got := a.input.String(); got != welcomeStarters[0].fills {
		t.Fatalf("the box holds %q, want %q", got, welcomeStarters[0].fills)
	}
	if len(agent.sent) != 0 {
		t.Fatalf("choosing a starting point sent %v", agent.sent)
	}
	if !a.welcome.open {
		t.Fatal("filling the box put the greeting away")
	}
}

// AND IT NEVER OVERWRITES A DRAFT. The selection only moves over an empty box,
// so a person who has typed something cannot lose it to an arrow key.
func TestAStartingPointNeverDestroysADraft(t *testing.T) {
	a := firstChatApp(t)
	a.input.setText("my own sentence")
	drive(t, a, key("down"))
	if a.welcome.starter >= 0 {
		t.Fatalf("↓ selected a starting point over a draft (slot %d)", a.welcome.starter)
	}
	// And the direct road refuses too, which is what a click can reach.
	a.takeStarter(0)
	if got := a.input.String(); got != "my own sentence" {
		t.Fatalf("the draft became %q", got)
	}
}

// THE COMPOSER DOES NOT MOVE WHEN TYPING BEGINS, and it is spent by the send.
//
// Every later greeting is dismissed by the first keystroke and the box drops to
// the foot of the frame. On a first conversation that moves the one thing the
// person was aiming at, mid-word.
func TestTheFirstConversationsComposerStaysWhereItIs(t *testing.T) {
	a := firstChatApp(t)
	_, _, before := a.frame()
	drive(t, a, key("h"), key("e"), key("l"), key("l"), key("o"))
	if !a.welcome.open {
		t.Fatal("typing dismissed the first conversation's greeting")
	}
	if a.input.String() != "hello" {
		t.Fatalf("the keystrokes landed as %q", a.input.String())
	}
	_, _, after := a.frame()
	if after != before {
		t.Fatalf("the caret moved from row %d to row %d as typing began", before, after)
	}
	// The send is what spends it, and then the box is at the foot of the frame
	// like every other conversation's.
	drive(t, a, key("enter"))
	if a.welcome.open || !a.welcome.spent {
		t.Fatalf("the send left the greeting up (open=%v spent=%v)", a.welcome.open, a.welcome.spent)
	}
}

// AND A FOLDER WITH CONVERSATIONS IN IT IS NOT HAVING ITS FIRST ONE, whatever
// the profile marker says — so it keeps the ordinary greeting and its ordinary
// dismissal.
func TestAFolderWithEarlierConversationsGetsTheOrdinaryGreeting(t *testing.T) {
	a, _ := welcomeApp(t, fourSessions())
	if a.welcome.first {
		t.Fatal("a folder with four recent sessions was treated as a first conversation")
	}
	if strings.Contains(welcomeScreen(a), welcomeFirstTitle) {
		t.Fatalf("the first-conversation heading was drawn over a returning folder:\n%s", welcomeScreen(a))
	}
	drive(t, a, key("h"))
	if a.welcome.open {
		t.Fatal("the ordinary greeting no longer goes on the first keystroke")
	}
}
