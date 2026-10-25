package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/charmbracelet/x/ansi"
)

func clickMemoryLabel(t *testing.T, a *app, label string) {
	t.Helper()
	lines, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	for y, line := range lines {
		text := ansi.Strip(line)
		if at := strings.Index(text, label); at >= 0 {
			drive(t, a, tea.MouseClickMsg{X: ansi.StringWidth(text[:at]) + 2, Y: y, Button: tea.MouseLeft})
			placeFrameText(a)
			return
		}
	}
	t.Fatal("missing switch: " + label)
}

func TestMemoryKnowledgeSwitchIsClickableBothWays(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, key("alt+u"))
	placeFrameText(a)
	clickMemoryLabel(t, a, "Knowledge")
	if got := placeFrameText(a); !strings.Contains(got, "Storage") {
		t.Fatal("saved-memory switch did not open knowledge: " + got)
	}
	clickMemoryLabel(t, a, "Saved memory")
	if got := placeFrameText(a); strings.Contains(got, "Storage") || !strings.Contains(got, "Knowledge") {
		t.Fatal("knowledge switch did not return to saved memory: " + got)
	}
}

func TestSavedMemoryKnowledgeSwitchDoesNotEnableCapture(t *testing.T) {
	a := placeApp(t)
	v := knowledge.Open(t.TempDir(), a.workspace)
	a.agent = &knowledgeTestAgent{fakeAgent: &fakeAgent{model: "m"}, vault: v}
	drive(t, a, key(placeChord(pageMemory)))
	placeFrameText(a)
	clickMemoryLabel(t, a, "Knowledge")
	if got := placeFrameText(a); !strings.Contains(got, "Create knowledge base") {
		t.Fatal("switch did not open create view: " + got)
	}
	if v.Enabled() {
		t.Fatal("view switch enabled capture without consent")
	}
}
