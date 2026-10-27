package tui3

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── CHATS ON OTHER MACHINES, IN THE SESSIONS PANEL ──────────────────────────
//
// A chat somebody left running on another computer is a row like any other, in
// the place they look for their chats. Its words come whole from [chatlist]
// (`running on desktop`, `desktop offline`, `2 turns from desktop: merge / discard`),
// so this screen and `codeaf cell list --all` never spell them twice.
//
// THE EMPTINESS LAW DECIDES WHAT IS SAID. A chat this machine holds says
// nothing about where it is, and neither does a released one — the status is a
// dim clause only where it tells a person something they could not see.
//
// THE NOTE OUTRANKS THE TITLE'S TAIL. The status is the row's `note` and is the
// point of the row, so a narrow frame cuts the chat's title with an ellipsis
// before it gives the sentence up ([homeCell.giveWay]); only below a title of
// [homeCellTitleFloor] cells does the sentence go whole.

// machineReading is the last listing the other machines gave, and whether the
// last ask failed. A failed ask keeps the rows and explains whether the reading
// reached its deadline or the connection could not be made.
type machineReading struct {
	err             error
	sendHolds       map[string]sendHold
	computerRemoved bool
	rows            []chatlist.Row
	// at is when the listing was read. Each row's age is counted from it, so a
	// row keeps ageing on the clock between two asks instead of standing still.
	at   time.Time
	down bool
	// version is the directory version the listing was read at (0 when the
	// source keeps none), which is what a change frame is compared with.
	version uint64
	// merge says a branch row can be merged from here, which decides the
	// sentence it says (chatlist.BranchLine).
	merge    bool
	separate bool
	// away says the device a row names is not online now; nil says none is
	// known to be away. It is set when home is rebuilt, so presence is as new as
	// the feed.
	away func(chatlist.Row) bool
}

// homeMachinesMsg is one listing, coming BACK from the source.
type homeMachinesMsg struct {
	rows      []chatlist.Row
	version   uint64
	err       error
	holdCell  string
	holdFence uint64
}

// machinesAskTimeout bounds one ask without treating silence as a network failure.
const machinesAskTimeout = 5 * time.Second

// askMachines lists the other machines, off the update loop for the reason a
// repository's status is asked (homeband_repo.go): a keystroke may not wait on
// a network. One ask is in flight at a time.
func (a *app) askMachines() tea.Cmd {
	return tea.Batch(a.askChats(), a.askFleet(), a.askRoster())
}

// askChats is the listing half of [app.askMachines].
func (a *app) askChats() tea.Cmd {
	if a.machines == nil || a.machinesAsking {
		return nil
	}
	a.machinesAsking = true
	src, file := a.machines, a.file
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), machinesAskTimeout)
		defer cancel()
		rows, version, err := chatlist.RowsAt(ctx, src)
		msg := homeMachinesMsg{rows: rows, version: version, err: err}
		if local, ok := src.(sendLeaseSource); ok && file != "" {
			msg.holdCell = filepath.Base(session.DirOf(file))
			_, msg.holdFence, _ = local.SendLease(msg.holdCell)
		}
		return msg
	}
}

// tookMachines files the answer. An error keeps the last rows and only marks
// them stale.
func (a *app) tookMachines(msg homeMachinesMsg) tea.Cmd {
	a.machinesAsking = false
	if a.machinesRetaken {
		a.machinesRetaken = false
		return a.askMachines()
	}
	return a.recordMachines(msg)
}

// recordMachines also accepts a send's independent read without ending an
// outstanding listing request that still owes its own answer.
func (a *app) recordMachines(msg homeMachinesMsg) tea.Cmd {
	if errors.Is(msg.err, directory.ErrRevoked) {
		a.removedComputer()
	}
	if msg.err == nil && a.machineRead.computerRemoved {
		if source, ok := a.machines.(interface{ Removed() bool }); ok && !source.Removed() {
			a.machineRead.computerRemoved = false
			a.homeAccountNotice = ""
		}
	}
	if msg.err == nil {
		a.rememberSendHolds(msg)
	}
	before, wasElsewhere := a.heldElsewhere()
	a.fileMachines(msg)
	a.machineRead.err = msg.err
	if row, elsewhere := a.heldElsewhere(); msg.err == nil && elsewhere && (!wasElsewhere || before.DeviceID != row.DeviceID || before.Device != row.Device) {
		a.note(chatlist.Superseded(nonemptyDevice(row.Device)))
	}
	a.machineRead.down = msg.err != nil && !a.machineRead.computerRemoved
	a.machineRead.merge = a.branches.Merge != nil
	a.machineRead.separate = a.branches.Open != nil
	a.considerResume()
	a.rebuildMachines()
	var owed tea.Cmd
	if !a.machinesAsking {
		owed = a.readOwedByFrames(msg.err == nil)
	}
	return tea.Batch(owed, a.finishBring(), a.continueAfterQuit(msg.err))
}

