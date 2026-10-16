package main

// ── codeaf app: PORT OBSERVATION (V77) ─────────────────────────────────────
//
// ports.inspect and ports.suggest are answered by internal/ports, which binds
// and releases probe sockets and, for a taken port, asks the system lsof for
// the holder. Both are observations: nothing is reserved, and a port held by
// another program is never touched. The calls run on the wire's per-call
// goroutine under a deadline, never on a reader or a UI thread.

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/ports"
)

var _ appwire.Porter = (*appBackend)(nil)

// appPortChecker is the process's one checker, so its concurrency budget is
// shared by every conversation's calls.
var appPortChecker = ports.New()

func (b *appBackend) InspectPort(ctx context.Context, port int) (appwire.PortInspection, error) {
	got, err := appPortChecker.Inspect(ctx, port)
	if err != nil {
		return appwire.PortInspection{}, appPortError(err)
	}
	out := appwire.PortInspection{Port: got.Port, State: string(got.State), CheckedAt: got.CheckedAt, Detail: got.Detail}
	if got.Holder != nil {
		out.Holder = &appwire.PortHolder{PID: got.Holder.PID, Name: got.Holder.Name}
	}
	return out, nil
}

func (b *appBackend) SuggestPort(ctx context.Context, after int) (appwire.PortSuggestion, error) {
	got, err := appPortChecker.Suggest(ctx, after)
	if err != nil {
		return appwire.PortSuggestion{}, appPortError(err)
	}
	return appwire.PortSuggestion{Port: got.Port, CheckedAt: got.CheckedAt, CheckedCount: got.CheckedCount}, nil
}

func appPortError(err error) error {
	switch {
	case errors.Is(err, ports.ErrInvalid):
		return appwire.ErrPortInvalid
	case errors.Is(err, ports.ErrNoCandidate):
		return appwire.ErrPortNoCandidate
	case errors.Is(err, ports.ErrBusy):
		return appwire.ErrPortBusy
	}
	return err
}
