package directory

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
)

// RaftBackend is the host's consensus boundary. Change returns only a durable
// committed result. Listing and ReadCell pass a fresh current-term ReadIndex,
// await durable application, and recheck committed authorization at that view.
// The host authenticates the original ingress and preserves its request ID;
// forwarding through this facade never chooses a new expectation or retries a
// tool. P11 installs this boundary only after the legacy writer is frozen.
type RaftBackend interface {
	Change(context.Context, string, clusterstate.Mutation) (clusterstate.Result, error)
	Listing(context.Context, string) (Listing, error)
	ReadCell(context.Context, string, string) (CellView, error)
}

type Raft struct{ backend RaftBackend }

func NewRaft(backend RaftBackend) *Raft  { return &Raft{backend: backend} }
func (r *Raft) For(device string) Client { return &raftClient{r: r, device: device} }

type raftClient struct {
	r      *Raft
	device string
}

var _ Directory = (*Raft)(nil)
var _ Client = (*raftClient)(nil)

func (c *raftClient) change(ctx context.Context, m clusterstate.Mutation) (CellView, error) {
	r, err := c.r.backend.Change(ctx, c.device, m)
	if err != nil {
		return CellView{}, err
	}
	if err = resultError(r.Code); err != nil {
		return CellView{}, err
	}
	if r.View != nil {
		return *r.View, nil
	}
	return CellView{}, nil
}
func (c *raftClient) List(ctx context.Context) (Listing, error) {
	l, e := c.r.backend.Listing(ctx, c.device)
	return withoutFrames(l), e
}
func (c *raftClient) Cell(ctx context.Context, id string) (CellView, error) {
	return c.r.backend.ReadCell(ctx, c.device, id)
}
func (c *raftClient) PutDevice(ctx context.Context, id string, d Device) error {
	_, e := c.change(ctx, clusterstate.Mutation{PutDevice: &clusterstate.PutDevice{ID: id, Device: d}})
	return e
}
func (c *raftClient) Revoke(ctx context.Context, id string) error {
	_, e := c.change(ctx, clusterstate.Mutation{Revoke: &clusterstate.DeviceID{Device: id}})
	return e
}
func (c *raftClient) SetVault(ctx context.Context, old, next string) error {
	_, e := c.change(ctx, clusterstate.Mutation{SetVault: &clusterstate.VaultCAS{Old: old, New: next}})
	return e
}
func (c *raftClient) Create(ctx context.Context, id string, in CellInit) (CellView, error) {
	return c.change(ctx, clusterstate.Mutation{Create: &clusterstate.CreateCell{Cell: id, Init: in, V1: true}})
}
func (c *raftClient) Acquire(ctx context.Context, id string, o AcquireOpts) (CellView, error) {
	v := &clusterstate.AcquireCell{Cell: id, Force: o.Force, V1: true}
	if o.Expect != nil {
		v.Expect = *o.Expect
	} else {
		if o.RequireConditional {
			return CellView{}, ErrConditionalTake
		}
		v.Legacy = true
	}
	return c.change(ctx, clusterstate.Mutation{Acquire: v})
}
func (c *raftClient) Heartbeat(ctx context.Context, id string, b Beat) (CellView, error) {
	return c.change(ctx, clusterstate.Mutation{Beat: &clusterstate.BeatCell{Cell: id, Beat: b}})
}
func (c *raftClient) Publish(ctx context.Context, id string, p Publish) (CellView, error) {
	return c.change(ctx, clusterstate.Mutation{Publish: &clusterstate.PublishCell{Cell: id, Publish: p, V1: true}})
}
func (c *raftClient) Release(ctx context.Context, id string, f uint64) error {
	_, e := c.change(ctx, clusterstate.Mutation{Release: &clusterstate.ReleaseCell{Cell: id, Fence: f}})
	return e
}
func (c *raftClient) ReleaseQuiet(ctx context.Context, id string, f uint64) error {
	_, e := c.change(ctx, clusterstate.Mutation{Release: &clusterstate.ReleaseCell{Cell: id, Fence: f, Quiet: true}})
	return e
}
func (c *raftClient) Archive(ctx context.Context, id string) error {
	_, e := c.change(ctx, clusterstate.Mutation{Archive: &clusterstate.CellID{Cell: id}})
	return e
}
func (c *raftClient) Presence(ctx context.Context) (PresenceView, error) {
	l, e := c.List(ctx)
	return presenceOf(l, nil), e
}
func (c *raftClient) Rotate(ctx context.Context, req RotationReq) (RotationView, error) {
	_, e := c.change(ctx, clusterstate.Mutation{Rotate: &clusterstate.RotateRequest{Op: string(req.Op), GraceMS: req.GraceMS}})
	if e != nil {
		return RotationView{}, e
	}
	return c.Rotation(ctx)
}
func (c *raftClient) Rotation(ctx context.Context) (RotationView, error) {
	l, e := c.List(ctx)
	return viewOf(l.Identity, l.Now, DefaultGraceBounds), e
}

// Pairing commits a sealed decision dependency; delivery follows through the
// durable effects outbox. There is no callback that can expose a grant before commit.
func (c *raftClient) ApproveRequest(ctx context.Context, code string, a Approval) error {
	d, e := decisionOf(a)
	if e != nil {
		return e
	}
	_, e = c.change(ctx, clusterstate.Mutation{DecideJoin: &clusterstate.JoinDecision{Code: NormalizeCode(code), Device: d.Device, Record: a.Device, Grant: a.Grant, Cert: a.Cert}})
	return e
}
func (c *raftClient) DenyRequest(ctx context.Context, code string) error {
	_, e := c.change(ctx, clusterstate.Mutation{DecideJoin: &clusterstate.JoinDecision{Code: NormalizeCode(code), Deny: true}})
	return e
}

var errRaftConflict = errors.New("directory: request sequence conflict")

func resultError(code clusterstate.Code) error {
	switch code {
	case clusterstate.OK:
		return nil
	case clusterstate.NotFound:
		return ErrNotFound
	case clusterstate.Exists:
		return ErrExists
	case clusterstate.LeaseHeld:
		return ErrLeaseHeld
	case clusterstate.FenceStale:
		return ErrFenceStale
	case clusterstate.HeadMoved:
		return ErrHeadMoved
	case clusterstate.CAS:
		return ErrCAS
	case clusterstate.Revoked:
		return ErrRevoked
	case clusterstate.SelfRevoke:
		return ErrSelfRevoke
	case clusterstate.Rotated:
		return ErrRotated
	case clusterstate.Unauthorized:
		return ErrUnauthorized
	case clusterstate.RotationStep:
		return ErrRotationStep
	case clusterstate.BadGrace:
		return ErrBadGrace
	case clusterstate.RequestGone:
		return ErrRequestGone
	case clusterstate.AlreadyDecided:
		return ErrAlreadyDecided
	case clusterstate.BadRequest:
		return ErrBadRequest
	case clusterstate.Capacity:
		return ErrFull
	case clusterstate.Conflict:
		return errRaftConflict
	default:
		return ErrUnreachable
	}
}
