package blobstore

import (
	"context"
	"sync/atomic"
)

// Counters is a running tally of what one Store was asked to move. The fields
// are atomic so a batcher and a fetcher can share one tally.
type Counters struct {
	Puts, Gets, Has, BytesUp, BytesDown, ObjectsUp atomic.Int64
}

// Counting is a Store that counts every request it passes to Inner, whether or
// not the request succeeds: the relay counts what it received, and the two
// tallies only agree if a failed request is counted on both sides.
type Counting struct {
	Inner Store
	C     *Counters
}

var _ Store = Counting{}

// PutFrame counts one put, the frame's bytes, and the objects in its header.
func (c Counting) PutFrame(ctx context.Context, frame []byte) (FrameID, error) {
	c.C.Puts.Add(1)
	c.C.BytesUp.Add(int64(len(frame)))
	c.C.ObjectsUp.Add(objectsIn(frame))
	return c.Inner.PutFrame(ctx, frame)
}

// Get counts one get and the bytes it returned.
func (c Counting) Get(ctx context.Context, rid string) ([]byte, error) {
	c.C.Gets.Add(1)
	b, err := c.Inner.Get(ctx, rid)
	c.C.BytesDown.Add(int64(len(b)))
	return b, err
}

// GetMany counts one get however many objects answer it, and their bytes: the
// relay counts one request and the same bytes, so the tallies agree.
func (c Counting) GetMany(ctx context.Context, rids []string) ([]Object, error) {
	c.C.Gets.Add(1)
	got, err := c.Inner.GetMany(ctx, rids)
	c.C.BytesDown.Add(sizeOf(got))
	return got, err
}

// Has counts one request, however many rids it asks about.
func (c Counting) Has(ctx context.Context, rids []string) ([]bool, error) {
	c.C.Has.Add(1)
	return c.Inner.Has(ctx, rids)
}

// GetFrame counts one get and the frame's bytes.
func (c Counting) GetFrame(ctx context.Context, frame string) ([]byte, error) {
	c.C.Gets.Add(1)
	b, err := c.Inner.GetFrame(ctx, frame)
	c.C.BytesDown.Add(int64(len(b)))
	return b, err
}

// Locate counts one request, however many rids it asks about.
func (c Counting) Locate(ctx context.Context, rids []string) (map[string]Location, error) {
	c.C.Has.Add(1)
	return c.Inner.Locate(ctx, rids)
}

// objectsIn reads the object count from a frame's header. A frame that does
// not decode is counted as carrying none: the store will refuse it anyway.
func objectsIn(frame []byte) int64 {
	h, _, err := Decode(frame)
	if err != nil {
		return 0
	}
	return int64(len(h.Objects))
}
