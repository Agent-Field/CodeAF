package manual

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/packed"
)

// Contract 7: the shipped compression path preserves answers to dev box questions.
func TestDevboxPackedManualAnswersTheSameQuestions(t *testing.T) {
	archive, err := packed.Pack("chat")
	if err != nil {
		t.Fatal(err)
	}
	if err := packed.Verify(archive, "chat"); err != nil {
		t.Fatal(err)
	}
	corpus := newCorpus(packed.New(archive), "chat/*.md")
	const page = "a-dev-box-on-your-own-railway"
	for _, question := range []string{"can I run codeaf on my own server", "set up a dev box", "always-on computer in the cloud", "how do I remove my dev box", "how much does a dev box cost"} {
		found := false
		for _, section := range corpus.Search(question, DefaultResults) {
			if section.Page == page {
				found = true
			}
		}
		if !found {
			t.Errorf("packed lookup for %q misses %s", question, page)
		}
	}
}
