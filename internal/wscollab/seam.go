package wscollab

import (
	"context"
	"sync"
)

// Journal is the recipient conversation's single-writer seam. Session
// implements this without importing this package; the host adapts and binds.
type Journal interface {
	// Recorded is whether this conversation's own journal already holds the
	// line. A resume asks this before telling the landing again.
	Recorded(id DeliveryID) bool
	// Append is the one writer. The router does not go around it.
	Append(ctx context.Context, env Envelope) error
}

// Queue is the live mailbox accept. Accepted is not recorded.
type Queue interface {
	Accept(ctx context.Context, env Envelope) string
}

// Seam is the bound conversation: its journal and its live queue. A host that
// has retired unbinds; pending envelopes wait.
type Seam interface {
	Journal
	Queue
}

// Host is the engine host that owns a conversation. Wake asks it to reconnect.
// Alive false, or [ErrRetired], means leave the delivery pending — never a
// dummy success, and never a second in-process journal.
type Host interface {
	Alive() bool
	Wake(ctx context.Context, conversationID string) error
}

// HostFinder locates the engine host for a conversation. The host package
// registers one through [RegisterHostFinder]; session never does.
type HostFinder interface {
	Find(ctx context.Context, conversationID string) (Host, error)
}

var (
	hostMu sync.Mutex
	hosts  HostFinder
)

// RegisterHostFinder installs the locator the router asks when a conversation
// has no bound seam. It is the host's door, called the way the run engine
// calls [session.RegisterRunEngine]: the package that cannot import the other
// owns the var, and the host binds it. Nil is "no host today", which is
// pending, not an invented success.
func RegisterHostFinder(h HostFinder) {
	hostMu.Lock()
	defer hostMu.Unlock()
	hosts = h
}

func registeredHosts() HostFinder {
	hostMu.Lock()
	defer hostMu.Unlock()
	return hosts
}
