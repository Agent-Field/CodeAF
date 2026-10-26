package tui3

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestKnowledgePageEditorSavesAndCancels(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, key("ctrl+e"))
	got := placeFrameText(a)
	if !strings.Contains(got, "Edit Architecture") || !strings.Contains(got, "Save") {
		t.Fatal("editor did not open: " + got)
	}
	drive(t, a, key("enter"))
	for _, r := range "Human clarification." {
		drive(t, a, key(string(r)))
	}
	drive(t, a, key("ctrl+s"))
	got = placeFrameText(a)
	if !strings.Contains(got, "Human clarification.") || !strings.Contains(got, "Edited by you") {
		t.Fatal("save did not return durable human article: " + got)
	}
	drive(t, a, key("ctrl+e"))
	placeFrameText(a)
	drive(t, a, key("enter"))
	for _, r := range "Discard this sentence." {
		drive(t, a, key(string(r)))
	}
	drive(t, a, key("esc"))
	got = placeFrameText(a)
	if strings.Contains(got, "Discard this sentence.") || !strings.Contains(got, "Human clarification.") {
		t.Fatal("cancel changed the saved article: " + got)
	}
}

func TestKnowledgeDeleteRequiresNamedConfirmation(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, key("ctrl+d"))
	got := placeFrameText(a)
	for _, want := range []string{"Delete Architecture?", "original chats remain", "Cancel", "Delete page"} {
		if !strings.Contains(got, want) {
			t.Fatal("delete confirmation missing " + want + ": " + got)
		}
	}
	drive(t, a, key("enter"))
	got = placeFrameText(a)
	if !strings.Contains(got, "Storage") {
		t.Fatal("default Enter must cancel deletion: " + got)
	}
	drive(t, a, key("ctrl+d"))
	placeFrameText(a)
	drive(t, a, key("ctrl+d"))
	got = placeFrameText(a)
	if strings.Contains(got, "Storage") || strings.Contains(got, "Architecture") {
		t.Fatal("confirmed deletion retained page: " + got)
	}
	if !strings.Contains(got, "SQLite") {
		t.Fatal("delete must keep another page readable: " + got)
	}
}

func TestKnowledgeEditorPasteStaysInThePage(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, key("ctrl+e"))
	placeFrameText(a)
	drive(t, a, tea.PasteMsg{Content: "\n\nPasted paragraph.\n\nSee [[SQLite]]."})
	drive(t, a, key("ctrl+s"))
	got := placeFrameText(a)
	if !strings.Contains(got, "Pasted paragraph.") || !strings.Contains(got, "[[SQLite]]") {
		t.Fatal("multiline pasted edit was lost: " + got)
	}
	if strings.Contains(a.input.String(), "Pasted paragraph.") {
		t.Fatal("page paste leaked into conversation draft")
	}
}

func TestKnowledgeHumanMarkdownKeepsClickableWikiLinks(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, key("ctrl+e"))
	placeFrameText(a)
	drive(t, a, key("enter"))
	for _, r := range "A **strong** explanation with [[SQLite]]." {
		drive(t, a, key(string(r)))
	}
	drive(t, a, key("ctrl+s"))
	got := placeFrameText(a)
	if !strings.Contains(got, "strong explanation") || strings.Contains(got, "**strong**") {
		t.Fatal("human article Markdown is not rendered: " + got)
	}
	clickMemoryLabel(t, a, "[[SQLite]]")
	if got := placeFrameText(a); !strings.Contains(got, "Durability") {
		t.Fatal("rendered wiki link does not navigate: " + got)
	}
}

func TestKnowledgeEditorTabPasteKeepsCaretAndOriginalMarkdown(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, key("ctrl+e"))
	placeFrameText(a)
	drive(t, a, tea.PasteMsg{Content: "\n\tIndented"})
	lines, _, x, y := a.placeDraw(placeMemory{}, a.width, a.height)
	if y < 0 || y >= len(lines) || x != 13 || !strings.HasPrefix(ansi.Strip(lines[y]), "     Indented") {
		t.Fatalf("tab-expanded caret not at the end of visible text: x=%d y=%d", x, y)
	}
	drive(t, a, key("ctrl+s"))
	placeFrameText(a)
	page, err := a.agent.(*knowledgeTestAgent).vault.Read("architecture")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(knowledge.PageMarkdown(page), "\n\tIndented") {
		t.Fatal("display expansion changed the saved Markdown tab")
	}
}

