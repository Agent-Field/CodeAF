package composer

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// -- the glyph ruler (12, 12.7) ------------------------------------------------

// TestEveryGlyphThisSurfaceDrawsIsOneCell measures the characters this package
// puts on screen against BOTH shipping rulers — grapheme and wcwidth — because
// a two-cell caret or a two-cell spinner would move the row it sits on every
// time it changed, which is the width instability 5.17 exists to forbid.
func TestEveryGlyphThisSurfaceDrawsIsOneCell(t *testing.T) {
	cells := []string{caretBlock, tokens.GlyphPromptChat, tokens.GlyphPromptSteer,
		tokens.GlyphSeparator, tokens.GlyphEllipsis, tokens.GlyphAccentRail,
		glyphEnter, glyphCtrl,
		tokens.NerdFont.Glyph(tokens.GPromptChat),
		tokens.NerdFont.Glyph(tokens.GPromptSteer)}
	cells = append(cells, tokens.SpinnerFrames[:]...)
	for _, cell := range cells {
		if w := ansi.StringWidth(cell); w != 1 {
			t.Fatalf("%q measures %d cells under the grapheme ruler", cell, w)
		}
		if w := ansi.StringWidthWc(cell); w != 1 {
			t.Fatalf("%q measures %d cells under wcwidth", cell, w)
		}
	}
}

// TestThePlainTierDrawsNoPrivateUseGlyph is 12's plain-twin rule, enforced from
// the consumer's side: every cell this package draws under [tokens.Plain] is a
// character an unpatched font has, and flipping the tier moves no column.
func TestThePlainTierDrawsNoPrivateUseGlyph(t *testing.T) {
	build := func(set tokens.GlyphSet, prompt Prompt) *Model {
		m := New(Options{Prompt: prompt, Styler: tokens.NewStylerIn(tokens.NoColor, tokens.FocusNormal, set)})
		m.Focus(true)
		return m
	}
	for _, prompt := range []Prompt{PromptChat, PromptSteer} {
		for _, streaming := range []bool{false, true} {
			for _, draft := range []string{"", "a draft with words in it"} {
				plain, nerd := build(tokens.Plain, prompt), build(tokens.NerdFont, prompt)
				typeString(plain, draft)
				typeString(nerd, draft)
				plain.Streaming(streaming, 4)
				nerd.Streaming(streaming, 4)

				out := plain.Render(60, 1)
				for _, r := range out {
					if r >= 0xE000 && r <= 0xF8FF {
						t.Fatalf("the plain tier drew a private-use rune %U: %q", r, out)
					}
				}
				if a, b := ansi.StringWidth(out), ansi.StringWidth(nerd.Render(60, 1)); a != b {
					t.Fatalf("the tiers disagree about the row's width (%d vs %d): %q", a, b, out)
				}
			}
		}
	}
}

// TestWidthSweep20To140 is the brief's sweep, walked over every state this lane
// added: hints, a caret at both ends of a draft, a multi-line draft, an open
// completion and a spinning prompt. Nothing panics, nothing overruns the
// rectangle it was handed, and no frame is taller than its height.
func TestWidthSweep20To140(t *testing.T) {
	sty := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	states := map[string]func() *Model{
		"empty and focused": func() *Model {
			m := New(Options{Styler: sty})
			m.Focus(true)
			return m
		},
		"one line": func() *Model {
			m := New(Options{Styler: sty})
			m.Focus(true)
			typeString(m, "a single line of draft")
			return m
		},
		"many lines": func() *Model {
			m := New(Options{Styler: sty, NewlineKeys: []string{"alt+enter"}})
			m.Focus(true)
			for i := 0; i < 12; i++ {
				typeString(m, "a line of a long draft")
				m.Key(altEnterKey())
			}
			return m
		},
		"streaming": func() *Model {
			m := New(Options{Styler: sty})
			m.Focus(true)
			typeString(m, "answer me")
			m.Streaming(true, 7)
			return m
		},
		"list open": func() *Model {
			m := New(Options{Styler: sty, Commands: slashCatalog, OnCommand: func(string) tea.Cmd { return nil }})
			m.Focus(true)
			typeString(m, "/")
			return m
		},
	}
	for name, build := range states {
		for width := 20; width <= 140; width++ {
			m := build()
			for _, height := range []int{1, 2, 1 + m.GrowRows(width), 20} {
				if height <= 0 {
					continue
				}
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%s: Render(%d,%d) panicked: %v", name, width, height, r)
						}
					}()
					lines := strings.Split(m.Render(width, height), "\n")
					if len(lines) > height {
						t.Fatalf("%s: Render(%d,%d) returned %d lines", name, width, height, len(lines))
					}
					for i, line := range lines {
						if w := ansi.StringWidth(line); w > width {
							t.Fatalf("%s: Render(%d,%d) line %d is %d cells: %q", name, width, height, i, w, line)
						}
					}
				}()
			}
		}
	}
}

