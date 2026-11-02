package tui3

import (
	"strings"
	"testing"
)

func TestKnowledgeEditorKeyDoesNotUnfoldHiddenTaskProposal(t *testing.T) {
	a := knowledgeArticleApp(t)
	drive(t, a, proposal(a, 97, 0))
	drive(t, a, key("ctrl+e"))
	if got := placeFrameText(a); !strings.Contains(got, "Edit Architecture") {
		t.Fatal("hidden proposal took the Knowledge edit key: " + got)
	}
}
