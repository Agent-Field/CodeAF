package tui3

import (
	tea "charm.land/bubbletea/v2"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/knowledgefixture"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
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
	for _, want := range []string{"Knowledge", "Build knowledge base", "saved chats"} {
		if !strings.Contains(got, want) {
			t.Errorf("Knowledge screen missing %q: %s", want, got)
		}
	}
	drive(t, a, key("esc"))
	if got := placeFrameText(a); strings.Contains(got, "Build knowledge base") {
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
	v := knowledge.Open(t.TempDir())
	a.agent = &knowledgeTestAgent{fakeAgent: &fakeAgent{model: "m"}, vault: v}
	drive(t, a, key(placeChord(pageMemory)), key("alt+u"))
	if v.Enabled() {
		t.Fatal("opening the view must not enable capture")
	}
	drive(t, a, key("enter"))
	if !v.Enabled() {
		t.Fatal("Build knowledge base did not persist consent")
	}
	if got := placeFrameText(a); !strings.Contains(got, "Building knowledge base") {
		t.Fatal(got)
	}
	drive(t, a, key("esc"), key("alt+u"))
	if got := placeFrameText(a); strings.Contains(got, "Build knowledge base") {
		t.Fatal("created vault was lost after navigation")
	}
}

func knowledgeArticleApp(t *testing.T) *app {
	t.Helper()
	a := placeApp(t)
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(v.Directory(), "knowledge")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := `{"sources":{"s1":{"role":"user","ts":"2026-10-25T12:00:00Z","text":"Durable records use SQLite."}},"claims":[{"id":"c1","text":"Durable records use SQLite.","kind":"fact","status":"active","evidence":[{"source":"s1","quote":"Durable records use SQLite."}]}],"notes":[{"id":"architecture","title":"Architecture","kind":"subject","folder":["Project"],"claims":["c1"],"sections":[{"heading":"Storage","paragraphs":[{"text":"Uses SQLite.","claims":["c1"],"inline_links":[{"start":5,"end":11,"id":"sqlite"}]}]}],"links":[{"id":"sqlite","kind":"mention"}]},{"id":"sqlite","title":"SQLite","kind":"subject","folder":["Project"],"claims":["c1"],"sections":[{"heading":"Durability","paragraphs":[{"text":"Durable records use SQLite.","claims":["c1"]}]}],"links":[]}]}`
	if err := knowledgefixture.Write(filepath.Join(filepath.Dir(dir), "knowledge.sqlite"), []byte(fixture), 0600); err != nil {
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
	v := knowledge.Open(t.TempDir())
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
	a := knowledgeLongTreeApp(t)
	beforeRows, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	before := strings.Join(beforeRows, "\n")
	y := 0
	for i, line := range beforeRows {
		if strings.Contains(ansi.Strip(line), "Topic 000") {
			y = i
			break
		}
	}
	if y == 0 {
		t.Fatal("visible tree row missing")
	}
	drive(t, a, tea.MouseWheelMsg{X: 5, Y: y, Button: tea.MouseWheelDown}, pointerMsg{})
	afterRows, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	if after := strings.Join(afterRows, "\n"); after == before {
		t.Fatal("wheel did not move knowledge selection")
	}
}

func TestKnowledgeReopeningUsesTheCurrentConversationProject(t *testing.T) {
	a := knowledgeArticleApp(t)
	placeFrameText(a)
	drive(t, a, key(placeChord(pageHome)))
	a.agent = &knowledgeTestAgent{fakeAgent: &fakeAgent{model: "m"}, vault: knowledge.Open(t.TempDir())}
	drive(t, a, key(placeChord(pageMemory)))
	got := placeFrameText(a)
	if strings.Contains(got, "[[SQLite]]") || !strings.Contains(got, "Build knowledge base") {
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
	path := filepath.Join(v.Directory(), "knowledge.sqlite")
	raw, err := knowledgefixture.Read(path)
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
	if err = knowledgefixture.Write(path, raw, 0600); err != nil {
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

func TestKnowledgeInlineLinksHaveVisibleLinkAffordance(t *testing.T) {
	a := knowledgeArticleApp(t)
	a.pal = newPalette(tokens.TrueColor, false)
	lines, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	for _, line := range lines {
		if strings.Contains(ansi.Strip(line), "Uses [[SQLite]].") {
			if !strings.Contains(line, "\x1b[4m") {
				t.Fatal("inline navigation must be underlined like existing UI links")
			}
			return
		}
	}
	t.Fatal("article link missing")
}

func TestKnowledgeTreeHasNoDecorativePageGlyphs(t *testing.T) {
	a := knowledgeArticleApp(t)
	if got := placeFrameText(a); strings.Contains(got, "◇") {
		t.Fatal("page tree repeats decorative icons: " + got)
	}
}

func TestKnowledgeLinkHoverUsesExistingLinkHighlight(t *testing.T) {
	a := knowledgeArticleApp(t)
	a.pal = newPalette(tokens.TrueColor, false)
	lines, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	for y, line := range lines {
		text := ansi.Strip(line)
		if at := strings.Index(text, "[[SQLite]]"); at >= 0 {
			drive(t, a, tea.MouseMotionMsg{X: ansi.StringWidth(text[:at]) + 3, Y: y}, pointerMsg{})
			after, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
			if after[y] == line {
				t.Fatal("hover did not highlight the link")
			}
			return
		}
	}
	t.Fatal("no link to hover")
}

func TestKnowledgeFollowingLinkKeepsTreeSelectionOnDestination(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, key("ctrl+l"), key("enter"), key("enter"))
	if got := placeFrameText(a); !strings.Contains(got, "Durability") {
		t.Fatal("Enter reopened the previous tree item: " + got)
	}
}

func TestKnowledgeBackKeepsTreeAndArticleTogether(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, key("ctrl+l"), key("enter"), key("left"), key("enter"))
	if got := placeFrameText(a); !strings.Contains(got, "Storage") {
		t.Fatal("Enter after Back opened a different tree page: " + got)
	}
}

func TestKnowledgeHostedConversationNeverShowsLocalVault(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, key(placeChord(pageHome)))
	a.agent = &fakeAgent{model: "m"}
	a.host = "remote-box"
	drive(t, a, key(placeChord(pageMemory)))
	got := placeFrameText(a)
	if strings.Contains(got, "Architecture") || strings.Contains(got, "SQLite") || strings.Contains(got, "Build knowledge base") || !strings.Contains(got, "local project conversation") {
		t.Fatal("hosted conversation exposed a local knowledge vault: " + got)
	}
}

func TestKnowledgeHoverRepaintsVisibleControls(t *testing.T) {
	for _, name := range []string{"tree", "related", "return", "build"} {
		t.Run(name, func(t *testing.T) {
			a := knowledgeArticleApp(t)
			if name == "build" {
				a = placeApp(t)
				a.agent = &knowledgeTestAgent{fakeAgent: &fakeAgent{model: "m"}, vault: knowledge.Open(t.TempDir())}
				drive(t, a, key(placeChord(pageMemory)), key("alt+u"))
			}
			a.pal = newPalette(tokens.TrueColor, false)
			label := "Architecture"
			if name == "build" {
				label = "Build knowledge base"
			}
			if name == "related" {
				label = "1. SQLite"
			}
			if name == "return" {
				drive(t, a, key("ctrl+o"))
				label = "Return to article"
			}
			before := a.View().Content
			lines := strings.Split(before, "\n")
			for y, line := range lines {
				plain := ansi.Strip(line)
				at := strings.Index(plain, label)
				if at < 0 || (name == "tree" && at > 40) {
					continue
				}
				drive(t, a, tea.MouseMotionMsg{X: ansi.StringWidth(plain[:at]) + 2, Y: y})
				if after := a.View().Content; after == before {
					t.Fatal("hover failed to repaint visible " + name + " control")
				}
				return
			}
			t.Fatal("missing hover control: " + label)
		})
	}
}

func TestKnowledgeLinkedPagePickerHoverSelectsEnterTarget(t *testing.T) {
	a := knowledgeArticleApp(t)
	path := filepath.Join(a.agent.(*knowledgeTestAgent).vault.Directory(), "knowledge.sqlite")
	raw, err := knowledgefixture.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	notes := data["notes"].([]any)
	first := notes[0].(map[string]any)
	first["links"] = append(first["links"].([]any), map[string]any{"id": "cache", "kind": "mention"})
	data["notes"] = append(notes, map[string]any{"id": "cache", "title": "Cache", "kind": "subject", "sections": []any{map[string]any{"heading": "Retention"}}})
	raw, err = json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = knowledgefixture.Write(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	drive(t, a, key("ctrl+r"), key("ctrl+l"))
	a.pal = newPalette(tokens.TrueColor, false)
	before := a.View().Content
	for y, line := range strings.Split(before, "\n") {
		plain := ansi.Strip(line)
		at := strings.Index(plain, "2. Cache")
		if at < 0 {
			continue
		}
		drive(t, a, tea.MouseMotionMsg{X: ansi.StringWidth(plain[:at]) + 3, Y: y})
		if after := a.View().Content; after == before {
			t.Fatal("linked-page picker has no visible hover")
		}
		drive(t, a, key("enter"))
		if got := placeFrameText(a); !strings.Contains(got, "Retention") {
			t.Fatal("Enter ignored hovered connection: " + got)
		}
		return
	}
	t.Fatal("missing second connection")
}
