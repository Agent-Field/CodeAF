package tui3

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/knowledgefixture"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func knowledgeNestedFolders(t *testing.T) *app {
	t.Helper()
	a := knowledgeArticleApp(t)
	path := filepath.Join(a.agent.(*knowledgeTestAgent).vault.Directory(), "knowledge.sqlite")
	raw, err := knowledgefixture.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	for _, n := range data["notes"].([]any) {
		n.(map[string]any)["folder"] = []string{"Project", "Nested"}
	}
	raw, err = json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := knowledgefixture.Write(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	drive(t, a, key("ctrl+r"))
	placeFrameText(a)
	return a
}

func knowledgeVisibleTree(a *app) string {
	lines, hits, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	var out []string
	for y, line := range lines {
		if h, ok := hits[y].(knowledgeHit); ok && h.tree >= 0 && h.tree < len(a.kb.targets) {
			out = append(out, strings.Split(ansi.Strip(line), "│")[0])
		}
	}
	return strings.Join(out, "\n")
}

func TestKnowledgeCollapseAllRetainsArticleAndNestedFolds(t *testing.T) {
	a := knowledgeNestedFolders(t)
	for _, r := range "Architecture" {
		drive(t, a, key(string(r)))
	}
	if got := placeFrameText(a); !strings.Contains(got, "Search: Architecture") {
		t.Fatal("filter input absent: " + got)
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl})
	got := placeFrameText(a)
	if !strings.Contains(got, "Storage") {
		t.Fatal("collapse must retain the selected article: " + got)
	}
	if strings.Contains(got, "Search: Architecture") {
		t.Fatal("collapse must clear the filter so folders can actually fold")
	}
	if tree := knowledgeVisibleTree(a); strings.Contains(tree, "Architecture") || strings.Contains(tree, "Nested") {
		t.Fatal("collapse left descendants visible: " + tree)
	}
	drive(t, a, key("enter"))
	placeFrameText(a)
	if tree := knowledgeVisibleTree(a); !strings.Contains(tree, "Nested") || strings.Contains(tree, "Architecture") {
		t.Fatal("opening parent must retain descendant folds: " + tree)
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl})
	placeFrameText(a)
	if tree := knowledgeVisibleTree(a); !strings.Contains(tree, "Architecture") || !strings.Contains(tree, "SQLite") {
		t.Fatal("expand all must reveal all descendants: " + tree)
	}
}

func TestKnowledgeFolderControlsAreClickableAtNarrowWidth(t *testing.T) {
	a := knowledgeNestedFolders(t)
	a.width = 72
	click := func(label string) {
		t.Helper()
		lines, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
		for y, line := range lines {
			text := ansi.Strip(line)
			if i := strings.Index(text, label); i >= 0 {
				drive(t, a, tea.MouseClickMsg{X: ansi.StringWidth(text[:i]) + 1, Y: y, Button: tea.MouseLeft})
				placeFrameText(a)
				return
			}
		}
		t.Fatal("missing visible control: " + label)
	}
	click("Collapse all")
	if tree := knowledgeVisibleTree(a); strings.Contains(tree, "Architecture") {
		t.Fatal("mouse collapse did not fold tree: " + tree)
	}
	click("Expand all")
	if tree := knowledgeVisibleTree(a); !strings.Contains(tree, "Architecture") {
		t.Fatal("mouse expand did not reveal tree: " + tree)
	}
}
