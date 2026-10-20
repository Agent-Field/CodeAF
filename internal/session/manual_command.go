package session

import "strings"

const ManualTourAsk = "What can codeaf do? Answer from codeaf's own manual — the manual tool — and name the pages worth reading first."
const ManualQuestionLead = "Answer from codeaf's own manual — the manual tool — and say which page it came from: "

// ManualCommand is the terminal's authored line and the instruction sent behind it.
func ManualCommand(question string) (text, said string) {
	question = strings.TrimSpace(question)
	if question == "" {
		return ManualTourAsk, "/manual"
	}
	return ManualQuestionLead + question, "/manual " + question
}
