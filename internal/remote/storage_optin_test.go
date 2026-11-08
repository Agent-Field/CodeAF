package remote

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"github.com/Agent-Field/codeaf/internal/wireauth"
	"testing"
)

func TestStorageOptInRemoteSavingRetainsRefusal(t *testing.T) {
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{}, SessionFile: "/chat/transcript.jsonl", FinishSaving: func(WrappedAgent) error {
			return session.SavingError("/chat/transcript.jsonl", syncwords.UploadFailure(wireauth.ErrStorageOff))
		}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	err = loop.Client.Agent().Close()
	if !syncwords.UploadPending(err) || !errors.Is(err, wireauth.ErrStorageOff) || !loop.Client.Agent().LocalDurable() {
		t.Fatalf("remote receipt lost storage refusal: %v", err)
	}
}
