package crewagent

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestAskReturnsNotBuilt(t *testing.T) {
	agent := New(svc.Config{}, &svc.Services{})
	_, err := agent.Ask(context.Background(), svc.CrewAsk{App: "crm", Crew: "pricing", Text: "Hello"})
	var typed *bridge.Error
	if !errors.As(err, &typed) || typed.Code != bridge.CodeNotBuilt {
		t.Fatalf("error=%v", err)
	}
}
