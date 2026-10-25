package executor

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// WithEnvironment routes conventional scratch/cache variables per seat. This is
// process configuration, not filesystem containment: a command can override it.
// Unspecified variables, including HOME, retain the caller's environment.
func WithEnvironment(seat Seat, environment []string) Seat {
	if len(environment) == 0 {
		return seat
	}
	return environmentSeat{Seat: seat, environment: append([]string(nil), environment...)}
}

type environmentSeat struct {
	Seat
	environment []string
}

func (s environmentSeat) In(dir string) Runner {
	return environmentRunner{Runner: s.Seat.In(dir), environment: s.environment}
}
func (s environmentSeat) ForSetup() Seat                  { return WithEnvironment(ForSetup(s.Seat), s.environment) }
func (s environmentSeat) NoteModelCall(call ModelCall)    { NoteModelCall(s.Seat, call) }
func (s environmentSeat) Interrupted() Interrupted        { return InterruptedOn(s.Seat) }
func (s environmentSeat) Settle(ctx context.Context)      { Settle(ctx, s.Seat) }
func (s environmentSeat) Drain(ctx context.Context) error { return Drain(ctx, s.Seat) }

type environmentRunner struct {
	Runner
	environment []string
}

func (r environmentRunner) Exec(ctx context.Context, request ExecRequest, output func(Chunk)) (ExecResult, error) {
	request.Env = mergeEnvironment(request.Env, r.environment)
	return r.Runner.Exec(ctx, request, output)
}
func (r environmentRunner) Command(ctx context.Context, request ExecRequest) (*exec.Cmd, error) {
	request.Env = mergeEnvironment(request.Env, r.environment)
	return r.Runner.Command(ctx, request)
}
func mergeEnvironment(base, extra []string) []string {
	if base == nil {
		base = os.Environ()
	}
	result := make([]string, 0, len(base)+len(extra))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		replaced := false
		for _, replacement := range extra {
			name, _, _ := strings.Cut(replacement, "=")
			if key == name {
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, entry)
		}
	}
	return append(result, extra...)
}
