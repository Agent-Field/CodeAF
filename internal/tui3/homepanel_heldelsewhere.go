package tui3

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── A WINDOW ON A CHAT ANOTHER MACHINE NOW HOLDS ────────────────────────────
//
// A message typed into a chat whose lease another machine holds would not reach
// the chat: it would make a branch nobody asked for. So enter shows the door
// instead: home, with the chat's row from that machine under the cursor and the
// `Continue here` card up. The words stay in the box.
//
// A submitted message asks once off the loop before choosing the door, because
// the cached holder may have changed. The draft stays intact until the answer
// arrives; an unreachable reading keeps the existing local availability rules.

// heldElsewhere is the row of the chat on screen when another machine holds it.
func (a *app) heldElsewhere() (chatlist.Row, bool) {
	if a.file == "" || a.hosted() {
		return chatlist.Row{}, false
	}
	cell := filepath.Base(session.DirOf(a.file))
	for _, row := range a.machineRead.rows {
		if row.Cell == cell && !row.Mine && (row.Status == chatlist.Running || row.Status == chatlist.Off || row.Status == chatlist.Idle) {
			return row, true
		}
	}
	return chatlist.Row{}, false
}

// heldElsewhereDoor shows the door and reports that it did, so the caller sends
// nothing.
func (a *app) heldElsewhereDoor() (tea.Cmd, bool) {
	row, ok := a.heldElsewhere()
	if !ok {
		return nil, false
	}
	if a.taker == nil || a.machineRead.down {
		a.note(chatlist.Superseded(row.Device))
		return nil, true
	}
	cmd := a.openHome()
	a.home.pointAt(func(l homeLine) bool { return l.kind == homeMachineRow && l.remote != nil && l.remote.Cell == row.Cell })
	return tea.Batch(cmd, a.askContinue(row)), true
}

// ownershipReadMsg carries one submission's directory reading. The draft and
// tray identify what was waiting, so an answer cannot send words the person
// changed or move a message into a different conversation.
type ownershipReadMsg struct {
	file, draft string
	chips       []chip
	reading     homeMachinesMsg
	resume      func() tea.Cmd
	first       *composerState
}

// checkBeforeSend closes the interval between another device's acquire and
// its change frame arriving. This one read per submitted message stays off the
// update loop, and a never-paired home remains quiet through its source's gate.
func (a *app) checkBeforeSend() tea.Cmd {
	return a.checkBeforeSendWith(func() tea.Cmd { return a.enterCheckedLine(true) })
}

// needsOwnershipRead lets an in-turn send chord preserve its own action across
// the read, rather than returning early before it can promote a parked draft.
func (a *app) needsOwnershipRead() bool {
	line := strings.TrimSpace(a.input.String())
	return a.watchesConversation() && !strings.HasPrefix(line, "/") && (line != "" || len(a.chips) > 0)
}

func (a *app) checkBeforeSendWith(resume func() tea.Cmd) tea.Cmd {
	src, file, draft := a.machines, a.file, a.input.String()
	chips := slices.Clone(a.chips)
	var first *composerState
	if a.conversationPreview != nil && !a.conversationOpening {
		a.conversationPreview.ownershipPending = true
		composer := a.liveComposer()
		first = &composer
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), machinesAskTimeout)
		defer cancel()
		rows, version, err := chatlist.RowsAt(ctx, src)
		return ownershipReadMsg{file: file, draft: draft, chips: chips, resume: resume, first: first, reading: homeMachinesMsg{rows: rows, version: version, err: err}}
	}
}

func (a *app) afterOwnershipRead(msg ownershipReadMsg) tea.Cmd {
	if a.file != msg.file {
		if msg.first != nil {
			// Switching cancels this send, but its accepted words still belong
			// to that conversation alongside the follow-up parked with it.
			if held := a.behind[a.convKey(msg.file)]; held != nil {
				if held.side == nil {
					held.side = &aside{}
				}
				side := held.side
				first := &app{input: msg.first.box, pastes: msg.first.pastes}
				next := &app{input: side.mainBox(), pastes: side.pastes}
				side.draft = first.pastesUnfolded(first.input.String())
				if !next.input.empty() {
					side.draft += "\n" + next.pastesUnfolded(next.input.String())
				}
				side.chips = append(slices.Clone(msg.first.chips), side.chips...)
				side.pastes, side.draftCursor = nil, nil
			}
		}
		return nil
	}
	if msg.first != nil {
		// Opening accepted the first send. The box now belongs to the next
		// draft, which must not invalidate that send while ownership is read.
		next := a.liveComposer()
		a.putComposer(*msg.first)
		reading := a.recordMachines(msg.reading)
		cmd := msg.resume()
		if a.liveComposer().empty() {
			a.putComposer(next)
		} else {
			a.chips = append(a.chips, next.chips...)
			if !next.box.empty() {
				draft := &app{input: next.box, pastes: next.pastes}
				a.input.setText(a.input.String() + "\n" + draft.pastesUnfolded(next.box.String()))
			}
		}
		return tea.Batch(reading, cmd)
	}
	if a.input.String() != msg.draft || !slices.Equal(a.chips, msg.chips) {
		return nil
	}
	reading := a.recordMachines(msg.reading)
	return tea.Batch(reading, msg.resume())
}
