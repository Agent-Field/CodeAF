package session

import (
	"context"
	"encoding/json"
	"time"

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

// A small set of unambiguous status-only questions is answered directly. Other
// wording, and anything asking for action, keeps the ordinary conversation.
func (a *Agent) answerFactoryStatus(ctx context.Context, hub *eventHub, user userMessage) bool {
	if user.factoryStatus == "" {
		return false
	}
	text := user.factoryStatus
	a.record(textMessage("assistant", text))
	hub.send(Event{Kind: EventTextDelta, Text: text})
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(Usage{Turns: 1}, time.Now(), a.Model())})
	return true
}
