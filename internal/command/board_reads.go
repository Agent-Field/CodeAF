package command

import (
	"bytes"
	"io"
	"os"
	"strings"
)

// The work page's engine-side reads.
//
// Almost everything the work board draws is already a store read — the charters,
// the services, the per-job usage, the models that billed a run — and the window
// asks *store.Store for those directly, through the same optional-interface seam
// [chat.Ledger] uses. Nothing is restated here that the store can answer, because
// a second door onto one fact is how two surfaces end up disagreeing about it.
//
// What IS here is the one thing the store cannot answer: a service's log tail
// lives on the FILESYSTEM, at a path the store merely records. A terminal
// package opening an arbitrary path out of a render call is the layering
// inversion this file exists to avoid — the engine owns the process, so the
// engine owns the bytes it wrote.

const (
	// ServiceLogTailLines is how many lines a service's detail page shows.
	// It is v1's number (internal/tui/services.go) and it is a READING rather
	// than a log viewer: ten lines answers "is it saying anything sensible",
	// which is the whole question a detail page has, and a page that tried to
	// answer "what happened at 3am" would be a pager wearing a card's clothes.
	ServiceLogTailLines = 10
	// ServiceLogTailBytes bounds the read regardless of how long a line is. A
	// service that writes one 400MB line is a service, not an attack, and this
	// is what keeps the render path's cost a property of the surface rather
	// than of what somebody's process happened to print.
	ServiceLogTailBytes = 32 << 10
)

// ServiceLogTail is the last few lines a service wrote, newest LAST — the order
// they were written, which is the order a person reads a log in.
//
// Every failure is an empty answer and never an error row: a log that has not
// been created yet, a path that moved, a permission the window does not have,
// are all "there is nothing to show you here", and the detail page says that in
// one calm sentence. A card that rendered an errno would be spending its
// loudest row on the least of its facts.
//
// The read is BACKWARDS-BOUNDED rather than streamed: seek to at most
// [ServiceLogTailBytes] before the end and read forward. The first line of that
// window is dropped when the seek landed mid-file, because half a line is not a
// line — it is the middle of somebody's sentence presented as its beginning.
func (c *Commander) ServiceLogTail(path string, lines, maxBytes int) []string {
	return ServiceLogTail(path, lines, maxBytes)
}

// ServiceLogTail is the read itself. It is a function rather than only a method
// because it depends on nothing the commander holds — the path is the whole
// input — and a package-level door is what lets a test exercise the real read
// instead of a copy of it.
func ServiceLogTail(path string, lines, maxBytes int) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if lines <= 0 {
		lines = ServiceLogTailLines
	}
	if maxBytes <= 0 {
		maxBytes = ServiceLogTailBytes
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return nil
	}
	size := info.Size()
	offset := int64(0)
	if size > int64(maxBytes) {
		offset = size - int64(maxBytes)
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)))
	if err != nil || len(raw) == 0 {
		return nil
	}
	if offset > 0 {
		if cut := bytes.IndexByte(raw, '\n'); cut >= 0 {
			raw = raw[cut+1:]
		} else {
			// One line longer than the whole window. There is no line boundary
			// to trust, so there is no line to show.
			return nil
		}
	}
	return tailLines(string(raw), lines)
}

// tailLines is the last n non-empty-at-the-end lines of a blob, in written
// order. A trailing newline is a file that ended tidily and not a blank line
// somebody wrote, so it is dropped before counting.
func tailLines(blob string, n int) []string {
	blob = strings.TrimRight(blob, "\n")
	if blob == "" {
		return nil
	}
	all := strings.Split(blob, "\n")
	if len(all) > n {
		all = all[len(all)-n:]
	}
	out := make([]string, 0, len(all))
	for _, line := range all {
		out = append(out, strings.TrimRight(line, "\r"))
	}
	return out
}
