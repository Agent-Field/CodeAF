package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// ── one stamp per place ─────────────────────────────────────────────────────
//
// THE ONE STAMP ABOVE CANNOT ANSWER "TWO THINGS CHANGED IN MEMORY". It is one
// instant for the whole machine, written when home closes, so it means *since
// you last shut the screen* and nothing narrower. A tab bar that wore a count
// against it would say the same number about every place at once: open home,
// glance at the task list for ten minutes, and memory still claims two new
// things because the machine-wide stamp has not moved.
//
// So a place gets its own stamp, written when that place is left. Seven small
// instants in ONE small file rather than seven files, because they are read
// together — a tab bar asks about all of them on the same beat — and because
// seven dotfiles beside the project buckets is seven things a person has to
// wonder about.
//
// IT DOES NOT REPLACE THE STAMP ABOVE AND IT MUST NOT. [LastLook] is what the
// whole surface's "since you left" ledger is measured from, it is written by a
// path this file does not touch, and it goes on meaning exactly what it meant.
// A place with no stamp of its own falls back to nothing — zero, "no claim" —
// and a caller that wants the machine-wide origin instead asks for it by name.
//
// The same failure direction as the stamp above: a lost write, a torn file, two
// windows closing at once and one overwriting the other's map — every one of
// them costs a place its origin, and a place with no origin marks NOTHING as
// news rather than everything.

// looksStampName is the per-place file, beside [lookStampName]. It is JSON
// rather than one line per file because it is a map and JSON is what this
// codebase writes maps in.
const looksStampName = "looks.json"

// lookStamps is the file's shape. The map is nested under a key rather than
// being the document itself so that a later reader of this file can add a
// second fact about looking without every old file failing to decode.
type lookStamps struct {
	Places map[string]string `json:"places,omitempty"`
}

// looksMu serializes this process's read-modify-writes. Two PROCESSES are not
// serialized at all, and that is a deliberate bargain rather than an oversight:
// a lock file for a convenience stamp would be a lock every window has to take
// on the way out, and what a lost race costs is one place's origin — which is
// the same harmless direction [NoteLook] already fails in.
var looksMu sync.Mutex

// LastLookAt is when this place was last left over this places root, and zero
// when it never has been — or when the file is unreadable, or when the place is
// not in it. All four are one fact for every caller: there is no origin to
// measure this place's news from, so nothing in it is news.
//
// Places are plain strings — "home", "tasks", "standing", "memory", "spend",
// "search", "settings" — and NOT an enum here, because the list of places is the
// surface's to decide and a records package that held one would be the second
// place it was written down.
func LastLookAt(root, place string) time.Time {
	place = strings.ToLower(strings.TrimSpace(place))
	if strings.TrimSpace(root) == "" || place == "" {
		return time.Time{}
	}
	stamps := readLookStamps(root)
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(stamps.Places[place]))
	if err != nil {
		return time.Time{}
	}
	return at
}

// NoteLookAt records that somebody has just finished looking at one place.
// Errors are dropped for [NoteLook]'s reason, and a zero instant writes nothing:
// stamping a place with the zero time would be indistinguishable from never
// having stood in it.
func NoteLookAt(root, place string, at time.Time) {
	place = strings.ToLower(strings.TrimSpace(place))
	root = strings.TrimSpace(root)
	if root == "" || place == "" || at.IsZero() {
		return
	}
	// The root may not exist yet on a machine that has held no conversation;
	// creating it for a stamp alone would invent state the reader then walks
	// ([NoteLook] makes the same refusal).
	if _, err := os.Stat(root); err != nil {
		return
	}
	looksMu.Lock()
	defer looksMu.Unlock()
	stamps := readLookStamps(root)
	if stamps.Places == nil {
		stamps.Places = map[string]string{}
	}
	stamps.Places[place] = at.UTC().Format(time.RFC3339Nano)
	payload, err := json.Marshal(stamps)
	if err != nil {
		return
	}
	writeLookStamps(root, payload)
}

// writeLookStamps puts one whole document where [readLookStamps] will find it.
//
// WRITTEN WHOLE OR NOT AT ALL. The document is a read-modify-write of every
// place's stamp, so a process that died halfway through an in-place write would
// cost a person every origin they had rather than one — the rename is what
// keeps the failure the size of the thing that failed.
//
// AND EVERY WRITER GETS A TEMPORARY FILE OF ITS OWN, which is what makes that
// rename worth anything. A shared `looks.json.tmp` re-opened the door the
// rename closed: [looksMu] serializes one process and two windows are not
// serialized at all, so both could truncate-and-write the same temporary path
// and either could then rename a half-written or interleaved file onto
// looks.json — and a file that will not parse answers empty for EVERY place at
// once ([readLookStamps]), which is a failure the size of everything. With a
// private file the last writer wins and the cost is one place's origin, which
// is exactly the bargain [looksMu] states.
//
// The temporary file is made BESIDE the final one and never in the machine's
// temporary directory: a rename across two filesystems is not atomic, and on
// most of them is not a rename at all.
func writeLookStamps(root string, payload []byte) {
	temporary, err := os.CreateTemp(root, looksStampName+".*")
	if err != nil {
		return
	}
	name := temporary.Name()
	if _, err := temporary.Write(append(payload, '\n')); err != nil {
		_ = temporary.Close()
		_ = os.Remove(name)
		return
	}
	if temporary.Close() != nil {
		_ = os.Remove(name)
		return
	}
	if os.Rename(name, filepath.Join(root, looksStampName)) != nil {
		_ = os.Remove(name)
	}
}

// readLookStamps is the file or an empty one. Every failure — missing,
// unreadable, not JSON at all — answers empty, because a caller cannot act on
// the difference: in each case there is no origin for any place.
func readLookStamps(root string) lookStamps {
	var stamps lookStamps
	raw, err := os.ReadFile(filepath.Join(root, looksStampName))
	if err != nil {
		return lookStamps{}
	}
	if json.Unmarshal(raw, &stamps) != nil {
		return lookStamps{}
	}
	return stamps
}
