package enginehost

// binary.go is the host's memory of the file it was started from.
//
// ── WHY A PROCESS WATCHES ITS OWN BINARY ────────────────────────────────────
//
// A host outlives the connection that started it, and that is the feature. It
// also outlives the BUILD that started it, and that is a trap: `rm bin/aforge
// && make build` on a machine leaves the new binary answering `aforge version`
// while the old process is still holding the socket, so every surface that
// dials in is spliced onto a build nobody has been running for an hour. When
// the protocol moved with that build, what the person got was the old host's
// refusal — telling them to update a machine they had just updated.
//
// The exchange in internal/remote fixes the connection that finds it. This
// fixes the one that never comes: a host whose binary was replaced RETIRES ON
// ITS OWN as soon as it is holding nothing, so a stale one drains away rather
// than waiting for somebody to trip over it. There is nothing to lose by
// leaving — the next connection starts a host from the binary that is actually
// on disk, which takes about a second and is invisible.
//
// THE TEST IS THE FILE'S IDENTITY AND NOT ITS CONTENT. os.SameFile is the
// standard library's own answer to "is this the same file", and an install that
// removes and replaces (the only install this tree allows — CLAUDE.md's own
// standing order, because a copy over a running binary is what macOS answers
// with `Killed: 9`) mints a new inode every time. The size and the modification
// time are checked beside it so that a copy in place, which keeps the inode, is
// caught too.
//
// A MACHINE THAT CANNOT ANSWER "WHICH FILE AM I" SIMPLY DOES NOT HAVE THIS.
// os.Executable fails on a few platforms and can be lied to on more; a host
// that could not learn its own path never claims to have been replaced, so the
// capability is absent rather than present and guessing.

import "os"

// hostBinary is the file this host was started from, as it was at the moment it
// started.
type hostBinary struct {
	path string
	was  os.FileInfo
}

// thisBinary reads the running file's identity. Both failures — no path, no
// stat — are answered the same way, with a value whose [hostBinary.replaced] is
// forever false.
func thisBinary() hostBinary {
	path, err := os.Executable()
	if err != nil {
		return hostBinary{}
	}
	was, err := os.Stat(path)
	if err != nil {
		return hostBinary{}
	}
	return hostBinary{path: path, was: was}
}

// replaced reports that the file this host was started from is not the file
// that is there now: removed and rebuilt, copied over, or gone entirely.
func (b hostBinary) replaced() bool {
	if b.path == "" || b.was == nil {
		return false
	}
	now, err := os.Stat(b.path)
	if err != nil {
		// The path no longer resolves to a file at all, which on Linux is also
		// what a running process whose binary was deleted reads as: os.Executable
		// answered `/…/bin/aforge (deleted)` and nothing stats that.
		return true
	}
	return !os.SameFile(b.was, now) ||
		!now.ModTime().Equal(b.was.ModTime()) ||
		now.Size() != b.was.Size()
}
