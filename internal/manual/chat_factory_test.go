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
