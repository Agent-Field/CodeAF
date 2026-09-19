package manual

import (
	"strings"
	"testing"
)

// Wave 1 J01–J08 actions, named as CONTRACTS.md froze them. The live tmux
// journey is t-w1-live; this gate is that the chat corpus already quotes those
// names so a question reaches a true page instead of a denial that the UI does
// not exist.
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
