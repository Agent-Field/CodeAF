package manual

import "testing"

// These probes keep the plan card's confirmation, steps and receipt reachable in a person's words.
func TestDesktopPlanCardQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"can you propose a plan and wait for my go ahead",
		"what happens when I press Go Edit or Cancel on a plan",
		"what steps can a plan card hold",
		"what if a task in the plan was deleted",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-plan-card" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-plan-card", question)
		})
	}
}
