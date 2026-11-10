package tui3

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

const homeLocalOpenTimeout = chatlist.OpenReadTimeout

type homeLocalOpenMsg struct {
	line       homeLine
	fromFile   string
	generation uint64
	reading    homeMachinesMsg
}

// homeLocalReadNeeded limits restart recovery to ambiguous display readings.
// Reusing an open window still lets a displayed free row refresh a stale viewer.
func (a *app) homeLocalReadNeeded(line homeLine) bool {
	if a.teamsPaired != nil && !a.teamsPaired() || a.addMachine.known && a.addMachine.size < fleetEnough {
		return false
	}
	if line.row.Transcript == a.file || a.holding(line.row.Transcript) {
		return false
	}
	id := homeSessionID(line.row)
	for _, row := range a.machineRead.rows {
		if row.Cell == id {
			return !row.AutoOpen && row.Mine && row.Status == chatlist.Here
		}
	}
	return true
}

// homeResolveLocal checks a potentially stale local copy without making the
// sync service a requirement for opening a chat saved on this computer.
func (a *app) homeResolveLocal(line homeLine, source chatlist.SendSource) tea.Cmd {
	msg := homeLocalOpenMsg{line: line, fromFile: a.file, generation: a.continueGeneration}
	base := a.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(base, homeLocalOpenTimeout)
		defer cancel()
		rows, version, err := source.RowsForSend(ctx)
		msg.reading = homeMachinesMsg{rows: rows, version: version, err: err, readWithin: homeLocalOpenTimeout}
		return msg
	}
}

func (a *app) homeLocalOpenReady(msg homeLocalOpenMsg) tea.Cmd {
	if !a.at(pageHome) || msg.generation != a.continueGeneration || msg.fromFile != a.file || a.hosted() || a.machineRead.computerRemoved {
		return nil
	}
	if msg.reading.err != nil {
		return a.homeOpenLocalLine(msg.line)
	}
	recorded := a.recordMachines(msg.reading)
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
