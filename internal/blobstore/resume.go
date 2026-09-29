package blobstore

import (
	"context"
	"errors"
	"sync"
)

// Resuming wraps a Store so that a frame the far side already holds is never
// sent twice. Two things make a frame arrive more than once: a put that timed
// out after the relay had in fact stored it, and a flush that failed on a later
// frame and so starts over from the first. Puts are idempotent, so both are
// correct but cost every byte again, which on a large first publish is many
// times the folder. Resuming removes both: it remembers what it delivered, and
// after a transport failure it asks the store whether the frame landed anyway.
type Resuming struct {
	Inner Store

	mu     sync.Mutex
	landed map[FrameID]bool
}

var _ Store = (*Resuming)(nil)

// NewResuming wraps inner.
func NewResuming(inner Store) *Resuming {
	return &Resuming{Inner: inner, landed: map[FrameID]bool{}}
}

// PutFrame sends frame unless this store has already taken it.
func (r *Resuming) PutFrame(ctx context.Context, frame []byte) (FrameID, error) {
	id := IDOf(frame)
	if r.isLanded(id) {
		return id, nil
	}
	_, err := r.Inner.PutFrame(ctx, frame)
	if errors.Is(err, ErrUnreachable) && r.holds(ctx, frame) {
		err = nil
	}
	if err != nil {
		return "", err
	}
	r.markLanded(id)
	return id, nil
}

// holds asks the store, in batches the wire allows, whether it has every object
// of frame. Any failure to find out answers false: the caller then reports the
// original error and the frame is sent again, which costs bytes but not safety.
func (r *Resuming) holds(ctx context.Context, frame []byte) bool {
	h, _, err := Decode(frame)
	if err != nil {
		return false
	}
	rids := make([]string, len(h.Objects))
	for i, o := range h.Objects {
		rids[i] = o.RID
	}
	for len(rids) > 0 {
		n := min(MaxHas, len(rids))
		have, err := r.ask(ctx, rids[:n])
		if err != nil || !allTrue(have) {
			return false
		}
		rids = rids[n:]
	}
	return true
}

// confirmAttempts is how many times a question about a possibly landed frame
// is asked. A relay that is still writing the frame answers late, and a second
// ask is far cheaper than sending the frame again.
const confirmAttempts = 3

// ask is Has, asked again while the answer does not arrive.
func (r *Resuming) ask(ctx context.Context, rids []string) ([]bool, error) {
	have, err := r.Inner.Has(ctx, rids)
	for i := 1; i < confirmAttempts && errors.Is(err, ErrUnreachable); i++ {
		have, err = r.Inner.Has(ctx, rids)
	}
	return have, err
}

func allTrue(have []bool) bool {
	for _, h := range have {
		if !h {
			return false
		}
	}
	return true
}

func (r *Resuming) isLanded(id FrameID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.landed[id]
}

func (r *Resuming) markLanded(id FrameID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.landed[id] = true
}

// Get implements Store.
func (r *Resuming) Get(ctx context.Context, rid string) ([]byte, error) {
	return r.Inner.Get(ctx, rid)
}

// Has implements Store.
func (r *Resuming) Has(ctx context.Context, rids []string) ([]bool, error) {
	return r.Inner.Has(ctx, rids)
}
