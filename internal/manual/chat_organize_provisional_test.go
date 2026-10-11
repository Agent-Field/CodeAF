package manual

import "testing"

func TestTheChatManualAnswersProvisionalOrganizeQuestions(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"why does it say this plan is provisional", "desktop-organize-provisional"},
		{"what does a provisional plan mean", "desktop-organize-provisional"},
		{"a local model embedded the chats", "desktop-organize-provisional"},
	}
	for _, q := range asked {
		found := false
		for _, section := range Chat().Search(q.question, DefaultResults) {
			if section.Page == q.page {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach %s", q.question, q.page)
		}
	}
}