// TestRender_WidthStability is the golden-harness-style sweep the brief asks
// for: every width from 1 to 110, a couple of heights, empty and non-empty
// drafts, focused and not — nothing may panic and no rendered line may
// exceed the width it was given.
func TestRender_WidthStability(t *testing.T) {
	sty := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	for _, height := range []int{1, 2, 3, 6, 20} {
		for width := 1; width <= 110; width++ {
			m := New(Options{Styler: sty})
			typeString(m, "a reasonably long draft that should wrap across several rows of the composer")
			m.Focus(width%2 == 0)

			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("Render(%d,%d) panicked: %v", width, height, r)
					}
				}()
				out := m.Render(width, height)
				lines := strings.Split(out, "\n")
				if len(lines) > height {
					t.Fatalf("Render(%d,%d) returned %d lines, want at most %d", width, height, len(lines), height)
				}
				for i, line := range lines {
					if w := ansi.StringWidth(line); w > width {
						t.Fatalf("Render(%d,%d) line %d has width %d: %q", width, height, i, w, line)
					}
				}
			}()
		}
	}
}

func TestRender_ZeroRectNeverPanics(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	typeString(m, "hello")
	for _, wh := range [][2]int{{0, 0}, {0, 5}, {5, 0}, {-1, 5}, {5, -1}} {
		if out := m.Render(wh[0], wh[1]); out != "" {
			t.Fatalf("Render(%d,%d) = %q, want empty for a degenerate rect", wh[0], wh[1], out)
		}
	}
}

// -- the ghost hints (8, 15) ---------------------------------------------------

// TestGhostHintIdleTeachesTheGrammars: an empty focused composer at rest says
// what the surface can DO — ask, and the two characters that open the other two
// grammars — where a "Type a message" label used to name what the reader is
// already looking at.
func TestGhostHintIdleTeachesTheGrammars(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	m.Focus(true)
	out := m.Render(60, 1)
	for _, cell := range hintCells[HintIdle] {
		if !strings.Contains(out, cell) {
			t.Fatalf("Render() = %q, want the ghost hint %q", out, cell)
		}
	}
	if !strings.Contains(out, hintSep) {
		t.Fatalf("Render() = %q, want the cells joined by the telemetry separator", out)
	}
}

// TestGhostHintsVanishOnTheFirstKeystrokeAndStayGone is 8's exact rule: they go
// on the first character and do NOT come back when the draft is backspaced to
// empty again — they teach an opening move, and a person mid-sentence is not at
// the opening.
func TestGhostHintsVanishOnTheFirstKeystrokeAndStayGone(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	m.Focus(true)
	if !strings.Contains(m.Render(60, 1), hintCells[HintIdle][0]) {
		t.Fatalf("an empty focused composer drew no hints")
	}
	typeString(m, "a")
	if strings.Contains(m.Render(60, 1), hintCells[HintIdle][0]) {
		t.Fatalf("the hints survived the first keystroke: %q", m.Render(60, 1))
	}
	m.Key(backspaceKey())
	if m.Value() != "" {
		t.Fatalf("Value() = %q, want the draft emptied", m.Value())
	}
	if strings.Contains(m.Render(60, 1), hintCells[HintIdle][0]) {
		t.Fatalf("the hints reappeared mid-draft: %q", m.Render(60, 1))
	}
}

