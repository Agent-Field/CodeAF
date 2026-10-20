package remote

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/session"
)

const MethodRunHarness = "RunHarnessRequest"

type RunHarnessArgs struct {
	Name     string
	Text     string
	Model    string
	ClientID string
}

type harnessRequestDoor interface {
	RunHarnessRequest(context.Context, string, string, string) (<-chan session.Event, error)
}

type harnessClientDoor interface {
	RunHarnessRequestWithClientID(context.Context, string, string, string, string) (<-chan session.Event, error)
}

func harnessRequestsKnown(agent any) bool {
	_, ok := agent.(harnessRequestDoor)
	return ok
}

// HarnessRequestsKnown prevents an older engine's absent door being dispatched.
func (a *Agent) HarnessRequestsKnown() bool {
	return a.welcomeSays(func(w Welcome) bool { return w.HarnessRequests })
}

func (a *Agent) RunHarnessRequest(ctx context.Context, name, text, model string) (<-chan session.Event, error) {
	return a.RunHarnessRequestWithClientID(ctx, name, text, model, "")
}

// RunHarnessRequestWithClientID carries only words and correlation, never files.
func (a *Agent) RunHarnessRequestWithClientID(ctx context.Context, name, text, model, clientID string) (<-chan session.Event, error) {
	if !a.HarnessRequestsKnown() {
		return nil, errors.New("harnesses are unavailable here")
	}
	return a.open(ctx, MethodRunHarness, RunHarnessArgs{Name: name, Text: text, Model: model, ClientID: clientID})
}
