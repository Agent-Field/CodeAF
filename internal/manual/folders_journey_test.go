package manual

import (
	"strings"
	"testing"
)

// Wave 1 J01–J08 and Wave 2 J09–J18 actions, named as CONTRACTS.md froze
// them. The live tmux journeys are t-w1-live and t-w2-live; this gate is that
// the chat corpus already quotes those names so a question reaches a true
// page instead of a denial that the UI does not exist.
func TestWave1FolderJourneyNamesStandInTheChatManual(t *testing.T) {
	text := strings.ToLower(mustPage(t, "collections"))
	for _, want := range []string{
		"folders",
		"logical groups of chats · /folders create billing",
		"/folders create",
		"/folders add",
		"/folders nest",
		"/folders rename",
		"`n` new chat here · `f` add current chat · `m` move this placement · `w` why here · `x` remove this placement",
		"also in",
		"/folder",
		"/folders",
		"root",
		"automatic organization",
		"inherited instructions",
		"standing guidance for chats in this folder",
		"/folders instruct",
		"instruct this folder",
		"discovery delayed",
		"keyword-only",
		"organizer",
		"memory off",
		"checked",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("collections.md is missing the frozen journey name %q", want)
		}
	}
}

func mustPage(t *testing.T, name string) string {
	t.Helper()
	text, ok := Chat().Page(name)
	if !ok {
		t.Fatalf("chat manual has no page %s", name)
	}
	return text
}