// TestGhostHintsReturnWhenTheDraftIsGone: sent, stashed or killed, there is an
// opening again — and the hints are what an opening looks like.
func TestGhostHintsReturnWhenTheDraftIsGone(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(*Model)
	}{
		{"sent", func(m *Model) { m.Key(enterKey()) }},
		{"stashed", func(m *Model) { m.Key(escKey()) }},
		{"killed", func(m *Model) { m.Key(ctrlKey('u')) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal), OnSubmit: func(string) {}})
			m.Focus(true)
			typeString(m, "words")
			tc.end(m)
			if m.Value() != "" {
				t.Fatalf("Value() = %q, want the draft gone", m.Value())
			}
			if !strings.Contains(m.Render(60, 1), hintCells[HintIdle][0]) {
				t.Fatalf("the hints did not return after the draft was %s: %q", tc.name, m.Render(60, 1))
			}
		})
	}
}

// TestGhostHintsShedFromTheRight: `?` is the door to every other door, so it is
// the last cell to leave a narrowing terminal — and a cell is never cut in half,
// because half a chord teaches a key that does not exist.
func TestGhostHintsShedFromTheRight(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	m.Focus(true)
	seen := 0
	for width := 60; width >= 4; width-- {
		out := m.Render(width, 1)
		shown := 0
		for _, cell := range hintCells[HintIdle] {
			if strings.Contains(out, cell) {
				shown++
			}
		}
		if shown > seen && width != 60 {
			t.Fatalf("width %d showed MORE cells (%d) than the wider frame did (%d)", width, shown, seen)
		}
		seen = shown
		for i := shown; i < len(hintCells[HintIdle]); i++ {
			if strings.Contains(out, hintCells[HintIdle][i]) {
				t.Fatalf("width %d dropped a cell out of order: %q", width, out)
			}
		}
	}
}

// TestGhostHintsOnlyWhereTheKeyboardIs: an unfocused composer teaches nobody,
// because nobody is typing into it.
func TestGhostHintsOnlyWhereTheKeyboardIs(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	out := m.Render(60, 1)
	if strings.Contains(out, hintCells[HintIdle][0]) {
		t.Fatalf("an unfocused composer drew ghost hints: %q", out)
	}
	if strings.TrimSpace(out) != edged(60, tokens.GlyphPromptChat) {
		t.Fatalf("an unfocused empty composer = %q, want the edge and the prompt and nothing else", out)
	}
}

// -- the prompt and the caret (1, 12) -----------------------------------------

func TestRender_PromptGlyphPresent(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	typeString(m, "hi")
	out := m.Render(40, 3)
	if !strings.Contains(out, tokens.GlyphPromptChat) {
		t.Fatalf("Render() = %q, want the › prompt glyph", out)
	}
}

// TestPromptWearsItsTier: the prompt is resolved through the glyph tier, so a
// patched font draws the icon and every other terminal draws the 5.17 twin.
// Both are one cell, so the draft's text column does not move.
func TestPromptWearsItsTier(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prompt Prompt
		plain  string
		id     tokens.GlyphID
	}{
		{"chat", PromptChat, tokens.GlyphPromptChat, tokens.GPromptChat},
		{"steer", PromptSteer, tokens.GlyphPromptSteer, tokens.GPromptSteer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plain := New(Options{Prompt: tc.prompt, Styler: tokens.NewStylerIn(tokens.NoColor, tokens.FocusNormal, tokens.Plain)})
			typeString(plain, "hi")
			if got := m0(plain.Render(40, 1)); !strings.HasPrefix(got, edged(40, tc.plain)) {
				t.Fatalf("plain tier drew %q, want it to lead with %q", got, edged(40, tc.plain))
			}
			nerd := New(Options{Prompt: tc.prompt, Styler: tokens.NewStylerIn(tokens.NoColor, tokens.FocusNormal, tokens.NerdFont)})
			typeString(nerd, "hi")
			want := tokens.NerdFont.Glyph(tc.id)
			if got := m0(nerd.Render(40, 1)); !strings.HasPrefix(got, edged(40, want)) {
				t.Fatalf("nerd tier drew %q, want it to lead with %q", got, edged(40, want))
			}
			if ansi.StringWidth(tc.plain) != ansi.StringWidth(want) {
				t.Fatalf("the two tiers disagree about the prompt's width")
			}
		})
	}
}

