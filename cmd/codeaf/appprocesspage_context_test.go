package main

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

type contextErrorPager struct {
	*quietAgent
	err error
}

func (p contextErrorPager) ProcessPage(context.Context, session.ProcessPageAsk) (session.ProcessPage, error) {
	return session.ProcessPage{}, p.err
}

func TestProcessPageResidentContextErrorsPreserveSentinels(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled} {
		t.Run(cause.Error(), func(t *testing.T) {
			far := contextErrorPager{quietAgent: &quietAgent{}, err: cause}
			loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{
				Boot: func(remote.Hello) (*remote.Engine, error) { return &remote.Engine{Agent: far}, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = loop.Close() })
			backend, _, agent := workBackend(t, &fakeConversation{})
			backend.locked(func() { backend.agents[agent.ID].live.conn.Conv = loop.Client.Agent() })
			// A live caller forces the resident reply to supply the error.
			_, err = backend.PageProcesses(context.Background(), appwire.ProcessPageAsk{Agent: agent.ID})
			if !errors.Is(err, cause) || err != cause {
				t.Fatalf("resident process page lost %v: %v", cause, err)
			}
			if err.Error() != cause.Error() {
				t.Fatalf("resident error text changed: %q", err.Error())
			}
		})
	}
}
