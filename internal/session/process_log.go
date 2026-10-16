package session

// A PROCESS'S OUTPUT IS READ BY BYTE OFFSET, WITHIN A BUDGET, FROM THE FILE ITS
// OWN RECORD NAMES.
//
// The line-numbered reader beside the app (processes.output) reaches line N by
// reading every line before it, so the cost of the next screenful grew with
// everything the process ever printed, and it opened the path with a call that
// follows links. This reader takes a byte offset, reads at most the asked
// number of bytes with one positioned read, cuts at a line end so a line is
// never shown half, and never accepts a path from anybody: the caller resolves
// the path from the owning conversation's record by the exact job number.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

var errNotRegular = errors.New("not a regular file")

// The log read's bounds.
const (
	ProcessLogDefaultBytes = 64 << 10
	ProcessLogMaxBytes     = 512 << 10
)

// ErrProcessLogStale is a read whose offset or revision no longer describes the
// file: it was replaced, or it is now shorter than the offset.
var ErrProcessLogStale = errors.New("the output changed under that offset; read it again from the end")

// ProcessLogAsk is one read. Offset nil reads the tail.
type ProcessLogAsk struct {
	Offset   *int64
	Limit    int
	Revision string
}

// ProcessLog is one read of a process's output.
type ProcessLog struct {
	Text       string
	Offset     int64
	Next       int64
	Size       int64
	EOF        bool
	MoreBefore bool
	// Gone says the record exists and its output does not: it expired after
	// the retention window or was never written. The record outlives it.
	Gone     bool
	Revision string
}

// CheckProcessLogAsk is the effective byte limit, or an error naming the bound.
func CheckProcessLogAsk(ask ProcessLogAsk) (int, error) {
	limit := ask.Limit
	if limit == 0 {
		limit = ProcessLogDefaultBytes
	}
	if limit < 1 || limit > ProcessLogMaxBytes {
		return 0, fmt.Errorf("limit_bytes is 1 to %d", ProcessLogMaxBytes)
	}
	if ask.Offset != nil && *ask.Offset < 0 {
		return 0, errors.New("offset is zero or more")
	}
	if len(ask.Revision) > 128 {
		return 0, errors.New("revision is one a previous read gave")
	}
	return limit, nil
}

// logRevision names the opened file: a log replaced by another file is a new
// revision; one compacted in place keeps its identity, and the record's
// OutputTruncated says older output was discarded.
func logRevision(info os.FileInfo) string {
	_, dev, ino := fileIdentity(info)
	identity := strconv.FormatUint(dev, 36) + ":" + strconv.FormatUint(ino, 36)
	if dev == 0 && ino == 0 {
		// No portable identity here; the modification time stands in.
		identity = strconv.FormatInt(info.ModTime().UnixNano(), 36)
	}
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:8])
}

// ReadProcessLog reads one window of the log at path. redact is applied to the
// text before it leaves; a nil redact leaves the text as written.
func ReadProcessLog(path string, ask ProcessLogAsk, redact func(string) string) (ProcessLog, error) {
	limit, err := CheckProcessLogAsk(ask)
	if err != nil {
		return ProcessLog{}, err
	}
	if strings.TrimSpace(path) == "" {
		return ProcessLog{Gone: true, EOF: true}, nil
	}
	file, err := openRegularNoFollow(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ProcessLog{Gone: true, EOF: true}, nil
	}
	if err != nil {
		return ProcessLog{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ProcessLog{}, err
	}
	size := info.Size()
	revision := logRevision(info)
	if ask.Revision != "" && ask.Revision != revision {
		return ProcessLog{}, ErrProcessLogStale
	}
	tail := ask.Offset == nil
	var start int64
	if tail {
		start = max(size-int64(limit), 0)
	} else {
		start = *ask.Offset
		if start > size {
			return ProcessLog{}, ErrProcessLogStale
		}
	}
	end := min(start+int64(limit), size)
	buf := make([]byte, end-start)
	if n, err := file.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		return ProcessLog{}, err
	} else {
		buf = buf[:n]
		end = start + int64(n)
	}
	// A TAIL BEGINS AT A LINE. The first bytes of a window that starts mid-file
	// are the end of a line whose beginning is not in it.
	if tail && start > 0 {
		if i := strings.IndexByte(string(buf), '\n'); i >= 0 && i+1 < len(buf) {
			buf, start = buf[i+1:], start+int64(i+1)
		}
	}
	// A WINDOW ENDS AT A LINE, unless one line is longer than the whole window;
	// then it is split, at a rune boundary, and the rest comes on the next read.
	// The bytes after the last newline at the end of the file are a line still
	// being written, and they are left for the next read too.
	if i := strings.LastIndexByte(string(buf), '\n'); i >= 0 {
		buf = buf[:i+1]
	} else if len(buf) == limit {
		for len(buf) > 0 && !utf8.Valid(buf) {
			buf = buf[:len(buf)-1]
		}
	} else {
		buf = buf[:0]
	}
	next := start + int64(len(buf))
	text := strings.ToValidUTF8(string(buf), "�")
	if redact != nil {
		text = redactLines(text, redact)
	}
	return ProcessLog{
		Text:       text,
		Offset:     start,
		Next:       next,
		Size:       size,
		EOF:        end >= size,
		MoreBefore: start > 0,
		Revision:   revision,
	}, nil
}

// redactLines applies a line-wise redactor over complete lines.
func redactLines(text string, redact func(string) string) string {
	lines := strings.SplitAfter(text, "\n")
	for i, line := range lines {
		body := strings.TrimRight(line, "\r\n")
		lines[i] = redact(body) + line[len(body):]
	}
	return strings.Join(lines, "")
}