// into merges the other machines' chats into the panel's own lines, newest
// first, cuts to the panel's cap, and puts the unreachable sentence at the foot.
func (m machineReading) into(in *homeGridInput, own []homeLine) homePanelRows {
	lines := append(own, m.lines(in, own)...)
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].stamp().After(lines[j].stamp()) })
	rows := homePanelCut(in, panelSessions, lines)
	if m.down {
		rows.lines = append(rows.lines, homeLine{kind: homeMachineRow, cell: &homeCell{
			kind: cellWhisper, panel: panelSessions, title: m.unavailableWord()}})
	}
	return rows
}

// unavailableWord preserves the distinction between a deadline and a failed connection.
func (m machineReading) unavailableWord() string {
	var timeout interface{ Timeout() bool }
	if errors.Is(m.err, context.DeadlineExceeded) || errors.As(m.err, &timeout) && timeout.Timeout() {
		return chatlist.ReadTimedOut
	}
	return chatlist.Unreachable
}

// lines are the rows to draw: none of the kind that only this machine can hold,
// and none for a chat this machine already lists unless another machine holds it
// live. That one is drawn WITH its door (`running on desktop`, enter takes it),
// because the local row of a chat taken elsewhere is a stale window, and the way
// back to it is the same take the cold path runs.
func (m machineReading) lines(in *homeGridInput, own []homeLine) []homeLine {
	local := map[string]bool{}
	for _, line := range own {
		local[line.row.ID] = true
	}
	var out []homeLine
	for _, row := range m.rows {
		if m.away != nil && m.away(row) {
			row = row.Quiet()
		}
		if drawnAsRemote(row, local[row.Cell]) {
			out = append(out, machineLine(row, m.at, in.now, m.merge, m.separate))
		}
	}
	return out
}

// drawnAsRemote says a row from the directory is a row of the panel. A chat
// held here is never drawn twice; a chat listed here is drawn again only while
// another machine holds it, live or gone quiet.
func drawnAsRemote(row chatlist.Row, listedHere bool) bool {
	if row.Status == chatlist.Here {
		return false
	}
	return !listedHere || heldElsewhere(row)
}

// heldElsewhere says the chat's last holder was another machine and has not
// let go: its lease is live, or lapsed with nobody to release it. The local
// copy of such a chat is a stale window, whichever of the two it is, and
// whichever of the local list and the directory reading arrived first.
func heldElsewhere(row chatlist.Row) bool {
	return row.Status == chatlist.Running || (row.Status == chatlist.Off && !row.Mine)
}

// machineLine is one chat on another machine as a row of the sessions panel.
// The turn's time is fixed once, from the moment of the read; the age drawn is
// then counted to now, so it grows with the clock however long ago the ask was.
func machineLine(row chatlist.Row, readAt, now time.Time, merge bool, separate ...bool) homeLine {
	// THE DRAW CLOCK IS THE WORLD'S READING AND THE READ TIME IS THE ASK'S, two
	// samples of one wall clock a few milliseconds apart. A draw that seems to
	// come before the read would make a turn look younger than the directory said
	// it was (`4h` for a chat that is exactly five hours old), so it never counts
	// as earlier than the read.
	if now.Before(readAt) {
		now = readAt
	}
	at := readAt.Add(-row.DurableAgo)
	note, short := chatlist.StatusLine(row), ""
	if row.Status == chatlist.Branch {
		// A branch's sentence names what can be done with it and has a narrow
		// spelling for frames that cannot hold the whole.
		note, short = chatlist.BranchLine(row, merge, separate...), chatlist.BranchShort(row)
	}
	return homeLine{kind: homeMachineRow, since: at, remote: &row, cell: &homeCell{
		kind: cellRow, panel: panelSessions, title: row.Title,
		note: note, noteShort: short, keepNote: true, right: sinceAt(at, now),
		key: "machine:" + row.Cell,
	}}
}

// stamp is when a line's chat last moved, for ordering the merged panel.
func (l homeLine) stamp() time.Time {
	if l.kind == homeMachineRow {
		return l.since
	}
	return l.row.At
}

// machineRowTexts paints a chat on another machine at a compact width through
// the same cell painter the wide grid uses, so a row is the same row at every
// tier and the cell's own order of giving way keeps the name whole.
func (a *app) machineRowTexts(line homeLine, at, width int, pal palette) []string {
	var texts []string
	for _, drawn := range a.homeLineRows(line, at, width, pal, false, false) {
		texts = append(texts, drawn.text)
	}
	return texts
}
