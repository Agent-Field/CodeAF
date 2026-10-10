package manual

import "testing"

// File-tab icons are asked for by the kind of file, not by the registry name.
func TestDesktopFileTypeIconQuestionsReachTheirPage(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"what icon does a json file show", "desktop-file-type-icon"},
		{"what icon does a picture file tab use", "desktop-file-type-icon"},
		{"does a diff tab use the file icon", "desktop-file-type-icon"},
	}
	for _, ask := range asked {
		found := Chat().Search(ask.question, DefaultResults)
		if len(found) == 0 {
			t.Errorf("%q reaches nothing in the chat manual", ask.question)
			continue
		}
		var reached bool
		for _, section := range found {
			if section.Page == ask.page {
				reached = true
				break
			}
		}
		if !reached {
			pages := make([]string, 0, len(found))
			for _, section := range found {
				pages = append(pages, section.Page)
			}
			t.Errorf("%q should reach %s; it reached %v", ask.question, ask.page, pages)
		}
	}
	const named = "desktop file type icon"
	found := Chat().Search(named, 8)
	var reached bool
	for _, section := range found {
		if section.Page == "desktop-file-type-icon" {
			reached = true
			break
		}
	}
	if !reached {
		t.Errorf("%q should reach desktop-file-type-icon", named)
	}
}
