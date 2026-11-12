package session

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// trimAppWindowLocked drops oldest complete lines in a bounded batch. Keeping
// the live inode preserves its writer lease and durable id reservation. A line
// longer than the whole limit is cut by bytes so arbitrary output stays bounded.
func (s *jobSink) trimAppWindowLocked() bool {
	drop := s.chunkBytes / 8
	if drop < 1 {
		drop = 1
	}
	probe := make([]byte, 32<<10)
	boundary := int64(-1)
	for offset := drop; offset < s.spoolBytes; {
		size := min(int64(len(probe)), s.spoolBytes-offset)
		n, err := s.file.ReadAt(probe[:size], offset)
		if err != nil {
			s.breakSpoolLocked(err)
			return false
		}
		if i := bytes.IndexByte(probe[:n], '\n'); i >= 0 {
			boundary = offset + int64(i+1)
			break
		}
		offset += int64(n)
	}
	if boundary >= 0 {
		drop = boundary
	} else {
		// An unfinished tail may follow a few complete old lines. Remove those
		// first; only a line occupying the whole window needs a byte cut.
		last := int64(-1)
		for offset := int64(0); offset < drop; {
			size := min(int64(len(probe)), drop-offset)
			n, err := s.file.ReadAt(probe[:size], offset)
			if err != nil {
				s.breakSpoolLocked(err)
				return false
			}
			if i := bytes.LastIndexByte(probe[:n], '\n'); i >= 0 {
				last = offset + int64(i+1)
			}
			offset += int64(n)
		}
		if last > 0 {
			drop = last
		}
	}
	remaining := s.spoolBytes - drop
	if remaining < 0 {
		remaining = 0
	}
	buf := make([]byte, 32<<10)
	for offset := int64(0); offset < remaining; {
		size := int64(len(buf))
		if remaining-offset < size {
			size = remaining - offset
		}
		n, err := s.file.ReadAt(buf[:size], drop+offset)
		if err != nil {
			s.breakSpoolLocked(err)
			return false
		}
		written, err := s.file.WriteAt(buf[:n], offset)
		if err != nil || written != n {
			s.breakSpoolLocked(fmt.Errorf("process output trim: %w", errorsOrShort(err)))
			return false
		}
		offset += int64(n)
	}
	if err := s.file.Truncate(remaining); err != nil {
		s.breakSpoolLocked(err)
		return false
	}
	if _, err := s.file.Seek(remaining, io.SeekStart); err != nil {
		s.breakSpoolLocked(err)
		return false
	}
	s.spoolBytes = remaining
	s.noticeText = "log truncated: only the most recent " + spoolSizeText(s.chunkBytes) + " is kept"
	return true
}
func errorsOrShort(err error) error {
	if err != nil {
		return err
	}
	return io.ErrShortWrite
}

// localProcessURL finds a candidate loopback server address in output. It only
// finds; [LoopbackOrigin] decides, and the boundaries around the match are
// checked in [firstLoopbackURL].
var localProcessURL = regexp.MustCompile(`(?:https?://)?(?:localhost|127\.0\.0\.1):[0-9]+`)

// firstLoopbackURL is the first VALID loopback address in buf, normalized to its
// origin, or "". A match must start at a boundary (so `xlocalhost:3000`,
// `evil.com/localhost:3000` and `user@localhost:3000` are not addresses), must
// not run into more word characters, and must name a real port. A candidate
// that fails any of that is skipped, not fatal: a later one may be real.
// skipAtZero ignores a match at offset 0, whose left context is not in buf.
func firstLoopbackURL(buf []byte, skipAtZero bool) string {
	for _, at := range localProcessURL.FindAllIndex(buf, -1) {
		start, end := at[0], at[1]
		if start == 0 && skipAtZero {
			continue
		}
		if start > 0 && strings.IndexByte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-/@:", buf[start-1]) >= 0 {
			continue
		}
		if end < len(buf) && strings.IndexByte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_", buf[end]) >= 0 {
			continue
		}
		if origin, ok := LoopbackOrigin(string(buf[start:end])); ok {
			return origin
		}
	}
	return ""
}

// Capture the first address before compaction can discard it. A tiny overlap
// with the old ring covers an address split across stdout writes.
func (s *jobSink) captureProcessURLLocked(data []byte) {
	if s.url != "" {
		return
	}
	// The window is the last 128 bytes of the ring and the first 128 of the new
	// data, plus ONE byte of context before the window when the ring has more
	// behind it: the byte that decides whether a match at the window's edge
	// begins at a boundary. A match at offset 0 of a window that starts inside
	// the ring has no known left neighbour and is skipped; the one before it
	// was already judged when it was written.
	start := len(s.ring) - 128
	contextual := start > 0
	if start < 0 {
		start = 0
	}
	if contextual {
		start--
	}
	end := len(data)
	if end > 128 {
		end = 128
	}
	overlap := append(append([]byte{}, s.ring[start:]...), data[:end]...)
	if url := firstLoopbackURL(overlap, contextual); url != "" {
		s.url = url
		return
	}
	s.url = firstLoopbackURL(data, len(s.ring) > 0)
}
func (s *jobSink) processURL() string { s.mu.Lock(); defer s.mu.Unlock(); return s.url }
