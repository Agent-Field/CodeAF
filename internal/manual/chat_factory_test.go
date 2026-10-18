package manual

import (
	"strings"
	"testing"
)

// THE FACTORY PAGE IS REACHED BY THE WORDS A PERSON USES. Retrieval is the
// feature: a page that is complete, correct and unreachable is a page the chat
// talks over the top of, so each question here is a real one in a person's own
// words and must reach the section that answers it, with the fact that makes the
// answer true on that section's body.
func TestTheFactoryQuestionsReachTheirSections(t *testing.T) {
	asked := []struct {
		question string
		page     string
		title    string
		says     []string
	}{
		{
			question: "how do I run a team factory on my own server and what port does codeaf factory serve listen on",
			page:     "factory",
			title:    "codeaf factory serve",
			says:     []string{"127.0.0.1:7420", "--listen", "runs no job itself"},
		},
		{
			question: "how do I give a teammate access to the factory with an access link or token and revoke it",
			page:     "factory",
			title:    "give someone access",
			says:     []string{"codeaf factory token add", "codeaf factory revoke <id>", "codeaf factory revoke-sessions"},
		},
		{
			question: "how do I connect my machine to someone else's factory with codeaf attach and go back with detach",
			page:     "factory",
			title:    "codeaf attach and codeaf detach",
			says:     []string{"attach.json", "does not dial the factory", "`codeaf detach` removes the file"},
		},
		{
			question: "how do I add a runner to a factory with codeaf runner join and the invite link",
			page:     "factory",
			title:    "codeaf runner join",
			says:     []string{"sign in with GitHub", "the runner works as you and never as the operator", "`--attach-only`"},
		},
		{
			question: "can you hand this issue to the factory from chat, what is factory_add and does it start anything",
			page:     "factory",
			title:    "from chat to the factory",
			says:     []string{"`factory_add`", "writes nothing until you say yes", "none of them starts a job"},
		},
		{
			question: "what does a factory stage conversation get, stage_result and plan_edit",
			page:     "factory-stage-conversations",
			title:    "a stage is a conversation",
			says:     []string{"`stage_result`", "`plan_edit`"},
		},
	}
	for _, ask := range asked {
		reached := false
		for _, section := range Chat().Search(ask.question, DefaultResults) {
			if section.Page != ask.page || !strings.Contains(section.Title, ask.title) {
				continue
			}
			reached = true
			for _, needle := range ask.says {
				if !strings.Contains(section.Body, needle) {
					t.Errorf("%q reached %q but its body omits %q", ask.question, section.Title, needle)
				}
			}
		}
		if !reached {
			t.Errorf("%q did not reach %s › %s", ask.question, ask.page, ask.title)
		}
	}
}

// THE THINKING SECTION IS REACHED BY THE ASKER'S WORDS and says the three
// things a person cannot see from the card alone: what the model is told when
// a step is left alone, what a bad level is refused with, and that a later
// turn's opening note lists thinking per step when steps differ.
func TestTheFactoryThinkingQuestionsReachTheirSection(t *testing.T) {
	for _, ask := range []string{
		"I said think strong but the plan step is still cheap, why did one step not change",
		"the job has mixed thinking, how does the factory item chat tell me which steps think what",
		"what does the receipt say after I say yes to think strong and a step was fixed",
		"what happens if I ask factory_item for an effort that is not cheap strong or default",
		"does think the usual amount reset every step including the ones switched off",
	} {
		reached := false
		for _, section := range Chat().Search(ask, DefaultResults) {
			if section.Page != "factory" || !strings.Contains(section.Title, "change one job's thinking") {
				continue
			}
			reached = true
			for _, needle := range []string{
				"Invalid arguments: effort is one of cheap, strong, default, or left out.",
				"left as they were",
				"opening note",
				"(off)",
			} {
				if !strings.Contains(section.Body, needle) {
					t.Errorf("%q reached the thinking section but its body omits %q", ask, needle)
				}
			}
		}
		if !reached {
			t.Errorf("%q did not reach the thinking section", ask)
		}
	}
	for _, section := range Chat().Search("think strong", DefaultResults) {
		if strings.Contains(section.Body, "not a thinking level") {
			t.Errorf("%s › %s quotes a refusal the chat never sees", section.Page, section.Title)
		}
	}
}
