package tui3

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func knowledgeLongTreeApp(t *testing.T) *app {
	t.Helper()
	a := knowledgeArticleApp(t)
	path := filepath.Join(a.agent.(*knowledgeTestAgent).vault.Directory(), "knowledge", "vault.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	notes := data["notes"].([]any)
	for i := 0; i < 80; i++ {
		notes = append(notes, map[string]any{"id": fmt.Sprintf("page-%03d", i), "title": fmt.Sprintf("Topic %03d", i), "kind": "subject", "folder": []any{"Project"}})
	}
	data["notes"] = notes
	raw, err = json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	drive(t, a, key("ctrl+r"), key("down"), key("enter"))
	return a
}

func TestKnowledgeSidebarWheelScrollsWhileArticleIsOpen(t *testing.T) {
	a := knowledgeLongTreeApp(t)
	before := placeFrameText(a)
	if !strings.Contains(before, "Architecture") || !strings.Contains(before, "Storage") {
		t.Fatal("missing initial reader")
	}
	drive(t, a, tea.MouseWheelMsg{X: 8, Y: 12, Button: tea.MouseWheelDown})
	got := placeFrameText(a)
	if strings.Contains(got, "▾ Project") {
		t.Fatal("sidebar viewport did not scroll immediately: " + got)
	}
	if !strings.Contains(got, "Storage") || !strings.Contains(got, "Uses [[SQLite]].") {
		t.Fatal("sidebar wheel moved the article: " + got)
	}
	drive(t, a, tea.MouseWheelMsg{X: 8, Y: 12, Button: tea.MouseWheelUp})
	if got := placeFrameText(a); !strings.Contains(got, "▾ Project") {
		t.Fatal("sidebar wheel did not return to top: " + got)
	}
}

func TestKnowledgeArticleWheelKeepsSidebarPosition(t *testing.T) {
	a := knowledgeLongTreeApp(t)
	path := filepath.Join(a.agent.(*knowledgeTestAgent).vault.Directory(), "knowledge", "vault.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), "Uses SQLite.", "Uses SQLite. "+strings.Repeat("Durable storage survives restarts. ", 80), 1))
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	drive(t, a, key("ctrl+r"))
	a.height = 18
	before := placeFrameText(a)
	if !strings.Contains(before, "Storage") {
		t.Fatal("article heading not visible before scroll: " + before)
	}
	sidebarBefore := knowledgeVisibleTree(a)
	wheelArticle := func() {
		t.Helper()
		lines, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
		for y, line := range lines {
			plain := ansi.Strip(line)
			if at := strings.Index(plain, "Durable storage"); at >= 0 {
				drive(t, a, tea.MouseWheelMsg{X: ansi.StringWidth(plain[:at]) + 2, Y: y, Button: tea.MouseWheelDown})
				return
			}
		}
		t.Fatal("no visible article paragraph to scroll")
	}
	wheelArticle()
	after := placeFrameText(a)
	if before == after || strings.Contains(after, "│ Architecture") {
		t.Fatal("wheel over article did not move its title out of view: " + after)
	}
	// Edit/Delete add a header row. One tick now moves the article title off
	// screen; another moves the following section heading off screen as well.
	wheelArticle()
	after = placeFrameText(a)
	if strings.Contains(after, "Storage") {
		t.Fatal("continued wheel did not move the section heading: " + after)
	}
	if sidebarAfter := knowledgeVisibleTree(a); sidebarBefore != sidebarAfter {
		t.Fatal("article wheel moved sidebar: " + after)
	}
}

func TestKnowledgeNarrowSidebarWheelReachesLaterPages(t *testing.T) {
	a := knowledgeLongTreeApp(t)
	a.width = 60
	a.height = 20
	drive(t, a, key("esc"))
	placeFrameText(a)
	for i := 0; i < 20; i++ {
		drive(t, a, tea.MouseWheelMsg{X: 8, Y: 12, Button: tea.MouseWheelDown}, pointerMsg{})
		placeFrameText(a)
	}
	got := placeFrameText(a)
	if !strings.Contains(got, "Topic 060") || strings.Contains(got, "▾ Project") {
		t.Fatal("narrow tree did not scroll to later pages: " + got)
	}
}
