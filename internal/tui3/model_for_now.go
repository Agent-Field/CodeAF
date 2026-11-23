package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

const modelChangeRefusedWord = "could not change model · "

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

func (a *app) takeModelCommands() tea.Cmd {
	cmds := a.modelCommands
	a.modelCommands = nil
	return tea.Batch(cmds...)
}

// Temporary picks and explicit picks behind them share the ordered door line.
// The display changes immediately; send commands wait for the agent's token,
// which completes with the ask rather than its later UI acknowledgment.
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
	if window <= 0 {
		window = a.windowFor(id)
	}
	selected := &modelSelection{done: make(chan struct{}), model: id, window: window, temporary: temporary && rename == nil}
	modelSelections.Store(agent, selected)
	a.pendingModelSelection = selected.done
	cmd := a.offLoop(func() func(bool) tea.Cmd {
		defer func() {
			modelSelections.CompareAndDelete(agent, selected)
			close(selected.done)
		}()
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
		// Durable choices keep their order even after leaving the conversation.
		if err == nil && save != nil && remember != "" {
			_ = save(remember)
		}
		return func(here bool) tea.Cmd {
			if err != nil && here && a.agent == agent {
				a.note(modelChangeRefusedWord + err.Error())
			}
			return nil
		}
	})
	a.modelCommands = append(a.modelCommands, cmd)
	wasSwitching := a.creditSwitching
	a.creditSwitching = quiet
	a.displayModelSwitched(id, window, false)
	a.creditSwitching = wasSwitching
	if a.connPanel.open {
		a.connPanel.adopt(a.connectionRows())
	}
	if a.at(pageSettings) {
		a.sheet.build()
	}
	a.hudStale = true
	a.touch()
}
