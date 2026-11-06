package tui3

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// startRequestedChat keeps the requesting conversation in front. The new model
// and approvals are applied off the UI loop before the first message is sent.
func (a *app) startRequestedChat(request *session.ChatStart, owner Agent) tea.Cmd {
	if request == nil || request.ID == "" {
		return nil
	}
	if a.chatStarts == nil {
		a.chatStarts = map[string]bool{}
	}
	if a.chatStarts[request.ID] {
		return nil
	}
	a.chatStarts[request.ID] = true
	if a.shared || a.start == nil || !a.canStart() {
		a.note("could not start chat · " + newUnavailableWord)
		return nil
	}
	start := a.start
	listed := a.requireListedModel
	var models []Model
	if listed {
		models = a.modelList()
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		claimer, ok := owner.(interface{ ClaimChatStart(string) (bool, error) })
		if !ok {
			return func(bool) tea.Cmd { a.note("could not start chat · launch ownership unavailable"); return nil }
		}
		claimed, claimErr := claimer.ClaimChatStart(request.ID)
		if claimErr != nil {
			return func(bool) tea.Cmd { a.note("could not start chat · " + claimErr.Error()); return nil }
		}
		if !claimed {
			return func(bool) tea.Cmd { return nil }
		}
		conv, err := start(request.Workspace)
		var events <-chan session.Event
		if err == nil && conv.Agent == nil {
			err = fmt.Errorf("the new conversation has no agent")
		}
		if err == nil {
			// The request carries the parent's saved choice. Availability is
			// reconciled for this child before its first request, without saving
			// a temporary replacement inherited from the parent.
			conv.Agent.SetModel(request.Model)
			if request.Approval != "" {
				if setter, ok := conv.Agent.(interface{ SetApprovalPosture(string) error }); ok {
					err = setter.SetApprovalPosture(request.Approval)
				} else {
					err = fmt.Errorf("the new conversation cannot inherit approvals")
				}
			}
		}
		if err == nil && listed {
			model, available := availableConversationModel(request.Model, models)
			if !available {
				err = fmt.Errorf("%s", noAvailableModelWord)
			} else if model.ID != request.Model {
				if door, ok := conv.Agent.(interface{ UseModelForNow(string) error }); ok {
					err = door.UseModelForNow(model.ID)
				} else {
					conv.Agent.SetModel(model.ID)
				}
			}
			if err == nil && model.ContextLength > 0 {
				conv.Agent.SetContextWindow(model.ContextLength)
			}
		}
		if err == nil {
			events, err = conv.Agent.Submit(context.Background(), request.Brief)
		}
		if err != nil && conv.Agent != nil {
			_ = conv.Agent.Close()
		}
		return func(bool) tea.Cmd {
			if err != nil {
				a.note("could not start chat · " + err.Error())
				return nil
			}
			cmd := a.stow(conv, nil)
			key := a.convKey(conv.SessionFile)
			if held := a.behind[key]; held != nil {
				held.watch.adopt(events, nil)
			}
			a.trafficBehindTop(key)
			a.chatTabBar = tabBar{}
			a.note("started chat · " + request.Model)
			return cmd
		}
	})
}
