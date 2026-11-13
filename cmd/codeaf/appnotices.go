package main

// ── codeaf app: THE ONE NOTICE EACH FAILURE EPISODE LEAVES IN ITS CHAT ─────
//
// A kept-running process's failure episode leaves one notice in the chat of
// the agent that started it (appservices.go). The notice is not in the
// conversation's journal — the journal is the model's transcript, and a line
// written into it between a person's turns is a line the model would then be
// told — so it is kept beside it, in a record of its own:
//
//	<app>/service-notices/<agent>.jsonl        one notice a line, appended
//	<app>/service-notices/<agent>.index.json   how far the record was read, and what it held
//
// THE RECORD IS CANONICAL AND NEVER CUT. A line is appended and synced, and
// nothing ever rewrites or drops a line that was finished, so a chat's history
// shows every episode it ever had, and each notice's link to its process's
// output — kept after a removal (internal/service/archive.go) — still opens.
// The only bytes this file ever takes back are the ones its own failed write
// left after the record's end in that same call; a fragment it finds there
// later — a crash in the middle of a write — is ended with a newline, never
// cut, so a notice that lacked only its newline is kept and anything else is
// one unreadable line a reading passes over.
//
// WHAT IS HELD IS BOUNDED. A page is read backwards from its cursor, one
// chunk at a time (appNoticeReader). An agent's index holds the newest episode
// of at most appNoticeServices of its services, and at most appNoticeOwners
// agents' indexes are held at once; a reading of the record holds one line.
//
// THE INDEX IS ONLY A SHORTCUT, and it says how far it goes: the offset of the
// end of the last line it has counted. Every question first reads whatever
// the record holds past that offset — after a crash between a line and its
// index, an index that could not be written, or a sync that failed after the
// line reached the file — so a line on the disk is always counted before a
// second one could be written. An index that is missing, unreadable, too
// long, or that does not end at a line's end of the record is thrown away and
// the record is read again from its start. A service the index had to let go
// of to stay bounded is looked for in the whole record. Each of those reads is
// linear in the record's length and holds one line at a time; it is paid in
// the caller, which is the supervisor's listener and the app's start.
//
// One app writes these files. Two processes appending to one record at once,
// or another program putting a FIFO where a record is between the look and
// the open, are not defended against.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

const (
	appNoticeFolder = "service-notices"
	// appNoticeChunk is how much of a record is read at a time.
	appNoticeChunk = 64 << 10
	// appNoticeLine is the longest line kept; a notice's words are a short
	// title, and a longer line is not one this file wrote.
	appNoticeLine = 8 << 10
	// appNoticeServices is how many services' newest episodes one agent's
	// index holds: four times the supervisor's sixteen a person may have
	// running (internal/service/spec.go), so only services long removed are
	// ever let go of.
	appNoticeServices = 64
	// appNoticeOwners is how many agents' indexes are held at once.
	appNoticeOwners = 32
	// appNoticeIndexMax is the longest index read; a full one is under 2KiB.
	appNoticeIndexMax = 16 << 10
	appNoticeIndexV   = 1
)

// appNoticeAgent is an agent id that can name a file: a conversation folder's
// own name.
var appNoticeAgent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// appServiceNotice is one episode's notice in its agent's chat.
type appServiceNotice struct {
	Agent   string    `json:"agent"`
	Ref     string    `json:"ref"`
	Episode string    `json:"episode"`
	Text    string    `json:"text"`
	At      time.Time `json:"at"`
}

// id is the notice's entry id, the same live and read back.
func (n appServiceNotice) id() string { return "svc" + n.Ref + "." + n.Episode }

func appServiceNoticeEntry(n appServiceNotice) appwire.Entry {
	footer := appFooter(n.id(), n.Agent, 0, appwire.FooterServiceTrouble, n.Text, n.At)
	footer.Ref = n.Ref
	return footer
}

