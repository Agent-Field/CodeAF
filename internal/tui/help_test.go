package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestQuestionMarkOpensHelpOnlyForEmptyDraft(t *testing.T) {
	model := New(&fakeBackend{}, "help")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if model.palette != paletteHelp {
		t.Fatalf("empty-input ? opened palette %v, want help", model.palette)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	model.input.SetValue("is this real")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if model.palette == paletteHelp || model.input.Value() != "is this real?" {
		t.Fatalf("composing ? palette=%v draft=%q", model.palette, model.input.Value())
	}
	model.input.Reset()
	model.nodeViewID = "worker"
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if model.palette != paletteHelp {
		t.Fatal("empty steer input did not open global help")
	}
}

// Help is a modal above every reading surface, including the notebook: '?'
// with an empty draft opens it over open beliefs, help owns the keys while it
// is up, and esc returns to the notebook exactly where it was.
func TestHelpSitsAboveOpenNotebookAndEscReturnsToIt(t *testing.T) {
	commander := newFakeCommander()
	commander.facts = []store.Fact{{
		Seq: 1, Scope: "user", Kind: store.FactPreference,
		Body: "Keep status updates compact.", Status: store.FactActive,
	}}
	model := NewWithCommander(&fakeBackend{}, "help-notebook", commander)
	_ = model.executeSlash("/notebook")
	if !model.notebookOpen {
		t.Fatal("slash command did not open the notebook")
	}
	returnFocus := model.focus
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if model.palette != paletteHelp {
		t.Fatal("? over the open notebook did not open the help modal")
	}
	if !model.notebookOpen {
		t.Fatal("opening help closed the notebook underneath it")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.palette == paletteHelp {
		t.Fatal("esc did not close the help modal")
	}
	if !model.notebookOpen || model.focus != returnFocus {
		t.Fatalf("esc did not return to the notebook: open=%v focus=%v want %v",
			model.notebookOpen, model.focus, returnFocus)
	}
}

func TestHelpScrollsWithKeysAndMouseWheel(t *testing.T) {
	model := New(&fakeBackend{}, "help-scroll")
	_, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 18})
	model.openHelp()
	_ = model.View()
	if model.helpMaxOffset() == 0 {
		t.Fatal("short terminal unexpectedly fit all help content")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.paletteSelected != 1 {
		t.Fatalf("down scrolled help to %d", model.paletteSelected)
	}
	_ = model.View()
	_, _ = model.Update(tea.MouseMsg{
		X: model.helpBounds.x + 1, Y: model.helpBounds.y + 2,
		Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	})
	if model.paletteSelected != 4 {
		t.Fatalf("wheel scrolled help to %d", model.paletteSelected)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if model.paletteSelected != 3 {
		t.Fatalf("up scrolled help to %d", model.paletteSelected)
	}
}

func TestCtrlJInsertsNewlineWithoutSending(t *testing.T) {
	backend := &fakeBackend{}
	model := New(backend, "newline")
	model.input.SetValue("first")
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if command != nil || model.input.Value() != "first\n" {
		t.Fatalf("ctrl+j command=%v draft=%q", command, model.input.Value())
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("second")})
	if model.input.Value() != "first\nsecond" || model.input.LineCount() != 2 || len(backend.posted) != 0 {
		t.Fatalf("multiline draft=%q lines=%d posted=%d", model.input.Value(), model.input.LineCount(), len(backend.posted))
	}
}

func TestHeaderHelpClickAndModalClosePaths(t *testing.T) {
	model := New(&fakeBackend{}, "help-click")
	_ = model.View()
	if model.headerHelpBounds.width == 0 {
		t.Fatal("header ? has no click bounds")
	}
	_, _ = model.Update(tea.MouseMsg{
		X: model.headerHelpBounds.x, Y: model.headerHelpBounds.y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if model.palette != paletteHelp {
		t.Fatalf("header click opened palette %v", model.palette)
	}
	_ = model.View()
	_, _ = model.Update(tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if model.palette != paletteNone {
		t.Fatal("click outside did not close help")
	}
	model.openHelp()
	_, quit := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if quit != nil || model.palette != paletteNone {
		t.Fatal("esc did not close help before quitting")
	}
}

func TestHelpOverlayCategoriesAt80And120Columns(t *testing.T) {
	for _, width := range []int{80, 120} {
		model := New(&fakeBackend{}, "help-width")
		_, _ = model.Update(tea.WindowSizeMsg{Width: width, Height: 120})
		model.openHelp()
		view := model.View()
		plain := ansi.Strip(view)
		for _, category := range []string{
			"talking", "moving around", "self", "models", "asking for work",
			"money", "memory", "slash commands", "glyphs",
		} {
			if !strings.Contains(plain, category) {
				t.Fatalf("width %d missing category %q:\n%s", width, category, plain)
			}
		}
		for _, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d produced %d-cell line: %q", width, got, ansi.Strip(line))
			}
		}
		if width == 80 {
			lines := model.helpContentLines(model.helpOverlayWidth() - 2)
			indices := make([]int, 0, 8)
			for _, category := range helpCategories() {
				for index, line := range lines {
					if strings.TrimRight(ansi.Strip(line), " ") == category.title {
						indices = append(indices, index)
						break
					}
				}
			}
			if len(indices) != len(helpCategories()) {
				t.Fatalf("80-column help did not stack all categories: %v", indices)
			}
			for index := 1; index < len(indices); index++ {
				if indices[index] <= indices[index-1] {
					t.Fatalf("80-column category order is not stacked: %v", indices)
				}
			}
		}
	}
}

// The panel reserves one column of padding on each side, so a content line
// wider than the panel minus two is word-wrapped by lipgloss into stray
// fragments hanging off the left margin. Every fragment must also carry the
// panel background: lipgloss closes a styled run with a full reset, so an
// unpainted run falls back to the terminal background and the modal reads as
// mottled bands rather than one calm surface.
func TestHelpPanelLinesFitTheirColumnAndStayPainted(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)

	for _, width := range []int{60, 80, 100, 120, 160} {
		model := New(&fakeBackend{}, "help-paint")
		_, _ = model.Update(tea.WindowSizeMsg{Width: width, Height: 60})
		model.openHelp()

		content := max(1, model.helpOverlayWidth()-2)
		for index, line := range model.helpContentLines(content) {
			if got := lipgloss.Width(line); got > content {
				t.Fatalf("width %d content line %d is %d cells, panel holds %d: %q",
					width, index, got, content, ansi.Strip(line))
			}
			row := helpPanelRow(line, content)
			if strings.Contains(row, "\n") || lipgloss.Width(row) != content+2 {
				t.Fatalf("width %d content line %d laid out as %d cells over %d rows",
					width, index, lipgloss.Width(row), strings.Count(row, "\n")+1)
			}
		}

		view := model.View()
		for index, line := range strings.Split(view, "\n") {
			if index < model.helpBounds.y || index >= model.helpBounds.y+model.helpBounds.height {
				continue
			}
			panel := ansi.Cut(line, model.helpBounds.x, model.helpBounds.x+model.helpBounds.width)
			if bare := unpaintedCells(panel); bare > 0 {
				t.Fatalf("width %d row %d left %d cells unpainted: %q",
					width, index, bare, ansi.Strip(panel))
			}
		}
	}
}

var sgrSequence = regexp.MustCompile("\x1b\\[[0-9;]*m")

// unpaintedCells counts the visible cells rendered while no background is set.
func unpaintedCells(line string) int {
	painted, bare, rest := false, 0, line
	for {
		found := sgrSequence.FindStringIndex(rest)
		if found == nil {
			if !painted {
				bare += len([]rune(rest))
			}
			return bare
		}
		if !painted {
			bare += len([]rune(rest[:found[0]]))
		}
		switch code := rest[found[0]:found[1]]; {
		case strings.Contains(code, "48;"):
			painted = true
		case code == "\x1b[0m":
			painted = false
		}
		rest = rest[found[1]:]
	}
}

func TestSlashDispatcherAndHelpShareCompleteCommandTable(t *testing.T) {
	var slashHelp helpCategory
	for _, category := range helpCategories() {
		if category.title == "slash commands" {
			slashHelp = category
			break
		}
	}
	helpNames := make(map[string]bool, len(slashHelp.rows))
	for _, row := range slashHelp.rows {
		helpNames[strings.TrimPrefix(row.key, "/")] = true
	}
	for _, command := range slashCommands {
		if command.handler == nil {
			t.Fatalf("/%s is listed but has no dispatcher", command.name)
		}
		if !helpNames[command.name] {
			t.Fatalf("dispatcher command /%s is missing from help", command.name)
		}
	}
	if len(helpNames) != len(slashCommands) {
		t.Fatalf("help commands=%d dispatch commands=%d", len(helpNames), len(slashCommands))
	}
}

func TestHelpSitsAbovePendingQuestionAndEscClosesItFirst(t *testing.T) {
	model := New(&fakeBackend{}, "question-help")
	model.cards = []jobCard{{
		ID: "question", State: cardQuestion, QuestionKind: questionText,
		Title: "Waiting", Question: "Which branch?", BirthSeq: 2,
	}}
	model.setSize(100, 30)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if model.palette != paletteHelp || model.cardByID("question") == nil {
		t.Fatal("help did not layer above the pending question")
	}
	_, quit := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if quit != nil || model.palette != paletteNone || model.questionDismissed["question"] {
		t.Fatal("esc changed the question instead of closing help first")
	}
	if card := model.activeTextQuestion(); card == nil || card.ID != "question" {
		t.Fatal("pending question did not return after help closed")
	}
}

func TestHelpSitsAboveNodeViewAndOwnsKeysUntilClosed(t *testing.T) {
	model := New(&fakeBackend{}, "node-help")
	model.nodeViewID = "worker"
	model.openHelp()
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.paletteSelected != 1 || model.nodeViewID != "worker" {
		t.Fatalf("help down leaked to node view: offset=%d node=%q", model.paletteSelected, model.nodeViewID)
	}
	_, quit := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if quit != nil || model.palette != paletteNone || model.nodeViewID != "worker" {
		t.Fatal("esc changed the node view instead of closing help first")
	}
}

func TestHelpGlyphGrammarIsComplete(t *testing.T) {
	wanted := []string{"▸", "▾", "⋯", "⟨×⟩", "⌄", "»", "⏱", "⚒", "⚖", "⌾", "♪", "▶"}
	seen := map[string]bool{}
	for _, category := range helpCategories() {
		if category.title != "glyphs" {
			continue
		}
		for _, row := range category.rows {
			seen[row.key] = true
		}
	}
	for _, glyph := range wanted {
		if !seen[glyph] {
			t.Fatalf("glyph %q missing from help", glyph)
		}
	}
}