// m0 is the first row of a render.
func m0(out string) string { return strings.SplitN(out, "\n", 2)[0] }

// TestBindSwapsThePrompt: a region whose binding moves with the selected row
// says so through the model, not by rewriting the paint.
func TestBindSwapsThePrompt(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	m.Focus(true)
	if got := m0(m.Render(60, 1)); !strings.HasPrefix(got, edged(60, tokens.GlyphPromptChat)) ||
		!strings.Contains(got, hintCells[HintIdle][0]) {
		t.Fatalf("the resting binding drew %q", got)
	}

	m.Bind(PromptSteer)
	m.SetHint(HintSteer, "")
	got := m0(m.Render(60, 1))
	if !strings.HasPrefix(got, edged(60, tokens.GlyphPromptSteer)) {
		t.Fatalf("bound to steer, the row is %q", got)
	}
	if !strings.Contains(got, hintCells[HintSteer][0]) || strings.Contains(got, hintCells[HintIdle][0]) {
		t.Fatalf("bound to steer, the hint reads %q", got)
	}

	m.Bind(PromptChat)
	m.SetHint(HintIdle, "")
	if got := m0(m.Render(60, 1)); !strings.HasPrefix(got, edged(60, tokens.GlyphPromptChat)) ||
		!strings.Contains(got, hintCells[HintIdle][0]) {
		t.Fatalf("bound back to chat, the row is %q", got)
	}
}

// -- the hint is state, not a label (8 as amended) -----------------------------

// TestEveryHintStateSpeaksItsOwnLine walks the whole table: each state puts its
// own words on the empty line, and no state is silent.
func TestEveryHintStateSpeaksItsOwnLine(t *testing.T) {
	for state := Hint(0); state < hintCount; state++ {
		m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
		m.Focus(true)
		m.SetHint(state, "")
		out := m0(m.Render(70, 1))
		for _, cell := range hintCells[state] {
			if !strings.Contains(out, cell) {
				t.Fatalf("state %d drew %q, want the cell %q", state, out, cell)
			}
		}
	}
	// And the copy itself is the law's, verbatim — a hint nobody proofread is a
	// hint that drifts back into naming the widget.
	want := map[Hint]string{
		HintIdle: "ask for anything · @ jobs · / commands",
		// No accelerator: the key that stops a turn is named by the bar row's
		// `interrupt esc` chip, which is gated on esc actually working (§8.2.21).
		HintWorking:   "keep typing — messages queue",
		HintDelivered: "ask about the results · r rerun",
		HintQuestion:  "answer 1–3, or say it in words",
		HintSettled:   "this work is settled — ask about it",
		HintSteer:     "steer — one-way",
	}
	for state, sentence := range want {
		if got := strings.Join(hintCells[state], hintSep); got != sentence {
			t.Fatalf("state %d reads %q, want %q", state, got, sentence)
		}
	}
}

// TestStreamingOwnsTheHintWhileItRuns: the spinner is one cell to the left of
// these words, so the composer asserts the working line for itself rather than
// letting a stale state contradict the motion beside it.
func TestStreamingOwnsTheHintWhileItRuns(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	m.Focus(true)
	m.SetHint(HintDelivered, "")
	if got := m0(m.Render(70, 1)); !strings.Contains(got, hintCells[HintDelivered][0]) {
		t.Fatalf("idle row = %q", got)
	}
	m.Streaming(true, 2)
	got := m0(m.Render(70, 1))
	if !strings.Contains(got, hintCells[HintWorking][0]) {
		t.Fatalf("streaming row = %q, want the working line", got)
	}
	if strings.Contains(got, hintCells[HintDelivered][0]) {
		t.Fatalf("streaming row still carries the delivered line: %q", got)
	}
	m.Streaming(false, 0)
	if got := m0(m.Render(70, 1)); !strings.Contains(got, hintCells[HintDelivered][0]) {
		t.Fatalf("after streaming the row = %q, want the host's state back", got)
	}
}

