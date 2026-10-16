package appwire

import (
	"context"
	"errors"
	"time"
)

// Port observation (V77): is this port taken, by whom, and which port past it
// is free. Both answers are OBSERVATIONS at CheckedAt — nothing is reserved,
// so a command that binds later can still lose a race, and the app must say
// so rather than promise the port.

// The states a port was seen in. unknown is never to be read as free.
const (
	PortFree    = "free"
	PortInUse   = "in_use"
	PortUnknown = "unknown"
)

// PortHolder is the program holding a port: a positive process id and the
// program's name only — never a command line or a path.
type PortHolder struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

// PortInspection is ports.inspect's answer. Holder is present only when the
// engine could prove it; its absence on an in_use port means "not named".
type PortInspection struct {
	Port      int         `json:"port"`
	State     string      `json:"state" enum:"free,in_use,unknown"`
	CheckedAt time.Time   `json:"checked_at"`
	Holder    *PortHolder `json:"holder,omitempty"`
	Detail    string      `json:"detail,omitempty"`
}

// PortSuggestion is ports.suggest's answer: a port that was free when bound
// at CheckedAt, after CheckedCount candidates were looked at.
type PortSuggestion struct {
	Port         int       `json:"port"`
	CheckedAt    time.Time `json:"checked_at"`
	CheckedCount int       `json:"checked_count"`
}

// Porter is the engine's half of port observation. An [Engine] that is not
// one refuses ports.* as unknown_method. The context carries the call's
// deadline; an implementation reports ErrPortInvalid, ErrPortNoCandidate or
// ErrPortBusy (or a context error) and the route turns them into wire errors.
type Porter interface {
	InspectPort(ctx context.Context, port int) (PortInspection, error)
	SuggestPort(ctx context.Context, after int) (PortSuggestion, error)
}

// What a Porter answers when a port cannot be had, so the route can pick the
// wire code without knowing the engine's own types.
var (
	ErrPortInvalid     = errors.New("port out of range")
	ErrPortNoCandidate = errors.New("no free port found")
	ErrPortBusy        = errors.New("port checks are busy")
)

// portCallBudget is the most one ports.* call may take, the engine's own
// bounds included.
const portCallBudget = 3 * time.Second

func init() {
	for name, m := range portMethods {
		methods[name] = m
	}
}

func porter(e Engine, name string) (Porter, error) {
	p, ok := e.(Porter)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return p, nil
}

func portError(err error) error {
	switch {
	case errors.Is(err, ErrPortInvalid):
		return Errorf(CodeBadParams, "port must be a whole number from 1 to 65535")
	case errors.Is(err, ErrPortNoCandidate):
		return Errorf(CodeNotFound, "no free port was found after it")
	case errors.Is(err, ErrPortBusy):
		return Errorf(CodeBusy, "port checks are busy; try again")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return Errorf(CodeEngine, "the port check ran out of time")
	}
	return err
}

var portMethods = map[string]method{
	"ports.inspect": typed(func(e Engine, p struct {
		Port int `json:"port"`
	}) (*PortInspection, error) {
		s, err := porter(e, "ports.inspect")
		if err != nil {
			return nil, err
		}
		if p.Port < 1 || p.Port > 65535 {
			return nil, portError(ErrPortInvalid)
		}
		ctx, cancel := context.WithTimeout(context.Background(), portCallBudget)
		defer cancel()
		out, err := s.InspectPort(ctx, p.Port)
		if err != nil {
			return nil, portError(err)
		}
		if out.Holder != nil && (out.Holder.PID <= 0 || out.Holder.Name == "") {
			out.Holder = nil
		}
		return &out, nil
	}),
	"ports.suggest": typed(func(e Engine, p struct {
		After int `json:"after"`
	}) (*PortSuggestion, error) {
		s, err := porter(e, "ports.suggest")
		if err != nil {
			return nil, err
		}
		// 65535 has nothing after it and the scan never wraps.
		if p.After < 1 || p.After > 65534 {
			return nil, Errorf(CodeBadParams, "after must be a whole number from 1 to 65534")
		}
		ctx, cancel := context.WithTimeout(context.Background(), portCallBudget)
		defer cancel()
		out, err := s.SuggestPort(ctx, p.After)
		if err != nil {
			return nil, portError(err)
		}
		if out.Port < 1 || out.Port > 65535 {
			return nil, Errorf(CodeEngine, "the engine answered a port out of range")
		}
		return &out, nil
	}),
}
