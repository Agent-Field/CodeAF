package manual

import "testing"

func TestChatManualAnswersPlaceShapeQuestions(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"why was this place folded into the place above", "desktop-place-shape"},
		{"what happens with fewer than four chats", "desktop-place-shape"},
		{"when is a new place above several others offered", "desktop-place-shape"},
		{"why did this place become a view", "desktop-place-shape"},
		{"how deep can places go", "desktop-place-shape"},
	}
	for _, ask := range asked {
		t.Run(ask.question, func(t *testing.T) {
			found := Chat().Search(ask.question, DefaultResults)
			if len(found) == 0 {
				t.Fatalf("%q reaches nothing", ask.question)
			}
			for _, section := range found {
				if section.Page == ask.page {
					return
				}
			}
			t.Fatalf("%q did not reach %s", ask.question, ask.page)
		})
	}
}
