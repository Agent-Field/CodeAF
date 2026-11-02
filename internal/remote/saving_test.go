package remote

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

type plainSaveFailure struct{ *fakeAgent }

func (a plainSaveFailure) Close() error { return errors.New("snapshot: dial unix missing.sock") }

// Contract D2 keeps a known transcript even when an older peer sends only error text.
func TestOldSavingReplyKeepsTheWelcomeTranscript(t *testing.T) {
	const transcript = "/temporary/remote-chat/transcript.jsonl"
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: plainSaveFailure{&fakeAgent{}}, SessionFile: transcript}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	err = loop.Client.Agent().Close()
	var unsaved *session.UnsavedChatError
	if !errors.As(err, &unsaved) || unsaved.Transcript != transcript {
		t.Fatalf("an older saving reply lost the known transcript: %v", err)
	}
	before := loop.CallsMade()
	if since, period, held := loop.Client.Agent().RenewalStatus(); !since.IsZero() || period != 0 || held {
		t.Fatalf("a peer without renewal health supplied %v, %v, %v", since, period, held)
	}
	if loop.CallsMade() != before {
		t.Fatal("reading absent health queried an older peer")
	}
}
