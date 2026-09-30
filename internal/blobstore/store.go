package blobstore

import (
	"context"
	"errors"
	"fmt"
)

// Store holds encrypted objects, addressed only by content (L5). It never sees
// plaintext (L6). Implementations: Memory (fake), Disk, HTTP, S3.
type Store interface {
	// PutFrame stores every object in one frame. It validates the frame with
	// Decode first. Idempotent: an object already stored with identical bytes
	// is not an error; the same rid with different bytes is ErrConflict.
	PutFrame(ctx context.Context, frame []byte) (FrameID, error)
	// Get answers one object's bytes by remote id. ErrNotFound if absent.
	Get(ctx context.Context, rid string) ([]byte, error)
	// Has answers, per rid, whether the store holds it. len(rids) <= MaxHas.
	Has(ctx context.Context, rids []string) ([]bool, error)
	// GetMany answers the objects of the longest prefix of rids that fits in
	// one frame's worth of bytes (TargetFrame), in request order and at least
	// one, so a taker that wants thousands of objects asks once per frame and
	// not once per object. It stops early, with what it has, at the first
	// object that is absent; ErrNotFound only when rids[0] itself is. The
	// caller asks again for the rest. len(rids) <= MaxGetMany.
	GetMany(ctx context.Context, rids []string) ([]Object, error)
}

// FrameID is the lowercase hex SHA-256 of a whole frame.
type FrameID = string

const (
	// MaxHas bounds one Has request so a caller cannot make a store scan without limit.
	MaxHas = 1000
	// MaxGetMany bounds one GetMany request. It is sized so that the ids of a
	// request (about 16 KB) stay small beside the frame that answers it.
	MaxGetMany = 256
	// MaxFrame bounds a whole frame in bytes.
	MaxFrame = 16 << 20
	// MaxHeader bounds the JSON header of a frame in bytes.
	MaxHeader = 1 << 20
	// TargetFrame is the size at which writers flush a frame.
	TargetFrame = 1 << 20
)

var (
	ErrNotFound    = errors.New("blobstore: not found")
	ErrBadFrame    = errors.New("blobstore: bad frame")
	ErrBadRID      = errors.New("blobstore: bad remote id")
	ErrConflict    = errors.New("blobstore: rid exists with different bytes")
	ErrUnreachable = errors.New("blobstore: unreachable") // transport; callers degrade
	ErrFull        = errors.New("blobstore: full")        // quota or disk; retry later
	ErrTooMany     = errors.New("blobstore: too many rids")
	ErrDamaged     = errors.New("blobstore: stored data is damaged")
)

// ValidRID reports whether s is 64 lowercase hex characters.
func ValidRID(s string) bool { return isLowerHex(s, 64) }

// isLowerHex reports whether s is exactly n lowercase hex characters.
func isLowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// checkHas is the one place the Has argument rules live, so every
// implementation refuses the same requests the same way: too many ids is a
// caller bug (ErrTooMany), and an id that is not a remote id could never be stored.
func checkHas(rids []string) error {
	if len(rids) > MaxHas {
		return fmt.Errorf("%w: %d ids, the limit is %d", ErrTooMany, len(rids), MaxHas)
	}
	for _, rid := range rids {
		if !ValidRID(rid) {
			return fmt.Errorf("%w: %q", ErrBadRID, rid)
		}
	}
	return nil
}

// checkGet refuses an id that could never have been stored before any lookup
// touches memory or the disk with it; on Disk the id becomes a path, so this
// is also what keeps a hostile id from naming a file outside the store.
func checkGet(rid string) error {
	if !ValidRID(rid) {
		return fmt.Errorf("%w: %q", ErrBadRID, rid)
	}
	return nil
}

// checkMany is checkHas for GetMany: the same rules at GetMany's own bound, and no id twice,
// because the answer is a frame and a frame cannot hold one object twice.
func checkMany(rids []string) error {
	if len(rids) > MaxGetMany {
		return fmt.Errorf("%w: %d ids, the limit is %d", ErrTooMany, len(rids), MaxGetMany)
	}
	seen := make(map[string]bool, len(rids))
	for _, rid := range rids {
		if err := checkGet(rid); err != nil {
			return err
		}
		if seen[rid] {
			return fmt.Errorf("%w: %q asked for twice", ErrBadRID, rid)
		}
		seen[rid] = true
	}
	return nil
}
