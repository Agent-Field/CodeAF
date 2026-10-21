package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appHarnessRunner is the terminal picker's optional door. A remote handle
// lacking it refuses locally, without dispatching a message to another host.
type appHarnessRunner interface {
	RunHarnessRequest(context.Context, string, string, string) (<-chan session.Event, error)
}

type appHarnessClientRunner interface {
	RunHarnessRequestWithClientID(context.Context, string, string, string, string) (<-chan session.Event, error)
}

// RunHarness uses the picked-harness producer and the ordinary send lifecycle.
// Attachments stay staged for an ordinary message, just as in the terminal.
func (b *appBackend) RunHarness(id, name, text, clientID string) (uint64, error) {
	b.resetCloseResumes(id)
	a, err := b.find(id)
	if err != nil {
		return 0, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	view, opened, err := b.ensureLocked(a)
	if err != nil {
		return 0, err
	}
	if opened {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	live := b.liveOf(a)
	runner, ok := live.conn.Conv.(appHarnessRunner)
	if known, advertised := live.conn.Conv.(interface{ HarnessRequestsKnown() bool }); advertised && !known.HarnessRequestsKnown() {
		ok = false
	}
	if !ok {
		return 0, appwire.Errorf(appwire.CodeUnknownMethod, "harnesses are unavailable here")
	}
	return b.startTurnLocked(a, text, clientID, nil, func(_ *appLive, keptID string) (<-chan session.Event, error) {
		if marked, ok := runner.(appHarnessClientRunner); ok && keptID != "" {
			return marked.RunHarnessRequestWithClientID(context.Background(), name, text, "", keptID)
		}
		return runner.RunHarnessRequest(context.Background(), name, text, "")
	})
}
