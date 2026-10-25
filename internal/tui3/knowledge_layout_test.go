package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestKnowledgeSidebarOwnsHeadingMetadataAndSearch(t *testing.T) {
	a := knowledgeArticleApp(t)
	lines, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	found := false
	for _, line := range lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "Knowledge") && strings.Contains(plain, "│") {
			parts := strings.SplitN(plain, "│", 2)
			if !strings.Contains(parts[1], "Architecture") {
				t.Fatal("article title should align with sidebar heading: " + plain)
			}
			found = true
		}
		if strings.Contains(plain, "Search:") || strings.Contains(plain, "2 pages") {
			if !strings.Contains(plain, "│") {
				t.Fatal("sidebar controls still span full reader: " + plain)
			}
			right := strings.SplitN(plain, "│", 2)[1]
			if strings.Contains(right, "Search:") || strings.Contains(right, "2 pages") {
				t.Fatal("sidebar metadata escaped into article column: " + plain)
			}
		}
	}
	if !found {
		t.Fatal("Knowledge heading is not in the left column")
	}
}

func TestKnowledgeSidebarSearchCanBeClicked(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, key("up")) // shared navbar currently owns keyboard focus
	lines, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	for y, line := range lines {
		plain := ansi.Strip(line)
		at := strings.Index(plain, "Search:")
		if at < 0 {
			continue
		}
		drive(t, a, tea.MouseClickMsg{X: at + 8, Y: y, Button: tea.MouseLeft}, key("S"), key("Q"), key("L"))
		if got := placeFrameText(a); !strings.Contains(got, "Search: SQL") {
			t.Fatal("click did not focus sidebar search: " + got)
		}
		return
	}
	t.Fatal("sidebar search missing")
}
