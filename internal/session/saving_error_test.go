package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/knowledge"
)

type failedSavingSeat struct {
	executor.Seat
	err error
}

func (s failedSavingSeat) Drain(context.Context) error { return s.err }

// Contract A3 names the local transcript when close cannot seal the chat.
func TestCloseSavingFailureNamesTheRetainedTurns(t *testing.T) {
	cause := errors.New("saving is stuck")
	place := newPlace(t, false)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place, config.SessionFile = place, place.Transcript()
		config.knowledgeReadVault = knowledge.Open(t.TempDir())
		config.Seat = failedSavingSeat{Seat: executor.Host, err: cause}
	})
	err := agent.Close()
	var unsaved *UnsavedChatError
	if !errors.As(err, &unsaved) || unsaved.Transcript != place.Transcript() || !errors.Is(err, cause) || !strings.Contains(err.Error(), place.Transcript()) {
		t.Fatalf("close lost the saved recovery location: %v", err)
	}
}

// Contract A3 gives publish failures the same local recovery location.
func TestPublishSavingFailureNamesTheRetainedTurns(t *testing.T) {
	cause := errors.New("relay unavailable")
	err := SavingError("/temporary/chat/transcript.jsonl", cause)
	var unsaved *UnsavedChatError
	if !errors.As(err, &unsaved) || !errors.Is(err, cause) || !strings.Contains(err.Error(), unsaved.Transcript) {
		t.Fatal(err)
	}
	if SavingError("unused", nil) != nil {
		t.Fatal("successful save became a failure")
	}
}
