package composer

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The `/` line. Every test here is about the two things 5.22 rule 3 promises —
// that the list is the catalog and that typing is an accelerator rather than
// the only door — plus the one thing the composer owes the draft: that a
// grammar which opens over your words never eats them.

func slashCatalog() []Command {
	return []Command{
		{ID: "slash.settings", Word: "settings", Title: "open every setting in one place", Key: "alt+,"},
		{ID: "slash.model", Word: "model", Title: "choose any model slot"},
		{ID: "slash.notebook", Word: "notebook", Title: "browse or search the scoped notebook"},
		{ID: "slash.open", Word: "open", Title: "open the focused deliverable in your OS",
			Reason: "no OS-open on this surface yet"},
	}
}

func slashModel(t *testing.T, ran *[]string) *Model {
	t.Helper()
	return New(Options{
		SendKey:  "enter",
		Styler:   tokens.NewStyler(tokens.NoColor, tokens.FocusNormal),
		Commands: slashCatalog,
		OnCommand: func(id string) tea.Cmd {
			*ran = append(*ran, id)
			return nil
		},
	})
}

// A '/' typed first on an empty draft opens the line.
func TestSlashOpensOnAnEmptyDraft(t *testing.T) {
	var ran []string
	m := slashModel(t, &ran)
	m.Focus(true)
	typeString(m, "/")
	if !m.slash.open {
		t.Fatal("the line did not open")
	}
	if got := m.Value(); got != "/" {
		t.Fatalf("the draft reads %q — the '/' is ordinary text and must stay", got)
	}
	if len(m.slash.hits) != len(slashCatalog()) {
		t.Fatalf("an empty needle matched %d of %d rows", len(m.slash.hits), len(slashCatalog()))
	}
}

// And a '/' anywhere else is a slash: a path, a fraction, a date. The popup
// appearing over "and/or" would be the surface interrupting prose to offer
// commands nobody asked for.
func TestSlashInsideASentenceIsJustText(t *testing.T) {
	var ran []string
	for _, draft := range []string{"read src/main.go", "and/or", " /settings", "1/2"} {
		m := slashModel(t, &ran)
		m.Focus(true)
		typeString(m, draft)
		if m.slash.open {
			t.Fatalf("%q opened the command line", draft)
		}
		if got := m.Value(); got != draft {
			t.Fatalf("%q came out as %q", draft, got)
		}
	}
}

// A pasted slash is a path somebody copied, not a reach for the grammar.
func TestAPastedSlashDoesNotOpenTheLine(t *testing.T) {
	var ran []string
	m := slashModel(t, &ran)
	m.Focus(true)
	m.Paste(tea.PasteMsg{Content: "/etc/hosts"})
	if m.slash.open {
		t.Fatal("a paste opened the command line")
	}
}

// The needle narrows the same list, and the rows stay the catalog's.
func TestTypingNarrowsTheSameCatalog(t *testing.T) {
	var ran []string
	m := slashModel(t, &ran)
	m.Focus(true)
	typeString(m, "/set")
	// The needle ranks rather than filters to an exact alias — it scores the
	// title too, which is what makes "/set" find the settings row from either
	// half of it — so the assertion is about the WINNER, which is the row a
	// completion would take, and about the list having narrowed at all.
	if n := len(m.slash.hits); n == 0 || n >= len(slashCatalog()) {
		t.Fatalf("/set left %d of %d rows", n, len(slashCatalog()))
	}
	command, ok := m.slash.selected()
	if !ok || command.ID != "slash.settings" {
		t.Fatalf("/set selected %+v", command)
	}
	// And a needle nothing answers says so in a sentence rather than by
	// rendering an empty rectangle.
	typeString(m, "zzz")
	rows := m.slashRows(m.activeStyler(), 60, 6)
	if len(rows) != 1 || !strings.Contains(rows[0], noSlashMatch) {
		t.Fatalf("a dead needle drew %q", rows)
	}
}

