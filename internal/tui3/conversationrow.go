package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
	"strings"
)

// openConversationRow opens one conversation from a place that is not home.
//
// IT IS [app.homeOpenLine]'S LADDER WITH HOME'S COLUMN TAKEN OUT, and the order
// of the checks is the feature there and here alike. Identity comes first,
// because a transcript THIS PROCESS holds answers [session.InUse] true about
// itself — a flock rides the open file description rather than the process — so
// a conversation one keystroke away would otherwise be refused as somebody
// else's window (keeper.go's [app.holding] states the whole rule).
//
// The two differences from home's are both about where a refusal goes. Home
// says its refusals on its own message line, which belongs to home's view; a
// place that is not home says them on [app.pageMsg], which is the router's one
// line for exactly this. And there is no "home already knows this row is
// locked" shortcut here, because this place never read the flock: the open
// reports it instead, which is what the resume picker has always done.
func (a *app) openConversationRow(row session.SessionRow) tea.Cmd {
	switch {
	case a.holding(row.Transcript):
		// A conversation this terminal already has open: the one on screen, or one
		// running behind it. Either way enter goes to it rather than opening
		// anything — reopening would drop the lock, replay the journal and land
		// exactly where it started. It says nothing, for home's reason: closing
		// into the conversation somebody just confirmed IS the thing happening.
		cmd, _ := a.bringForward(row.Transcript)
		a.standDownFullscreen()
		a.closeRoom()
		a.touch()
		return cmd
	case !a.canOpen():
		a.pageMsg = resumeUnavailableWord
		return nil
	}
	where := strings.TrimSpace(row.ProjectDir)
	if where != "" && !homeFolderThere(where) {
		// ONE os.Stat, ON THE KEYSTROKE. An agent whose tool root does not exist
		// fails every bash and every relative path in a way nothing on screen
		// explains, so the door is checked before it is walked through.
		a.pageMsg = homeGoneWord + " · " + where
		return nil
	}
	// AND THE CONVERSATION THIS WINDOW WAS IN GOES ON RUNNING. It is detached
	// rather than closed and put in the keeper, exactly as home's own door leaves
	// it — which is the whole of what makes any of these places a way BETWEEN
	// conversations rather than a list of ones to open in another terminal.
	cmd, refusal := a.openBeside(where, row.Transcript)
	if refusal != "" {
		a.pageMsg = refusal
		return nil
	}
	a.standDownFullscreen()
	a.closeRoom()
	a.touch()
	return cmd
}
