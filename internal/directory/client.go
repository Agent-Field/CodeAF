package directory

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// Client is the whole directory as one device sees it. The device is a fact of
// the Client itself (the relay reads it from the request signature), so no
// method takes one. Every write is a compare-and-swap decided by rules.go.
type Client interface {
	List(ctx context.Context) (Listing, error) // everything a home list needs, one request
	Cell(ctx context.Context, id string) (CellView, error)

	PutDevice(ctx context.Context, id string, d Device) error // upsert this device's record; never touches Revoked
	Revoke(ctx context.Context, id string) error              // stop another device of this identity; ErrNotFound, ErrSelfRevoke
	SetVault(ctx context.Context, old, new string) error      // CAS identity.vault; ErrCAS

	Create(ctx context.Context, id string, in CellInit) (CellView, error)       // ErrExists
	Acquire(ctx context.Context, id string, opts AcquireOpts) (CellView, error) // ErrLeaseHeld unless opts.Force
	Heartbeat(ctx context.Context, id string, b Beat) (CellView, error)         // ErrFenceStale
	Publish(ctx context.Context, id string, p Publish) (CellView, error)        // ErrFenceStale, ErrHeadMoved
	Release(ctx context.Context, id string, fence uint64) error                 // ErrFenceStale
	Archive(ctx context.Context, id string) error                               // idempotent

	// Presence says which devices hold a watch socket now, and when the
	// others were last seen. Live changes arrive on the watch socket instead.
	Presence(ctx context.Context) (PresenceView, error)

	// ApproveRequest lets a device that asked to join (see Requests) into this
	// identity: it writes the device's record, keeps the grant for it and
	// tells the other devices. The first decision wins; repeating it is a
	// success. ErrRequestGone, ErrAlreadyDecided, ErrBadRequest, ErrTooBig.
	ApproveRequest(ctx context.Context, code string, a Approval) error
	// DenyRequest turns a request down. ErrRequestGone, ErrAlreadyDecided.
	DenyRequest(ctx context.Context, code string) error

	// Rotate makes one move of the identity's replacement (freeze, thaw,
	// retire); Rotation reads where it stands. A replaced identity answers every
	// write with ErrRotated and every read as before.
	Rotate(ctx context.Context, req RotationReq) (RotationView, error)
	Rotation(ctx context.Context) (RotationView, error)
}

// AcquireOpts says how a device takes a lease. Its JSON is the acquire
// request body, and an empty body means the zero value.
type AcquireOpts struct {
	// RequireConditional asks the coordinating runtime to check the expected
	// holder and fence even when the observed holder has gone quiet. A service
	// without conditional takes still takes a lapsed lease, because its
	// live-holder check is already atomic; only Force waits for the promise.
	RequireConditional bool `json:"-"`
	// Force takes the lease from a live holder. It is for a person who chose to
	// continue a chat where it runs now: waiting out the holder's lease would
	// make them sit through the time a dead holder's row takes to go off.
	Force bool `json:"force,omitempty"`
	// Expect is the holder and fence the person chose to displace. Omitting it
	// preserves the acquire behavior of clients predating conditional takes.
	Expect *LeaseExpectation `json:"expect,omitempty"`
}

// LeaseExpectation retains the conditional-take tuple shared by every runtime.
type LeaseExpectation = clusterstate.LeaseExpectation

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
	LeaseTTL       = time.Duration(clusterstate.LeaseTTLMillis) * time.Millisecond
	HeartbeatEvery = time.Duration(clusterstate.RenewEveryMillis) * time.Millisecond
)

// The errors a Client returns. Callers compare with errors.Is.
var (
	ErrNotFound     = clusterstate.ErrNotFound
	ErrExists       = clusterstate.ErrExists
	ErrLeaseHeld    = clusterstate.ErrLeaseHeld
	ErrFenceStale   = clusterstate.ErrFenceStale
	ErrHeadMoved    = clusterstate.ErrHeadMoved
	ErrCAS          = clusterstate.ErrCAS
	ErrRevoked      = wireauth.ErrRevoked // every verb of a revoked device; the wires share this one sentinel
	ErrSelfRevoke   = clusterstate.ErrSelfRevoke
	ErrUnauthorized = errors.New("directory: unauthorized")
	ErrUnreachable  = errors.New("directory: unreachable") // transport; callers degrade
	ErrTooOld       = errors.New("directory: this relay is too old for that")
	// ErrConditionalTake is the one feature an older service cannot offer
	// safely. It says what still works and the one thing to do instead.
	ErrConditionalTake = errors.New(conditionalTakeFeature + " waits for an update to the sync service; everything else still works, so close the chat on that computer and it opens here")
)

// ReleaseQuiet distinguishes background preparation from quitting an open
// conversation. Older clients can still release; their records have no quit clock.
func ReleaseQuiet(ctx context.Context, client Client, id string, fence uint64) error {
	if quiet, ok := client.(interface {
		ReleaseQuiet(context.Context, string, uint64) error
	}); ok {
		return quiet.ReleaseQuiet(ctx, id, fence)
	}
	return client.Release(ctx, id, fence)
}
