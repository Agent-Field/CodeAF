package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// ManagerStatusDoor exposes current saved facts without asking a model to
// investigate its own old narration. The status path cannot mutate the run.
type ManagerStatusDoor interface {
	ManagerStatus(context.Context, string) (factory.ManagerStatus, error)
	AnswerPending(context.Context, string, string, string) (string, error)
}

func (a *Agent) factoryStatusTool() bare.Tool {
	return bare.Tool{Name: "factory_status", Description: "Read authoritative saved status of the ONE job this conversation manages: completed steps, current blocker, live worker, last progress and supported next action. Read this first for progress questions and before answering a persisted decision. No model, workspace investigation or repair is started.", Schema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`), Execute: func(ctx context.Context, _ json.RawMessage) (string, bool, error) {
		d, ok := a.runDoorForTurn().(ManagerStatusDoor)
		if !ok {
			return "No saved manager status is available on this turn.", true, nil
		}
		status, err := d.ManagerStatus(ctx, a.config.SessionFile)
		if err != nil {
			return err.Error(), true, nil
		}
		return status.Text, false, nil
	}}
}

// ReadFactoryStatus is a pure side read, even during a provider call or while
// an old follow-up remains queued. It never enters the turn lifecycle.
func (a *Agent) ReadFactoryStatus(ctx context.Context, text string) (string, bool, error) {
	if a.config.Stage != nil || !factory.StatusOnlyRequest(text) {
		return "", false, nil
	}
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return "", true, errors.New("session: agent is closed")
	}
	door, ok := a.config.FactoryRun.(ManagerStatusDoor)
	if !ok {
		return "", false, nil
	}
	status, err := door.ManagerStatus(ctx, a.config.SessionFile)
	if errors.Is(err, factory.ErrNotManager) {
		return "", false, nil
	}
	if err != nil {
		return "", true, err
	}
	return strings.Split(status.Text, "\nPending question ID:")[0], true, nil
}

func factoryStatusStream(text string) <-chan Event {
	events := make(chan Event, 1)
	// No turn.done: this reading must not finish the foreground turn in any UI.
	events <- Event{Kind: EventNotice, Text: text, Unrecorded: true}
	close(events)
	return events
}

func statusOnlyPresentation(p *messagePresentation, text string) bool {
	return p == nil || (len(p.Reads) == 0 && len(p.Attachments) == 0 && p.Composed == nil && (p.Text == nil || strings.TrimSpace(*p.Text) == strings.TrimSpace(text)))
}
