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

	Create(ctx context.Context, id string, in CellInit) (CellView, error) // ErrExists
	Acquire(ctx context.Context, id string) (CellView, error)             // ErrLeaseHeld
	Heartbeat(ctx context.Context, id string, b Beat) (CellView, error)   // ErrFenceStale
	Publish(ctx context.Context, id string, p Publish) (CellView, error)  // ErrFenceStale, ErrHeadMoved
	Release(ctx context.Context, id string, fence uint64) error           // ErrFenceStale
	Archive(ctx context.Context, id string) error                         // idempotent
}

// The lease timings every device and the directory agree on.
const (
	LeaseTTL       = 30 * time.Second
	HeartbeatEvery = 10 * time.Second
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
