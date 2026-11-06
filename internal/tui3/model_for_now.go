package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// modelChange belongs to the conversation that queued it. Its acknowledgment
// may arrive after a later pick or after the surface has switched chats.
type modelChange struct {
	front int
	retry func()
}

// configuredModel keeps reconciliation anchored to the person's saved choice.
// Agents predating the run-only door keep the existing live-model behavior.
func (a *app) configuredModel() string {
	if door, ok := a.agent.(interface{ ConfiguredModel() string }); ok {
		return door.ConfiguredModel()
	}
	if a.model != "" {
		return a.model
	}
	return a.agent.Model()
}

func (a *app) useModelForNow(id string, window int) {
	a.queueModelChange(id, window, true, true, nil)
}

func (a *app) modelChangePending() bool {
	return a.modelChange != nil && a.modelChange.front == a.frontGen
}

func (a *app) takeModelCommands() tea.Cmd {
	cmds := a.modelCommands
	a.modelCommands = nil
	return tea.Batch(cmds...)
}

// Temporary picks and any explicit picks behind them share the ordered door
// line. The surface only adopts the last acknowledged selection, and sends
// cannot pass its gate while that selection is pending.
func (a *app) queueModelChange(id string, window int, temporary, quiet bool, rename *modelRename) {
	agent := a.agent
	save := a.saveModel
	remember := ""
	if !quiet {
		remember = strings.TrimSpace(id)
	}
	if rename != nil {
		remember = rename.save
	}
	change := &modelChange{front: a.frontGen}
	a.modelChange = change
	cmd := a.offLoop(func() func(bool) tea.Cmd {
		var err error
		if rename != nil {
			if door, ok := agent.(interface {
				SetConversationModels(session.ConversationModels) ([]session.ModelSwitch, error)
			}); rename.pair != nil && ok {
				_, err = door.SetConversationModels(*rename.pair)
			} else if rename.save != "" {
				agent.SetModel(rename.save)
			}
		}
		if err == nil {
			if door, ok := agent.(interface{ UseModelForNow(string) error }); temporary && ok {
				err = door.UseModelForNow(id)
			} else if rename == nil {
				agent.SetModel(id)
			}
		}
		// Durable choices keep their order even if the window leaves before
		// the reply. Only display work depends on still being in front.
		if err == nil && save != nil && remember != "" {
			_ = save(remember)
		}
		return func(here bool) tea.Cmd {
			if a.modelChange != change {
				return nil
			}
			if !here {
				a.modelChange = nil
				return nil
			}
			if err != nil {
				change.retry = func() { a.queueModelChange(id, window, temporary, quiet, rename) }
				a.note("could not change model · " + err.Error())
				return nil
			}
			a.modelChange = nil
			wasSwitching := a.creditSwitching
			a.creditSwitching = quiet
			a.modelSwitched(id, window, false)
			a.creditSwitching = wasSwitching
			a.ensureAvailableModel()
			if a.connPanel.open {
				a.connPanel.adopt(a.connectionRows())
			}
			if a.at(pageSettings) {
				a.sheet.build()
			}
			a.hudStale = true
			a.touch()
			return a.takeModelCommands()
		}
	})
	a.modelCommands = append(a.modelCommands, cmd)
}