// Whitespace ends the session: an alias is one word, so a space means the
// reader went back to writing prose.
func TestASpaceClosesTheLineAndKeepsTheDraft(t *testing.T) {
	var ran []string
	m := slashModel(t, &ran)
	m.Focus(true)
	typeString(m, "/set ")
	if m.slash.open {
		t.Fatal("a space left the line open")
	}
	if got := m.Value(); got != "/set " {
		t.Fatalf("the draft reads %q", got)
	}
}

// tab and enter both complete, and completing RUNS the row and clears the
// draft — a command is the whole of what the reader meant, not a fragment of a
// sentence they are still writing.
func TestCompletingRunsTheRowAndClearsTheDraft(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyTab}, enterKey()} {
		var ran []string
		m := slashModel(t, &ran)
		m.Focus(true)
		typeString(m, "/set")
		m.Key(key)
		if len(ran) != 1 || ran[0] != "slash.settings" {
			t.Fatalf("%v ran %v", key, ran)
		}
		if got := m.Value(); got != "" {
			t.Fatalf("%v left %q in the draft", key, got)
		}
		if m.slash.open {
			t.Fatalf("%v left the line open", key)
		}
	}
}

// The id goes back untouched. This is the whole of the "one catalog" property
// at this seam: the composer decides nothing about what a command means.
func TestTheComposerHandsBackTheIDItWasGiven(t *testing.T) {
	var ran []string
	m := slashModel(t, &ran)
	m.Focus(true)
	typeString(m, "/notebook")
	m.Key(enterKey())
	if len(ran) != 1 || ran[0] != "slash.notebook" {
		t.Fatalf("ran %v", ran)
	}
}

// A refused row is visible WITH its reason and runs nothing (5.20 rule 3).
// Hiding it would be the catalog pretending the verb does not exist; running it
// would be the affordance lying.
func TestARefusedRowShowsItsReasonAndRunsNothing(t *testing.T) {
	var ran []string
	m := slashModel(t, &ran)
	m.Focus(true)
	typeString(m, "/open")
	refused := slashCatalog()[3]
	rows := m.slashRows(m.activeStyler(), 100, 6)
	found := false
	for _, row := range rows {
		if strings.Contains(row, refused.Reason) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the refused row did not carry its reason: %q", rows)
	}

	// Walk to it and try to run it. Fuzzy ranking scores titles too, so the
	// alias is not necessarily the top hit — what matters is that the row a
	// reader can reach refuses.
	for range len(m.slash.hits) {
		if command, ok := m.slash.selected(); ok && command.ID == refused.ID {
			break
		}
		m.Key(downKey())
	}
	command, ok := m.slash.selected()
	if !ok || command.ID != refused.ID {
		t.Fatalf("could not reach the refused row; landed on %+v", command)
	}
	m.Key(enterKey())
	if len(ran) != 0 {
		t.Fatalf("a refused row ran %v", ran)
	}
	if !m.slash.open {
		t.Fatal("a refused row closed the line, taking its own reason off screen")
	}
}

// esc closes and the draft is intact, character for character. Closing the line
// is forgetting an index — see the file comment.
func TestEscClosesTheLineWithTheDraftIntact(t *testing.T) {
	var ran []string
	m := slashModel(t, &ran)
	m.Focus(true)
	typeString(m, "/mod")
	m.Key(escKey())
	if m.slash.open {
		t.Fatal("esc left the line open")
	}
	if got := m.Value(); got != "/mod" {
		t.Fatalf("esc changed the draft to %q", got)
	}
	if len(ran) != 0 {
		t.Fatalf("esc ran %v", ran)
	}
	// And the second esc is the composer's own ladder key again, unshadowed:
	// the line is gone, so esc means what it always meant.
	m.Key(escKey())
	if m.Value() != "" {
		t.Fatalf("the second esc left %q — it should have stashed the draft", m.Value())
	}
}

