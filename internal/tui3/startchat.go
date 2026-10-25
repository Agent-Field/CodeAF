package tui3

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// startRequestedChat keeps the requesting conversation in front. The new model
// and approvals are applied off the UI loop before the first message is sent.
func (a *app) startRequestedChat(request *session.ChatStart) tea.Cmd {
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
	return a.besideLine(func() func(bool) tea.Cmd {
		conv, err := start(request.Workspace)
		var events <-chan session.Event
		if err == nil && conv.Agent == nil {
			err = fmt.Errorf("the new conversation has no agent")
		}
		if err == nil {
			conv.Agent.SetModel(request.Model)
			if request.Approval != "" {
				if setter, ok := conv.Agent.(interface{ SetApprovalPosture(string) error }); ok {
					err = setter.SetApprovalPosture(request.Approval)
				} else {
					err = fmt.Errorf("the new conversation cannot inherit approvals")
				}
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
