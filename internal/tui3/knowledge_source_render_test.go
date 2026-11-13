package tui3

import (
	"github.com/Agent-Field/codeaf/internal/knowledgefixture"
	"github.com/charmbracelet/x/ansi"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnowledgeSourceMarkdownRefreshesAndResizes(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, key("ctrl+o"))
	if got := placeFrameText(a); !strings.Contains(got, "Durable records use SQLite.") {
		t.Fatal("source missing: " + got)
	}
	path := filepath.Join(a.agent.(*knowledgeTestAgent).vault.Directory(), "knowledge.sqlite")
	raw, err := knowledgefixture.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.ReplaceAll(string(raw), "Durable records use SQLite.", "**Updated** durable records use SQLite."))
	if err = knowledgefixture.Write(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	drive(t, a, key("ctrl+r"))
	got := placeFrameText(a)
	if !strings.Contains(got, "Updated") || strings.Contains(got, "**Updated**") {
		t.Fatal("refresh retained stale or raw source: " + got)
	}
	a.width = 90
	rows, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	for _, row := range rows {
		if ansi.StringWidth(row) > a.width {
			t.Fatal("resized source exceeds terminal width")
		}
	}
}