func TestKnowledgeSaveConflictPreservesDraft(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, key("ctrl+e"))
	placeFrameText(a)
	drive(t, a, tea.PasteMsg{Content: "\nMy unsaved draft."})
	vault := a.agent.(*knowledgeTestAgent).vault
	current, err := vault.Read("architecture")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.EditPage(current.ID, current.Revision, "A newer external edit."); err != nil {
		t.Fatal(err)
	}
	drive(t, a, key("ctrl+s"))
	got := placeFrameText(a)
	if !strings.Contains(got, "Could not save") || !strings.Contains(got, "My unsaved draft.") || !strings.Contains(got, "Edit Architecture") {
		t.Fatal("conflict discarded the editing draft: " + got)
	}
	drive(t, a, key("esc"))
	got = placeFrameText(a)
	if !strings.Contains(got, "A newer external edit.") {
		t.Fatal("cancel did not reload latest page after conflict: " + got)
	}
}

func TestKnowledgeMarkdownCodeLiteralIsNotANavigationTarget(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, key("ctrl+e"))
	placeFrameText(a)
	drive(t, a, tea.PasteMsg{Content: "\n\nExample: `[[SQLite]]`\n\nFollow [[SQLite]]."}, key("ctrl+s"))
	got := placeFrameText(a)
	if strings.ContainsAny(got, "\u2063\u2064\u200c\u2060") {
		t.Fatal("internal link markers leaked into article")
	}
	clickOnLine := func(prefix string) {
		t.Helper()
		lines, _, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
		for y, line := range lines {
			text := ansi.Strip(line)
			if at := strings.Index(text, prefix); at >= 0 {
				link := strings.Index(text[at:], "[[SQLite]]")
				if link >= 0 {
					drive(t, a, tea.MouseClickMsg{X: ansi.StringWidth(text[:at+link]) + 3, Y: y, Button: tea.MouseLeft})
					placeFrameText(a)
					return
				}
			}
		}
		t.Fatal("line absent: " + prefix)
	}
	clickOnLine("Example:")
	if got := placeFrameText(a); !strings.Contains(got, "Storage") {
		t.Fatal("literal inside inline code became a link: " + got)
	}
	clickOnLine("Follow")
	if got := placeFrameText(a); !strings.Contains(got, "Durability") {
		t.Fatal("real link after code literal lost its target: " + got)
	}
}

func TestKnowledgeEditorWheelScrollsWithoutMovingTheDraftCaret(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, key("ctrl+e"))
	placeFrameText(a)
	var text strings.Builder
	for i := 0; i < 70; i++ {
		fmt.Fprintf(&text, "\nLine %02d", i)
	}
	drive(t, a, tea.PasteMsg{Content: text.String()})
	before := placeFrameText(a)
	drive(t, a, tea.MouseWheelMsg{X: 20, Y: 12, Button: tea.MouseWheelUp})
	after := placeFrameText(a)
	if before == after {
		t.Fatal("wheel did not scroll the editor")
	}
	a.placeDraw(placeMemory{}, a.width, a.height)
	if a.View().Cursor != nil {
		t.Fatal("offscreen editor caret must be hidden")
	}
	drive(t, a, key("end"))
	got := placeFrameText(a)
	if !strings.Contains(got, "Line 69") {
		t.Fatal("keyboard motion did not restore caret following: " + got)
	}
}

func TestKnowledgeEditedArticlePointsNewEvidenceToSources(t *testing.T) {
	a := knowledgeArticleApp(t)
	for i := range a.kb.reading.Pages {
		if a.kb.reading.Pages[i].ID == a.kb.selected {
			a.kb.reading.Pages[i].HumanEdited = true
			a.kb.reading.Pages[i].NeedsEditorialUpdate = true
		}
	}
	got := placeFrameText(a)
	if !strings.Contains(got, "New evidence is available in Sources") || strings.Contains(got, "Recent knowledge; guide integration") {
		t.Fatal("edited article points at an absent generated section: " + got)
	}
}
