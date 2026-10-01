package tui3

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/session"
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
	// Tall enough for the form AND the example panel under it; the tests about
	// a short window size the frame themselves.
	a.width, a.height = 120, 44
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
	// And enter on it confirms rather than changes — and then goes on to the
	// next row, as it does on the limit, so a person pressing enter to get
	// through the form is not handed the same list again.
	was := a.model
	pressSetup(a, key("enter"))
	if a.model != was {
		t.Fatalf("enter on the model in use switched to %q", a.model)
	}
	if a.setup.modelOpen || a.setup.control != controlReview {
		t.Fatalf("after taking a model the focus is on control %v with the list open=%v, want the review row and the list closed", a.setup.control, a.setup.modelOpen)
	}
	// The last row of the catalog is reachable by walking, and the count says
	// how far there is to go.
	pressSetup(a, key("shift+tab"), key("enter"))
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
			if strings.Contains(line, "enter sets the limit") {
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
		if !strings.Contains(legend, setupMovesWord) {
			t.Fatalf("%s dropped %q from the keyboard line: %q", where, setupMovesWord, legend)
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
// each row the panel's own columns and nothing else — the columns its own top
// edge spans, from its top-left corner to its top-right one. Cutting the panel
// out first is what makes "the panel says X" a question with an answer, and
// what makes a claim about its frame a claim about a box.
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
	topRunes := []rune(strings.TrimRight(rows[top], " "))
	width := len(topRunes) - left
	cut := func(row string) string {
		runes := []rune(row)
		if left >= len(runes) {
			return ""
		}
		return string(runes[left:min(left+width, len(runes))])
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
	// The label on the panel's top edge is what keeps it from being read as a
	// report about this machine.
	if !strings.Contains(screen, showcaseTitleWord) {
		t.Fatalf("the example panel must be labelled as one; got:\n%s", screen)
	}
	a.settleSetupDemo()
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
	// And on a window with no rows to spare under the form, the panel is gone
	// entirely — never cut, because its last line is the one that says nothing
	// in it has run.
	a.width, a.height = 120, 24
	short, _, _ := a.frame()
	if strings.Contains(plain(short), showcaseTitleWord) {
		t.Fatalf("the example panel was drawn on a 24-row window with no room for the whole of it:\n%s", plain(short))
	}
}

// THE PANEL STANDS ABOVE THE FORM, AT THE COMPOSITION'S WIDTH, AND THE KEYS
// LINE IS THE FORM'S SECOND ROW. The panel used to be a second column beside
// the form, drawn from 112 columns up, and then for a day it stood under the
// legend, where the legend read as a caption for the wrong object. Now the
// order is panel, two blank rows, the heading, the keys line, the rows — and
// the panel's top edge is as wide as the composition, so the request stands on
// one row and the example's title rides on the edge instead of spending a row
// of the body. The arrows are not on the keys line: the panel's own edge
// carries them.
func TestTheExamplePanelStandsAboveTheFormWithTheKeysLineUnderTheHeading(t *testing.T) {
	a, _ := controlsApp(t, nil)
	a.settleSetupDemo()
	rows := strings.Split(rawSetupFrame(a), "\n")
	legend, title, top, bottom := -1, -1, -1, -1
	box := framePiecesOf(a.pal)
	for i, row := range rows {
		switch {
		case strings.Contains(row, setupMovesWord):
			legend = i
		case strings.Contains(row, controlsTitle):
			title = i
		case strings.Contains(row, showcaseTitleWord):
			top = i
		case top >= 0 && bottom < 0 && strings.Contains(row, box.bl):
			bottom = i
		}
	}
	if legend < 0 || title < 0 || top < 0 || bottom < 0 {
		t.Fatalf("legend %d, form title %d, panel top %d, panel bottom %d on the frame:\n%s", legend, title, top, bottom, rawSetupFrame(a))
	}
	if top != 2 {
		t.Fatalf("the panel's top edge is on row %d, want row 2 under the header and its blank", top)
	}
	if title != bottom+1+setupShowcaseGap {
		t.Fatalf("the heading is on row %d and the panel's bottom edge on row %d; want %d blank rows between them", title, bottom, setupShowcaseGap)
	}
	if legend != title+1 {
		t.Fatalf("the keys line is on row %d and the heading on row %d; want the keys line directly under the heading", legend, title)
	}
	if strings.Contains(rows[legend], "examples") {
		t.Fatalf("the keys line names the arrows, which the panel's own edge carries: %q", rows[legend])
	}
	for _, row := range rows {
		if strings.Contains(row, "Nothing here has run") {
			t.Fatalf("the panel still carries the foot line:\n%s", rawSetupFrame(a))
		}
	}
	panel := showcasePanel(a)
	if got := len([]rune(panel[0])); got != setupShowcaseWidth {
		t.Fatalf("the panel is %d cells wide, want the composition's %d:\n%s", got, setupShowcaseWidth, panel[0])
	}
	// The example's title is on the top edge, after the label, and not inside.
	example := setupExamples[a.setup.example]
	if !strings.Contains(panel[0], showcaseTitleWord+" · "+example.title) {
		t.Fatalf("the top edge does not carry %q after the label:\n%s", example.title, panel[0])
	}
	for _, row := range panel[1:] {
		if strings.Contains(row, example.title) {
			t.Fatalf("the example's title is still inside the panel:\n%s", strings.Join(panel, "\n"))
		}
	}
	// The request stands on one row inside it, which is what the width is for.
	found := false
	for _, row := range panel {
		if strings.Contains(row, example.ask) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the request %q wrapped inside a %d-cell panel:\n%s", example.ask, setupShowcaseWidth, strings.Join(panel, "\n"))
	}
	// And a click still lands on the form's rows where they now stand.
	x, y := setupRowOf(t, a, controlModelLabel)
	pressSetup(a, clickAt(x+2, y))
	if a.setup.control != controlChatModel || !a.setup.modelOpen {
		t.Fatalf("a press on the model row under the panel left the focus on %v with the list open=%v", a.setup.control, a.setup.modelOpen)
	}
}

// THE HAND-OFF EXAMPLE SPELLS /senior-dev AND WEARS ITS CHIP. It stands beside
// `Start a conversation`, and the command in its request is painted the way the
// composer paints a recognised command, so the panel shows the word as one the
// program knows rather than as prose.
func TestTheSeniorDevExampleStandsOnTheLastRowWithItsCommandChipped(t *testing.T) {
	// The program's row is on the command table once the engine's list has
	// landed (delegate.go), which is what makes `/senior-dev` a word the
	// composer chips; the fixture's agent has no list, so the row is installed
	// the way the launch installs it.
	installDelegateCommands([]session.DelegateRow{{Name: "senior-dev", Description: "an autonomous coding agent"}})
	t.Cleanup(func() { installDelegateCommands(nil) })
	a, _ := controlsApp(t, nil)
	walkToControl(t, a, controlStart)
	a.settleSetupDemo()
	example := setupExamples[a.setup.example]
	if !strings.HasPrefix(example.ask, "/senior-dev ") || example.title != "Hand off complex coding tasks" {
		t.Fatalf("the last row's example is %q / %q, want the senior-dev hand-off", example.title, example.ask)
	}
	frame, _, _ := a.frame()
	if !strings.Contains(frame, a.pal.chip("/senior-dev")) {
		t.Fatalf("the panel does not paint /senior-dev as a command chip:\n%s", plain(frame))
	}
	if !strings.Contains(setupScreen(a), example.title) {
		t.Fatalf("the panel's edge does not carry %q:\n%s", example.title, setupScreen(a))
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
		if len(panel) < 6 {
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
			if got := len([]rune(row)); got != setupShowcaseWidth {
				t.Fatalf("ascii=%v: a panel row is %d cells, want %d: %q",
					ascii, got, setupShowcaseWidth, row)
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

// ENTER GETS A PERSON THROUGH THE WHOLE FORM. Each row answers enter by going on
// to the next: the limit commits and moves, the model row opens its list and a
// taken model moves, the review opens and then moves, and the way out starts.
// An earlier build left the focus on the model row after a choice, so the next
// enter opened the list again and nothing a person did with enter alone ever
// reached `Start a conversation`.
func TestEnterAloneWalksTheWholeControlsScreen(t *testing.T) {
	a, dir := controlsApp(t, nil)
	a.models = func() []Model { return []Model{{ID: "openai/gpt-4.1-mini"}, {ID: "b/bravo"}} }
	pressSetup(a, key("enter")) // the limit, taken as it stands
	if a.setup.control != controlChatModel {
		t.Fatalf("after the limit the focus is on %v, want the chat model", a.setup.control)
	}
	pressSetup(a, key("enter")) // opens the list
	pressSetup(a, key("enter")) // takes the model in use and goes on
	if a.setup.control != controlReview || a.setup.modelOpen {
		t.Fatalf("after the model the focus is on %v (list open=%v), want the review", a.setup.control, a.setup.modelOpen)
	}
	pressSetup(a, key("enter")) // shows the review
	if !a.setup.reviewOpen || a.setup.control != controlReview {
		t.Fatal("enter on the review row did not show it")
	}
	if screen := setupScreen(a); !strings.Contains(screen, "enter goes on") {
		t.Fatalf("the legend on an open review must say enter goes on; got:\n%s", screen)
	}
	pressSetup(a, key("enter")) // goes on, leaving the review readable
	if a.setup.control != controlStart || !a.setup.reviewOpen {
		t.Fatalf("after the review the focus is on %v (review open=%v), want the way out with the review still showing", a.setup.control, a.setup.reviewOpen)
	}
	pressSetup(a, key("enter")) // starts
	if a.setup.open {
		t.Fatal("enter on `Start a conversation` left the setup up")
	}
	if !config.DailyBudgetConfigured(dir) {
		t.Fatal("leaving the screen did not write the day's limit")
	}
}

// setupRowOf finds the frame row a control's label is drawn on, so a click test
// aims at the row a person sees rather than at a number the test invented.
func setupRowOf(t *testing.T, a *app, label string) (x, y int) {
	t.Helper()
	frame, _, _ := a.frame()
	for row, line := range strings.Split(plain(frame), "\n") {
		if at := strings.Index(line, label); at >= 0 {
			return at, row
		}
	}
	t.Fatalf("no row of the screen carries %q:\n%s", label, plain(frame))
	return 0, 0
}

// THE CONTROLS SCREEN ANSWERS THE POINTER. A press on a row is the key that row
// would have taken: a press on the limit focuses it, a press on the model row
// opens its list, a press on one model of the list takes it, and a press on
// `Start a conversation` leaves. The screen used to swallow every press, on the
// argument that it was three keystrokes; new people clicked its rows and read
// the silence as a menu that could not be selected.
func TestAClickOnTheControlsScreenActsLikeTheKeyOnThatRow(t *testing.T) {
	a, dir := controlsApp(t, nil)
	a.models = func() []Model {
		return []Model{{ID: "openai/gpt-4.1-mini"}, {ID: "b/bravo"}, {ID: "c/charlie"}}
	}
	// A press on the model row opens the list.
	x, y := setupRowOf(t, a, controlModelLabel)
	pressSetup(a, clickAt(x+2, y))
	if a.setup.control != controlChatModel || !a.setup.modelOpen {
		t.Fatalf("a press on the model row left the focus on %v with the list open=%v", a.setup.control, a.setup.modelOpen)
	}
	// A press on one row of the list takes that model and goes on.
	x, y = setupRowOf(t, a, modelWord("c/charlie"))
	pressSetup(a, clickAt(x, y))
	if a.model != "c/charlie" {
		t.Fatalf("a press on a model row put the conversation on %q, want c/charlie", a.model)
	}
	if a.setup.modelOpen || a.setup.control != controlReview {
		t.Fatalf("after the press the focus is on %v (list open=%v), want the review", a.setup.control, a.setup.modelOpen)
	}
	// A press on the limit only focuses it: what a press on an amount means is
	// "I want to type here".
	x, y = setupRowOf(t, a, controlLimitLabel)
	pressSetup(a, clickAt(x, y))
	if a.setup.control != controlLimit {
		t.Fatalf("a press on the limit row left the focus on %v", a.setup.control)
	}
	// A press on a sentence does nothing.
	x, y = setupRowOf(t, a, "waits until midnight")
	pressSetup(a, clickAt(x, y))
	if a.setup.control != controlLimit || a.setup.modelOpen {
		t.Fatalf("a press on a sentence changed the focus to %v (list open=%v)", a.setup.control, a.setup.modelOpen)
	}
	// A press on the way out leaves, with the limit written.
	x, y = setupRowOf(t, a, controlStartWord)
	pressSetup(a, clickAt(x, y))
	if a.setup.open {
		t.Fatal("a press on `Start a conversation` left the setup up")
	}
	if !config.DailyBudgetConfigured(dir) {
		t.Fatal("leaving by a press did not write the day's limit")
	}
}

// AND THE WHEEL TURNS THE OPEN LIST, and only the list: with it closed a notch
// changes nothing on the screen.
func TestTheWheelWalksTheOpenModelList(t *testing.T) {
	a, _ := controlsApp(t, nil)
	catalog := make([]Model, 0, 8)
	for _, id := range []string{"openai/gpt-4.1-mini", "b/bravo", "c/charlie", "d/delta", "e/echo", "f/foxtrot", "g/golf", "h/hotel"} {
		catalog = append(catalog, Model{ID: id})
	}
	a.models = func() []Model { return catalog }
	wheel := func(down bool) tea.MouseWheelMsg {
		button := tea.MouseWheelUp
		if down {
			button = tea.MouseWheelDown
		}
		return tea.MouseWheelMsg{X: 20, Y: 10, Button: button}
	}
	before := setupScreen(a)
	pressSetup(a, wheel(true))
	if setupScreen(a) != before {
		t.Fatal("a notch with no list open changed the screen")
	}
	walkToControl(t, a, controlChatModel)
	pressSetup(a, key("enter"))
	// A run of notches is folded by the pointer coalescer and spent at the
	// frame boundary (coalesce.go), which the settle message stands for here.
	pressSetup(a, wheel(true), wheel(true), wheel(true), pointerMsg{})
	if got := a.setupModelChoices()[a.setup.modelAt].ID; got != "d/delta" {
		t.Fatalf("three notches down put the cursor on %q, want d/delta", got)
	}
	pressSetup(a, wheel(false), pointerMsg{})
	if got := a.setupModelChoices()[a.setup.modelAt].ID; got != "c/charlie" {
		t.Fatalf("a notch up put the cursor on %q, want c/charlie", got)
	}
}

// A LOW ACCOUNT CUTS THE LIST TO FREE ROWS. With the default provider's balance
// known low, the model list offers the `:free` ids and the catalog rows priced
// at zero, says `free only` on its count line, and the line under the field
// says why. The model in use stays on the list whatever it costs, so enter
// still confirms rather than changes. A reading that lands while the list is
// open re-aims the cursor at the model in use.
func TestALowAccountOffersOnlyFreeModelsOnTheSetupScreen(t *testing.T) {
	a, dir := controlsApp(t, nil)
	catalog := []Model{
		{ID: "openai/gpt-4.1-mini", PriceKnown: true, PromptPrice: 0.4, CompletionPrice: 1.6},
		{ID: "paid/alpha", PriceKnown: true, PromptPrice: 1, CompletionPrice: 2},
		{ID: "qwen/qwen3.8-27b:free", PriceKnown: true},
		{ID: "zero/priced", PriceKnown: true},
		{ID: "unknown/price"},
	}
	a.models = func() []Model { return catalog }
	a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{Known: true, Low: true}, nil }
	// Before any reading the whole catalog is offered.
	if got := len(a.setupModelChoices()); got != len(catalog) {
		t.Fatalf("with no reading the list offers %d rows, want the catalog's %d", got, len(catalog))
	}
	walkToControl(t, a, controlChatModel)
	pressSetup(a, key("enter"))
	pressSetup(a, key("down"), key("down"), key("down"))
	// The reading lands while the list is open, by the road the read takes.
	if err := config.WriteCreditsReading(dir, config.APIKeyAt(dir), credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
	pressSetup(a, creditReadMsg{reading: credits.Reading{Known: true, Low: true}})
	if !a.creditsLow {
		t.Fatal("the fixture's reading did not read as low")
	}
	choices := a.setupModelChoices()
	ids := make([]string, 0, len(choices))
	for _, model := range choices {
		ids = append(ids, model.ID)
	}
	want := []string{"openai/gpt-4.1-mini", "qwen/qwen3.8-27b:free", "zero/priced"}
	if strings.Join(ids, " ") != strings.Join(want, " ") {
		t.Fatalf("a low account offers %v, want %v (the model in use, then the free rows)", ids, want)
	}
	if got := choices[a.setup.modelAt].ID; got != a.model {
		t.Fatalf("after the reading the cursor is on %q, want the model in use %q", got, a.model)
	}
	screen := setupScreen(a)
	for _, wanted := range []string{"free only", setupLowCreditsWord} {
		if !strings.Contains(screen, wanted) {
			t.Fatalf("the screen must say %q; got:\n%s", wanted, screen)
		}
	}
	// AND THE WARNING IS ON THE FRAME'S LAST ROW, right aligned, where the keys
	// row under a conversation's message box carries it — not under the field.
	rows := strings.Split(rawSetupFrame(a), "\n")
	foot := strings.TrimRight(rows[len(rows)-1], " ")
	if !strings.HasSuffix(foot, setupLowCreditsWord) || ansi.StringWidth(foot) != a.width-1 {
		t.Fatalf("the warning is not right-aligned on the last row: %q", foot)
	}
	for _, row := range rows[:len(rows)-1] {
		if strings.Contains(row, setupLowCreditsWord) {
			t.Fatalf("the warning is still drawn inside the form:\n%s", rawSetupFrame(a))
		}
	}
	// And typing narrows the cut list, never the whole catalog.
	pressSetup(a, key("a"))
	for _, model := range a.setupModelChoices() {
		if model.ID == "paid/alpha" {
			t.Fatal("typing reached a paid row through the filter")
		}
	}
}

// THE SECOND ENTER SENDS. A starting point fills the box on the first enter and
// stays selected; the next enter is the ordinary send of what is in the box.
// An earlier build kept taking that enter for the starting point — which could
// fill nothing, the box being full — so pressing enter twice did nothing at all.
func TestEnterAfterAStartingPointSendsWhatItFilled(t *testing.T) {
	a := firstChatApp(t)
	agent, ok := a.agent.(*fakeAgent)
	if !ok {
		t.Skip("this fixture's agent cannot be asked what it was sent")
	}
	drive(t, a, key("down"), key("enter"))
	if len(agent.sent) != 0 {
		t.Fatalf("the first enter sent %v", agent.sent)
	}
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || !strings.Contains(agent.sent[0], welcomeStarters[0].fills) {
		t.Fatalf("the second enter sent %v, want the starting point's sentence", agent.sent)
	}
}

// AN EXPIRED KEY DOES NOT CUT THE LIST — the free rows fail on the same key —
// but the line under the field says what is wrong and where the fix is.
func TestAnExpiredKeyIsSaidUnderTheModelRowWithoutCuttingTheList(t *testing.T) {
	a, dir := controlsApp(t, nil)
	catalog := []Model{
		{ID: "openai/gpt-4.1-mini", PriceKnown: true, PromptPrice: 0.4, CompletionPrice: 1.6},
		{ID: "paid/alpha", PriceKnown: true, PromptPrice: 1, CompletionPrice: 2},
		{ID: "qwen/qwen3.8-27b:free", PriceKnown: true},
	}
	a.models = func() []Model { return catalog }
	a.readCredits = func(context.Context) (credits.Reading, error) {
		return credits.Reading{Known: true, Expired: true}, nil
	}
	if err := config.WriteCreditsReading(dir, config.APIKeyAt(dir), credits.Reading{Known: true, Expired: true}); err != nil {
		t.Fatal(err)
	}
	pressSetup(a, creditReadMsg{reading: credits.Reading{Known: true, Expired: true}})
	if !a.creditsExpired || a.setupFreeOnly() {
		t.Fatalf("expired=%v freeOnly=%v, want expired and the whole list", a.creditsExpired, a.setupFreeOnly())
	}
	if got := len(a.setupModelChoices()); got != len(catalog) {
		t.Fatalf("an expired key cut the list to %d rows, want the catalog's %d", got, len(catalog))
	}
	screen := setupScreen(a)
	if !strings.Contains(screen, setupExpiredKeyWord) {
		t.Fatalf("the screen must say %q; got:\n%s", setupExpiredKeyWord, screen)
	}
	if strings.Contains(screen, "free only") || strings.Contains(screen, setupLowCreditsWord) {
		t.Fatalf("an expired key was spelled as a low account:\n%s", screen)
	}
}

// THE EXAMPLES GO ROUND: `→` on the last one is the first, `←` on the first is
// the last, and the count on the edge says where you are.
func TestTheExamplesWrapAroundInBothDirections(t *testing.T) {
	a, _ := controlsApp(t, nil)
	a.setup.example = len(setupExamples) - 1
	pressSetup(a, key("right"))
	if a.setup.example != 0 {
		t.Fatalf("→ on the last example landed on %d, want the first", a.setup.example)
	}
	pressSetup(a, key("left"))
	if a.setup.example != len(setupExamples)-1 {
		t.Fatalf("← on the first example landed on %d, want the last", a.setup.example)
	}
	a.settleSetupDemo()
	count := strings.Join(strings.Fields(setupShowcaseCount(len(setupExamples)-1, len(setupExamples))), " ")
	if !strings.Contains(setupScreen(a), count) {
		t.Fatalf("the edge does not say where the walk is:\n%s", setupScreen(a))
	}
}

// A ROW'S NAME IS PAINTED BY ONE RULE ON EVERY ROW: the accent while the focus
// is on it, dim once enter has answered it, and the body ink until then.
func TestRowNamesAreAccentWhenFocusedDimWhenAnsweredAndInkUntilThen(t *testing.T) {
	a, _ := controlsApp(t, nil)
	a.models = func() []Model { return []Model{{ID: "openai/gpt-4.1-mini"}, {ID: "b/bravo"}} }
	pal := a.pal
	limit := padTo(controlLimitLabel, controlLabelWidth)
	model := padTo(controlModelLabel, controlLabelWidth)
	frame, _, _ := a.frame()
	if !strings.Contains(frame, pal.accent(limit)) {
		t.Fatalf("the focused limit's name is not the accent:\n%s", plain(frame))
	}
	if !strings.Contains(frame, pal.ink(model)) {
		t.Fatalf("the untouched model row's name is not the body ink:\n%s", plain(frame))
	}
	pressSetup(a, key("enter")) // the limit, answered; the focus moves on
	frame, _, _ = a.frame()
	if !strings.Contains(frame, pal.dim(limit)) {
		t.Fatalf("the answered limit's name is not dim:\n%s", plain(frame))
	}
	if !strings.Contains(frame, pal.accent(model)) {
		t.Fatalf("the focused model row's name is not the accent:\n%s", plain(frame))
	}
	// Opening the list and leaving it with esc answers nothing.
	pressSetup(a, key("enter"), key("esc"))
	if frame, _, _ = a.frame(); !strings.Contains(frame, pal.accent(model)) {
		t.Fatalf("esc out of the list changed the focused row's paint:\n%s", plain(frame))
	}
	pressSetup(a, key("enter"), key("enter")) // open, take the model in use; the focus moves on
	frame, _, _ = a.frame()
	if !strings.Contains(frame, pal.dim(model)) {
		t.Fatalf("the answered model row's name is not dim:\n%s", plain(frame))
	}
	if !strings.Contains(frame, pal.accent(fit(controlReviewDefaults, setupFormWidth-2))) {
		t.Fatalf("the focused review row's name is not the accent:\n%s", plain(frame))
	}
	if !strings.Contains(frame, pal.ink(controlStartWord)) {
		t.Fatalf("the untouched way out is not the body ink:\n%s", plain(frame))
	}
	// Walking back onto an answered row paints it the accent again; leaving
	// it, dim again.
	pressSetup(a, key("up"))
	if frame, _, _ = a.frame(); !strings.Contains(frame, pal.accent(model)) {
		t.Fatalf("the answered row under the focus is not the accent:\n%s", plain(frame))
	}
	pressSetup(a, key("down"))
	if frame, _, _ = a.frame(); !strings.Contains(frame, pal.dim(model)) {
		t.Fatalf("the answered row is not dim again once left:\n%s", plain(frame))
	}
}
