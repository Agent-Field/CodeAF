package tui3

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

const taskComputerHint = "where a task goes when you do not name a computer"

const taskRunHereKey = "h"
const taskRunHereVerb = taskRunHereKey + " run it here"
const taskRunHereSuffix = " — run it here"
const taskComputerCycleKey = "ctrl+d"
const taskComputerCycleHint = taskComputerCycleKey + " computer"

type taskRunHereAgent interface {
	RunTaskHere(context.Context, uint64) (string, error)
}

// A connection has the Go methods even when its engine predates them. Its
// welcome must explicitly allow these controls before a window draws them.
func (a *app) taskComputersAvailable() bool {
	if a.taskComputers == nil {
		return false
	}
	if connected, ok := a.agent.(interface{ TaskComputersSupported() bool }); ok {
		return connected.TaskComputersSupported()
	}
	return true
}

// These are the same applicability facts RunTaskHere checks. The owning live
// notice supplies them; an interrupted local row supplies none of them.
func (a *app) taskCanRunHere(entry session.TaskIndexEntry) bool {
	if !a.taskComputersAvailable() || a.taskSheet.awayOwner.on || a.taskHerePending == tasksKeyOf(entry) {
		return false
	}
	if _, ok := a.agent.(taskRunHereAgent); !ok {
		return false
	}
	node := a.taskSheetNodeFor(&entry)
	if node == nil || node.remote == nil || node.remote.Abandoned {
		return false
	}
	r := node.remote
	return r.StartFailed || r.Unreachable && !r.Finished || node.state == session.TaskInterrupted
}

func (a *app) runTaskHere(entry session.TaskIndexEntry) tea.Cmd {
	if !a.taskCanRunHere(entry) {
		return nil
	}
	node := a.taskSheetNodeFor(&entry)
	door := a.agent.(taskRunHereAgent)
	ctx, id, key, file := a.ctx, node.id, tasksKeyOf(entry), a.file
	a.taskHerePending = key
	a.touch()
	return a.offLoop(func() func(bool) tea.Cmd {
		var words string
		var err error
		if bound, ok := door.(interface {
			RunTaskHereIn(context.Context, uint64, string) (string, error)
		}); ok {
			words, err = bound.RunTaskHereIn(ctx, id, file)
		} else {
			words, err = door.RunTaskHere(ctx, id)
		}
		return func(here bool) tea.Cmd {
			if a.taskHerePending == key {
				a.taskHerePending = tasksKey{}
			}
			if here {
				if err != nil {
					a.note(err.Error())
				} else if words != "" {
					a.note(words)
				}
				a.touch()
			}
			return nil
		}
	})
}

// The roster is read on entry, never while drawing. A failed refresh retains
// the last names rather than claiming a paired computer was removed.
func (a *app) readTaskComputers() tea.Cmd {
	if !a.taskComputersAvailable() {
		return nil
	}
	computers, ctx := a.taskComputers, a.ctx
	return a.besideLine(func() func(bool) tea.Cmd {
		rows, err := computers.List(ctx)
		localName := computers.ThisComputer()
		return func(here bool) tea.Cmd {
			if !here || err != nil {
				return nil
			}
			a.taskComputerRows = nil
			a.taskComputerHere = localName
			for _, row := range rows {
				if row.CanTakeTasks {
					a.taskComputerRows = append(a.taskComputerRows, row)
				}
			}
			if a.sheet.registry != nil {
				a.sheet.taskComputerRows = a.taskComputerRows
				a.sheet.build()
			}
			if a.task != nil {
				a.markCardStale(a.task)
			}
			a.touch()
			return nil
		}
	})
}

func (a *app) proposalComputerName(card *taskCard) string {
	if !a.taskComputersAvailable() {
		return ""
	}
	named := strings.TrimSpace(card.computer)
	if named == "" {
		return taskComputerName(a.taskComputerRows, card.computerPreference)
	}
	if strings.EqualFold(named, "here") || strings.EqualFold(named, config.TaskRunsHere) || strings.EqualFold(named, a.taskComputerHere) {
		return config.TaskRunsHere
	}
	for _, computer := range a.taskComputerRows {
		if strings.EqualFold(named, computer.Name) {
			return computer.Name
		}
	}
	return named
}

