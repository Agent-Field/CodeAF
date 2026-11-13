package tui3

import (
	"github.com/Agent-Field/codeaf/internal/knowledgefixture"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestKnowledgeSourcesStayBehindDisclosure(t *testing.T) {
	a := knowledgeArticleApp(t)
	got := placeFrameText(a)
	if strings.Contains(got, "Durable records use SQLite.") || strings.Contains(got, "2026-10-25T12:00:00Z") {
		t.Fatal("article exposes source quotations or raw timestamps before disclosure: " + got)
	}
	if !strings.Contains(got, "Sources · 1") {
		t.Fatal("missing compact source disclosure: " + got)
	}
	drive(t, a, key("ctrl+o"))
	got = placeFrameText(a)
	if !strings.Contains(got, "Durable records use SQLite.") || !strings.Contains(got, "Oct 25, 2026") || !strings.Contains(got, "You") {
		t.Fatal("source disclosure must show readable provenance and its quotation: " + got)
	}
	if strings.Contains(got, "2026-10-25T12:00:00Z") {
		t.Fatal("raw timestamp clutters source reader")
	}
	drive(t, a, key("esc"))
	got = placeFrameText(a)
	if !strings.Contains(got, "[[SQLite]]") || strings.Contains(got, "Durable records use SQLite.") {
		t.Fatal("Escape must restore article: " + got)
	}
}

func TestKnowledgeSourcesDisclosureRespondsToMouse(t *testing.T) {
	a := knowledgeArticleApp(t)
	lines, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	for y, line := range lines {
		text := ansi.Strip(line)
		if at := strings.Index(text, "Sources · 1"); at >= 0 {
			drive(t, a, tea.MouseClickMsg{X: ansi.StringWidth(text[:at]) + 2, Y: y, Button: tea.MouseLeft})
			if got := placeFrameText(a); !strings.Contains(got, "Durable records use SQLite.") {
				t.Fatal("source disclosure not clickable: " + got)
			}
			return
		}
	}
	t.Fatal("missing source disclosure")
}

func TestKnowledgeSourceDisclosureRestoresReadingPosition(t *testing.T) {
	a := knowledgeArticleApp(t)
	// Extend the persisted guide so the reader genuinely scrolls before opening
	// sources. All subsequent observations go through Memory actions and frames.
	v := a.agent.(*knowledgeTestAgent).vault
	path := filepath.Join(v.Directory(), "knowledge.sqlite")
	content, err := knowledgefixture.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.Replace(string(content), "Uses SQLite.", "Uses SQLite. "+strings.Repeat("Storage remains durable across restarts. ", 120), 1))
	if err := knowledgefixture.Write(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	drive(t, a, key("ctrl+r"), key("pgdown"))
	before := placeFrameText(a)
	drive(t, a, key("ctrl+o"))
	if got := placeFrameText(a); !strings.Contains(got, "Durable records use SQLite.") {
		t.Fatal("sources did not open from a scrolled article: " + got)
	}
	drive(t, a, key("esc"))
	if got := placeFrameText(a); got != before {
		t.Fatal("Escape lost the article reading position")
	}
}
