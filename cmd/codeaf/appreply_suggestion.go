package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// Suggestions never open an unheld conversation, send a turn or invoke a tool.
// Crew, factory and bare chats all use their actual agent's metered model door.
func (b *appBackend) SuggestReply(ctx context.Context, p appwire.ReplySuggestionParams) (appwire.ReplySuggestion, error) {
	_, live, endRead, err := b.borrowExisting(p.Agent)
	if err != nil {
		return appwire.ReplySuggestion{}, err
	}
	defer endRead()
	if live == nil {
		return appwire.ReplySuggestion{}, nil
	}
	door, ok := live.conn.Conv.(interface {
		SuggestReply(context.Context, string) (string, error)
	})
	if !ok {
		return appwire.ReplySuggestion{}, nil
	}
	text, err := door.SuggestReply(ctx, p.Context)
	if err != nil {
		return appwire.ReplySuggestion{}, nil
	}
	return appwire.ReplySuggestion{Text: text}, nil
}
