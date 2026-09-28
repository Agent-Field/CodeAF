package session

import "testing"

// Contract 3.3: one classifier knows a team wake note by the very sentences the
// session composes it from, and nothing else.
func TestTeamWakeNoteKnowsTheWakeSentencesAndNothingElse(t *testing.T) {
	for _, text := range []string{
		teamWakeMemberLead + "\n\nTeam traffic in \"harbor\" (@api), from your manager:\ndirective from manager: status?",
		teamWakeManagerLead + "\n\nWhat your members in \"harbor\" did:\n@api finished",
		"  " + teamWakeManagerLead,
	} {
		if !TeamWakeNote(text) {
			t.Errorf("a wake note was not known: %q", text)
		}
	}
	for _, text := range []string{
		"",
		"Task 3 finished: the parser is fixed.",
		"Team traffic in \"harbor\" (@api), from your manager:\ndirective from manager: status?",
		"Your team is great; the person did not speak.",
	} {
		if TeamWakeNote(text) {
			t.Errorf("a note that is not a wake was taken for one: %q", text)
		}
	}
}