// ↑/↓ walk the list and clamp rather than wrap: this list is at most a
// screenful and a jump from the last row to the first costs the reader
// their place.
func TestTheArrowsWalkAndClamp(t *testing.T) {
	var ran []string
	m := slashModel(t, &ran)
	m.Focus(true)
	typeString(m, "/")
	m.Key(upKey())
	if m.slash.sel != 0 {
		t.Fatalf("up from the top landed on %d", m.slash.sel)
	}
	for range len(slashCatalog()) + 3 {
		m.Key(downKey())
	}
	if m.slash.sel != len(m.slash.hits)-1 {
		t.Fatalf("down past the end landed on %d of %d", m.slash.sel, len(m.slash.hits))
	}
}

// A no-match line must not swallow the send of a draft the reader has finished
// typing — the same restraint the `@` filter shows.
func TestANoMatchLineDoesNotSwallowTheSend(t *testing.T) {
	var ran []string
	sent := ""
	m := New(Options{
		SendKey:   "enter",
		OnSubmit:  func(text string) { sent = text },
		Commands:  slashCatalog,
		OnCommand: func(id string) tea.Cmd { ran = append(ran, id); return nil },
	})
	m.Focus(true)
	typeString(m, "/zzzz")
	m.Key(enterKey())
	if sent != "/zzzz" {
		t.Fatalf("the send was swallowed; OnSubmit got %q", sent)
	}
	if len(ran) != 0 {
		t.Fatalf("a no-match line ran %v", ran)
	}
}

