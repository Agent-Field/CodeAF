package directory

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// The states of a replaced identity. An identity with no Rotation is live.
const (
	StateFrozen  = "frozen"  // read-only; the device that froze it may thaw it or retire it
	StateRetired = "retired" // read-only until RetireAt, then deleted by the relay
)

// Rotation retains the shared record without importing the directory into wasm.
type Rotation = clusterstate.Rotation

// RotationOp is one move of the rotation state machine.
type RotationOp string

const (
	OpFreeze RotationOp = "freeze"
	OpThaw   RotationOp = "thaw"
	OpRetire RotationOp = "retire"
)

// RotationReq is the body of a rotation call.
type RotationReq struct {
	Op      RotationOp `json:"op"`
	GraceMS int64      `json:"grace_ms,omitempty"` // retire only; 0 is the relay's default
}

// GraceBounds is what a relay accepts as a grace period. The relay's own
// clock and these numbers decide, and the client reads them back.
type GraceBounds struct{ Min, Max, Default time.Duration }

// DefaultGraceBounds is one hour to thirty days, a week unless told.
var DefaultGraceBounds = GraceBounds{Min: time.Duration(clusterstate.MinGraceMillis) * time.Millisecond, Max: time.Duration(clusterstate.MaxGraceMillis) * time.Millisecond, Default: time.Duration(clusterstate.DefaultGraceMillis) * time.Millisecond}

// RotationView is the answer to both rotation calls: the state now, the
// directory's time, and the bounds it enforces, so a conformance run can test
// what a relay says and a client can show what it will do.
type RotationView struct {
	Now       int64     `json:"now"`
	Rotation  *Rotation `json:"rotation,omitempty"`
	MinMS     int64     `json:"min_grace_ms"`
	MaxMS     int64     `json:"max_grace_ms"`
	DefaultMS int64     `json:"default_grace_ms"`
}

func viewOf(rec IdentityRec, now int64, b GraceBounds) RotationView {
	return RotationView{Now: now, Rotation: rec.Rotation,
		MinMS: b.Min.Milliseconds(), MaxMS: b.Max.Milliseconds(), DefaultMS: b.Default.Milliseconds()}
}

var (
	// ErrRotated is the refusal of a write to a replaced identity.
	ErrRotated = wireauth.ErrRotated
	// ErrRotationStep is a rotation call that does not fit the state: a retire
	// before a freeze.
	ErrRotationStep = errors.New("directory: that rotation step does not fit the identity's state")
	// ErrBadGrace is a retire whose grace period is outside the relay's bounds.
	ErrBadGrace = errors.New("directory: grace period outside what this relay accepts")
)

// refusesWrites is the rule every write verb meets first: a replaced identity
// takes no write, whoever the device is.
func refusesWrites(rec IdentityRec) error {
	if rec.Rotation != nil {
		return ErrRotated
	}
	return nil
}

// A step moves the rotation of one identity; it answers the next one.
type rotationStep func(cur *Rotation, device string, req RotationReq, now int64, b GraceBounds) (*Rotation, error)

var rotationSteps = map[RotationOp]rotationStep{OpFreeze: freeze, OpThaw: thaw, OpRetire: retire}

// RotateBy is the pure rule of the rotation state machine: it answers the record
// after device's call, or the refusal. Freezing is first come: a second device
// that freezes an identity somebody else froze is refused, so two people who
// both hold the root cannot both be rotating it.
func RotateBy(rec IdentityRec, device string, req RotationReq, now int64, b GraceBounds) (IdentityRec, error) {
	step, ok := rotationSteps[req.Op]
	if !ok {
		return rec, errBadRequest
	}
	next, err := step(rec.Rotation, device, req, now, b)
	if err != nil {
		return rec, err
	}
	rec.Rotation = next
	return rec, nil
}

func freeze(cur *Rotation, device string, _ RotationReq, now int64, _ GraceBounds) (*Rotation, error) {
	switch {
	case cur == nil:
		return &Rotation{State: StateFrozen, By: device, At: now}, nil
	case cur.By == device:
		return cur, nil
	}
	return nil, ErrRotated
}

// thaw is open to any device of the identity: it is how a computer undoes a
// rotation whose keeper lost its disk. It cannot undo a retire.
func thaw(cur *Rotation, _ string, _ RotationReq, _ int64, _ GraceBounds) (*Rotation, error) {
	if cur != nil && cur.State == StateRetired {
		return nil, ErrRotated
	}
	return nil, nil
}

func retire(cur *Rotation, device string, req RotationReq, now int64, b GraceBounds) (*Rotation, error) {
	switch {
	case cur == nil:
		return nil, ErrRotationStep
	case cur.By != device:
		return nil, ErrRotated
	case cur.State == StateRetired:
		return cur, nil
	}
	grace := time.Duration(req.GraceMS) * time.Millisecond
	if grace == 0 {
		grace = b.Default
	}
	if grace < b.Min || grace > b.Max {
		return nil, ErrBadGrace
	}
	return &Rotation{State: StateRetired, By: cur.By, At: cur.At, RetireAt: now + grace.Milliseconds()}, nil
}

// Gate is a Client's rotation calls as the three moves a rotation makes.
type Gate struct {
	Client Client
	// Store discovers the hosted handoff route without changing the rotation wire.
	Store interface {
		Stats(context.Context) (blobstore.Counts, error)
	}
	Source string
}

// Freeze stops every write to the identity.
func (g Gate) Freeze(ctx context.Context) error { return g.move(ctx, RotationReq{Op: OpFreeze}) }

// HandoffTo carries hosted storage permission after freezing and before uploads.
// Relays without the stats field receive no additional request.
func (g Gate) HandoffTo(ctx context.Context, successor identity.Identity) error {
	if g.Store == nil {
		return nil
	}
	counts, err := g.Store.Stats(ctx)
	if err != nil {
		return err
	}
	if counts.Storage == "" {
		return nil
	}
	client, ok := g.Client.(interface {
		HandoffStorage(context.Context, string, identity.Identity) error
	})
	if !ok {
		return ErrTooOld
	}
	return client.HandoffStorage(ctx, g.Source, successor)
}

// HandoffStorage signs consent with the successor root; the HTTP request is
// still signed by a device of the source, just like other store requests.
func (h *HTTP) HandoffStorage(ctx context.Context, source string, successor identity.Identity) error {
	proof := successor.Sign([]byte("codeaf/storage-handoff/v1\n" + source + "\n" + successor.ID()))
	request := struct {
		Successor    string `json:"successor"`
		SuccessorKey string `json:"successor_key"`
		Proof        string `json:"proof"`
	}{successor.ID(), hex.EncodeToString(successor.PublicKey()), hex.EncodeToString(proof)}
	return h.do(ctx, http.MethodPost, "/v1/store/handoff", request, nil)
}

// Thaw undoes a freeze.
func (g Gate) Thaw(ctx context.Context) error { return g.move(ctx, RotationReq{Op: OpThaw}) }

// Retire makes the freeze permanent and has the relay delete the identity after grace.
func (g Gate) Retire(ctx context.Context, grace time.Duration) error {
	return g.move(ctx, RotationReq{Op: OpRetire, GraceMS: grace.Milliseconds()})
}

func (g Gate) move(ctx context.Context, req RotationReq) error {
	_, err := g.Client.Rotate(ctx, req)
	return err
}