// TestHintDetailReplacesTheSentenceAndKeepsTheKeys: the room's own words go
// where the sentence was, and the accelerators a state teaches survive them.
func TestHintDetailReplacesTheSentenceAndKeepsTheKeys(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	m.Focus(true)
	m.SetHint(HintDelivered, "ask about the NavCtx rework")
	out := m0(m.Render(70, 1))
	if !strings.Contains(out, "ask about the NavCtx rework") {
		t.Fatalf("row = %q, want the room's own words", out)
	}
	if !strings.Contains(out, hintCells[HintDelivered][1]) {
		t.Fatalf("row = %q, want the accelerator the state teaches", out)
	}
	if strings.Contains(out, hintCells[HintDelivered][0]) {
		t.Fatalf("row = %q, want the state's own sentence replaced", out)
	}
}

// TestAnOutOfRangeHintStillSpeaks: a caller handing this a number must not put
// the surface's voice out.
func TestAnOutOfRangeHintStillSpeaks(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	m.Focus(true)
	m.SetHint(Hint(200), "")
	if got := m0(m.Render(70, 1)); !strings.Contains(got, hintCells[HintIdle][0]) {
		t.Fatalf("an out-of-range state drew %q, want the idle line", got)
	}
}

// -- the terminal's own cursor (8, 11's third motion) --------------------------

// TestCaretAtNamesTheCellThePaintUsed: the terminal cursor and the painted block
// read one plan, so a host placing the real cursor cannot land it on a different
// cell than the one the reader is looking at.
func TestCaretAtNamesTheCellThePaintUsed(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal),
		NewlineKeys: []string{"alt+enter"}})
	m.Focus(true)
	for _, draft := range []string{"", "hello", "hello\nsecond line", "a much longer draft that has to wrap at this width"} {
		m.reset()
		m.typed = false
		for _, r := range draft {
			if r == '\n' {
				m.Key(altEnterKey())
				continue
			}
			m.Key(charKey(r))
		}
		width, height := 30, 1+m.GrowRows(30)
		x, y, ok := m.CaretAt(width, height)
		if !ok {
			t.Fatalf("draft %q reported no caret", draft)
		}
		rows := strings.Split(ansi.Strip(m.Render(width, height)), "\n")
		if y < 0 || y >= len(rows) {
			t.Fatalf("draft %q put the caret on row %d of %d", draft, y, len(rows))
		}
		if x < 0 || x > ansi.StringWidth(rows[y]) {
			t.Fatalf("draft %q put the caret at column %d of %q", draft, x, rows[y])
		}
		// The painted block is on exactly that cell.
		if got := []rune(rows[y]); x < len(got) && string(got[x]) != caretBlock &&
			m.cursor == len(m.value) {
			t.Fatalf("draft %q: row %q has %q at column %d, want the caret block", draft, rows[y], string(got[x]), x)
		}
	}
	unfocused := New(Options{})
	if _, _, ok := unfocused.CaretAt(30, 3); ok {
		t.Fatalf("an unfocused composer claimed a caret cell")
	}
}

