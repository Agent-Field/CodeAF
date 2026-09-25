package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ── THE PAYLOAD RULE, HELD TO ITS OWN SENTENCE ──────────────────────────────
//
// A LINE MAY BE QUIET; THE FACT IT CARRIES MAY NOT BE (payload.go).
//
// What these tests pin is the DIFFERENCE and never the value: every assertion
// below asks whether the datum is painted in a different role from the prose
// around it, and asks the palette which roles those are. A test that spelled a
// hex would be a second author of the palette, which designlanguage_test.go
// forbids by name.

// lifted reports whether row paints text in [palette.data] — the hue the
// payload rule lifts a datum into.
func lifted(pal palette, row, text string) bool {
	return strings.Contains(row, pal.data(text))
}

// dimmed is the same question about the quiet tier the prose stays in.
func dimmed(pal palette, row, text string) bool {
	return strings.Contains(row, pal.dim(text))
}

// noteRows renders one note the way the transcript draws it.
func noteRows(a *app, text string, facts ...string) []string {
	e := entry{kind: entryNote, text: text, facts: facts}
	return a.renderEntry(0, &e, a.width)
}

// THE LINE THE WHOLE RULE WAS WRITTEN FOR. `/crew` answers with the four models
// it just set, and until this wave the ids and the words around them were one
// flat dim sentence — so the person who typed the command to find out WHICH
// MODELS read a line where the answer had exactly the weight of the question.
func TestACrewLineCarriesItsModelsInTheDataHueAndItsProseInDim(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width = 200 // one row, so the assertion is about paint and not about wrap

	line := "crew → balanced · brain kimi-k3:low · hands deepseek-v4-flash · checks qwen3.8-27b"
	rows := noteRows(a, line, "kimi-k3:low", "deepseek-v4-flash", "qwen3.8-27b")
	if len(rows) != 1 {
		t.Fatalf("the crew line wanted one row, got %d: %q", len(rows), rows)
	}
	row := rows[0]

	for _, model := range []string{"kimi-k3:low", "deepseek-v4-flash", "qwen3.8-27b"} {
		if !lifted(a.pal, row, model) {
			t.Fatalf("the model %q is not in the data hue — the answer is as quiet as the question:\n%q",
				model, row)
		}
	}
	// AND THE ROLE WORDS STAY WHERE THEY WERE. They are the question the ids
	// answer, and a line where everything is bright is a line where nothing is.
	for _, word := range []string{"brain", "hands", "checks"} {
		if lifted(a.pal, row, word) {
			t.Fatalf("the role word %q was lifted with its model — the rule lifts the "+
				"answer and not the label:\n%q", word, row)
		}
	}
	// AND THE LANE IS STILL THE LANE. The marker and the words that open the
	// sentence are the surface talking about itself and stay exactly where they
	// were — the lead is painted with the prose, in one run, as it always was.
	if !strings.HasPrefix(row, a.pal.dim("· crew → balanced · brain ")) {
		t.Fatalf("the note lost its dim lead — a note is still the surface talking "+
			"about itself:\n%q", row)
	}
	// AND NOTHING ON SCREEN MOVED. The rule repaints runes; it never adds one.
	if got := ansi.Strip(row); got != "· "+line {
		t.Fatalf("the payload rule changed the words:\n\tgot  %q\n\twant %q", got, "· "+line)
	}
}

// A NOTE THAT NAMES NO DATA IS THE NOTE IT ALWAYS WAS. The rule is opt-in from
// the builder's side, so every line on this surface that has not been thought
// about is still one flat dim sentence rather than one this file guessed at.
func TestANoteThatNamesNoFactsIsDrawnExactlyAsItWas(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width = 200

	rows := noteRows(a, "stopped")
	if len(rows) != 1 || rows[0] != a.pal.dim("· stopped") {
		t.Fatalf("a note with no facts is not the plain dim line it was:\n%q", rows)
	}
}

