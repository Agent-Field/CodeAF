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
	"path/filepath"
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

// ProcessLogAsk is one read. Offset nil reads the tail. Final says the
// process has ended, so bytes after the last newline are its last line and
// not a line still being written.
type ProcessLogAsk struct {
	Offset   *int64
	Limit    int
	Revision string
	Final    bool
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
	Gone bool
	// Partial says the window reached the end of the file and left bytes after
	// the last newline for a later read, because the process may still be
	// writing that line. EOF is true only when Next is the file's size.
	Partial  bool
	Revision string
}

// errProcessLogUnmanaged is a record whose log path is not its conversation's
// own managed log for that job.
var errProcessLogUnmanaged = errors.New("this process's output is not in its conversation's logs folder")

// ProcessLogLocation is where a record's output may be read from: its
// conversation's folder, which the caller trusts, and the managed path below
// it, logs/jobs/<id>.log. A record whose LogPath spells anything else is
// refused — the record's bytes name a file, and only the file codeaf would
// have written for that job is opened. Empty means the record names no log.
func ProcessLogLocation(convDir string, n JobNotice) (anchor string, rel []string, err error) {
	if strings.TrimSpace(n.LogPath) == "" {
		return "", nil, nil
	}
	if strings.TrimSpace(convDir) == "" || n.ID <= 0 {
		return "", nil, errProcessLogUnmanaged
	}
	rel = []string{placeLogs, droppingJobs, strconv.Itoa(n.ID) + ".log"}
	want := filepath.Join(append([]string{convDir}, rel...)...)
	if filepath.Clean(n.LogPath) != filepath.Clean(want) {
		return "", nil, errProcessLogUnmanaged
	}
	return convDir, rel, nil
}

// ProcessLogFile is a managed log as it was opened just now: its path, size
// and revision ([logRevision], the same identity a log read answers), or that
// the record names none, or that the file it names is not there.
//
// THE PATH IS A FACT, NOT A GRANT. It says which file was verified; a reader
// that opens it later must open it again its own way and compare the revision,
// because the file can be replaced the instant this one is closed.
type ProcessLogFile struct {
	Path     string
	Size     int64
	Revision string
	NoLog    bool
	Gone     bool
}

// ProcessLogTarget is a record's managed log ([ProcessLogLocation]), opened
// the way [ReadProcessLog] opens it and closed again.
func ProcessLogTarget(convDir string, n JobNotice) (ProcessLogFile, error) {
	anchor, rel, err := ProcessLogLocation(convDir, n)
	if err != nil {
		return ProcessLogFile{}, err
	}
	if anchor == "" {
		return ProcessLogFile{NoLog: true}, nil
	}
	return ManagedLogFile(anchor, rel...)
}

// ManagedLogFile opens anchor/rel… with nothing below the trusted anchor
// followed through a link, a special file refused without blocking, and the
// opened file's identity checked against what was inspected; then it reads
// its size and revision and closes it. A file that is not there is Gone.
func ManagedLogFile(anchor string, rel ...string) (ProcessLogFile, error) {
	file, err := openRegularIn(anchor, rel...)
	if errors.Is(err, fs.ErrNotExist) {
		return ProcessLogFile{Gone: true}, nil
	}
	if err != nil {
		return ProcessLogFile{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ProcessLogFile{}, err
	}
	return ProcessLogFile{
		Path:     filepath.Join(append([]string{anchor}, rel...)...),
		Size:     info.Size(),
		Revision: logRevision(info),
	}, nil
}

// ErrProcessLogUnmanaged reports whether err is a refused log location.
func ErrProcessLogUnmanaged(err error) bool { return errors.Is(err, errProcessLogUnmanaged) }

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

// ReadProcessLog reads one window of a record's output from its managed
// location ([ProcessLogLocation]): nothing below the conversation's folder is
// followed through a link, and a special file is refused without blocking.
// redact is applied to the text before it leaves.
func ReadProcessLog(convDir string, n JobNotice, ask ProcessLogAsk, redact func(string) string) (ProcessLog, error) {
	limit, err := CheckProcessLogAsk(ask)
	if err != nil {
		return ProcessLog{}, err
	}
	anchor, rel, err := ProcessLogLocation(convDir, n)
	if err != nil {
		return ProcessLog{}, err
	}
	if anchor == "" {
		return ProcessLog{Gone: true, EOF: true}, nil
	}
	file, err := openRegularIn(anchor, rel...)
	if errors.Is(err, fs.ErrNotExist) {
		return ProcessLog{Gone: true, EOF: true}, nil
	}
	if err != nil {
		return ProcessLog{}, err
	}
	defer file.Close()
	return readLogWindow(file, ask, limit, redact)
}

func readLogWindow(file *os.File, ask ProcessLogAsk, limit int, redact func(string) string) (ProcessLog, error) {
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
	n, err := file.ReadAt(buf, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return ProcessLog{}, err
	}
	buf, end = buf[:n], start+int64(n)
	atEnd := end >= size
	// A TAIL BEGINS AT A LINE. The first bytes of a window that starts mid-file
	// are the end of a line whose beginning is not in it.
	if tail && start > 0 {
		if i := strings.IndexByte(string(buf), '\n'); i >= 0 && i+1 < len(buf) {
			buf, start = buf[i+1:], start+int64(i+1)
		}
	}
	partial := false
	switch i := strings.LastIndexByte(string(buf), '\n'); {
	case atEnd && ask.Final:
		// AN ENDED PROCESS'S LAST BYTES ARE ITS LAST LINE, newline or not:
		// `printf x` has no newline to wait for, and nothing will ever write one.
	case i >= 0:
		// A WINDOW ENDS AT A LINE. What follows the last newline is left for the
		// next read: the rest of a line longer than this window, or, at the end
		// of a running process's file, a line it is still writing.
		partial = atEnd && i+1 < len(buf)
		buf = buf[:i+1]
	case !atEnd:
		// ONE LINE LONGER THAN THE WHOLE WINDOW is split, at a character when it
		// is one — only an incomplete last character is held back, so invalid
		// bytes never stop the read from moving.
		buf = cutIncompleteRune(buf)
	default:
		partial = len(buf) > 0
		buf = buf[:0]
	}
	next := start + int64(len(buf))
	text := strings.ToValidUTF8(string(buf), "\uFFFD")
	if redact != nil {
		text = redactLines(text, redact)
	}
	return ProcessLog{
		Text:       text,
		Offset:     start,
		Next:       next,
		Size:       size,
		EOF:        next >= size,
		Partial:    partial,
		MoreBefore: start > 0,
		Revision:   revision,
	}, nil
}

// cutIncompleteRune drops a character cut short at the end of buf, and only
// that; a window too small to hold one character keeps its bytes so the read
// still advances.
func cutIncompleteRune(buf []byte) []byte {
	for back := 1; back <= utf8.UTFMax && back <= len(buf); back++ {
		c := buf[len(buf)-back]
		if utf8.RuneStart(c) {
			if !utf8.FullRune(buf[len(buf)-back:]) && back < len(buf) {
				return buf[:len(buf)-back]
			}
			return buf
		}
	}
	return buf
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