// TestHostCursorStopsTheSecondCaret: when the shell puts the terminal's real
// cursor on the cell, this package draws none — two carets on one screen is the
// surface disagreeing with itself about where typing lands.
func TestHostCursorStopsTheSecondCaret(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	m.Focus(true)
	typeString(m, "hi")
	if !strings.Contains(m.Render(40, 1), caretBlock) {
		t.Fatalf("the floor draws no caret")
	}
	m.HostCursor(true)
	out := m.Render(40, 1)
	if strings.Contains(out, caretBlock) {
		t.Fatalf("the host owns the cursor and the paint drew one too: %q", out)
	}
	if !strings.Contains(out, "hi") {
		t.Fatalf("the draft went missing with the caret: %q", out)
	}
	// The empty line keeps its hint, one column further left.
	empty := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	empty.Focus(true)
	empty.HostCursor(true)
	if got := m0(empty.Render(60, 1)); !strings.Contains(got, hintCells[HintIdle][0]) {
		t.Fatalf("the empty line lost its hint under a host cursor: %q", got)
	}
}

// TestCursorSequenceIsDECSCUSR pins the two sequences 11's third motion is
// spent on. They are asserted as bytes because a wrong Ps is invisible in
// review and loud on a terminal.
func TestCursorSequenceIsDECSCUSR(t *testing.T) {
	if got, want := CursorSequence(true), "\x1b[1 q"; got != want {
		t.Fatalf("focused = %q, want DECSCUSR blinking block %q", got, want)
	}
	if got, want := CursorSequence(false), "\x1b[0 q"; got != want {
		t.Fatalf("unfocused = %q, want DECSCUSR reset-to-default %q", got, want)
	}
	if CursorBlinkingBlock == CursorDefault {
		t.Fatalf("the request and the restore are the same bytes")
	}
}

func TestRender_CursorVisibleWhenFocused(t *testing.T) {
	sty := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	unfocused := New(Options{Styler: sty})
	typeString(unfocused, "hi")
	plain := unfocused.Render(40, 3)

	focused := New(Options{Styler: sty})
	typeString(focused, "hi")
	focused.Focus(true)
	withCaret := focused.Render(40, 3)

	if plain == withCaret {
		t.Fatalf("focused render is byte-identical to unfocused render; no caret drawn")
	}
	if !strings.Contains(withCaret, caretBlock) {
		t.Fatalf("focused render has no accent block caret: %q", withCaret)
	}
}

// TestCaretIsAnAccentBlockAtTheEndOfTheDraft: the caret's usual home is one past
// the last character, and there it is the block itself — a CHARACTER, so it
// survives a profile with no background to spend.
func TestCaretIsAnAccentBlockAtTheEndOfTheDraft(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.TrueColor, tokens.ANSI256, tokens.ANSI16, tokens.NoColor} {
		m := New(Options{Styler: tokens.NewStyler(profile, tokens.FocusNormal)})
		typeString(m, "hi")
		m.Focus(true)
		if got := m.Render(40, 1); !strings.Contains(got, caretBlock) {
			t.Fatalf("profile %v drew no caret: %q", profile, got)
		}
	}
}

func TestRender_CursorMidTextVisible(t *testing.T) {
	sty := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	m := New(Options{Styler: sty})
	typeString(m, "abc")
	m.cursor = 1 // between 'a' and 'bc'
	m.Focus(true)
	out := m.Render(40, 3)
	// A caret over a letter keeps the letter and takes the accent on the band:
	// PaintOn always emits a background-setting SGR, which foreground-only text
	// never does.
	if !strings.Contains(out, "\x1b[48;2;") {
		t.Fatalf("no caret band drawn for a mid-text cursor: %q", out)
	}
	if !strings.Contains(out, "a") || !strings.Contains(out, "c") {
		t.Fatalf("surrounding text missing from render: %q", out)
	}
}

// -- the streaming indicator (8, 11) ------------------------------------------