// THE DOOR A NOTE NAMES WEARS THE CHIP IT WEARS EVERYWHERE ELSE. A slash
// command is the one word on this surface a person can type back verbatim, and
// slashchip.go marks it in the box and in the sent message — a refusal that
// pointed at `/model` in the same dim as its own apology was the one place the
// mark was missing.
func TestANoteChipsTheDoorItNames(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width = 200

	rows := noteRows(a, "memory is off · turn it on under /settings")
	if !strings.Contains(rows[0], a.pal.chip("/settings")) {
		t.Fatalf("the door in a note is not chipped:\n%q", rows[0])
	}
	// AND A WORD THAT ONLY LOOKS LIKE ONE IS NOT PROMISED. The chip's second law
	// (slashchip.go) survives the move into this lane.
	plain := noteRows(a, unknownCommandWord("tsak"))
	if strings.Contains(plain[0], a.pal.chip("/tsak")) {
		t.Fatalf("a word this surface refuses was chipped as though it ran:\n%q", plain[0])
	}
}

// THE KEY SHEET'S CHORD READS ABOVE ITS EXPLANATION, and its commands keep the
// chip rather than borrowing the ink — the two marks mean two different things
// and /help is the one page where both are on screen at once.
func TestTheKeySheetLiftsItsChordsAndChipsItsCommands(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width = 200

	help := helpText("", chordSpelling{})
	rows := noteRows(a, help, columnFacts(help, true)...)
	body := strings.Join(rows, "\n")

	for _, chord := range []string{"ctrl+b", "ctrl+o", "alt+enter", "@path"} {
		if !lifted(a.pal, body, chord) {
			t.Fatalf("the key sheet draws %q at the weight of the sentence beside it", chord)
		}
	}
	if !strings.Contains(body, a.pal.chip("/help")) {
		t.Fatalf("a command on the key sheet lost the chip it wears everywhere else")
	}
	// AND THE EXPLANATIONS STAY QUIET. If the whole sheet stepped up it would be
	// the flat page it was, one tier louder.
	if lifted(a.pal, body, "copy mode") {
		t.Fatalf("the sentence beside a chord was lifted with it")
	}
}

// /status AND /cost CARRY THEIR PAYLOAD IN THE SECOND COLUMN: the label is
// furniture that sits in the same place every time, and the figure is what the
// command was typed for.
func TestAStatusFigureReadsAboveItsLabel(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width = 200

	text := "spend        $0.42\nmodel calls  14"
	rows := noteRows(a, text, columnFacts(text, false)...)
	body := strings.Join(rows, "\n")

	if !lifted(a.pal, body, "$0.42") || !lifted(a.pal, body, "14") {
		t.Fatalf("a /cost figure is drawn at the weight of its own label:\n%q", body)
	}
	if lifted(a.pal, body, "spend") || lifted(a.pal, body, "model calls") {
		t.Fatalf("a label was lifted with its figure:\n%q", body)
	}
}

// THE COLUMN IS READ BACK OFF THE TEXT, in both directions, so the fact list and
// the note cannot be two spellings of one thing that drift apart.
func TestColumnFactsReadEitherHalfOfATwoColumnNote(t *testing.T) {
	text := "codeaf\n\n/help          what you can type\nctrl+b         copy mode\nsession · x.json"
	if got := columnFacts(text, true); len(got) != 2 || got[0] != "/help" || got[1] != "ctrl+b" {
		t.Fatalf("the leading column is not the two keys: %q", got)
	}
	if got := columnFacts(text, false); len(got) != 2 || got[0] != "what you can type" {
		t.Fatalf("the trailing column is not the two sentences: %q", got)
	}
	// AN INDENT IS NOT A GUTTER: /crew's listing sets its class rows four spaces
	// in, and a rule that cut at the first run of spaces would call the whole row
	// the second column.
	indented := "  balanced — a line\n    mastermind    kimi-k3:low"
	if got := columnFacts(indented, false); len(got) != 1 || got[0] != "kimi-k3:low" {
		t.Fatalf("an indented two-column row was not read: %q", got)
	}
}

