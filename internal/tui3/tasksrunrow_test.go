package tui3

import (
	"strings"
	"testing"
)

// THE PAGE'S UNIT IS A RUN. A conversation is context at the root's dim tail,
// never a row over the work, and descendants do not repeat it.
func TestTheTasksPageDrawsRunsAtItsEdgeWithOneConversationTail(t *testing.T) {
	world, win, now := tasksFamilyFixture()
	reading := readTasks(world, tasksMine{}, win, tasksSort{}, now, now)
	reading.open = map[tasksKey]bool{{session: "room-a", id: "1"}: true}
	lines := reading.lay(120)
	for _, line := range lines {
		if line.kind == tasksLineChat {
			t.Fatalf("the page drew a conversation row: %+v", line.chat)
		}
	}
	root := tasksLineOf(t, lines, "port the parser")
	if strings.HasPrefix(root.kin, tasksKinPad+tasksKinPad) || !strings.Contains(workConversationTail(root.item), "the split") {
		t.Fatalf("root kin=%q tail=%q, want page-edge run with conversation tail", root.kin, workConversationTail(root.item))
	}
	child := tasksLineOf(t, lines, "port the lexer")
	if tail := workConversationTail(child.item); tail != "" {
		t.Fatalf("child repeated conversation tail %q", tail)
	}
}
