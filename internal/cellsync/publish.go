package cellsync

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// ErrSuperseded means another device took the lease while this one was
// publishing: the head did not move and the caller must branch (L7, L12).
var ErrSuperseded = fmt.Errorf("cellsync: another device continued this chat: %w", directory.ErrFenceStale)

// Publisher makes turns durable: objects into the store first, the head in the
// directory last. Dir is already bound to this device.
type Publisher struct {
	Engine Engine
	Store  blobstore.Store
	Dir    directory.Client
}

// Driving is the state of one cell this device is driving.
type Driving struct {
	Cell  cell.Cell
	Fence uint64 // 0 = no cell record yet
	Head  string // last durable head, "" before the first publish
	// Frames is the plan (contract §22.2): the frames this device says hold the
	// current head's closure, replaced at the record on every publish. It may
	// name any subset of them; takes treat it as a hint, never as the truth.
	Frames []string
	// Remote is the directory id being driven when it is not Cell.ID: after a
	// branch the local folder is unchanged but the directory record is new.
	Remote string
}

// ID is the directory id of the cell being driven.
func (d Driving) ID() string {
	if d.Remote != "" {
		return d.Remote
	}
	return d.Cell.ID
}

// PublishInfo is what a publish tells the directory about the cell.
type PublishInfo struct {
	Class, Title string
	Size         uint64
	Pending      uint32
	Keys         map[string]map[string]string
}

// Publish uploads what head needs and then moves the directory head. Any store
// or directory error leaves d unchanged, and the head never moves before every
// object it names is in the store.
func (p *Publisher) Publish(ctx context.Context, d *Driving, head string, in PublishInfo) error {
	_, err := p.publish(ctx, d, head, in)
	return err
}

// publish is Publish that also reports what was uploaded.
func (p *Publisher) publish(ctx context.Context, d *Driving, head string, in PublishInfo) (Export, error) {
	ex, frames, err := p.uploadPlan(ctx, d.Cell, head)
	if err != nil {
		return Export{}, err
	}
	frames = unionFrames(d.Frames, frames)
	fence, err := p.advance(ctx, *d, head, in, frames)
	if err != nil {
		return ex, err
	}
	d.Fence, d.Head, d.Frames = fence, head, frames
	return ex, nil
}

// unionFrames joins the plan so far with this publish's frames, first-seen
// order, no id twice: a frame holds the closure of the head it was uploaded
// for, and every head since still names it.
func unionFrames(plan, uploaded []string) []string {
	seen := make(map[string]bool, len(plan)+len(uploaded))
	out := make([]string, 0, len(plan)+len(uploaded))
	for _, id := range append(plan, uploaded...) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// Upload exports what the store lacks for head, puts every frame, and only
// then records them as published. Objects are content-addressed, so what a
// refused publish uploaded is reused by whatever comes next.
func (p *Publisher) Upload(ctx context.Context, c cell.Cell, head string) (Export, error) {
	ex, _, err := p.uploadPlan(ctx, c, head)
	return ex, err
}

// uploadPlan is Upload that also answers the ids of the frames it put, which
// is what the plan for the head names (contract §22.2). Frames skipHeld
// dropped are not named: their ids are whatever earlier upload carried them,
// which this publish does not know, and the take's want loop covers them.
func (p *Publisher) uploadPlan(ctx context.Context, c cell.Cell, head string) (Export, []string, error) {
	ex, err := p.Engine.Export(ctx, c, head)
	if err != nil {
		return Export{}, nil, err
	}
	ex = p.skipHeld(ctx, c, ex)
	ids := make([]string, 0, len(ex.Frames))
	paths := make([]string, len(ex.Frames))
	for i, f := range ex.Frames {
		id, err := p.put(ctx, f)
		if err != nil {
			return Export{}, nil, err
		}
		ids = append(ids, id)
		paths[i] = f.Path
	}
	if len(paths) == 0 {
		return ex, ids, nil
	}
	return ex, ids, p.Engine.Published(ctx, c, paths)
}

// put stores one frame and answers the id the store named it, which is what
// the plan records: the store's own name for the frame, not a local guess.
func (p *Publisher) put(ctx context.Context, f FrameFile) (string, error) {
	frame, err := os.ReadFile(f.Path)
	if err != nil {
		return "", err
	}
	return p.Store.PutFrame(ctx, frame)
}

// advance moves the directory head and answers the fence now held.
func (p *Publisher) advance(ctx context.Context, d Driving, head string, in PublishInfo, frames []string) (uint64, error) {
	if d.Fence != 0 {
		return p.move(ctx, d, head, in, frames)
	}
	fence, err := p.create(ctx, d, head, in, frames)
	if errors.Is(err, directory.ErrExists) {
		return p.resume(ctx, d, head, in, frames, err)
	}
	return fence, err
}

// move publishes head on the lease d already holds.
func (p *Publisher) move(ctx context.Context, d Driving, head string, in PublishInfo, frames []string) (uint64, error) {
	_, err := p.Dir.Publish(ctx, d.ID(), directory.Publish{
		Fence: d.Fence, OldHead: d.Head, Head: head,
		Size: in.Size, Class: in.Class, Title: in.Title, Pending: in.Pending,
		Frames: frames,
	})
	return d.Fence, refusal(err)
}

// resume finishes a create that already happened. A create the relay committed
// but whose answer never arrived (the drive was closed while it was in flight,
// or the connection dropped) leaves this device without a fence for a cell it
// holds, and creating again is refused as "exists" for good. Taking the lease
// back names the fence and the head the relay has, and the publish carries on
// from there. A cell somebody else holds stays the refusal it was: created is
// the original error.
func (p *Publisher) resume(ctx context.Context, d Driving, head string, in PublishInfo, frames []string, created error) (uint64, error) {
	v, err := p.Dir.Acquire(ctx, d.ID(), directory.AcquireOpts{})
	if err != nil {
		return 0, created
	}
	d.Fence, d.Head = v.Cell.Lease.Fence, v.Cell.Head
	if d.Head == head {
		return d.Fence, nil
	}
	return p.move(ctx, d, head, in, frames)
}

func (p *Publisher) create(ctx context.Context, d Driving, head string, in PublishInfo, frames []string) (uint64, error) {
	v, err := p.Dir.Create(ctx, d.ID(), directory.CellInit{
		Head: head, Class: in.Class, Title: in.Title, Size: in.Size, Keys: in.Keys,
		Frames: frames,
	})
	return v.Cell.Lease.Fence, err
}

// refusal names a stale fence as the person-facing ErrSuperseded.
func refusal(err error) error {
	if errors.Is(err, directory.ErrFenceStale) {
		return ErrSuperseded
	}
	return err
}
