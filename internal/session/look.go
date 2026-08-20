package session

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The LOOK STAMP: when somebody last stood in front of home.
//
// Home's one genuinely new answer over the world itself is the delta — "what
// landed while I was not looking" — and a delta needs an origin. This is that
// origin: one RFC3339 instant in a dotfile beside the project buckets, written
// when home closes and read when it opens. Work that finished after the stamp
// is news; work that finished before it has been seen.
//
// IT IS WRITTEN ON THE WAY OUT AND NOT ON THE WAY IN. A stamp taken when the
// screen opens would declare everything seen the moment it appeared, before a
// person's eye had crossed a single row; closing is the first instant "they
// looked" is actually true. A window that dies with home open writes nothing,
// and the next open marks the same work as news again — repeating news is the
// harmless direction to fail in.
//
// A machine with no stamp yet has no origin, and a delta with no origin is not
// a delta: the first look marks NOTHING as news, rather than everything. That
// is [LastLook] answering zero and every caller treating zero as "no claim".
const lookStampName = ".last-look"

// LastLook is when home was last closed over this places root, and zero when it
// never has been — or when the stamp is unreadable, which is the same fact for
// every caller: there is no origin to measure news from.
func LastLook(root string) time.Time {
	root = strings.TrimSpace(root)
	if root == "" {
		return time.Time{}
	}
	raw, err := os.ReadFile(filepath.Join(root, lookStampName))
	if err != nil {
		return time.Time{}
	}
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(raw)))
	if err != nil {
		return time.Time{}
	}
	return at
}

// NoteLook records that somebody is looking at home right now. Errors are
// dropped: the stamp is a convenience over a screen that works without it, and
// a read-only disk must not turn closing a dashboard into a fault.
func NoteLook(root string, at time.Time) {
	root = strings.TrimSpace(root)
	if root == "" || at.IsZero() {
		return
	}
	// The root may not exist yet on a machine that has held no conversation;
	// creating it for a stamp alone would invent state the reader then walks.
	if _, err := os.Stat(root); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(root, lookStampName),
		[]byte(at.UTC().Format(time.RFC3339Nano)+"\n"), 0o600)
}