func (a *app) canCycleTaskComputer(card *taskCard) bool {
	return card != nil && !card.forming && !card.settled() && a.taskComputersAvailable() && len(a.taskComputerRows) > 0
}

// A pick belongs to this proposal, not to the standing preference. The answer
// carries it only when the person later accepts or corrects the proposal.
func (a *app) cycleTaskComputer() {
	card := a.task
	if !a.canCycleTaskComputer(card) {
		return
	}
	choices := []string{config.TaskRunsHere}
	for _, computer := range a.taskComputerRows {
		choices = append(choices, computer.Name)
	}
	current, at := a.proposalComputerName(card), -1
	for i, choice := range choices {
		if strings.EqualFold(choice, current) {
			at = i
			break
		}
	}
	a.holdTask(card.id)
	card.computer, card.computerPicked = choices[(at+1)%len(choices)], true
	a.markCardStale(card)
	a.touch()
}

func taskComputerName(rows []session.TaskComputer, device string) string {
	for _, row := range rows {
		if row.Device == device {
			return row.Name
		}
	}
	return config.TaskRunsHere
}

func (s *sheet) taskComputerSetting(row config.Setting) config.Setting {
	if row.Key == config.KeyTaskRunsOn {
		row.Choices = []string{""}
		row.Hint = taskComputerHint
		for _, computer := range s.taskComputerRows {
			row.Choices = append(row.Choices, computer.Device)
		}
	}
	return row
}

func (s *sheet) settingValueWord(row config.Setting) string {
	if row.Key == config.KeyTaskRunsOn {
		return taskComputerName(s.taskComputerRows, config.TaskRunsOnAt(s.profileDir))
	}
	return settingValueWord(row)
}

// Placement is a separate fact from the shared state reading. A checkpoint
// saying running alone never supplies a computer's identity.
func taskComputerWords(name string, unreachable, ran bool, state session.TaskState) string {
	if name == "" {
		return ""
	}
	if unreachable {
		return session.TaskComputerStoppedAnswering(name)
	}
	if state == session.TaskInterrupted {
		return "interrupted on " + name
	}
	if ran {
		return session.TaskRanOn(name)
	}
	if state == session.TaskRunning || state == session.TaskQueued {
		return session.TaskRunningOn(name)
	}
	return ""
}

func (a *app) taskComputerWords(node *taskNode) string {
	if !a.taskComputersAvailable() || node == nil {
		return ""
	}
	if node.remote != nil && node.remote.Abandoned {
		return ""
	}
	if node.remote != nil && node.remote.StartFailed && !node.remote.Abandoned {
		return strings.TrimSuffix(firstNonEmpty(node.doing, node.report), taskRunHereSuffix)
	}
	return taskComputerWords(node.computer, node.unreachable, node.ranElsewhere, node.state)
}

// The session carries placement on the phase lane too. It remains a placement fact,
// rather than becoming a second state word or being drawn twice in the room.
func taskDoingWords(node *taskNode) string {
	if node.computer != "" {
		switch node.doing {
		case session.TaskRunningOn(node.computer), session.TaskRanOn(node.computer), session.TaskComputerStoppedAnswering(node.computer):
			return ""
		}
		if node.remote != nil && node.remote.StartFailed {
			return ""
		}
	}
	return node.doing
}

func (a *app) taskEntryComputerWords(entry session.TaskIndexEntry) string {
	if !a.taskComputersAvailable() {
		return ""
	}
	if node := a.taskSheetNodeFor(&entry); node != nil {
		return a.taskComputerWords(node)
	}
	return taskComputerWords(entry.Computer, entry.Unreachable, entry.RanElsewhere, session.TaskState(entry.Status))
}
