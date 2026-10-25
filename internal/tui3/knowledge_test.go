package tui3

import (
	tea "charm.land/bubbletea/v2"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryOffersKnowledgeWithoutStartingCapture(t *testing.T) {
	a := placeApp(t)
	drive(t, a, key(placeChord(pageMemory)))
	if got := placeFrameText(a); !strings.Contains(got, "alt+u") {
		t.Fatal("Memory must show how to open Knowledge")
	}
	drive(t, a, key("alt+u"))
	got := placeFrameText(a)
	for _, want := range []string{"Knowledge", "Create knowledge base", "future chats"} {
		if !strings.Contains(got, want) {
			t.Errorf("Knowledge screen missing %q: %s", want, got)
		}
	}
	drive(t, a, key("esc"))
	if got := placeFrameText(a); strings.Contains(got, "Create knowledge base") {
		t.Fatal("escape should return to Saved memory")
	}
}

type knowledgeTestAgent struct {
	*fakeAgent
	vault      *knowledge.Vault
	captureErr string
}

func (a *knowledgeTestAgent) Knowledge() *knowledge.Vault   { return a.vault }
func (a *knowledgeTestAgent) KnowledgeCaptureError() string { return a.captureErr }

func TestMemoryCreatesProjectKnowledgeOnlyOnEnter(t *testing.T) {
	a := placeApp(t)
	v := knowledge.Open(t.TempDir(), a.workspace)
	a.agent = &knowledgeTestAgent{fakeAgent: &fakeAgent{model: "m"}, vault: v}
	drive(t, a, key(placeChord(pageMemory)), key("alt+u"))
	if v.Enabled() {
		t.Fatal("opening the view must not enable capture")
	}
	drive(t, a, key("enter"))
	if !v.Enabled() {
		t.Fatal("Create knowledge base did not persist consent")
	}
	if got := placeFrameText(a); !strings.Contains(got, "Your knowledge base is ready") {
		t.Fatal(got)
	}
	drive(t, a, key("esc"), key("alt+u"))
	if got := placeFrameText(a); strings.Contains(got, "Create knowledge base") {
		t.Fatal("created vault was lost after navigation")
	}
}

func knowledgeArticleApp(t *testing.T) *app {
	t.Helper()
	a := placeApp(t)
	v := knowledge.Open(t.TempDir(), a.workspace)
	if err := v.Create(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(v.Directory(), "knowledge")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := `{"sources":{"s1":{"role":"user","ts":"2026-10-25T12:00:00Z","text":"Durable records use SQLite."}},"claims":[{"id":"c1","text":"Durable records use SQLite.","kind":"fact","status":"active","evidence":[{"source":"s1","quote":"Durable records use SQLite."}]}],"notes":[{"id":"architecture","title":"Architecture","kind":"subject","folder":["Project"],"claims":["c1"],"sections":[{"heading":"Storage","paragraphs":[{"text":"Uses SQLite.","claims":["c1"],"inline_links":[{"start":5,"end":11,"id":"sqlite"}]}]}],"links":[{"id":"sqlite","kind":"mention"}]},{"id":"sqlite","title":"SQLite","kind":"subject","folder":["Project"],"claims":["c1"],"sections":[{"heading":"Durability","paragraphs":[{"text":"Durable records use SQLite.","claims":["c1"]}]}],"links":[]}]}`
	if err := os.WriteFile(filepath.Join(dir, "vault.json"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	a.agent = &knowledgeTestAgent{fakeAgent: &fakeAgent{model: "m"}, vault: v}
	drive(t, a, key(placeChord(pageMemory)), key("alt+u"))
	return a
}

func TestMemoryFollowsCanonicalKnowledgeAndReturnsWithSources(t *testing.T) {
	a := knowledgeArticleApp(t)
	if got := placeFrameText(a); !strings.Contains(got, "[[SQLite]]") {
		t.Fatal("inline canonical reference absent: " + got)
	}
	drive(t, a, key("ctrl+l"), key("enter"))
	if got := placeFrameText(a); !strings.Contains(got, "Durability") {
		t.Fatal("link did not open target: " + got)
	}
	if got := placeFrameText(a); !strings.Contains(got, "Sources") {
		t.Fatal("source access missing: " + got)
	}
	drive(t, a, key("left"))
	if got := placeFrameText(a); !strings.Contains(got, "[[SQLite]]") {
		t.Fatal("back did not return to article")
	}
}

func TestMemoryInlineKnowledgeLinkRespondsToMouse(t *testing.T) {
	a := knowledgeArticleApp(t)
	lines, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	for y, line := range lines {
		text := ansi.Strip(line)
		if at := strings.Index(text, "[[SQLite]]"); at >= 0 {
			x := ansi.StringWidth(text[:at]) + 3
			drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if got := placeFrameText(a); !strings.Contains(got, "Durability") {
				t.Fatal("inline click did not open SQLite: " + got)
			}
			return
		}
	}
	t.Fatal("no inline link to click")
}

func TestMemoryBlankSpaceDoesNotCreateKnowledge(t *testing.T) {
	a := placeApp(t)
	v := knowledge.Open(t.TempDir(), a.workspace)
	a.agent = &knowledgeTestAgent{fakeAgent: &fakeAgent{model: "m"}, vault: v}
	drive(t, a, key(placeChord(pageMemory)), key("alt+u"))
	placeFrameText(a)
	drive(t, a, tea.MouseClickMsg{X: 90, Y: 8, Button: tea.MouseLeft})
	if v.Enabled() {
		t.Fatal("blank space enabled automatic knowledge capture")
	}
}

func TestKnowledgeKeepsTheSharedNavigationBarUsable(t *testing.T) {
	a := knowledgeArticleApp(t)
	placeFrameText(a)
	drive(t, a, key("up"), key("right"), key("enter"))
	if a.page != pageAutomations {
		t.Fatalf("top navigation left user on %s", a.page.word())
	}
}

func TestKnowledgeWheelMovesTheVisibleTree(t *testing.T) {
	a := knowledgeArticleApp(t)
	before := placeFrameText(a)
	drive(t, a, tea.MouseWheelMsg{X: 5, Y: 9, Button: tea.MouseWheelDown}, pointerMsg{})
	if after := placeFrameText(a); after == before {
		t.Fatal("wheel did not move knowledge selection")
	}
}

func TestKnowledgeReopeningUsesTheCurrentConversationProject(t *testing.T) {
	a := knowledgeArticleApp(t)
	placeFrameText(a)
	drive(t, a, key(placeChord(pageHome)))
	a.agent = &knowledgeTestAgent{fakeAgent: &fakeAgent{model: "m"}, vault: knowledge.Open(t.TempDir(), "/another-project")}
	drive(t, a, key(placeChord(pageMemory)))
	got := placeFrameText(a)
	if strings.Contains(got, "[[SQLite]]") || !strings.Contains(got, "Create knowledge base") {
		t.Fatal("previous project's article remained visible: " + got)
	}
}

func TestKnowledgeArticleAndLinksFitANarrowTerminal(t *testing.T) {
	a := knowledgeArticleApp(t)
	a.width = 60
	a.height = 24
	placeFrameText(a)
	drive(t, a, key("down"), key("enter"))
	got := placeFrameText(a)
	if !strings.Contains(got, "Storage") || !strings.Contains(got, "[[SQLite]]") {
		t.Fatal("narrow reader did not open first article: " + got)
	}
	drive(t, a, key("ctrl+l"))
	if got := placeFrameText(a); !strings.Contains(got, "1. SQLite") {
		t.Fatal("connection picker not visible: " + got)
	}
	lines, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	for _, line := range lines {
		if ansi.StringWidth(line) > a.width {
			t.Fatalf("row exceeds terminal width: %q", line)
		}
	}
}

func TestKnowledgeShowsCaptureFailures(t *testing.T) {
	a := knowledgeArticleApp(t)
	a.agent.(*knowledgeTestAgent).captureErr = "journal copy failed"
	drive(t, a, key("alt+u"), key("alt+u"))
	if got := placeFrameText(a); !strings.Contains(got, "journal copy failed") {
		t.Fatal("capture failure hidden: " + got)
	}
}

func TestKnowledgeKeepsSelectedConnectionVisible(t *testing.T) {
	a := knowledgeArticleApp(t)
	v := a.agent.(*knowledgeTestAgent).vault
	path := filepath.Join(v.Directory(), "knowledge", "vault.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	notes := data["notes"].([]any)
	first := notes[0].(map[string]any)
	links := first["links"].([]any)
	for i := 0; i < 30; i++ {
		id := fmt.Sprintf("topic-%02d", i)
		links = append(links, map[string]any{"id": id, "kind": "mention"})
		notes = append(notes, map[string]any{"id": id, "title": fmt.Sprintf("Topic %02d", i), "kind": "subject", "links": []any{}})
	}
	first["links"] = links
	data["notes"] = notes
	raw, err = json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	drive(t, a, key("alt+u"), key("alt+u"), key("ctrl+l"))
	for i := 0; i < 30; i++ {
		drive(t, a, key("down"))
	}
	if got := placeFrameText(a); !strings.Contains(got, "› 31. Topic 29") {
		t.Fatal("selected connection scrolled offscreen: " + got)
	}
}
