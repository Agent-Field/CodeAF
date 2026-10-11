package manual

import "testing"

func TestPlaceEmbeddingQuestionsReachTheirManualPage(t *testing.T) {
	for _, question := range []string{
		"which model embeds places and how are embeddings billed",
		"do place embeddings work with a remote engine",
		"which jobs can the places organiser ask",
	} {
		reached := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-place-embeddings" {
				reached = true
			}
		}
		if !reached {
			t.Errorf("%q does not reach desktop-place-embeddings", question)
		}
	}
}
