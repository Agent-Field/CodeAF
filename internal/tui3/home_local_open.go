package tui3

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

type homeLocalOpenMsg struct {
	line       homeLine
	fromFile   string
	generation uint64
	reading    homeMachinesMsg
}

// homeResolveLocal uses the ownership door because Home's display can still
// contain this computer's old hold while its directory follower catches up.
func (a *app) homeResolveLocal(line homeLine, source chatlist.SendSource) tea.Cmd {
	msg := homeLocalOpenMsg{line: line, fromFile: a.file, generation: a.continueGeneration}
	base := a.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(base, machinesAskTimeout)
		defer cancel()
		rows, version, err := source.RowsForSend(ctx)
		msg.reading = homeMachinesMsg{rows: rows, version: version, err: err, readWithin: machinesAskTimeout}
		return msg
	}
}

func (a *app) homeLocalOpenReady(msg homeLocalOpenMsg) tea.Cmd {
	if !a.at(pageHome) || msg.generation != a.continueGeneration || msg.fromFile != a.file || a.hosted() || a.machineRead.computerRemoved {
		return nil
	}
	recorded := a.recordMachines(msg.reading)
	if msg.reading.err != nil {
		a.home.say(a.machineRead.unavailableWord(), "")
		return recorded
	}
	id := homeSessionID(msg.line.row)
	for _, row := range msg.reading.rows {
		if row.Cell != id {
			continue
		}
		if row.AutoOpen || heldElsewhere(row) && msg.line.row.Transcript != a.file && !a.holding(msg.line.row.Transcript) {
			return tea.Batch(recorded, a.homeMachineEnter(homeLine{remote: &row}))
		}
		break
	}
	return tea.Batch(recorded, a.homeOpenLocalLine(msg.line))
}
