package remote

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

const methodReplySuggestion = "conversation.suggest_reply"

type replySuggestionDoor interface {
	SuggestReply(context.Context, string) (string, error)
}
type replySuggestionArgs struct {
	Context string        `json:"context"`
	Budget  time.Duration `json:"budget"`
}

func replySuggestionKnown(agent any) bool { _, ok := agent.(replySuggestionDoor); return ok }

func replySuggestionCall(agent WrappedAgent, call Frame) (json.RawMessage, bool, error) {
	if call.Method != methodReplySuggestion {
		return nil, false, nil
	}
	door, ok := agent.(replySuggestionDoor)
	if !ok {
		return nil, true, errors.New("reply suggestions unavailable")
	}
	p, err := arg[replySuggestionArgs](call)
	if err != nil {
		return nil, true, err
	}
	if p.Budget < 0 || len(p.Context) == 0 || len(p.Context) > session.ReplySuggestionContextBytes {
		return nil, true, errors.New("invalid reply context")
	}
	wait := p.Budget
	if wait == 0 || wait > session.ReplySuggestionWait {
		wait = session.ReplySuggestionWait
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	text, err := door.SuggestReply(ctx, p.Context)
	if err != nil {
		return nil, true, err
	}
	data, err := json.Marshal(text)
	return data, true, err
}

// Hosted conversations run the optional errand on their owning engine, where
// their original budget and provider credentials remain authoritative.
func (a *Agent) SuggestReply(ctx context.Context, material string) (string, error) {
	if !a.c.Welcome().ReplySuggestion {
		return "", nil
	}
	budget, ok := teamAskBudget(ctx)
	if !ok {
		return "", ctx.Err()
	}
	data, err := a.c.call(ctx, methodReplySuggestion, replySuggestionArgs{Context: material, Budget: budget})
	if err != nil {
		return "", err
	}
	var text string
	err = json.Unmarshal(data, &text)
	return text, err
}
