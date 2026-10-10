package manual

import "testing"

// The place-home trail has to answer in the words a person uses for the line above the title.
func TestPlaceBreadcrumbQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"what is the breadcrumb trail above a place title",
		"how do I open a parent from the breadcrumb trail",
		"why does the breadcrumb follow only one parent",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-breadcrumb" {
					return
				}
			}
			t.Fatalf("%q should reach desktop-place-breadcrumb", question)
		})
	}
}
