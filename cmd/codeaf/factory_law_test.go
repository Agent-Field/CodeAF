package main

import "testing"

func TestTalkRecipeLawIsTheExactSentence(t *testing.T) {
	const want = "The recipe is the team's. A stage marked fixed is not yours to change; say so if the item needs it."
	if talkRecipeLaw != want || talkLawLine() != want {
		t.Fatalf("law = %q / %q", talkRecipeLaw, talkLawLine())
	}
}
