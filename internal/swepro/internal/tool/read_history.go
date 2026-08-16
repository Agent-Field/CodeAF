package tool

// aforge-embed: D11 — diff-aware repeat-read detection.
//
// A coder leaf that re-reads a file it has already read, when the file has
// not changed since, is burning tokens on material it already holds. The
// measured pathology — one replicate at 1.3M tokens — was driven in part by
// exactly this: the model re-reading unchanged files, each read adding the
// full content to the transcript on the turn it happened.
//
// This file adds a per-session file-read fingerprint: the path, the
// modification time and size at the moment of the last read, and the
// offset/limit the read used. A subsequent read of the same path with the
// same offset/limit, against a file whose mtime and size have not moved, is
// answered with a one-line notice instead of the full content — making the
// repeat read cost-visible to the model. A read of a file that HAS changed
// since the last read is always allowed, which is the diff-aware allowance:
// the file changed, so the read is legitimate.

import (
	"fmt"
	"os"
	"sync"
)

// readHistoryState is a per-session map of file reads, shared across
// per-context clones of the Registry via a pointer.
type readHistoryState struct {
	mu    sync.Mutex
	reads map[string]readFingerprint
}

// readFingerprint is what a read remembered: the file's mtime and size at
// the moment it was read, and the offset/limit the read used.
type readFingerprint struct {
	mtimeNanos int64
	size       int64
	offset     int
	limit      int
}

// unchangedSince reports whether the file at path has the same mtime and
// size as the fingerprint records.
func (f readFingerprint) unchangedSince(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.ModTime().UnixNano() == f.mtimeNanos && info.Size() == f.size
}

// checkRepeatRead returns a one-line notice if the file at resolved was
// already read with the same offset/limit and has not changed since, and
// records the read either way. ok=true means the caller should return the
// notice as the tool result; ok=false means the read should proceed normally.
func (h *readHistoryState) checkRepeatRead(resolved string, offset, limit int) (notice string, ok bool) {
	if h == nil {
		return "", false
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", false
	}
	key := fmt.Sprintf("%s\x00%d\x00%d", resolved, offset, limit)
	h.mu.Lock()
	defer h.mu.Unlock()
	if existing, found := h.reads[key]; found && existing.unchangedSince(resolved) {
		return "This file is unchanged since you last read it — the content you already have is current. " +
			"If you need a different section, use a different offset. Re-reading an unchanged file wastes tokens.", true
	}
	h.reads[key] = readFingerprint{
		mtimeNanos: info.ModTime().UnixNano(),
		size:       info.Size(),
		offset:     offset,
		limit:      limit,
	}
	return "", false
}