// appEpisodeNumber is an episode id's service and number: "s3.12" is s3, 12.
func appEpisodeNumber(episode string) (string, int, bool) {
	local, number, ok := strings.Cut(episode, ".")
	n, err := strconv.Atoi(number)
	return local, n, ok && err == nil && n > 0 && appServiceLocal.MatchString(local)
}

// appLocalNumber is a service's own number: s12 is 12.
func appLocalNumber(local string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(local, "s"))
	return n
}

// appNoticeIndex is one agent's shortcut: every finished line before Offset
// is counted in Newest, except the services numbered Floor or below that
// were let go of to keep it bounded.
type appNoticeIndex struct {
	V      int            `json:"v"`
	Offset int64          `json:"offset"`
	Floor  int            `json:"floor,omitempty"`
	Newest map[string]int `json:"newest"`
	used   uint64
}

func newAppNoticeIndex() *appNoticeIndex {
	return &appNoticeIndex{V: appNoticeIndexV, Newest: map[string]int{}}
}

// valid says a read-back index is one this file could have written.
func (ix *appNoticeIndex) valid() bool {
	if ix.V != appNoticeIndexV || ix.Offset < 0 || ix.Floor < 0 || ix.Newest == nil || len(ix.Newest) > appNoticeServices {
		return false
	}
	for local, n := range ix.Newest {
		if n <= 0 || !appServiceLocal.MatchString(local) {
			return false
		}
	}
	return true
}

// count takes one episode in. Past the bound, the lowest-numbered other
// service is let go of — service numbers only grow, so that is the one
// longest gone — and the floor rises to it, which sends a later question
// about it to the whole record.
func (ix *appNoticeIndex) count(local string, number int) {
	if ix.Newest[local] >= number {
		return
	}
	ix.Newest[local] = number
	for len(ix.Newest) > appNoticeServices {
		low, lowN := "", 0
		for other := range ix.Newest {
			if n := appLocalNumber(other); other != local && (low == "" || n < lowN) {
				low, lowN = other, n
			}
		}
		delete(ix.Newest, low)
		ix.Floor = max(ix.Floor, lowN)
	}
}

// appLineEpisode is a record line's service and episode number, read without
// its words or its time, and false for a line that is not this agent's notice.
func appLineEpisode(agent string, line []byte) (string, int, bool) {
	var n struct{ Agent, Episode string }
	if json.Unmarshal(line, &n) != nil || n.Agent != agent {
		return "", 0, false
	}
	return appEpisodeNumber(n.Episode)
}

// countLine takes one line of the agent's record in, if it is a notice.
func (ix *appNoticeIndex) countLine(agent string, line []byte) {
	if local, number, ok := appLineEpisode(agent, line); ok {
		ix.count(local, number)
	}
}

// appNoticeLog is the records of every agent, under one folder.
type appNoticeLog struct {
	dir string
	// mu orders the writers and guards the held indexes.
	mu     sync.Mutex
	owners map[string]*appNoticeIndex
	tick   uint64
}

func newAppNoticeLog(dir string) *appNoticeLog {
	return &appNoticeLog{dir: dir, owners: map[string]*appNoticeIndex{}}
}

func (l *appNoticeLog) paths(agent string) (record, index string, err error) {
	if !appNoticeAgent.MatchString(agent) {
		return "", "", errors.New("not an agent id")
	}
	return filepath.Join(l.dir, agent+".jsonl"), filepath.Join(l.dir, agent+".index.json"), nil
}

