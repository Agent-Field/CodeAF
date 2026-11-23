package tui3

import (
	"strings"
	"testing"
)

func TestEditedKnowledgeSpeaksWithoutMachineryWords(t *testing.T) {
	a := knowledgeArticleApp(t)
	page, ok := a.kb.page()
	if !ok {
		t.Fatal("no article to edit")
	}
	if err := a.kb.vault.EditPage(page.ID, page.Revision, "## Storage\n\nUse SQLite and keep a local copy."); err != nil {
		t.Fatal(err)
	}
	drive(t, a, key("ctrl+r"))
	text := placeFrameText(a)
	if !strings.Contains(text, "Edited by you") {
		t.Fatal("edited article has no label: " + text)
	}
	for _, word := range bannedWords {
		if strings.Contains(strings.ToLower(text), word) {
			t.Errorf("edited article says %q: %s", word, text)
		}
	}
	if !strings.Contains(text, "your additions have no source") {
		t.Error("edited article does not explain its additions")
	}
}
