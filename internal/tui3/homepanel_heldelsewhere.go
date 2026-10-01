package tui3

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// ── A WINDOW ON A CHAT ANOTHER MACHINE NOW HOLDS ────────────────────────────
//
// A message typed into a chat whose lease another machine holds would not reach
// the chat: it would make a branch nobody asked for. So enter shows the door
// instead: home, with the chat's row from that machine under the cursor and the
// `Continue here` card up. The words stay in the box.
//
// IT READS ONLY WHAT WAS ALREADY FETCHED ([app.machineRead]). Away from home
// nothing asks the directory, and this adds no ask: a window that has never
// read the other machines has nothing to say and sends as it always did.

// heldElsewhere is the row of the chat on screen when another machine holds it.
func (a *app) heldElsewhere() (chatlist.Row, bool) {
	if a.file == "" || a.hosted() {
		return chatlist.Row{}, false
	}
	cell := filepath.Base(filepath.Dir(a.file))
	for _, row := range a.machineRead.rows {
		if row.Cell == cell && row.Status == chatlist.Running {
			return row, true
		}
	}
	return chatlist.Row{}, false
}

// heldElsewhereDoor shows the door and reports that it did, so the caller sends
// nothing.
func (a *app) heldElsewhereDoor() (tea.Cmd, bool) {
	row, ok := a.heldElsewhere()
	if !ok || a.taker == nil || a.machineRead.down {
		return nil, false
	}
	cmd := a.openHome()
	a.home.pointAt(func(l homeLine) bool { return l.kind == homeMachineRow && l.remote != nil && l.remote.Cell == row.Cell })
	return tea.Batch(cmd, a.askContinue(row)), true
}
