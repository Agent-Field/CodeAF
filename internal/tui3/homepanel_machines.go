package tui3

import (
	"context"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// ── CHATS ON OTHER MACHINES, IN THE SESSIONS PANEL ──────────────────────────
//
// A chat somebody left running on another computer is a row like any other, in
// the place they look for their chats. Its words come whole from [chatlist]
// (`running on studio`, `studio off`, `2 turns from studio: merge / discard`),
// so this screen and `codeaf cell list --all` never spell them twice.
//
// THE EMPTINESS LAW DECIDES WHAT IS SAID. A chat this machine holds says
// nothing about where it is, and neither does a released one — the status is a
// dim clause only where it tells a person something they could not see.
//
// THE NAME IS WHOLE. The status is the row's `note`, which the cell gives up
// entire before it cuts a title, so a narrow frame draws the chat's name and its
// age and loses the sentence, never half of either.

// machineReading is the last listing the other machines gave, and whether the
// last ask failed. A failed ask keeps the rows: the list stays on screen, dim,
// under `other machines unreachable`.
type machineReading struct {
	rows []chatlist.Row
	down bool
}

// homeMachinesMsg is one listing, coming BACK from the source.
type homeMachinesMsg struct {
	rows []chatlist.Row
	err  error
}

// machinesAskTimeout bounds one ask, so a silent relay is unreachable rather
// than an ask that never returns.
const machinesAskTimeout = 5 * time.Second

// askMachines lists the other machines, off the update loop for the reason a
// repository's status is asked (homeband_repo.go): a keystroke may not wait on
// a network. One ask is in flight at a time.
func (a *app) askMachines() tea.Cmd {
	if a.machines == nil || a.machinesAsking {
		return nil
	}
	a.machinesAsking = true
	src := a.machines
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), machinesAskTimeout)
		defer cancel()
		rows, err := src.Rows(ctx)
		return homeMachinesMsg{rows: rows, err: err}
	}
}

// tookMachines files the answer. An error keeps the last rows and only marks
// them stale.
func (a *app) tookMachines(msg homeMachinesMsg) {
	a.machinesAsking = false
	if msg.err != nil {
		a.machineRead.down = true
	} else {
		a.machineRead = machineReading{rows: msg.rows}
	}
	a.home.others = a.machineRead
	if a.home.gridOn() {
		a.home.build()
	}
	a.touch()
}

// into merges the other machines' chats into the panel's own lines, newest
// first, cuts to the panel's cap, and puts the unreachable sentence at the foot.
func (m machineReading) into(in *homeGridInput, own []homeLine) homePanelRows {
	lines := append(own, m.lines(in, own)...)
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].stamp().After(lines[j].stamp()) })
	rows := homePanelCut(in, panelSessions, lines)
	if m.down {
		rows.lines = append(rows.lines, homeLine{kind: homeMachineRow, cell: &homeCell{
			kind: cellWhisper, panel: panelSessions, title: chatlist.Unreachable}})
	}
	return rows
}

// lines are the rows to draw: none for a chat this machine already lists, and
// none of the kind that only this machine can hold.
func (m machineReading) lines(in *homeGridInput, own []homeLine) []homeLine {
	local := map[string]bool{}
	for _, line := range own {
		local[line.row.ID] = true
	}
	var out []homeLine
	for _, row := range m.rows {
		if row.Status != chatlist.Here && !local[row.Cell] {
			out = append(out, machineLine(row, in.now))
		}
	}
	return out
}

// machineLine is one chat on another machine as a row of the sessions panel.
func machineLine(row chatlist.Row, now time.Time) homeLine {
	at := now.Add(-row.DurableAgo)
	return homeLine{kind: homeMachineRow, since: at, cell: &homeCell{
		kind: cellRow, panel: panelSessions, title: row.Title,
		note: chatlist.StatusLine(row), right: sinceAt(at, now),
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
