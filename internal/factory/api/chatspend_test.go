package api

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE JOB'S COST IS EVERYTHING DONE FOR IT: before any run, what its chat
// spent; with a run, the run's, which a start carried the chat's into.
func TestAJobsCostIsItsChatsBeforeARunAndItsRunsAfter(t *testing.T) {
	if got := FromItem(factory.Item{ID: 1, State: factory.StateNew, ChatSpent: 0.4}).Cost; got != 0.4 {
		t.Fatalf("before a run the job costs $%v, want its chat's $0.40", got)
	}
	if got := FromItem(factory.Item{ID: 1, State: factory.StateRunning, Stream: &factory.Stream{Spent: 1.4}}).Cost; got != 1.4 {
		t.Fatalf("with a run the job costs $%v, want the run's $1.40", got)
	}
}
