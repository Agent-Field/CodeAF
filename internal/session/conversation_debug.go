package session

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// DebugConversation keeps the record for this conversation's subsequent turns.
func (a *Agent) DebugConversation() (string, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return "", "", errors.New("session: agent is closed")
	}
	if a.debugRun == "" {
		a.debugRun = trace.NewRunID()
	}
	folder, message := trace.KeepConversation(trace.WithRun(context.Background(), a.debugRun))
	return folder, message, nil
}
