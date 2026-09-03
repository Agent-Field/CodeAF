package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// THE CHIP SAYS AN ADDRESS OR IT SAYS THIS WINDOW'S.
//
// `here` is where what you type will land, and a display name in that slot is
// not somewhere: `here aforge-v2` cannot be told from a second checkout of the
// same name, and the row above it says `here ~/aforge-v2` about the same
// machine. The rule is one line: a row answers with a path or with nothing, and
// nothing falls through to the workspace this window is standing in.
func TestTheScopeChipTakesARowsAddressAndNeverItsName(t *testing.T) {
	const project = "/work/aforge-v2"
	for _, c := range []struct {
		what string
		line homeLine
		want string
	}{
		{"a conversation", homeLine{kind: homeSession, project: "aforge-v2",
			row: session.SessionRow{ProjectDir: project}}, project},
		{"a standing item that knows its project root", homeLine{kind: homeItem, project: "aforge-v2",
			item: standing.Item{Workspace: project}}, project},
		{"a project heading", homeLine{kind: homeProject, project: "aforge-v2",
			proj: session.Project{Name: "aforge-v2", Path: project}}, project},
		// THE ROW THIS FIX IS ABOUT. It knows its project's NAME and nothing
		// else, and a name is not an answer to "where".
		{"a standing item that recorded no directory", homeLine{kind: homeItem, project: "aforge-v2"}, ""},
	} {
		got := scopeAddress(c.line)
		if got != c.want {
			t.Errorf("%s: the chip's address is %q, want %q", c.what, got, c.want)
		}
	}
}

// AND ON THE FRAME: two rows a keypress apart say `where` the same way.
//
// The chip is drawn from [app.scopeWorkspace], so the law above is only worth
// having if the drawn line obeys it. This puts a conversation and a directoryless
// standing item on one list and reads the chip on each.
func TestTwoHomeRowsSayWhereInTheSameWords(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 40)

	// The list this test needs is a conversation that records its project and an
	// item that records nothing but a name, side by side.
	const project = "/work/aforge-v2"
	a.home.hover = -1
	a.home.lines = []homeLine{
		{kind: homeSession, project: "aforge-v2", row: session.SessionRow{ProjectDir: project, Title: "porting the picker"}},
		{kind: homeItem, project: "aforge-v2", item: standing.Item{Words: "draft the weekly update"}},
	}

	a.home.cursor = 0
	onChat := a.scopeChip()
	a.home.cursor = 1
	onItem := a.scopeChip()

	if onChat == "" || onItem == "" {
		t.Fatalf("the chip drew nothing: on the conversation %q, on the item %q", onChat, onItem)
	}
	// The conversation's row is the one that was always right, and it is the
	// standard the item's row is held to.
	if !strings.HasPrefix(onChat, placeScopeWord+" ") || !strings.Contains(onChat, "/") {
		t.Fatalf("the conversation's chip reads %q, want %q and a path", onChat, placeScopeWord)
	}
	if !strings.Contains(onItem, "/") {
		t.Fatalf("the item's chip reads %q — a name in the slot that says where a task will land; the row above it reads %q",
			onItem, onChat)
	}
	if strings.HasSuffix(onItem, " aforge-v2") {
		t.Fatalf("the item's chip reads %q, which is a project's NAME and not an address", onItem)
	}
}
