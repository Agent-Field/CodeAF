// Package ports answers two questions about this machine's TCP ports, as an
// observation and nothing more: is this port taken right now, and by whom,
// and which port past this one is free right now.
//
// OBSERVATION, NEVER RESERVATION. A port reported free was free for the
// instant it was bound and released; nothing holds it afterwards, so a later
// bind can still lose a race and a caller must say so. A port held by another
// program is never touched, killed or reconfigured here.
//
// The package knows no application policy: it opens no connection to any
// host, runs no command a caller supplied, and raises no notice.
package ports

import (
	"context"
	"errors"
	"net"
	"strconv"
	"syscall"
	"time"
)

// The bounds and budgets. They are stated once so the wire document and the
// tests quote them rather than copy them.
const (
	// MinPort and MaxPort are the ports that can be asked about; port 0 asks
	// the kernel to choose and is never a port a person's command can name.
	MinPort = 1
	MaxPort = 65535
	// FirstSuggested is the lowest port a suggestion returns: the privileged
	// range is not offered to a command that is not run as root.
	FirstSuggested = 1024
	// MaxCandidates is the most ports one suggestion looks at, and
	// SuggestBudget the longest it takes doing so.
	MaxCandidates = 128
	SuggestBudget = time.Second
	// InspectBudget bounds one inspection, the holder lookup included.
	InspectBudget = 1500 * time.Millisecond
	// Concurrency is how many inspections or scans run at once; the next
	// is refused with ErrBusy rather than queued.
	Concurrency = 4
)

// State is what one port was seen to be.
type State string

const (
	StateFree  State = "free"
	StateInUse State = "in_use"
	// StateUnknown is every answer that is not a proof: a refusal, a deadline,
	// a platform that would not say. It is never read as free.
	StateUnknown State = "unknown"
)

// Holder is the program holding a port: a positive process id and the
// program's name, never its command line or a path.
type Holder struct {
	PID  int
	Name string
}

// Inspection is one port as it was seen.
type Inspection struct {
	Port      int
	State     State
	CheckedAt time.Time
	Holder    *Holder
	// Detail says in a few words why a state is unknown.
	Detail string
}

// Suggestion is a port that was free when the scan bound it.
type Suggestion struct {
	Port         int
	CheckedAt    time.Time
	CheckedCount int
}

var (
	// ErrInvalid is a port or starting point outside what can be asked.
	ErrInvalid = errors.New("port out of range")
	// ErrNoCandidate is a scan that found no free port within its bounds.
	ErrNoCandidate = errors.New("no free port found")
	// ErrBusy is the concurrency budget being spent.
	ErrBusy = errors.New("port checks are busy")
)

// Binder is one bind attempt, replaceable in tests. It answers nil when the
// address could be bound (and has already been released), and the bind error
// otherwise.
type Binder func(ctx context.Context, network, address string) error

// HolderFinder names the holder of a listening port, or nil when it cannot be
// proved. It never returns an error: not knowing is a nil holder.
type HolderFinder func(ctx context.Context, port int) *Holder

// Checker is the engine's port observer. The zero value is not usable; use
// [New].
type Checker struct {
	bind   Binder
	holder HolderFinder
	now    func() time.Time
	slots  chan struct{}
}

// New is a Checker that binds for real and looks holders up with the
// platform's lsof when there is one.
func New() *Checker {
	return &Checker{bind: nativeBind, holder: lsofHolder, now: time.Now, slots: make(chan struct{}, Concurrency)}
}

// probe is one address that must be free for a port to be free: both
// wildcards, because a wildcard bind is what a server does, and both
// loopbacks, because on some systems a loopback listener does not stop a
// wildcard bind.
var probes = []struct {
	network, host string
	optional      bool // an address family this machine may lack
}{
	{"tcp4", "0.0.0.0", false},
	{"tcp4", "127.0.0.1", false},
	{"tcp6", "::", true},
	{"tcp6", "::1", true},
}

func nativeBind(ctx context.Context, network, address string) error {
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, network, address)
	if err != nil {
		return err
	}
	// Closed on every path: a probe never keeps the port.
	return l.Close()
}

func (c *Checker) acquire(ctx context.Context) (func(), error) {
	select {
	case c.slots <- struct{}{}:
		return func() { <-c.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, ErrBusy
	}
}

// observe binds every probe address once and says what it saw.
func (c *Checker) observe(ctx context.Context, port int) (State, string) {
	state, detail := StateFree, ""
	for _, p := range probes {
		if ctx.Err() != nil {
			return StateUnknown, "the check ran out of time"
		}
		err := c.bind(ctx, p.network, net.JoinHostPort(p.host, strconv.Itoa(port)))
		switch {
		case err == nil:
		case errors.Is(err, syscall.EADDRINUSE) || addrInUse(err):
			return StateInUse, ""
		case p.optional && familyAbsent(err):
			// IPv6 missing here says nothing about the port.
		case ctx.Err() != nil:
			return StateUnknown, "the check ran out of time"
		default:
			state, detail = StateUnknown, bindDetail(err)
		}
	}
	return state, detail
}

func bindDetail(err error) string {
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return "not permitted to check this port"
	}
	return "this port could not be checked"
}

func familyAbsent(err error) bool {
	for _, e := range []error{syscall.EAFNOSUPPORT, syscall.EPROTONOSUPPORT, syscall.EADDRNOTAVAIL} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// Inspect says whether port is free, and when it is taken, by whom if that
// can be proved.
func (c *Checker) Inspect(ctx context.Context, port int) (Inspection, error) {
	if port < MinPort || port > MaxPort {
		return Inspection{}, ErrInvalid
	}
	release, err := c.acquire(ctx)
	if err != nil {
		return Inspection{}, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, InspectBudget)
	defer cancel()
	out := Inspection{Port: port}
	out.State, out.Detail = c.observe(ctx, port)
	out.CheckedAt = c.now()
	if out.State == StateInUse && c.holder != nil {
		if h := c.holder(ctx, port); h != nil && h.PID > 0 && h.Name != "" {
			out.Holder = h
		}
	}
	return out, nil
}

// Suggest answers the first port past after that was free when bound. It
// starts at after+1 (never below FirstSuggested), looks at no more than
// MaxCandidates ports in SuggestBudget, and never wraps past MaxPort. A
// candidate that is busy or could not be checked is skipped, never offered.
func (c *Checker) Suggest(ctx context.Context, after int) (Suggestion, error) {
	if after < MinPort || after > MaxPort {
		return Suggestion{}, ErrInvalid
	}
	release, err := c.acquire(ctx)
	if err != nil {
		return Suggestion{}, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, SuggestBudget)
	defer cancel()
	start := after + 1
	if start < FirstSuggested {
		start = FirstSuggested
	}
	checked := 0
	for port := start; port <= MaxPort && checked < MaxCandidates; port++ {
		if err := ctx.Err(); err != nil {
			return Suggestion{}, err
		}
		checked++
		state, _ := c.observe(ctx, port)
		if state == StateFree {
			return Suggestion{Port: port, CheckedAt: c.now(), CheckedCount: checked}, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return Suggestion{}, err
	}
	return Suggestion{CheckedCount: checked}, ErrNoCandidate
}
