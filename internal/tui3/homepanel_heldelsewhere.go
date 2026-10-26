package tui3

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
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
// arrives; an unreachable reading leaves those words unsent.

// heldElsewhere is the row of the chat on screen when another machine holds it.
func (a *app) heldElsewhere() (chatlist.Row, bool) {
	if a.file == "" || a.hosted() {
		return chatlist.Row{}, false
	}
	cell := filepath.Base(session.DirOf(a.file))
	for _, row := range a.machineRead.rows {
		if row.Cell == cell && (row.Status != chatlist.Here || a.machineRead.sendHolds[cell].blocked || a.machineRead.sendHolds[cell].fence != row.Fence) {
			return row, true
		}
	}
	return chatlist.Row{}, false
}

// ownershipReadMsg carries one submission's directory reading. The draft and
// tray identify what was waiting, so an answer cannot send words the person
// changed or move a message into a different conversation.
type ownershipReadMsg struct {
	file, draft     string
	chips           []chip
	reading         homeMachinesMsg
	resume          func() tea.Cmd
	first           *composerState
	head, leaseCell string
	fence           uint64
	proof           bool
}

type sendHold struct {
	fence   uint64
	blocked bool
	// failed marks a hold whose take did not finish, so its session is a
	// copy this window may not keep using even if a later take finds the
	// chat already here.
	failed bool
}

type sendLeaseSource interface {
	SendLease(string) (string, uint64, error)
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
		msg := ownershipReadMsg{file: file, draft: draft, chips: chips, resume: resume, first: first, reading: homeMachinesMsg{rows: rows, version: version, err: err}}
		id := filepath.Base(session.DirOf(file))
		if local, ok := src.(sendLeaseSource); ok {
			msg.proof = true
			msg.leaseCell, msg.fence, _ = local.SendLease(id)
		}
		remote := id
		if msg.leaseCell != "" {
			remote = msg.leaseCell
		}
		for _, row := range rows {
			if row.Cell == remote && (row.Status == chatlist.Idle || row.Status == chatlist.Off) {
				if head, _ := cellstore.Head(cell.Cell{ID: id, Root: session.DirOf(file)}); head != nil {
					msg.head = head.Turn.ID
				}
				break
			}
		}
		return msg
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
		cmd := a.resumeOwnedSend(msg)
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
	return tea.Batch(reading, a.resumeOwnedSend(msg))
}

// resumeOwnedSend spends only the submitted gesture after its fresh ownership
// reading. A free chat can resume silently only from the same saved turn.
func (a *app) resumeOwnedSend(msg ownershipReadMsg) tea.Cmd {
	if a.machineRead.computerRemoved {
		a.note(chatlist.Removed)
		return nil
	}
	id := filepath.Base(session.DirOf(msg.file))
	remote := id
	if msg.leaseCell != "" {
		remote = msg.leaseCell
	}
	if msg.reading.err != nil {
		// AN UNREACHABLE SERVICE DOES NOT TAKE A HELD CHAT AWAY. The window
		// keeps working on a chat whose hold it recorded and last saw as its
		// own, and on a chat that was never shared; its turns wait to publish.
		// Anything else stays unsent, because this window cannot show it holds
		// the chat.
		if a.offlineSendHeld(id, remote, msg) {
			return msg.resume()
		}
		a.note(chatlist.Unreachable)
		return nil
	}
	for _, row := range msg.reading.rows {
		if row.Cell != remote {
			continue
		}
		held := a.machineRead.sendHolds[id]
		fence := held.fence
		if msg.proof {
			fence = msg.fence
		}
		if row.Status == chatlist.Here && row.Fence == fence && !held.blocked {
			return msg.resume()
		}
		if !held.blocked && (row.Status == chatlist.Idle || row.Status == chatlist.Off) && row.Head != "" && row.Head == msg.head {
			if taker, ok := a.taker.(conditionalTaker); ok {
				draft := a.mainComposer()
				ctx := a.ctx
				return func() tea.Msg {
					taken, err := taker.TakeSeen(ctx, row.Cell, &directory.LeaseExpectation{Device: row.DeviceID, Fence: row.Fence})
					return homeTakenMsg{row: row, taken: taken, err: err, draft: &draft, send: msg.resume}
				}
			}
		}
		if a.taker == nil {
			a.note(chatlist.Superseded(nonemptyDevice(row.Device)))
			return nil
		}
		a.machineRead.sendHolds[id] = sendHold{fence: fence, blocked: true}
		cmd := a.openHome()
		return tea.Batch(cmd, a.askContinue(row))
	}
	if held, known := a.machineRead.sendHolds[id]; held.blocked || (known && held.fence != 0) || (msg.proof && msg.fence != 0) {
		a.note(chatlist.Superseded("another device"))
		return nil
	}
	return msg.resume()
}

// offlineSendHeld judges a send by the last good reading when the fresh one
// could not be made: the chat is this window's own at the fence it recorded, or
// it was never shared and nobody is known to hold it.
func (a *app) offlineSendHeld(id, remote string, msg ownershipReadMsg) bool {
	held := a.machineRead.sendHolds[id]
	if held.blocked {
		return false
	}
	fence := held.fence
	if msg.proof {
		fence = msg.fence
	}
	for _, row := range a.machineRead.rows {
		if row.Cell == remote {
			return fence != 0 && row.Status == chatlist.Here && row.Fence == fence
		}
	}
	return fence == 0
}

func nonemptyDevice(device string) string {
	if device == "" {
		return "another device"
	}
	return device
}

func (a *app) rememberSendHolds(rows []chatlist.Row) {
	if a.machineRead.sendHolds == nil {
		a.machineRead.sendHolds = map[string]sendHold{}
	}
	for _, row := range rows {
		if _, known := a.machineRead.sendHolds[row.Cell]; !known && row.Status == chatlist.Here {
			a.machineRead.sendHolds[row.Cell] = sendHold{fence: row.Fence}
		}
	}
}

func (a *app) failedTake(msg homeTakenMsg) string {
	if a.machineRead.sendHolds == nil {
		a.machineRead.sendHolds = map[string]sendHold{}
	}
	held := a.machineRead.sendHolds[msg.row.Cell]
	held.blocked, held.failed = true, true
	a.machineRead.sendHolds[msg.row.Cell] = held
	if a.file != "" {
		current := filepath.Base(session.DirOf(a.file))
		if current != msg.row.Cell {
			returned := a.machineRead.sendHolds[current]
			returned.blocked = true
			a.machineRead.sendHolds[current] = returned
		}
	}
	switch {
	case errors.Is(msg.err, directory.ErrLeaseHeld):
		return lostRaceWord
	case errors.Is(msg.err, directory.ErrUnreachable):
		return chatlist.Unreachable
	case errors.Is(msg.err, directory.ErrConditionalTake):
		return directory.ErrConditionalTake.Error()
	case errors.Is(msg.err, directory.ErrRevoked):
		a.removedComputer()
		return chatlist.Removed
	case strings.Contains(msg.err.Error(), "rewind cancelled"), strings.Contains(msg.err.Error(), "changed during rewind"):
		return continueFails + " because the folder changed during the move; your words are still in the box"
	default:
		return continueFails + " because the chat could not be put in this folder; your words are still in the box. Try continue here again."
	}
}
