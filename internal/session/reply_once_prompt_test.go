package session

import (
	"strings"
	"testing"
)

// A MODEL'S CHECKS ON ITSELF ARE WORKING, AND WORKING NEVER REACHES THE PERSON.
// In qloop run10 (engine 97c890c10, deepseek/deepseek-v4.1-flash through
// OpenRouter) a finished reply carried "Lounge: last message already there is
// unverified. Let me only cite what I saw. Links: default. Done." followed by a
// second draft of the same answer. The router's own accounting for that call
// said 142 completion tokens of which 4 were reasoning, and those 4 were the
// reasoning the engine recorded ("Confirmed. Reply."), so the self-check was
// written on the answer channel with no fence: the engine split the channels
// correctly, and there was nothing marked for answer.go to strip. A phrase
// filter would be a table of one model's habits, so the remedy is the prompt
// clause this test pins, which names the habit rather than its words, and which
// is folded into the sentence it extends because the fixed prefix is on a budget.
func TestTheSystemPromptKeepsSelfChecksOutOfTheReply(t *testing.T) {
	page := strings.Join(strings.Fields(systemPrompt), " ")
	want := "Final answers need no marker, stand alone, and carry no self-checks or second drafts."
	if !strings.Contains(page, want) {
		t.Errorf("prompts/system.md does not say %q, so nothing tells the model its self-checks and second drafts stay out of the reply", want)
	}
}
