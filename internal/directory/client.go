package directory

import (
	"context"
	"errors"
	"time"
)

// Client is the whole directory as one device sees it. The device is a fact of
// the Client itself (the relay reads it from the request signature), so no
// method takes one. Every write is a compare-and-swap decided by rules.go.
type Client interface {
	List(ctx context.Context) (Listing, error) // everything a home list needs, one request
	Cell(ctx context.Context, id string) (CellView, error)

	PutDevice(ctx context.Context, id string, d Device) error // upsert this device's record
	SetVault(ctx context.Context, old, new string) error      // CAS identity.vault; ErrCAS

	Create(ctx context.Context, id string, in CellInit) (CellView, error)       // ErrExists
	Acquire(ctx context.Context, id string, opts AcquireOpts) (CellView, error) // ErrLeaseHeld unless opts.Force
	Heartbeat(ctx context.Context, id string, b Beat) (CellView, error)         // ErrFenceStale
	Publish(ctx context.Context, id string, p Publish) (CellView, error)        // ErrFenceStale, ErrHeadMoved
	Release(ctx context.Context, id string, fence uint64) error                 // ErrFenceStale
	Archive(ctx context.Context, id string) error                               // idempotent
}

// AcquireOpts says how a device takes a lease. Its JSON is the acquire
// request body, and an empty body means the zero value.
type AcquireOpts struct {
	// Force takes the lease from a live holder. It is for a person who chose to
	// continue a chat where it runs now: waiting out the holder's lease would
	// make them sit through the time a dead holder's row takes to go off.
	Force bool `json:"force,omitempty"`
}

// Directory is one identity's shared directory; For gives each device its own
// Client. The relay handler calls For(deviceFromSignature) once per request.
type Directory interface {
	For(device string) Client
}

// The lease timings every device and the directory agree on. A publish renews
// the lease and a heartbeat is sent only while nothing was published, so the
// relay sees one request per HeartbeatEvery at most and the TTL still covers
// two missed beats.
const (
	LeaseTTL       = 90 * time.Second
	HeartbeatEvery = 30 * time.Second
)

// The errors a Client returns. Callers compare with errors.Is.
var (
	ErrNotFound     = errors.New("directory: not found")
	ErrExists       = errors.New("directory: exists")
	ErrLeaseHeld    = errors.New("directory: lease held by another device")
	ErrFenceStale   = errors.New("directory: fence superseded") // L7
	ErrHeadMoved    = errors.New("directory: head moved")
	ErrCAS          = errors.New("directory: compare-and-swap failed")
	ErrRevoked      = errors.New("directory: device revoked") // reserved; Stage 1 never returns it
	ErrUnauthorized = errors.New("directory: unauthorized")
	ErrUnreachable  = errors.New("directory: unreachable") // transport; callers degrade
)
