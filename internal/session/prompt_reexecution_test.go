package session

import (
	"strings"
	"testing"
)

// THE TRANSCRIPT IS EVIDENCE, NOT JUST THE LATEST TOOL RESULT. The old wording
// made a model whose reasoning is split across steps buy the same fact again
// before it could quote it. These wording pins keep every part of the visible
// conversation available and make needless re-derivation an explicit defect.
func TestTheSystemPromptDoesNotRewardReexecution(t *testing.T) {
	for _, want := range []string{
		"NUMBERS AND FACTS COME FROM THE CONVERSATION",
		"earlier turns, earlier steps of this turn, or stubbed output you have read",
		"Once read, its content remains available for the conversation",
		"BEFORE RUNNING A COMMAND, CHECK THE TRANSCRIPT",
		"Re-deriving a settled fact is a defect, not diligence",
	} {
		if !strings.Contains(systemPrompt, want) {
			t.Errorf("prompts/system.md does not say %q, so the model is still rewarded for re-running settled work", want)
		}
	}

	for _, gone := range []string{
		"every figure must appear in something a tool showed you this turn",
		"read` that path when you need the bytes",
	} {
		if strings.Contains(systemPrompt, gone) {
			t.Errorf("prompts/system.md still says %q, which limits usable evidence to the latest step", gone)
		}
	}
}
