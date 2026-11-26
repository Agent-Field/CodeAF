package tui3

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// A LIVE VIEW'S FOLDER BELONGS TO THE OTHER COMPUTER. The folder a chat runs in
// is a path on the computer that runs it, and Home's project (the default folder
// for the next conversation, the project the box names, the row the projects panel
// draws first) is a fact about THIS computer. The 2026-11-24 hand test opened chats
// that ran in /tmp/h4pc-ws on a PC from a Mac that has no such folder, and Home then
// said `project: /tmp/h4pc-ws`. These tests state what Home's project must be, from
// the person's side, around a live view and around a local conversation.

const otherComputersFolder = "/tmp/h4pc-ws"

// homeProjectRead is everything Home says about this computer's project, read
// with the cursor on no row so that the reading is the window's own and not the
// row the cursor happens to rest on.
type homeProjectRead struct {
	own, scope, launch string
	destinations       []string
}

func readHomeProject(a *app) homeProjectRead {
	cursor := a.home.cursor
	a.home.cursor = -1
	read := homeProjectRead{
		own:          a.ownWorkspace(),
		scope:        a.scopeWorkspace(),
		launch:       a.home.launch,
		destinations: a.targetDestinations(),
	}
	a.home.cursor = cursor
	return read
}

func (r homeProjectRead) equal(o homeProjectRead) bool {
	return r.own == o.own && r.scope == o.scope && r.launch == o.launch && slices.Equal(r.destinations, o.destinations)
}

// projectRig is a window on this computer's own folder with one chat held by an
// online computer of the person's own, whose live view reports that other
// computer's folder as its workspace, and a conversation of this computer's own
// behind it that closing the live view returns to.
func projectRig(t *testing.T) (a *app, own string) {
	t.Helper()
	a, _ = liveHomeRig(t, "dev_tower")
	own = filepath.Join(t.TempDir(), "parser")
	a.workspace = own
	a.file = filepath.Join(t.TempDir(), "cellA", "session.jsonl")
	wireFakeOpenLive(t, a, nil)
	inner := a.openLive
	a.openLive = func(ctx context.Context, row chatlist.Row) (Conversation, error) {
		conv, err := inner(ctx, row)
		conv.Workspace = otherComputersFolder
		return conv, err
	}
	drain(t, a, a.openHome())
	return a, own
}

// C-a: after opening a live view of another computer's chat, going back to Home
// and closing the live view, Home's project is what it was before. The live
// view itself is about the other computer's folder, and that does not leak.
func TestAnOpenLiveViewLeavesHomesProjectAlone(t *testing.T) {
	a, own := projectRig(t)
	before := readHomeProject(a)
	if before.own != own || before.launch != own {
		t.Fatalf("the rig does not start in its own folder: %+v, want %s", before, own)
	}

	standOn(t, a, "c1")
	if a.liveFront() == nil || a.at(pageHome) {
		t.Fatalf("the Enter did not end on a live view: live=%v home=%v", a.liveFront(), a.at(pageHome))
	}
	if a.workspace != otherComputersFolder {
		t.Fatalf("the live conversation does not carry the other computer's folder (%q), so this test proves nothing", a.workspace)
	}

	drain(t, a, a.openHome())
	during := readHomeProject(a)
	if !during.equal(before) {
		t.Fatalf("Home's project moved while a live view is in front:\nbefore %+v\nduring %+v", before, during)
	}
	if frame := homeText(a); strings.Contains(frame, "h4pc-ws") {
		t.Fatalf("Home draws the other computer's folder:\n%s", frame)
	}
	if where := a.targetProject(); strings.Contains(where, "h4pc-ws") {
		t.Fatalf("the project the box names is the other computer's folder: %q", where)
	}

	// Closing the live view returns to the conversation behind it.
	a.closeHome()
	cmd, closed := a.closeFront()
	if !closed {
		t.Fatal("the live view did not close: nothing was behind it, so the rig does not test the close")
	}
	drain(t, a, cmd)
	if a.liveFront() != nil {
		t.Fatal("a live view is still in front after it was closed")
	}
	drain(t, a, a.openHome())
	if after := readHomeProject(a); !after.equal(before) {
		t.Fatalf("Home's project is not what it was before the live view:\nbefore %+v\nafter  %+v", before, after)
	}
}

// C-a, the pin: a folder the person chose with alt+p stays chosen across a live
// view, as a pin always does.
func TestAnOpenLiveViewDoesNotMoveAPinnedProject(t *testing.T) {
	a, _ := projectRig(t)
	pinned := filepath.Join(t.TempDir(), "elsewhere")
	a.target.where = pinned

	standOn(t, a, "c1")
	drain(t, a, a.openHome())
	if got := a.targetWhere(); got != pinned {
		t.Fatalf("targetWhere = %q after a live view, want the pin %q", got, pinned)
	}
}

// C-b: a local conversation in another folder moves Home's project exactly as it
// always did. The window's folder is the last conversation's, and Home follows it.
func TestALocalConversationInAnotherFolderStillMovesHomesProject(t *testing.T) {
	a, own := projectRig(t)
	other := filepath.Join(t.TempDir(), "other")

	a.takeUp(Conversation{
		Agent:       &fakeAgent{model: "m"},
		SessionFile: filepath.Join(t.TempDir(), "cellB", "session.jsonl"),
		Workspace:   other,
	}, true)
	drain(t, a, a.openHome())

	read := readHomeProject(a)
	if read.own != other || read.scope != other || read.launch != other || a.workspace != other {
		t.Fatalf("Home did not follow a local conversation to %s: workspace=%q %+v", other, a.workspace, read)
	}
	if read.own == own {
		t.Fatal("Home's project did not move")
	}
	if !slices.Contains(read.destinations, other) {
		t.Fatalf("the projects the draft can walk to do not include the new folder: %v", read.destinations)
	}
}