// The whole grammar is opt-out. A composer with no Commands renders and behaves
// exactly as one built before this file existed.
func TestWithoutCommandsNothingChanges(t *testing.T) {
	plain := New(Options{SendKey: "enter", Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	plain.Focus(true)
	typeString(plain, "/settings")
	if plain.slash.open {
		t.Fatal("a composer with no catalog opened a line")
	}
	if plain.HintRows() != 0 {
		t.Fatalf("it asked for %d rows", plain.HintRows())
	}
	// One draft line and no chrome under it: exactly what a composer with
	// neither grammar has always drawn.
	if got := plain.Render(60, 3); strings.Contains(got, "\n") {
		t.Fatalf("render grew rows it never had: %q", got)
	}
}

// HintRows is what the region grows by, so it has to answer for the list that
// is actually drawn — capped at the six the list caps itself at.
func TestHintRowsMatchesTheListItAsksFor(t *testing.T) {
	var ran []string
	m := slashModel(t, &ran)
	m.Focus(true)
	if m.HintRows() != 0 {
		t.Fatalf("a closed line asked for %d rows", m.HintRows())
	}
	typeString(m, "/")
	if got, want := m.HintRows(), len(slashCatalog()); got != want {
		t.Fatalf("HintRows = %d, want %d", got, want)
	}
	typeString(m, "settings")
	if got, want := m.HintRows(), len(m.slash.hits); got != want || got == 0 {
		t.Fatalf("HintRows = %d for %d hits", got, want)
	}
	typeString(m, "zzz")
	if got := m.HintRows(); got != 1 {
		t.Fatalf("the no-match sentence asked for %d rows", got)
	}
	m.Key(escKey())
	if got := m.HintRows(); got != 0 {
		t.Fatalf("a closed line asked for %d rows", got)
	}
}

// -- geometry: the line opens UPWARD (8) --------------------------------------

// TestSlashOpensUpwardWithTheSelectionAgainstTheDraft: every candidate sits
// above the prompt, and the highlighted one is the row the prompt is under.
func TestSlashOpensUpwardWithTheSelectionAgainstTheDraft(t *testing.T) {
	var ran []string
	m := slashModel(t, &ran)
	m.Focus(true)
	typeString(m, "/")
	height := 1 + m.HintRows()
	rows := strings.Split(ansi.Strip(m.Render(70, height)), "\n")
	if len(rows) != height {
		t.Fatalf("drew %d rows in a %d-row rectangle", len(rows), height)
	}
	if !strings.HasPrefix(rows[len(rows)-1], edged(70, tokens.GlyphPromptChat)) {
		t.Fatalf("the draft is not the bottom row: %q", rows[len(rows)-1])
	}
	best, _ := m.slash.selected()
	if nearest := rows[len(rows)-2]; !strings.Contains(nearest, best.Word) {
		t.Fatalf("the row nearest the draft is %q, want the selection %q", nearest, best.Word)
	}
	if !strings.HasPrefix(rows[len(rows)-2], padded(70, tokens.GlyphAccentRail)) {
		t.Fatalf("the row nearest the draft carries no selection rail: %q", rows[len(rows)-2])
	}
	// And the least relevant row is furthest from the eye.
	worst := m.slash.rows[m.slash.hits[len(m.slash.hits)-1].idx].command
	if !strings.Contains(rows[0], worst.Word) {
		t.Fatalf("top row = %q, want the last-ranked row %q", rows[0], worst.Word)
	}
}

// TestClickingARowRunsTheRowThatWasDrawnThere is the pointer's half of the flip:
// the paint and the click read one plan, so a click lands on the command the
// reader is looking at rather than on its mirror image.
func TestClickingARowRunsTheRowThatWasDrawnThere(t *testing.T) {
	var ran []string
	m := slashModel(t, &ran)
	m.Focus(true)
	typeString(m, "/")
	height := 1 + m.HintRows()
	rows := strings.Split(ansi.Strip(m.Render(70, height)), "\n")

	for y := 0; y < m.HintRows(); y++ {
		var mine []string
		probe := slashModel(t, &mine)
		probe.Focus(true)
		typeString(probe, "/")
		cmd, taken := probe.ClickHint(70, height, y)
		if !taken {
			t.Fatalf("a click on row %d was not taken", y)
		}
		cmdMsg(cmd)
		// A runnable row ran; a refused one stayed put with the click's row
		// selected, which is the same answer asked a different way.
		word := ""
		if len(mine) == 1 {
			for _, c := range slashCatalog() {
				if c.ID == mine[0] {
					word = c.Word
				}
			}
		} else if command, ok := probe.slash.selected(); ok {
			word = command.Word
		}
		if word == "" || !strings.Contains(rows[y], word) {
			t.Fatalf("clicking row %d landed on %q, but that row reads %q", y, word, rows[y])
		}
	}
	// A click on the draft's own row is not a click on the list.
	if _, taken := m.ClickHint(70, height, height-1); taken {
		t.Fatalf("a click on the draft row was taken by the list")
	}
}

// TestHoveringARowSelectsIt: pointing is not choosing (5.14), and the row the
// pointer rests on is the row the paint drew there.
func TestHoveringARowSelectsIt(t *testing.T) {
	var ran []string
	m := slashModel(t, &ran)
	m.Focus(true)
	typeString(m, "/")
	height := 1 + m.HintRows()
	if !m.HoverHint(70, height, 0) {
		t.Fatalf("hovering the top row moved nothing")
	}
	if m.slash.sel != len(m.slash.hits)-1 {
		t.Fatalf("hovering the top row selected %d, want the last-ranked row", m.slash.sel)
	}
	if m.HoverHint(70, height, 0) {
		t.Fatalf("hovering the same row twice reported a move")
	}
	if !m.HoverHint(70, height, height-2) {
		t.Fatalf("hovering the row against the draft moved nothing")
	}
	if m.slash.sel != 0 {
		t.Fatalf("hovering the row against the draft selected %d, want the best match", m.slash.sel)
	}
	if len(ran) != 0 {
		t.Fatalf("hovering ran %v", ran)
	}
}

// Every row fits, at every width, painted or not. The rail's own law, applied
// to a list that lives inside somebody else's rectangle.
func TestSlashRowsNeverOverflow(t *testing.T) {
	var ran []string
	m := slashModel(t, &ran)
	m.Focus(true)
	typeString(m, "/")
	for width := 1; width <= 120; width++ {
		for _, sty := range []*tokens.Styler{
			tokens.NewStyler(tokens.NoColor, tokens.FocusNormal),
			tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal),
		} {
			for _, row := range m.slashRows(sty, width, 6) {
				if got := ansi.StringWidth(row); got > width {
					t.Fatalf("width %d: row %q measured %d", width, row, got)
				}
			}
		}
	}
}
