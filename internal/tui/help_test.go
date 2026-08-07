package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
			"talking", "moving around", "models", "asking for work", "money", "memory", "slash commands", "glyphs",
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
					if ansi.Strip(line) == category.title {
						indices = append(indices, index)
						break
					}
				}
			}
			if len(indices) != 8 {
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