// TestStreamingSpinsAtThePrompt: the one live signal this surface may carry
// takes the prompt's own cell, and gives it back the moment the answer lands.
func TestStreamingSpinsAtThePrompt(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	m.Focus(true)
	typeString(m, "hi")
	idle := m0(m.Render(40, 1))
	if !strings.HasPrefix(idle, edged(40, tokens.GlyphPromptChat)) {
		t.Fatalf("idle prompt = %q", idle)
	}

	for frame := 0; frame < len(tokens.SpinnerFrames); frame++ {
		m.Streaming(true, frame)
		got := m0(m.Render(40, 1))
		if want := edged(40, tokens.Spinner(frame)); !strings.HasPrefix(got, want) {
			t.Fatalf("frame %d drew %q, want it to lead with %q", frame, got, want)
		}
		if strings.HasPrefix(got, edged(40, tokens.GlyphPromptChat)) {
			t.Fatalf("frame %d still drew the prompt glyph: %q", frame, got)
		}
	}

	m.Streaming(false, 0)
	if got := m0(m.Render(40, 1)); got != idle {
		t.Fatalf("after streaming the prompt = %q, want the idle row back (%q)", got, idle)
	}
}

// TestStreamingCostsNoColumn: 5.21's width stability, in the smallest place it
// can be broken — the row must not move when the answer starts arriving.
func TestStreamingCostsNoColumn(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	typeString(m, "a draft that is being answered")
	before := ansi.StringWidth(m0(m.Render(50, 1)))
	m.Streaming(true, 3)
	if after := ansi.StringWidth(m0(m.Render(50, 1))); after != before {
		t.Fatalf("the row is %d cells while streaming and %d cells idle", after, before)
	}
}

// -- multiline growth (3) ------------------------------------------------------

// TestGrowRowsReportsTheDraftsOwnHeight: the region is budgeted for ONE draft
// row, so what the composer asks for is every row beyond it — and never more
// than [maxDraftRows], however long the draft gets.
func TestGrowRowsReportsTheDraftsOwnHeight(t *testing.T) {
	for _, tc := range []struct {
		lines int
		want  int
	}{{1, 0}, {3, 2}, {8, 7}, {9, 7}, {40, 7}} {
		m := New(Options{NewlineKeys: []string{"alt+enter"}})
		for i := 0; i < tc.lines; i++ {
			if i > 0 {
				m.Key(altEnterKey())
			}
			typeString(m, "line")
		}
		if got := m.GrowRows(40); got != tc.want {
			t.Fatalf("a %d-line draft asked for %d extra rows, want %d", tc.lines, got, tc.want)
		}
	}
}

// TestGrowRowsCountsWrappedRowsToo: a draft with no newline in it still takes
// the rows its width forces it into, and the answer moves with the width.
func TestGrowRowsCountsWrappedRowsToo(t *testing.T) {
	m := New(Options{})
	typeString(m, strings.Repeat("word ", 20))
	wide, narrow := m.GrowRows(100), m.GrowRows(20)
	if wide >= narrow {
		t.Fatalf("GrowRows(100) = %d and GrowRows(20) = %d; a narrower draft is a taller one", wide, narrow)
	}
	if narrow > maxDraftRows-1 {
		t.Fatalf("GrowRows(20) = %d, past the cap", narrow)
	}
}