// A FACT IS FOUND ON A WORD BOUNDARY AND NOWHERE ELSE. `14` inside `48.1k`
// is not the model-call count, and a figure highlighted in the middle of
// another figure is worse than no highlight at all.
func TestAFactIsNotFoundInsideAnotherWord(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width = 200

	rows := noteRows(a, "tokens 148.1k · calls 14", "14")
	if !strings.Contains(rows[0], a.pal.dim("· tokens 148.1k · calls ")) {
		t.Fatalf("the walk lifted two digits out of the middle of a figure:\n%q", rows[0])
	}
	if !lifted(a.pal, rows[0], "14") {
		t.Fatalf("the count itself was never lifted:\n%q", rows[0])
	}
}

// ── THE HINT SLOT ───────────────────────────────────────────────────────────

// THE CHORD READS ABOVE THE VERB. The legend is the border under the
// conversation and its right end is the only place this surface says what the
// next keystroke does — so the key steps to ink while the border and the word
// beside it stay at the dim value the rule itself is drawn in.
func TestTheLegendsChordReadsAboveItsExplanation(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width = 80
	a.state = stateWorking

	line := a.hintRow(a.width)
	if !lifted(a.pal, line, "esc") {
		t.Fatalf("the keys row draws its chord at the weight of its prose:\n%q", line)
	}
	if !dimmed(a.pal, line, " interrupt") {
		t.Fatalf("the verb beside the chord was lifted with it:\n%q", line)
	}
	// AND THE BORDER IS THE SAME BORDER. The rule repaints; it never re-measures.
	if got := ansi.StringWidth(line); got != a.width {
		t.Fatalf("the legend is %d cells wide and the frame is %d", got, a.width)
	}
}

// THE GRAMMAR NEVER LIFTS AN ARTICLE. The one-letter key and English's own
// one-letter word are the same character, and a segment that is a SENTENCE
// rather than a key and its verb is where they collide — four words, one past
// [chordSegmentWords]. The line is spelled here rather than quoted from a live
// hint because no hint on the surface has this shape today; the rule is what
// the next one to grow an article will meet.
func TestTheHintGrammarNeverLiftsAnArticle(t *testing.T) {
	said := "esc no · a task will stop"
	got := chordSpans(said)
	if len(got) != 1 {
		t.Fatalf("%q wanted one chord, got %d: %v", said, len(got), got)
	}
	value := []rune(said)
	if lifted := string(value[got[0].from:got[0].to]); lifted != "esc" {
		t.Fatalf("%q lifted %q rather than its key", said, lifted)
	}
}

