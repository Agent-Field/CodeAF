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
	ex, err := p.Upload(ctx, d.Cell, head)
	if err != nil {
		return Export{}, err
	}
	fence, err := p.advance(ctx, *d, head, in)
	if err != nil {
		return ex, err
	}
	d.Fence, d.Head = fence, head
	return ex, nil
}

// Upload exports what the store lacks for head, puts every frame, and only
// then records them as published. Objects are content-addressed, so what a
// refused publish uploaded is reused by whatever comes next.
func (p *Publisher) Upload(ctx context.Context, c cell.Cell, head string) (Export, error) {
	ex, err := p.Engine.Export(ctx, c, head)
	if err != nil {
		return Export{}, err
	}
	ex = p.skipHeld(ctx, c, ex)
	paths := make([]string, len(ex.Frames))
	for i, f := range ex.Frames {
		if err := p.put(ctx, f); err != nil {
			return Export{}, err
		}
		paths[i] = f.Path
	}
	if len(paths) == 0 {
		return ex, nil
	}
	return ex, p.Engine.Published(ctx, c, paths)
}

func (p *Publisher) put(ctx context.Context, f FrameFile) error {
	frame, err := os.ReadFile(f.Path)
	if err != nil {
		return err
	}
	_, err = p.Store.PutFrame(ctx, frame)
	return err
}

// advance moves the directory head and answers the fence now held.
func (p *Publisher) advance(ctx context.Context, d Driving, head string, in PublishInfo) (uint64, error) {
	if d.Fence == 0 {
		return p.create(ctx, d, head, in)
	}
	_, err := p.Dir.Publish(ctx, d.ID(), directory.Publish{
		Fence: d.Fence, OldHead: d.Head, Head: head,
		Size: in.Size, Class: in.Class, Title: in.Title, Pending: in.Pending,
	})
	return d.Fence, refusal(err)
}

func (p *Publisher) create(ctx context.Context, d Driving, head string, in PublishInfo) (uint64, error) {
	v, err := p.Dir.Create(ctx, d.ID(), directory.CellInit{
		Head: head, Class: in.Class, Title: in.Title, Size: in.Size, Keys: in.Keys,
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
