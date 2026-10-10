package manual

import "testing"

func TestTheChatManualAnswersKnowsPromoteQuestions(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"what happens when I answer the same way three times", "desktop-knows-promote"},
		{"where does learned from 3 of your answers come from", "desktop-knows-promote"},
		{"does a decided memory show up in what a place knows", "desktop-knows-promote"},
		{"can I undo a line the place learned", "desktop-knows-promote"},
	}
	for _, ask := range asked {
		t.Run(ask.question, func(t *testing.T) {
			for _, section := range Chat().Search(ask.question, DefaultResults) {
				if section.Page == ask.page {
					return
				}
			}
			t.Fatalf("%q did not reach %s", ask.question, ask.page)
		})
	}
}
