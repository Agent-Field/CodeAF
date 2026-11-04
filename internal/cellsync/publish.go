package cellsync

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// ErrSuperseded means another device took the lease while this one was
// publishing: the head did not move and the caller must branch (L7, L12).
var ErrSuperseded = fmt.Errorf("cellsync: another device continued this chat: %w", directory.ErrFenceStale)

// Publisher makes turns durable: objects into the store first, the head in the
// directory last. Dir is already bound to this device.
type Publisher struct {
	// CheckHeld makes even a small independent upload ask which objects the
	// relay lacks. Task-only cells use it because they have no directory head
	// from which to assume what another sender has already published.
	CheckHeld bool
	// WarmFrameHints opts into the bounded existing-frame prototype.
	WarmFrameHints bool
	Engine         Engine
	Store          blobstore.Store
	Dir            directory.Client
	// OnUpload hears how far an upload has got, in frame bytes: (0, total)
	// when it starts, after each frame, and (0, 0) when it ends either way.
	OnUpload func(sent, total int64)
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
	// Size is the record's size: the bytes of the frames the plan names, which
	// is what a computer without the chat copies. 0 is unknown, and a plan
	// that started before sizes were kept stays unknown.
	Size uint64
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
	hints        []directory.FrameHint
	savedAt      int64
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
	var err error
	in.savedAt, err = cellstore.SavedAt(d.Cell, head)
	if err != nil {
		return Export{}, err
	}
	ex, frames, err := p.uploadPlan(ctx, d.Cell, head, false)
	if err != nil {
		return Export{}, err
	}
	if p.WarmFrameHints {
		in.hints = latestFrameHints(ex.Frames, frames)
	}
	in.Size = planSize(*d, ex.Frames)
	frames = unionFrames(d.Frames, frames)
	fence, err := p.advance(ctx, *d, head, in, frames)
	if err != nil {
		return ex, syncwords.UploadFailure(err)
	}
	d.Fence, d.Head, d.Frames, d.Size = fence, head, frames, in.Size
	return ex, nil
}

// planSize is the record's size once this publish's frames join the plan: the
// bytes of every frame the plan names, so a move can say how much it copies.
// A new record's plan is exactly its first frames; a plan whose size is known
// grows by what was put; a plan begun before sizes were kept stays unknown
// rather than counting only its latest frames.
func planSize(d Driving, put []FrameFile) uint64 {
	var bytes uint64
	for _, f := range put {
		bytes += uint64(max(f.Bytes, 0))
	}
	switch {
	case d.Fence == 0 && len(d.Frames) == 0:
		return bytes
	case d.Size > 0:
		return d.Size + bytes
	}
	return 0
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
// then records them as published. An upload cut short still records the frames
// it put, and answers the export so the caller knows its size. Objects are content-addressed, so what a
// refused publish uploaded is reused by whatever comes next.
func (p *Publisher) Upload(ctx context.Context, c cell.Cell, head string) (Export, error) {
	ex, _, err := p.uploadPlan(ctx, c, head, true)
	return ex, err
}

// uploadPlan is Upload that also answers the ids of the frames it put, which
// is what the plan for the head names (contract §22.2). Frames skipHeld
// dropped are not named: their ids are whatever earlier upload carried them,
// which this publish does not know, and the take's want loop covers them.
//
// keep records the frames of an upload cut short (keepSent). Only Upload, which
// names no plan, keeps them: Publish and Branch name the frames they put in
// the head's plan, and a frame recorded by an attempt that failed would be
// left out of the next export and so out of the plan.
func (p *Publisher) uploadPlan(ctx context.Context, c cell.Cell, head string, keep bool) (Export, []string, error) {
	ex, err := p.Engine.Export(ctx, c, head)
	if err != nil {
		return Export{}, nil, err
	}
	ex = p.skipHeld(ctx, c, ex)
	ids := make([]string, 0, len(ex.Frames))
	paths := make([]string, len(ex.Frames))
	var sent, total int64
	for _, f := range ex.Frames {
		total += max(f.Bytes, 0)
	}
	if p.OnUpload != nil && total > 0 {
		p.OnUpload(0, total)
		defer p.OnUpload(0, 0)
	}
	for i, f := range ex.Frames {
		id, actualBytes, err := p.put(ctx, f)
		if err != nil {
			if keep {
				p.keepSent(ctx, c, paths[:i])
			}
			return ex, nil, err
		}
		ex.Frames[i].Bytes = actualBytes
		ids = append(ids, id)
		paths[i] = f.Path
		if sent += max(f.Bytes, 0); p.OnUpload != nil && total > 0 {
			p.OnUpload(sent, total)
		}
	}
	if len(paths) == 0 {
		return ex, ids, nil
	}
	return ex, ids, p.Engine.Published(ctx, c, paths)
}

// keepSent records the frames an interrupted upload already put. They are in
// the store whatever happens to the rest, and recording them lets the next
// export pack only what is still missing, so a large chat that runs out of time
// resumes where it stopped instead of sending everything again. The head does
// not move: only Publish or Create does that, after every frame is in.
func (p *Publisher) keepSent(ctx context.Context, c cell.Cell, paths []string) {
	if len(paths) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cellstore.SealWithin)
	defer cancel()
	_ = p.Engine.Published(ctx, c, paths)
}

// put stores one frame and answers the id the store named it, which is what
// the plan records: the store's own name for the frame, not a local guess.
func (p *Publisher) put(ctx context.Context, f FrameFile) (string, int64, error) {
	frame, err := os.ReadFile(f.Path)
	if err != nil {
		return "", 0, err
	}
	id, err := p.Store.PutFrame(ctx, frame)
	return id, int64(len(frame)), syncwords.UploadFailure(err)
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
		SavedAt: in.savedAt,
		Size:    in.Size, Class: in.Class, Title: in.Title, Pending: in.Pending,
		Frames: frames, FrameHints: in.hints,
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
		SavedAt: in.savedAt,
		Frames:  frames, FrameHints: in.hints,
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
