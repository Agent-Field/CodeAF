package manual

import "testing"

// These probes keep question evidence reachable without relying on UI labels.
func TestDesktopAttentionDetailsQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"which task dependencies are reported by desktop attention",
		"where does a desktop suggested answer and confidence come from",
		"which places does a desktop attention question belong to",
		"why did an answered desktop question disappear from attention",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-attention-details" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-attention-details", question)
		})
	}
}
