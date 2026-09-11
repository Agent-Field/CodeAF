// Package filememo is the one answer to "a file read at most once per change".
//
// THE SHAPE IT EXISTS FOR. A great many facts this program needs on the path of
// a keystroke or a turn live in a small file that almost never changes: the
// profile's config.json, which is read to answer whether a key is configured on
// EVERY Enter and whether this is the first prompt on EVERY turn; a project's
// AGENTS.md and CLAUDE.md, re-read whenever the system prompt's clock goes
// stale, which is precisely the came-back-from-lunch turn a person is already
// waiting on. Each of those was an os.ReadFile and a parse per call, and each
// was written separately because none of them looked expensive on its own.
//
// So there is ONE mechanism, and every one of those callers uses it: a [Memo]
// holds the derived value beside the reading of the file it came from, and
// spends one stat to decide whether it may answer. A stat is a syscall against a
// page the kernel has cached; a read-and-parse is the file's bytes through
// encoding/json and an allocation per key.
//
// WHAT MAKES AN ANSWER STALE, stated plainly because a memo that is wrong about
// this is worse than no memo at all:
//
//   - The file's modification time or its size differs from the reading the held
//     value came from. That is every ordinary write, including every write
//     through an atomic create-temp-and-rename, which lands a file with a fresh
//     mtime and usually a fresh size.
//   - The file appearing where there was none, or disappearing. Absence is a
//     value like any other and is held like one, so a profile with no config.json
//     — the ordinary state of a fresh install — is answered without a read too.
//   - A caller-supplied stamp moving ([Memo.Stamped]). It is how a package whose
//     own writer runs in this process says "that was me": config's every
//     persisted write bumps a counter, and reading that counter into the
//     freshness key means this process can never serve its own stale write, on
//     any filesystem, whatever its timestamp granularity.
//
// AND WHAT IT DOES NOT SEE: a file rewritten by ANOTHER process to the same size
// within one tick of the filesystem's timestamp resolution. On every filesystem
// this program runs on that tick is a nanosecond. It is written down here rather
// than guarded against, because guarding against it means reading the file.
package filememo

import (
	"os"
	"sync"
	"time"
)

// Derive turns one file's bytes into the value a caller wanted. It is handed the
// path it read, the bytes, and whether the file was there at all — because a
// missing file is very often a legitimate answer (an empty settings object, no
// project instructions) rather than a failure.
type Derive[T any] func(path string, data []byte, missing bool) (T, error)

// Memo is a derived value per path, held until the file behind it changes.
//
// The zero value is not usable: a memo is made by [New], which is what binds it
// to the one derivation it will ever perform. A memo per derivation rather than
// a memo per package is deliberate — two callers that parse the same file into
// two different shapes must not be able to hand each other the wrong one.
//
// It is safe for concurrent use. The lock is held across the read, so two
// callers arriving on a cold memo at once perform one read rather than two;
// that is the right trade for files this small and this hot.
type Memo[T any] struct {
	derive Derive[T]
	stamp  func() uint64

	mu   sync.Mutex
	held map[string]reading[T]
}

// reading is one file as it was when its value was derived.
type reading[T any] struct {
	value   T
	err     error
	size    int64
	modTime time.Time
	missing bool
	stamp   uint64
}

// New binds a memo to its derivation.
func New[T any](derive Derive[T]) *Memo[T] {
	return &Memo[T]{derive: derive}
}

// Stamped is [New] with an in-process invalidator: a counter the caller's own
// writer bumps, read into every freshness decision.
//
// IT IS THE ANSWER TO TIMESTAMP GRANULARITY AND NOT A SECOND CACHE. A package
// that writes the file it reads knows exactly when it did so, and a memo that
// reads that knowledge cannot serve its own writer a stale value even where the
// filesystem's clock is too coarse to tell two writes apart.
func Stamped[T any](stamp func() uint64, derive Derive[T]) *Memo[T] {
	return &Memo[T]{derive: derive, stamp: stamp}
}

// Read answers from the file at path, reading it only if it has changed since
// the held value was derived.
//
// An error from the derivation is held like a value. Retrying a parse failure on
// every call would be a read per call for a file that is broken until somebody
// fixes it, which is the exact cost this type exists to remove.
func (m *Memo[T]) Read(path string) (T, error) {
	now, missing := os.Stat(path)
	var size int64
	var modTime time.Time
	if missing == nil {
		size, modTime = now.Size(), now.ModTime()
	}
	absent := missing != nil
	stamp := uint64(0)
	if m.stamp != nil {
		stamp = m.stamp()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if held, known := m.held[path]; known && held.fresh(size, modTime, absent, stamp) {
		return held.value, held.err
	}

	var data []byte
	if !absent {
		bytes, err := os.ReadFile(path)
		if err != nil {
			// The file went away between the stat and the read, or became
			// unreadable. NOTHING IS HELD FOR IT: the reading this memo would
			// key on is the one that just turned out to be a lie, so the next
			// call stats again and finds whatever is true then.
			return m.derive(path, nil, true)
		}
		data = bytes
	}
	value, err := m.derive(path, data, absent)
	if m.held == nil {
		m.held = make(map[string]reading[T], 4)
	}
	m.held[path] = reading[T]{
		value: value, err: err,
		size: size, modTime: modTime, missing: absent, stamp: stamp,
	}
	return value, err
}

// Forget drops everything held. It exists for the tests that move a file under a
// memo faster than a filesystem can stamp it, and for a caller that has just
// learned its whole world moved. NOTHING ON A TURN'S PATH CALLS IT.
func (m *Memo[T]) Forget() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.held = nil
}

func (r reading[T]) fresh(size int64, modTime time.Time, missing bool, stamp uint64) bool {
	if r.stamp != stamp || r.missing != missing {
		return false
	}
	if missing {
		return true
	}
	return r.size == size && r.modTime.Equal(modTime)
}