// appOpenPlain opens a file only if it is a plain one. It looks before it
// opens, so a FIFO or a device left at the path is never opened (and never
// waited on), and checks after, so what was opened is what was looked at.
func appOpenPlain(path string, flag int) (*os.File, error) {
	before, err := os.Lstat(path)
	if err == nil && !before.Mode().IsRegular() {
		return nil, errors.New("not a plain file: " + path)
	}
	if err != nil && (!errors.Is(err, os.ErrNotExist) || flag&os.O_CREATE == 0) {
		return nil, err
	}
	file, err := os.OpenFile(path, flag, 0o600)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err == nil && (!after.Mode().IsRegular() || (before != nil && !os.SameFile(before, after))) {
		err = errors.New("not a plain file: " + path)
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

// readIndex is an agent's index from its file, or nil. Never more than
// appNoticeIndexMax is read, however large the file is.
func readIndex(path string) *appNoticeIndex {
	file, err := appOpenPlain(path, os.O_RDONLY)
	if err != nil {
		return nil
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, appNoticeIndexMax+1))
	if err != nil || len(raw) > appNoticeIndexMax {
		return nil
	}
	ix := &appNoticeIndex{}
	if json.Unmarshal(raw, ix) != nil || !ix.valid() {
		return nil
	}
	return ix
}

// heldLocked is an agent's index, held, read from its file, or new; the
// agent least recently asked about is let go of past appNoticeOwners.
func (l *appNoticeLog) heldLocked(agent, indexPath string) *appNoticeIndex {
	l.tick++
	ix := l.owners[agent]
	if ix == nil {
		if ix = readIndex(indexPath); ix == nil {
			ix = newAppNoticeIndex()
		}
		l.owners[agent] = ix
		for len(l.owners) > appNoticeOwners {
			oldest := ""
			for other, held := range l.owners {
				if other != agent && (oldest == "" || held.used < l.owners[oldest].used) {
					oldest = other
				}
			}
			delete(l.owners, oldest)
		}
	}
	ix.used = l.tick
	return ix
}

// appScanLines calls fn with each finished line in [from, to), one held at a
// time, and answers the offset just past the last newline it read. A line
// longer than appNoticeLine is passed over without being held.
func appScanLines(file *os.File, from, to int64, fn func([]byte)) (int64, error) {
	r := bufio.NewReaderSize(io.NewSectionReader(file, from, to-from), appNoticeChunk)
	end, at, long := from, from, false
	for {
		part, err := r.ReadSlice('\n')
		at += int64(len(part))
		if errors.Is(err, bufio.ErrBufferFull) {
			long = true
			continue
		}
		if err == nil {
			if !long && len(part)-1 <= appNoticeLine {
				fn(part[:len(part)-1])
			}
			end, long = at, false
			continue
		}
		if errors.Is(err, io.EOF) {
			return end, nil
		}
		return end, err
	}
}

// caughtUpLocked opens an agent's record and brings its index up to the
// record's end, or answers a nil file when there is no record. When repair is
// set, an unfinished last line is ended with a newline and counted too, so a
// line appended next starts a line of its own.
//
// BYTES ARE COUNTED ONLY AFTER A SYNC SUCCEEDS, so nothing is called kept
// that the disk was not asked to keep; a sync that fails is the caller's
// error, and nothing is appended past bytes that were not counted.
func (l *appNoticeLog) caughtUpLocked(agent string, repair bool) (*appNoticeIndex, *os.File, error) {
	record, indexPath, err := l.paths(agent)
	if err != nil {
		return nil, nil, err
	}
	ix := l.heldLocked(agent, indexPath)
	flag := os.O_RDWR | os.O_APPEND
	if repair {
		if err := os.MkdirAll(l.dir, 0o700); err != nil {
			return nil, nil, err
		}
		flag |= os.O_CREATE
	}
	_, statErr := os.Lstat(record)
	file, err := appOpenPlain(record, flag)
	if errors.Is(err, os.ErrNotExist) && !repair {
		if ix.Offset != 0 || len(ix.Newest) > 0 {
			*ix = appNoticeIndex{V: appNoticeIndexV, Newest: map[string]int{}, used: ix.used}
		}
		return ix, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if errors.Is(statErr, os.ErrNotExist) {
		appSyncDir(l.dir)
	}
	fail := func(err error) (*appNoticeIndex, *os.File, error) {
		_ = file.Close()
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	size := info.Size()
	// AN INDEX THAT DOES NOT END AT A LINE'S END OF THIS RECORD is not this
	// record's, and the record is counted again from its start.
	if ix.Offset > size || (ix.Offset > 0 && !appNewlineAt(file, ix.Offset-1)) {
		*ix = appNoticeIndex{V: appNoticeIndexV, Newest: map[string]int{}, used: ix.used}
	}
	if size == ix.Offset {
		return ix, file, nil
	}
	if err := file.Sync(); err != nil {
		return fail(err)
	}
	end, err := appScanLines(file, ix.Offset, size, func(line []byte) { ix.countLine(agent, line) })
	if err != nil {
		return fail(err)
	}
	ix.Offset = end
	if end < size && repair {
		// A FRAGMENT AFTER THE LAST NEWLINE is a write that never finished.
		// It is ended, not cut: it may be a whole notice short only its
		// newline, and it is then counted like any other line.
		var fragment []byte
		if size-end <= appNoticeLine {
			fragment = make([]byte, size-end)
			if _, err := file.ReadAt(fragment, end); err != nil {
				return fail(err)
			}
		}
		if _, err := file.Write([]byte{'\n'}); err != nil {
			return fail(err)
		}
		if err := file.Sync(); err != nil {
			return fail(err)
		}
		ix.countLine(agent, fragment)
		ix.Offset = size + 1
	}
	l.writeIndex(indexPath, ix)
	return ix, file, nil
}

// appNewlineAt says the record's byte at offset is a newline.
func appNewlineAt(file *os.File, offset int64) bool {
	b := make([]byte, 1)
	_, err := file.ReadAt(b, offset)
	return err == nil && b[0] == '\n'
}

// appSyncDir asks the disk to keep a folder's names; best-effort.
func appSyncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// knownLocked says the record already holds this episode, or a later one of
// its service. A service the index let go of is looked for in the record.
func (l *appNoticeLog) knownLocked(agent string, ix *appNoticeIndex, file *os.File, local string, number int) (bool, error) {
	if have, ok := ix.Newest[local]; ok || appLocalNumber(local) > ix.Floor {
		return have >= number, nil
	}
	if file == nil {
		return false, nil
	}
	have := 0
	_, err := appScanLines(file, 0, ix.Offset, func(line []byte) {
		if l, num, ok := appLineEpisode(agent, line); ok && l == local {
			have = max(have, num)
		}
	})
	if err != nil {
		return false, err
	}
	if have > 0 {
		ix.count(local, have)
	}
	return have >= number, nil
}

// Noticed says the episode's notice is already in the record. A record that
// cannot be read says no, and the Append that follows says why.
func (l *appNoticeLog) Noticed(n appServiceNotice) bool {
	local, number, ok := appEpisodeNumber(n.Episode)
	if !ok {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ix, file, err := l.caughtUpLocked(n.Agent, false)
	if err != nil {
		return false
	}
	if file != nil {
		defer file.Close()
	}
	known, err := l.knownLocked(n.Agent, ix, file, local, number)
	return err == nil && known
}

// Append writes one episode's notice, once, and answers nil only when the
// line is on the disk: a notice the record could not take is not called kept,
// and the next look at the episode tries again. A write that fails part-way
// takes its own bytes back; a sync that fails leaves the line uncounted, and
// the next look reads it back from the record before writing anything.
func (l *appNoticeLog) Append(n appServiceNotice) error {
	local, number, ok := appEpisodeNumber(n.Episode)
	if !ok {
		return errors.New("not an episode")
	}
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	if len(raw) >= appNoticeLine {
		return errors.New("the notice is too long")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ix, file, err := l.caughtUpLocked(n.Agent, true)
	if err != nil {
		return err
	}
	defer file.Close()
	if known, err := l.knownLocked(n.Agent, ix, file, local, number); err != nil || known {
		return err
	}
	line := append(raw, '\n')
	if wrote, err := appNoticeWrite(file, line); err != nil {
		if wrote > 0 {
			_ = file.Truncate(ix.Offset)
		}
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	ix.Offset += int64(len(line))
	ix.count(local, number)
	_, indexPath, _ := l.paths(n.Agent)
	l.writeIndex(indexPath, ix)
	return nil
}

// appNoticeWrite is the one write of a line, a seam a test can fail.
var appNoticeWrite = func(file *os.File, line []byte) (int, error) { return file.Write(line) }

// writeIndex writes an index whole, beside it and renamed over it, private to
// the person. A failure leaves the old one, which the next question reads
// past.
//
// IT IS NOT SYNCED, and need not be: every line it counts was synced before
// it was written, so it is never ahead of what the disk keeps, and an index a
// crash left empty or torn is refused and the record counted again. Syncing
// it would triple the cost of every notice for nothing a reading relies on.
func (l *appNoticeLog) writeIndex(path string, ix *appNoticeIndex) {
	raw, err := json.Marshal(ix)
	if err != nil {
		return
	}
	temp, err := os.CreateTemp(l.dir, filepath.Base(path)+".*")
	if err != nil {
		return
	}
	_, writeErr := temp.Write(raw)
	closeErr := temp.Close()
	if errors.Join(writeErr, closeErr) != nil || fsreplace.Rename(temp.Name(), path) != nil {
		_ = os.Remove(temp.Name())
	}
}

// appNoticeReader reads one record backwards, a chunk at a time.
type appNoticeReader struct {
	file  *os.File
	pos   int64
	carry []byte
	// lines are the complete lines of the chunk being read, oldest first.
	lines [][]byte
	peek  *appServiceNotice
}

// Reader opens an agent's record for reading from its end, or nil when it
// has none.
func (l *appNoticeLog) Reader(agent string) (*appNoticeReader, error) {
	record, _, err := l.paths(agent)
	if err != nil {
		return nil, nil
	}
	file, err := appOpenPlain(record, os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	r := &appNoticeReader{file: file, pos: info.Size()}
	// A last line still being written is not a line yet.
	r.fill(true)
	return r, nil
}

func (r *appNoticeReader) Close() { _ = r.file.Close() }

// fill reads the chunk before what has been read, until it has a line or the
// record's start.
func (r *appNoticeReader) fill(first bool) {
	for len(r.lines) == 0 && (r.pos > 0 || len(r.carry) > 0) {
		if r.pos == 0 {
			r.lines, r.carry = [][]byte{r.carry}, nil
			break
		}
		start := max(r.pos-appNoticeChunk, 0)
		chunk := make([]byte, r.pos-start, int(r.pos-start)+len(r.carry))
		if _, err := r.file.ReadAt(chunk, start); err != nil && !errors.Is(err, io.EOF) {
			r.pos, r.carry = 0, nil
			return
		}
		r.pos = start
		data := append(chunk, r.carry...)
		parts := bytes.Split(data, []byte{'\n'})
		if first {
			// What follows the last newline is a line not yet finished.
			parts, first = parts[:len(parts)-1], false
		}
		if r.pos > 0 && len(parts) > 0 {
			r.carry, parts = append([]byte(nil), parts[0]...), parts[1:]
			if len(r.carry) > appNoticeLine {
				r.carry = nil
			}
		} else {
			r.carry = nil
		}
		for _, p := range parts {
			if len(p) > 0 && len(p) <= appNoticeLine {
				r.lines = append(r.lines, p)
			}
		}
	}
}

// Prev is the notice before the last one taken, newest first, and false at
// the record's start. A line that does not read as a notice is passed over.
func (r *appNoticeReader) Prev() (appServiceNotice, bool) {
	if r.peek != nil {
		n := *r.peek
		r.peek = nil
		return n, true
	}
	for {
		r.fill(false)
		if len(r.lines) == 0 {
			return appServiceNotice{}, false
		}
		line := r.lines[len(r.lines)-1]
		r.lines = r.lines[:len(r.lines)-1]
		var n appServiceNotice
		if json.Unmarshal(line, &n) == nil && n.Ref != "" && n.Episode != "" {
			return n, true
		}
	}
}

// Peek is Prev without taking it.
func (r *appNoticeReader) Peek() (appServiceNotice, bool) {
	if r.peek != nil {
		return *r.peek, true
	}
	n, ok := r.Prev()
	if ok {
		r.peek = &n
	}
	return n, ok
}