// EVERY HINT THIS SURFACE WRITES, READ BY THE GRAMMAR THAT PAINTS THEM. The
// slot's own comment (render.go's [app.hintWord]) lists the states; this is what
// each of their lines actually lifts, written down so a reworded hint that
// stopped naming its key fails here rather than going quiet on the frame.
func TestTheHintGrammarReadsEveryHintThisSurfaceWrites(t *testing.T) {
	type hintGrammarCase struct {
		hint string
		want []string
	}
	cases := []hintGrammarCase{
		{"drag to select · any key ends it", nil},
		{"esc interrupt", []string{"esc"}},
		{pickerKeysSwitch, []string{"enter", "esc"}},
		// The picker's slot follows its cursor (palette.go's [picker.keysHint]).
		{pickerKeysModel, []string{"→", "alt+s", "enter", "ctrl+t", "esc"}},
		{pickerKeysModelTab, []string{"tab", "alt+s", "enter", "ctrl+t", "esc"}},
		{pickerKeysSwitchEffort, []string{"alt+s", "enter", "ctrl+t", "esc"}},
		{pickerKeysFold, []string{"←", "alt+s", "enter", "esc"}},
		{pickerKeysFoldTab, []string{"tab", "alt+s", "enter", "esc"}},
		{"↑↓ · ←→ family · enter apply · esc", []string{"↑↓", "←→", "enter", "esc"}},
		{"enter open · esc", []string{"enter", "esc"}},
		{"v select · a block · y yank · esc", []string{"v", "a", "y", "esc"}},
		{"esc again to rewind", []string{"esc"}},
		{"↑↓ recent · enter open", []string{"↑↓", "enter"}},
		{"tab take · enter run · esc", []string{"tab", "enter", "esc"}},
		{"enter answer · esc no", []string{"enter", "esc"}},
		// A standing card's answers under the errand pane's box, built from the
		// question rather than written down (homeexchange.go's
		// [exchangeAnswerWords]): the digits the card drew, then the key that
		// asks for the box instead.
		{"1 Set it up · 3 Only now, don't repeat · 0 Don't set it up · o Change…",
			[]string{"1", "3", "0", "o"}},
		{"1-3 shape · esc never mind", []string{"1-3", "esc"}},
		{"y allow · n deny · a always", []string{"y", "n", "a"}},
		{"↑↓ move · →← tree · enter open · alt+w wide · esc",
			[]string{"↑↓", "→←", "enter", "alt+w", "esc"}},
		// The roster's hold hint on a row that is the person's call
		// (tasksettle.go's [app.roomSettleHintFor]): a letter lifted out of each
		// chip the card is actually drawing.
		{"a accept · n not right · s tell it · esc", []string{"a", "n", "s", "esc"}},
		{"esc stops and sends", []string{"esc"}},
		{"opt+e effort · opt+a approvals · opt+k chats · / commands · space space home",
			[]string{"opt+e", "opt+a", "opt+k", "/", "space", "space"}},
		{"enter open where it was asked · p pause · s stop · n not here · esc",
			[]string{"enter", "p", "s", "n", "esc"}},
		{"enter open · ctrl+r reveal · ctrl+y copy · esc",
			[]string{"enter", "ctrl+r", "ctrl+y", "esc"}},
		{"ctrl+enter keeps this true", []string{"ctrl+enter"}},
		{"ctrl+g tasks", []string{"ctrl+g"}},
		{"x stop", []string{"x"}},
		{"nothing to rewind", nil},
		{"↑ or click to edit", []string{"↑"}},
	}
	// H6: every sentence the running-turn composer can produce lifts the key at
	// the head of every clause and leaves the verbs as prose. The five possible
	// send prefixes combine independently with the background and stop clauses.
	runPrefixes := []hintGrammarCase{
		{},
		{hint: enterWaitHint, want: []string{"enter"}},
		{hint: enterWaitHint + " · " + bargeKey + " " + bargeSendWord, want: []string{"enter", bargeKey}},
		{hint: steerShortHint, want: []string{"enter"}},
		{hint: steerShortHint + " · " + bargeKey + " " + bargeSendWord, want: []string{"enter", bargeKey}},
	}
	for _, prefix := range runPrefixes {
		for _, background := range []bool{false, true} {
			for _, stop := range []string{"esc interrupt", parkedHint[1]} {
				parts := []string{}
				want := append([]string(nil), prefix.want...)
				if prefix.hint != "" {
					parts = append(parts, prefix.hint)
				}
				if background {
					parts = append(parts, "ctrl+g backgrounds")
					want = append(want, "ctrl+g")
				}
				parts = append(parts, stop)
				want = append(want, "esc")
				cases = append(cases, hintGrammarCase{hint: strings.Join(parts, hintSegment), want: want})
			}
		}
	}
	for _, c := range cases {
		value := []rune(c.hint)
		var got []string
		for _, s := range chordSpans(c.hint) {
			got = append(got, string(value[s.from:s.to]))
		}
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Fatalf("%q lifts %v, wanted %v", c.hint, got, c.want)
		}
	}
}
