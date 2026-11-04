package session

import (
	"context"
	"errors"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"time"
)

// automaticFactoryRefusal is separate from chat budget: a person may still
// ask a stopped job's manager questions without granting execution authority.
func (a *Agent) automaticFactoryRefusal() string {
	if a.config.FactoryWorkRefusal != nil {
		return a.config.FactoryWorkRefusal()
	}
	if door, ok := a.config.FactoryRun.(interface{ AutomaticRefusal(string) string }); ok {
		return door.AutomaticRefusal(a.config.SessionFile)
	}
	return ""
}

// factoryTurnRefusal excludes every turn owed to the person, including steering.
func (a *Agent) factoryTurnRefusal(user userMessage) string {
	if user.factoryWork || user.wake && !user.answersPerson && !user.answer {
		return a.automaticFactoryRefusal()
	}
	return ""
}

func (a *Agent) finishFactoryStop(hub *eventHub, why string, usage Usage, started time.Time, model string) {
	a.record(ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: why}}})
	hub.send(Event{Kind: EventTextDelta, Text: why})
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(usage, started, model)})
}

// refuseFactoryTools records the paired results while executing none of a late batch.
func (a *Agent) refuseFactoryTools(hub *eventHub, calls []ai.ToolCall, why string) {
	for _, call := range calls {
		a.recordToolResult(ai.Message{Role: "tool", ToolCallID: call.ID, Content: []ai.ContentPart{{Type: "text", Text: why}}}, toolResult{text: why, isError: true, harness: true})
		hub.send(Event{Kind: EventToolFailed, Tool: call.Function.Name, CallID: call.ID, Output: why, HarnessMade: true})
	}
}

// The marker follows only automatic work through retries and auxiliary calls.
type factoryAutomaticTurnKey struct{}

func factoryCallRefusal(ctx context.Context) error {
	if refusal, _ := ctx.Value(factoryAutomaticTurnKey{}).(func() string); refusal != nil {
		if why := refusal(); why != "" {
			return errors.New(why)
		}
	}
	return nil
}
