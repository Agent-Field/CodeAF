package manual

import "testing"

// The suggested-place card has to answer in the words a person uses for that card and the sparkles line.
func TestSuggestionCardQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"where is the suggested place card and what do Create and Not now do",
		"why is there a sparkles line about chats that belong in a place",
		"what is the quiet merge or archive line on All places and in go to",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-suggestion-card" {
					return
				}
			}
			t.Fatalf("%q should reach desktop-suggestion-card", question)
		})
	}
}