// TestMultilineDraftFillsTheRowsItAskedFor: the region grows by exactly
// GrowRows, and every one of those rows is a row of the draft.
func TestMultilineDraftFillsTheRowsItAskedFor(t *testing.T) {
	m := New(Options{NewlineKeys: []string{"alt+enter"}, Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	m.Focus(true)
	for i := 0; i < 3; i++ {
		if i > 0 {
			m.Key(altEnterKey())
		}
		typeString(m, "line")
	}
	height := 1 + m.GrowRows(40)
	rows := strings.Split(m.Render(40, height), "\n")
	if len(rows) != 3 {
		t.Fatalf("a 3-line draft drew %d rows in a %d-row rectangle", len(rows), height)
	}
	if !strings.HasPrefix(rows[0], edged(40, tokens.GlyphPromptChat)) {
		t.Fatalf("the prompt is not on the first row: %q", rows[0])
	}
	for i, row := range rows[1:] {
		// The edge runs the whole height of the draft; the PROMPT does not.
		if !strings.HasPrefix(row, edged(40, "  ")) {
			t.Fatalf("continuation row %d does not carry the edge alone: %q", i+1, row)
		}
	}
}

// TestDraftScrollsPastTheCap: beyond the cap the draft keeps the caret's row on
// screen rather than asking for a taller region.
func TestDraftScrollsPastTheCap(t *testing.T) {
	m := New(Options{NewlineKeys: []string{"alt+enter"}})
	for i := 0; i < 20; i++ {
		typeString(m, "line")
		m.Key(altEnterKey())
	}
	typeString(m, "last")
	height := 1 + m.GrowRows(40)
	if height != maxDraftRows {
		t.Fatalf("a 21-line draft asked for a %d-row rectangle, want the cap %d", height, maxDraftRows)
	}
	rows := strings.Split(m.Render(40, height), "\n")
	if len(rows) != maxDraftRows {
		t.Fatalf("drew %d rows in a %d-row rectangle", len(rows), height)
	}
	if !strings.Contains(rows[len(rows)-1], "last") {
		t.Fatalf("the caret's row is not the last one drawn: %q", rows[len(rows)-1])
	}
}

func TestRender_StylerPassThrough(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	typeString(m, "hi")
	out := m.Render(40, 3)
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("Render() with a non-nil Styler produced no escape sequences at all: %q", out)
	}
}

func TestRender_NilStylerNeverEmitsEscapes(t *testing.T) {
	m := New(Options{})
	typeString(m, "hi")
	m.Focus(true)
	out := m.Render(40, 3)
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("Render() with a nil Styler emitted an escape sequence: %q", out)
	}
	if !strings.Contains(out, "hi") {
		t.Fatalf("Render() with a nil Styler dropped the draft text: %q", out)
	}
}

func TestRender_TailAnchoredWhenDraftOutgrowsHeight(t *testing.T) {
	m := New(Options{NewlineKeys: []string{"alt+enter"}})
	for i := 0; i < 10; i++ {
		typeString(m, "line")
		m.Key(altEnterKey())
	}
	typeString(m, "last")
	out := m.Render(40, 3)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want exactly 3 (height)", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "last") {
		t.Fatalf("last visible row = %q, want it to contain the cursor's row (\"last\")", lines[len(lines)-1])
	}
}

// TestDraftOpensPastItsGutter: the edge and the prompt hang in the gutter and
// the draft's own text begins at [promptGutter] on every row it occupies — the
// wrapped rows align under the TEXT, never under the prompt and never under the
// edge (§16's ALIGNMENT: columns within a section share x-positions).
//
// The number this pins used to be [tokens.LensIndent], the room's one left
// edge, and it is one cell further in now because the hug's state edge took
// column 0. The room's edge did not move — the bar row one plane back still
// opens there — the composer's gutter simply stopped being the same width by
// coincidence.
func TestDraftOpensPastItsGutter(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	// A run with no spaces in it, so every wrap is a HARD wrap: a soft wrap
	// leaves the space it broke at on the next row, and a leading space is a
	// fact about the wrapper rather than about the gutter this test is pinning.
	typeString(m, strings.Repeat("wordword", 8))
	rows := strings.Split(m.Render(30, 4), "\n")
	if len(rows) < 2 {
		t.Fatalf("the draft did not wrap: %q", rows)
	}
	edge := len(tokens.GlyphHugEdge)
	for i, row := range rows {
		if !strings.HasPrefix(row, tokens.GlyphHugEdge) {
			t.Fatalf("row %d carries no state edge at column 0: %q", i, row)
		}
		// The text column, measured in bytes past the edge's own bytes: every
		// cell of the gutter after the edge — §19's pad, the prompt's cell, the
		// separator — is one byte on a continuation row, so the content edge in
		// bytes is the edge's bytes plus the rest of [TextColumn].
		text := edge + TextColumn(30) - 1
		if len(row) <= text {
			continue
		}
		if got := row[text]; got == ' ' {
			t.Fatalf("row %d starts its text past the gutter: %q", i, row)
		}
		if i > 0 && strings.TrimLeft(row[edge:text], " ") != "" {
			t.Fatalf("a continuation row wrote into the prompt's cell: %q", row)
		}
	}
}
