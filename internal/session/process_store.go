package session

// A CONVERSATION'S PROCESS HISTORY IS ONE SMALL FILE PER JOB.
//
// WHAT WAS HERE BEFORE. The shelf was one JSON array, processes.json, beside
// the transcript. Every update of every job read the whole array and wrote the
// whole array back, and every reader read the whole array into memory. That is
// fine for a dozen jobs and wrong for a conversation that has run a server and
// a build a day for a month: the cost of one exit grew with the history, and a
// reader had no way to read "the newest fifty" without reading everything. It
// also never saw a task's processes at all — a task worker is its own agent,
// its SessionFile is a task journal, and its shelf landed in tasks/processes.json
// where nothing looked.
//
// WHAT IS HERE NOW. processes/<id>.json holds exactly one job, keyed by the job
// number the logs folder's persisted claim counter already hands out
// ([jobRetentionClaimAbove]). The conversation and its task workers share that
// logs folder, so the number is unique across all of them and a worker's job is
// written into the CONVERSATION's history under its own number. An update
// rewrites one file of bounded size. processes/head.json carries the highest
// number written (written BEFORE the job's own file, so a crash leaves at worst
// a hole, never a newest job no reader can find), an epoch that names this
// store for cursors, the legacy files already folded in, and how many records
// could not be kept — so a reader that runs out of history can say whether it
// ran out because there is no more or because some of it is unreadable.
//
// A READER WALKS NUMBERS DOWNWARDS. A page is "the records below this number",
// read one small file at a time, under a probe budget for holes (a task node's
// own registry entry takes a number and is never a record), a byte budget for
// the reply and a deadline. When any budget runs out the reader says where to
// carry on; it never claims to have reached the oldest record when it has not.
//
// THE OLD ARRAY IS FOLDED IN BY ITS OWNER AND LEFT WHERE IT IS. Only the engine
// that holds the conversation writes, and it streams the legacy array element
// by element into the store the first time it writes or pages, recording each
// file's size and modification time. An older build that still reads
// processes.json keeps reading what was there; a reader that sees a legacy file
// changed since it was folded in reports the history incomplete until the owner
// folds it again.

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The store's shape and budgets. Every one of them is a bound a reader can
// state, and PERF.md's rule applies: a cap that changes changes the contract.
const (
	processStoreName = "processes"
	processHeadName  = "head.json"
	// processRecordCap bounds one job's file. A command longer than the record
	// can hold is kept cut, and the record says so ([JobNotice.CommandCut]),
	// because a cut command run again would be a different command.
	processRecordCap = 64 << 10
	processHeadCap   = 64 << 10
	// processLegacyReadCap bounds what a reader that may not write reads of a
	// legacy array that has not been folded in yet.
	processLegacyReadCap = 16 << 20
	// processProbeBudget bounds how many numbers one walk looks at, holes
	// included.
	processProbeBudget = 4096
	// ProcessPageDefault and ProcessPageMax bound one page's history rows;
	// ProcessPageReplyBytes bounds the rows' encoded size.
	ProcessPageDefault    = 50
	ProcessPageMax        = 100
	ProcessPageReplyBytes = 4 << 20
	// processPageEnvelope is what a reply keeps for itself beyond its rows.
	processPageEnvelope = 64 << 10
	// processLegacyListMax bounds the legacy whole-list read
	// ([ReadProcessNotices], [Agent.JobNotices]); processes.page has no such
	// ceiling because it pages.
	processLegacyListMax = 1000
)

// Why a page row's state is unknown.
const (
	// ProcessUnknownNotHeld: the record says running and nothing holding the
	// conversation was asked, so nobody can say whether it still runs.
	ProcessUnknownNotHeld = "not_held"
	// ProcessUnknownOwnerLost: the engine holding the conversation has no such
	// live job. It ran under an earlier engine that ended without settling it;
	// its detached process may even still be alive.
	ProcessUnknownOwnerLost = "owner_lost"
)

var (
	errProcessCursor    = errors.New("that cursor is not one a page gave")
	ErrProcessStale     = errors.New("the process history changed; read it again from the first page")
	errProcessRecordBad = errors.New("process record is unreadable")
)

// processHead is head.json.
type processHead struct {
	Epoch    string       `json:"epoch"`
	High     int          `json:"high"`
	Migrated []legacyMark `json:"migrated,omitempty"`
	// Lost counts records that existed and could not be kept: a legacy element
	// over the record cap or past the legacy cap, a number reused with another
	// start. A page from a store with Lost above zero is incomplete.
	Lost int `json:"lost,omitempty"`
}

// legacyMark is one legacy array as it was when it was folded in.
//
// A FOLD IS RESUMABLE. Offset is the byte where the next element starts, so a
// fold cut by its time or byte budget carries on from there on the owner's next
// write or page instead of starting again or giving up; Done is set only when
// the closing bracket was reached. Corrupt says the array stops being JSON at
// Offset: nothing after that point can be read by anyone, the store counts it
// lost, and every page says incomplete — the tail is unreadable, not forgotten.
// A file whose size or modification time moved is folded again from byte 0;
// folding is idempotent per record.
type legacyMark struct {
	File    string `json:"file"`
	Size    int64  `json:"size"`
	MTime   int64  `json:"mtime"`
	Offset  int64  `json:"offset,omitempty"`
	Done    bool   `json:"done,omitempty"`
	Corrupt bool   `json:"corrupt,omitempty"`
}

// sameFile says a mark describes the file as it stands.
func (m legacyMark) sameFile(now legacyMark) bool {
	return m.File == now.File && m.Size == now.Size && m.MTime == now.MTime
}

// processLegacyFiles are the legacy arrays a conversation folder may hold, by
// their fixed names: the conversation's own, and the one every task worker of
// it wrote because its SessionFile was a task journal. They are named, never
// searched for.
func processLegacyFiles(dir string) []string {
	return []string{filepath.Join(dir, processLegacyNames[0]), filepath.Join(dir, processLegacyNames[1])}
}

// processLegacyNames are the legacy arrays' fixed names inside the folder,
// which is what a fold's mark is keyed by.
var processLegacyNames = [2]string{"processes.json", placeNodeJournals + "/processes.json"}

// processStore is one conversation's history, rooted at the conversation's
// folder.
//
// anchor, when set, is the conversation's folder already opened as a root (see
// [ReadProcessPageWithinRoot]): every READ then goes through it, so no component
// of the path above the folder can be swapped for a link mid-page.
type processStore struct {
	dir    string
	anchor *os.Root
}

func (s processStore) root() string { return filepath.Join(s.dir, processStoreName) }

func processFileName(id int) string { return strconv.Itoa(id) + ".json" }

// readSmall reads one regular file under the store of at most limit bytes,
// THROUGH THE ROOT ALREADY OPENED, so a store folder swapped for a link after
// it was opened cannot redirect the read. The name is opened without following
// a link and non-blocking ([rootReadFlags]), so a FIFO swapped in for a record
// never holds the caller (or the record lock it may hold); the opened file
// must be the regular file inspected, and a larger file is refused rather than
// read to the end.
func readSmall(root *os.Root, name string, limit int64) ([]byte, error) {
	seen, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !seen.Mode().IsRegular() {
		return nil, errNotRegular
	}
	file, err := root.OpenFile(name, os.O_RDONLY|rootReadFlags, 0)
	if err != nil {
		// A link swapped in at the name is refused as not a record; any other
		// failure (permissions, too many open files, the disk) is the caller's
		// to see as itself, never mistaken for a damaged record.
		if linkRefused(err) {
			return nil, errNotRegular
		}
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(seen, info) {
		return nil, errNotRegular
	}
	if info.Size() > limit {
		return nil, errProcessRecordBad
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errProcessRecordBad
	}
	return raw, nil
}

// writeSmall replaces one file under the store atomically.
func writeSmall(root *os.Root, name string, raw []byte, durable bool) error {
	tmp := "." + name + "." + randomToken(6)
	file, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = file.Write(raw)
	if err == nil && durable {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = root.Rename(tmp, name)
	}
	if err != nil {
		_ = root.Remove(tmp)
	}
	return err
}

func randomToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

// openRoot opens the store, creating it only when create is set. A store that
// is a link or not a directory is refused.
func (s processStore) openRoot(create bool) (*os.Root, error) {
	if s.anchor != nil && !create {
		return openDirWithin(s.anchor, processStoreName)
	}
	path := s.root()
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) && create {
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errProcessRecordBad
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	// THE ROOT MUST BE THE FOLDER INSPECTED. A store swapped for a link between
	// the inspection and the open would otherwise confine every later read and
	// write to wherever the link pointed.
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, errProcessRecordBad
	}
	return root, nil
}

func readHead(root *os.Root) (processHead, bool, error) {
	raw, err := readSmall(root, processHeadName, processHeadCap)
	if errors.Is(err, fs.ErrNotExist) {
		return processHead{}, false, nil
	}
	if err != nil {
		return processHead{}, false, err
	}
	var head processHead
	if err := json.Unmarshal(raw, &head); err != nil || head.Epoch == "" {
		return processHead{}, false, errProcessRecordBad
	}
	return head, true, nil
}

func writeHead(root *os.Root, head processHead) error {
	raw, err := json.Marshal(head)
	if err != nil {
		return err
	}
	return writeSmall(root, processHeadName, raw, true)
}

func readRecord(root *os.Root, id int) (JobNotice, bool, error) {
	raw, err := readSmall(root, processFileName(id), processRecordCap)
	if errors.Is(err, fs.ErrNotExist) {
		return JobNotice{}, false, nil
	}
	if err != nil {
		return JobNotice{}, false, err
	}
	var n JobNotice
	if err := json.Unmarshal(raw, &n); err != nil || n.ID != id {
		return JobNotice{}, false, errProcessRecordBad
	}
	return n, true, nil
}

// encodeRecord is one record's bytes, cut to the record cap by cutting the
// command and saying so.
func encodeRecord(n JobNotice) ([]byte, error) {
	raw, err := json.Marshal(n)
	if err != nil || len(raw) <= processRecordCap {
		return raw, err
	}
	over := len(raw) - processRecordCap + 1024
	if over >= len(n.Command) {
		return nil, errProcessRecordBad
	}
	n.Command = strings.ToValidUTF8(n.Command[:len(n.Command)-over], "")
	n.CommandCut = true
	raw, err = json.Marshal(n)
	if err == nil && len(raw) > processRecordCap {
		return nil, errProcessRecordBad
	}
	return raw, err
}

// sameJob says two records are the same run of one number. A number is never
// handed out twice while the logs folder's counter survives; a record of the
// same number with another start is a number reused after that counter was
// lost.
func sameJob(a, b JobNotice) bool {
	return a.ID == b.ID && (a.Started.IsZero() || b.Started.IsZero() || a.Started.Equal(b.Started))
}

// put writes one job's record. A settled record is never overwritten by a
// running one of the same run: notices can arrive out of order across the
// namer's goroutine, and an ending is final.
func (s processStore) put(root *os.Root, head *processHead, n JobNotice, durable bool) (bool, error) {
	return s.putDeferred(root, head, n, durable, false)
}

// putDeferred is put that leaves the head to the caller when deferHead is set:
// a fold writes its head once per batch, with the resume offset beside the
// high-water number, so a crash re-folds the batch rather than hiding it.
func (s processStore) putDeferred(root *os.Root, head *processHead, n JobNotice, durable, deferHead bool) (bool, error) {
	old, found, err := readRecord(root, n.ID)
	switch {
	case err != nil && !badRecord(err):
		return false, err
	case found && sameJob(old, n) && old.Over() && !n.Over():
		return false, nil
	case found && !sameJob(old, n):
		head.Lost++
	}
	if found && sameJob(old, n) {
		// What only the record knew survives a later notice that lacks it.
		if n.StartedBy == "" {
			n.StartedBy, n.Task = old.StartedBy, old.Task
		}
		if n.Dir == "" {
			n.Dir = old.Dir
		}
	}
	raw, err := encodeRecord(n)
	if err != nil {
		head.Lost++
		if deferHead {
			return true, nil
		}
		return true, writeHead(root, *head)
	}
	headChanged := false
	if n.ID > head.High {
		head.High, headChanged = n.ID, true
	}
	if found && !sameJob(old, n) {
		headChanged = true
	}
	if headChanged && !deferHead {
		if err := writeHead(root, *head); err != nil {
			return false, err
		}
	}
	return true, writeSmall(root, processFileName(n.ID), raw, durable)
}

// legacyMarkOf is a legacy file's mark as it stands now, and false when there
// is no regular file there.
func legacyMarkOf(path, name string) (legacyMark, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return legacyMark{}, false
	}
	// The mark is keyed by the array's fixed name inside the conversation
	// folder, never by the folder's own name, so a renamed folder is not
	// folded again.
	return legacyMark{File: name, Size: info.Size(), MTime: info.ModTime().UnixNano()}, true
}

func (h processHead) markFor(file string) (legacyMark, bool) {
	for _, m := range h.Migrated {
		if m.File == file {
			return m, true
		}
	}
	return legacyMark{}, false
}

// legacyPending says whether some legacy array is present and not folded in
// to its end as it now stands.
func (s processStore) legacyPending(head processHead) bool {
	for k := range processLegacyNames {
		now, ok := s.legacyMarkAt(k)
		if !ok {
			continue
		}
		if was, folded := head.markFor(now.File); !folded || !was.sameFile(now) || !was.Done {
			return true
		}
	}
	return false
}

// legacyCorrupt says a folded array stopped being readable part way.
func (h processHead) legacyCorrupt() bool {
	for _, m := range h.Migrated {
		if m.Corrupt {
			return true
		}
	}
	return false
}

// badRecord is a record file that is there and cannot be read as one: damaged,
// oversize, a link or a special file. A write replaces it; a read skips it and
// says the page is incomplete.
func badRecord(err error) bool {
	return errors.Is(err, errProcessRecordBad) || errors.Is(err, errNotRegular)
}

// legacyScan is how far one pass over a legacy array got.
type legacyScan struct {
	// next is the byte where the next element starts.
	next int64
	// done: the closing bracket was reached. corrupt: the bytes at next are not
	// JSON this reader can carry on past.
	done, corrupt bool
	// lost counts elements seen and not kept (over the record cap, not a
	// process record).
	lost int
}

// errLegacyBudget ends a pass that used its bytes or its time.
var errLegacyBudget = errors.New("legacy fold budget used")

// scanLegacy reads a legacy array from byte from, element by element, handing
// each readable record to fn, until the array ends, the bytes or the deadline
// run out, or the array stops being JSON.
//
// IT IS A BYTE SCANNER AND NOT A DECODER, for two reasons the decoder could not
// meet. An element larger than a record is SKIPPED in constant memory — its
// bytes are counted and not kept — so one enormous command cannot either be
// buffered whole or end the pass and take every later element with it. And
// the scanner knows the exact byte every element starts at, which is what lets
// a pass cut by its budget resume there.
func scanLegacy(path string, from, budget int64, deadline time.Time, fn func(JobNotice) error) (legacyScan, error) {
	return scanLegacyOpened(func() (*os.File, error) { return openRegularNoFollow(path) }, from, budget, deadline, fn)
}

// scanLegacyOpened is [scanLegacy] over a file the caller opens, so an
// anchored reader can open it through its root.
func scanLegacyOpened(open func() (*os.File, error), from, budget int64, deadline time.Time, fn func(JobNotice) error) (legacyScan, error) {
	scan := legacyScan{next: from}
	file, err := open()
	if errors.Is(err, fs.ErrNotExist) {
		scan.done = true
		return scan, nil
	}
	if err != nil {
		scan.corrupt = true
		return scan, nil
	}
	defer file.Close()
	if _, err := file.Seek(from, io.SeekStart); err != nil {
		scan.corrupt = true
		return scan, nil
	}
	r := &legacyReader{r: bufio.NewReaderSize(file, 64<<10), at: from}
	if from == 0 {
		if c, ok := r.skipSpace(); !ok || c != '[' {
			scan.corrupt = true
			return scan, nil
		}
		r.take()
	}
	used := int64(0)
	for {
		c, ok := r.skipSpace()
		if ok && c == ',' {
			r.take()
			c, ok = r.skipSpace()
		}
		if !ok {
			// The array ended without its bracket: what is there was read, and
			// the file is cut short.
			scan.next, scan.corrupt = r.at, true
			return scan, nil
		}
		if c == ']' {
			scan.next, scan.done = r.at+1, true
			return scan, nil
		}
		scan.next = r.at
		if used >= budget || (!deadline.IsZero() && time.Now().After(deadline)) {
			return scan, errLegacyBudget
		}
		start := r.at
		raw, whole, err := r.value(processRecordCap)
		used += r.at - start
		if err != nil {
			scan.next, scan.corrupt = start, true
			return scan, nil
		}
		var n JobNotice
		if !whole || json.Unmarshal(raw, &n) != nil || n.ID <= 0 {
			scan.lost++
			continue
		}
		if n.Kind == JobKindTask {
			// A task node's registry entry is the task itself, never a process.
			continue
		}
		if err := fn(n); err != nil {
			scan.next = start
			return scan, err
		}
	}
}

// legacyReader walks JSON bytes counting its position.
type legacyReader struct {
	r  *bufio.Reader
	at int64
}

func (l *legacyReader) skipSpace() (byte, bool) {
	for {
		c, err := l.r.ReadByte()
		if err != nil {
			return 0, false
		}
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			_ = l.r.UnreadByte()
			return c, true
		}
		l.at++
	}
}

func (l *legacyReader) take() {
	if _, err := l.r.ReadByte(); err == nil {
		l.at++
	}
}

// value reads one JSON value, keeping at most keep bytes of it; whole says it
// was kept entire. Strings and nesting are tracked so a bracket inside a
// command is not mistaken for the end.
func (l *legacyReader) value(keep int) ([]byte, bool, error) {
	var out []byte
	whole := true
	put := func(c byte) {
		l.at++
		if !whole {
			return
		}
		if len(out) >= keep {
			whole, out = false, nil
			return
		}
		out = append(out, c)
	}
	depth, inString, escaped := 0, false, false
	first := true
	for {
		c, err := l.r.ReadByte()
		if err != nil {
			return nil, false, io.ErrUnexpectedEOF
		}
		if !inString && depth == 0 && !first && (c == ',' || c == ']' || c == ' ' || c == '\t' || c == '\n' || c == '\r') {
			_ = l.r.UnreadByte()
			return out, whole, nil
		}
		first = false
		put(c)
		switch {
		case inString && escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case inString && c == '"':
			inString = false
			if depth == 0 {
				return out, whole, nil
			}
		case inString:
		case c == '"':
			inString = true
		case c == '{' || c == '[':
			depth++
		case c == '}' || c == ']':
			depth--
			if depth < 0 {
				return nil, false, errProcessRecordBad
			}
			if depth == 0 {
				return out, whole, nil
			}
		}
	}
}

// The owner's fold budgets: a job's own write folds a little, a page folds
// more, and neither ever holds the record lock longer than its share.
const (
	processFoldWriteTime = 50 * time.Millisecond
	processFoldPageTime  = 400 * time.Millisecond
	processFoldBytes     = 16 << 20
	// processPageLockWait bounds how long a page waits for the record lock.
	processPageLockWait = 250 * time.Millisecond
)

// migrate folds legacy arrays forward from where the last pass stopped, within
// the time it is given and [processFoldBytes]. A legacy row never overwrites a
// record of the same run unless the record is still running and the legacy row
// has settled; a legacy row of the same number and another run is counted lost
// (the store keeps one run per number). Owner fields are never taken from a
// legacy row, because no legacy row was written by anyone who said who started
// it. The head — offset and high-water number together — is written once per
// file per pass.
func (s processStore) migrate(root *os.Root, head *processHead, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for k, path := range processLegacyFiles(s.dir) {
		now, ok := legacyMarkOf(path, processLegacyNames[k])
		if !ok {
			continue
		}
		mark, folded := head.markFor(now.File)
		if !folded || !mark.sameFile(now) {
			mark = now
		}
		if mark.Done {
			continue
		}
		scan, err := scanLegacy(path, mark.Offset, processFoldBytes, deadline, func(n JobNotice) error {
			n.StartedBy, n.Task = "", 0
			old, found, err := readRecord(root, n.ID)
			if err != nil && !badRecord(err) {
				return err
			}
			if found {
				if !sameJob(old, n) {
					head.Lost++
					return nil
				}
				if old.Over() || !n.Over() {
					return nil
				}
				n.StartedBy, n.Task, n.Dir = old.StartedBy, old.Task, old.Dir
			}
			_, err = s.putDeferred(root, head, n, false, true)
			return err
		})
		cut := errors.Is(err, errLegacyBudget)
		if err != nil && !cut {
			return err
		}
		head.Lost += scan.lost
		if scan.corrupt && !mark.Corrupt {
			head.Lost++
		}
		mark.Offset, mark.Done, mark.Corrupt = scan.next, scan.done || scan.corrupt, scan.corrupt
		kept := head.Migrated[:0]
		for _, m := range head.Migrated {
			if m.File != mark.File {
				kept = append(kept, m)
			}
		}
		head.Migrated = append(kept, mark)
		if err := writeHead(root, *head); err != nil {
			return err
		}
		if cut {
			return nil
		}
	}
	return nil
}

// open is the owner's door: the store, created when missing, its head rebuilt
// when it cannot be read, and legacy arrays folded forward within foldTime.
func (s processStore) open(foldTime time.Duration) (*os.Root, processHead, error) {
	root, err := s.openRoot(true)
	if err != nil {
		return nil, processHead{}, err
	}
	head, found, err := readHead(root)
	switch {
	case err != nil && badRecord(err):
		// ONLY A HEAD THAT IS THERE AND IS NOT ONE IS REBUILT. A head that could
		// not be read for any other reason — too many open files, permissions,
		// a failing disk — is a good head this call cannot see, and replacing it
		// would throw away its epoch, its fold marks and its lost count.
		if head, err = s.rebuildHead(root, time.Now().Add(max(foldTime, processRebuildFloor))); err != nil {
			root.Close()
			return nil, processHead{}, err
		}
		found = true
	case err != nil:
		root.Close()
		return nil, processHead{}, err
	}
	if !found {
		head = processHead{Epoch: randomToken(8)}
		if err := writeHead(root, head); err != nil {
			root.Close()
			return nil, processHead{}, err
		}
	}
	if s.legacyPending(head) {
		if err := s.migrate(root, &head, foldTime); err != nil {
			root.Close()
			return nil, processHead{}, err
		}
	}
	return root, head, nil
}

// processRebuildEntries bounds the folder listing a head rebuild reads, and
// processRebuildFloor is the least time it is given; it otherwise shares the
// opening call's fold time, because it runs under the record lock every
// writer waits on.
const (
	processRebuildEntries = 1 << 20
	processRebuildFloor   = 50 * time.Millisecond
)

// rebuildHead replaces a head that is there and unreadable. The records are
// the truth and the head only indexes them, so the high-water number is found
// again from the store's own folder (never anywhere else), in batches, until
// the listing ends, its entry bound or its deadline. The epoch is new — every
// old cursor is stale, which is true — and every legacy array is folded again
// from byte 0, which is idempotent.
//
// THE REBUILT HEAD IS NEVER COMPLETE. What the old head counted lost, and
// whether the listing reached every record, cannot be known, so Lost starts at
// one and every page of this store says incomplete. A listing cut short also
// leaves numbers above the highest it saw unreachable until a write raises
// the high-water number past them, which is one more reason not to claim the
// history whole.
func (s processStore) rebuildHead(root *os.Root, deadline time.Time) (processHead, error) {
	head := processHead{Epoch: randomToken(8), Lost: 1}
	dir, err := root.Open(".")
	if err != nil {
		return processHead{}, err
	}
	defer dir.Close()
	for seen := 0; seen < processRebuildEntries && time.Now().Before(deadline); {
		entries, err := dir.ReadDir(256)
		for _, e := range entries {
			seen++
			name := e.Name()
			if !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
				continue
			}
			if id, err := strconv.Atoi(strings.TrimSuffix(name, ".json")); err == nil && id > head.High {
				head.High = id
			}
		}
		if err != nil {
			break
		}
	}
	_ = root.Rename(processHeadName, processHeadName+".unreadable-"+randomToken(4))
	return head, writeHead(root, head)
}

// ── reading a page ─────────────────────────────────────────────────────────

// ProcessPageAsk is one page's question. Cursor is empty for the first page.
type ProcessPageAsk struct {
	Limit  int
	Cursor string
}

// ProcessPageRow is one row of a page: the record, whether the engine holding
// the conversation vouched for it as live, and why its state is unknown when
// it is (a saved running record nobody vouched for).
type ProcessPageRow struct {
	JobNotice
	Live    bool   `json:",omitempty"`
	Unknown string `json:",omitempty"`
}

// ProcessPage is one page. Next is empty ONLY when the walk reached the
// oldest record; Incomplete says records exist that could not be read.
type ProcessPage struct {
	Rows       []ProcessPageRow
	Next       string `json:",omitempty"`
	Incomplete bool   `json:",omitempty"`
	Revision   string `json:",omitempty"`
}

// CheckProcessPageAsk is the effective limit, or an error naming the bound.
func CheckProcessPageAsk(ask ProcessPageAsk) (int, error) {
	limit := ask.Limit
	if limit == 0 {
		limit = ProcessPageDefault
	}
	if limit < 1 || limit > ProcessPageMax {
		return 0, fmt.Errorf("limit is 1 to %d", ProcessPageMax)
	}
	if len(ask.Cursor) > 512 {
		return 0, errProcessCursor
	}
	return limit, nil
}

// A cursor names the history it was cut from (the store's epoch, or the
// legacy arrays' marks) and where to carry on: inside the live rows below a
// number ("L<n>"), at the top of the saved history ("T"), or in the saved
// history below a number ("<n>").
type processCursor struct {
	epoch  string
	live   bool
	top    bool
	before int
}

func encodeProcessCursor(epoch string, before int) string {
	return encodeProcessPosition(epoch, strconv.Itoa(before))
}

func encodeProcessPosition(epoch, pos string) string {
	return base64.RawURLEncoding.EncodeToString([]byte("p1:" + epoch + ":" + pos))
}

func decodeProcessCursor(cursor string) (processCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return processCursor{}, errProcessCursor
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 3 || parts[0] != "p1" || parts[1] == "" {
		return processCursor{}, errProcessCursor
	}
	c := processCursor{epoch: parts[1]}
	pos := parts[2]
	switch {
	case pos == "T":
		c.top = true
		return c, nil
	case strings.HasPrefix(pos, "L"):
		c.live, pos = true, pos[1:]
	}
	if c.before, err = strconv.Atoi(pos); err != nil || c.before < 1 {
		return processCursor{}, errProcessCursor
	}
	return c, nil
}

// pageBudget tracks one reply's encoded size.
type pageBudget struct{ used, max int }

func (b *pageBudget) admit(row ProcessPageRow) bool {
	raw, err := json.Marshal(row)
	if err != nil {
		return false
	}
	if b.used+len(raw) > b.max {
		return false
	}
	b.used += len(raw)
	return true
}

func newPageBudget() *pageBudget {
	return &pageBudget{max: ProcessPageReplyBytes - processPageEnvelope}
}

// readPage walks the store below the cursor. vouch answers, for a record saved
// as running, the live notice and true when the engine holding the
// conversation runs it; nil means nothing holding the conversation was asked.
// skip leaves out numbers already answered as live on this page.
func (s processStore) readPage(ctx context.Context, root *os.Root, head processHead, before, limit int, budget *pageBudget, vouch func(int) (JobNotice, bool), skip map[int]bool) ProcessPage {
	page := ProcessPage{Revision: head.Epoch, Incomplete: head.Lost > 0 || head.legacyCorrupt() || s.legacyPending(head)}
	start := head.High
	if before > 0 && before-1 < start {
		start = before - 1
	}
	for id, probes := start, 0; id >= 1; id, probes = id-1, probes+1 {
		if probes >= processProbeBudget || ctx.Err() != nil {
			page.Next = encodeProcessCursor(head.Epoch, id+1)
			return page
		}
		if skip[id] {
			continue
		}
		n, found, err := readRecord(root, id)
		if err != nil {
			page.Incomplete = true
			continue
		}
		if !found {
			continue
		}
		row := pageRow(n, vouch)
		if len(page.Rows) == limit || !budget.admit(row) {
			page.Next = encodeProcessCursor(head.Epoch, id+1)
			return page
		}
		page.Rows = append(page.Rows, row)
	}
	return page
}

// pageRow is one record as a page shows it: a saved running record is live
// only when its owner vouches for it, and otherwise unknown — never stopped,
// never given an exit code or an ending nobody saw.
func pageRow(n JobNotice, vouch func(int) (JobNotice, bool)) ProcessPageRow {
	if n.State != JobRunning {
		return ProcessPageRow{JobNotice: n}
	}
	if vouch == nil {
		return ProcessPageRow{JobNotice: n, Unknown: ProcessUnknownNotHeld}
	}
	if live, ok := vouch(n.ID); ok && sameJob(live, n) {
		live.StartedBy, live.Task = n.StartedBy, n.Task
		if live.Dir == "" {
			live.Dir = n.Dir
		}
		return ProcessPageRow{JobNotice: live, Live: live.State == JobRunning}
	}
	return ProcessPageRow{JobNotice: n, Unknown: ProcessUnknownOwnerLost}
}

// legacyEpoch names an unfolded legacy history by the files' marks, so a
// cursor into it goes stale the moment either file changes.
func (s processStore) legacyEpoch() string {
	var parts []string
	for k := range processLegacyNames {
		if m, ok := s.legacyMarkAt(k); ok {
			parts = append(parts, strconv.FormatInt(m.Size, 36)+"."+strconv.FormatInt(m.MTime, 36))
		}
	}
	return "L" + strings.Join(parts, "-")
}

// readLegacyRows is every readable row of the unfolded legacy arrays, read
// within [processLegacyReadCap] bytes each and the context's deadline, newest
// first, and whether any could not be read or was not reached. It writes
// nothing.
func (s processStore) readLegacyRows(ctx context.Context) ([]JobNotice, bool) {
	byID := map[int]JobNotice{}
	incomplete := false
	deadline, _ := ctx.Deadline()
	for k := range processLegacyNames {
		scan, err := scanLegacyOpened(func() (*os.File, error) { return s.openLegacy(k) }, 0, processLegacyReadCap, deadline, func(n JobNotice) error {
			n.StartedBy, n.Task = "", 0
			if old, ok := byID[n.ID]; ok && (old.Over() || !n.Over()) {
				return nil
			}
			byID[n.ID] = n
			return nil
		})
		if err != nil || scan.lost > 0 || scan.corrupt || !scan.done {
			incomplete = true
		}
	}
	rows := make([]JobNotice, 0, len(byID))
	for _, n := range byID {
		rows = append(rows, n)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	return rows, incomplete
}

// ReadProcessPage is a page of a conversation's history for a reader that
// does not hold the conversation. It never creates, folds or repairs anything:
// a store that does not exist yet is answered from the legacy arrays read
// within their bounds, and a saved running record is unknown.
func ReadProcessPage(ctx context.Context, file string, ask ProcessPageAsk) (ProcessPage, error) {
	limit, err := CheckProcessPageAsk(ask)
	if err != nil {
		return ProcessPage{}, err
	}
	if file == "" {
		return ProcessPage{}, nil
	}
	s := processStore{dir: filepath.Dir(file)}
	return s.page(ctx, ask, limit, nil, nil, false)
}

// page is the shared walk.
//
// THE LIVE ROWS COME FIRST AND ARE PAGED LIKE ANYTHING ELSE. live is every job
// the engine holding the conversation runs now, newest first; it is bounded by
// how many jobs can run, which across a conversation's task workers can still
// be more than a page, so live rows count against the limit and the reply's
// bytes and carry their own continuation ("L<n>") before the saved history
// begins ("T"). vouch is nil for a reader that holds nothing.
//
// THE RECORD LOCK IS HELD ONLY TO OPEN. An owner takes it to create the store,
// rebuild an unreadable head and fold legacy arrays within the page's fold
// time; the walk itself reads files that are only ever replaced whole, and
// runs without it, so no page holds up another conversation's job writes.
func (s processStore) page(ctx context.Context, ask ProcessPageAsk, limit int, vouch func(int) (JobNotice, bool), live []ProcessPageRow, owner bool) (ProcessPage, error) {
	var cur processCursor
	if ask.Cursor != "" {
		var err error
		if cur, err = decodeProcessCursor(ask.Cursor); err != nil {
			return ProcessPage{}, err
		}
	}
	var root *os.Root
	var head processHead
	var found bool
	var err error
	// AN OWNER WAITS FOR THE RECORD LOCK ONLY WITHIN ITS SHARE OF THE PAGE'S
	// DEADLINE. A writer holding it longer than that leaves this page reading
	// what is already on disk, as a reader that holds nothing would: nothing is
	// created or folded, and an unfolded legacy array still makes the page
	// incomplete. The walk never waits on the lock at all.
	if owner {
		wait, cancel := context.WithTimeout(ctx, processPageLockWait)
		err = processRecordMu.LockCtx(wait)
		cancel()
		if err == nil {
			root, head, err = s.open(processFoldPageTime)
			processRecordMu.Unlock()
			found = err == nil
		} else {
			owner = false
		}
	}
	if !owner {
		root, err = s.openRoot(false)
		if err == nil {
			head, found, err = readHead(root)
		}
	}
	if root != nil {
		defer root.Close()
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ProcessPage{}, err
	}
	epoch := head.Epoch
	var legacy []JobNotice
	legacyIncomplete := false
	if !found {
		epoch = s.legacyEpoch()
	}
	if cur.epoch != "" && cur.epoch != epoch {
		return ProcessPage{}, ErrProcessStale
	}
	budget := newPageBudget()
	page := ProcessPage{Revision: epoch}
	// EVERY LIVE NUMBER IS LEFT OUT OF EVERY SAVED-HISTORY PAGE, whichever page
	// this is. A live job has a record from its first notice, and the live
	// rows were (or will be) answered in the live phase; skipping only the
	// rows on this page let a job shown live on page one come back on the
	// first history page, and a sleep or a quit reading these pages stopped it
	// twice.
	skip := make(map[int]bool, len(live))
	for _, row := range live {
		skip[row.ID] = true
	}
	if ask.Cursor == "" || cur.live {
		for _, row := range live {
			if cur.live && row.ID >= cur.before {
				continue
			}
			if len(page.Rows) == limit || !budget.admit(row) {
				page.Next = encodeProcessPosition(epoch, "L"+strconv.Itoa(row.ID+1))
				page.Incomplete = found && (head.Lost > 0 || head.legacyCorrupt() || s.legacyPending(head))
				return page, nil
			}
			page.Rows = append(page.Rows, row)
		}
		cur = processCursor{epoch: epoch, top: true}
	}
	remaining := limit - len(page.Rows)
	before := 0
	if !cur.top {
		before = cur.before
	}
	if !found {
		legacy, legacyIncomplete = s.readLegacyRows(ctx)
		rest := legacyPageRows(ctx, epoch, legacy, before, remaining, budget, vouch, skip)
		rest.Rows = append(page.Rows, rest.Rows...)
		rest.Incomplete = rest.Incomplete || legacyIncomplete
		rest.Revision = epoch
		return rest, nil
	}
	if remaining == 0 {
		page.Next = encodeProcessPosition(epoch, "T")
		page.Incomplete = head.Lost > 0 || head.legacyCorrupt() || s.legacyPending(head)
		return page, nil
	}
	rest := s.readPage(ctx, root, head, before, remaining, budget, vouch, skip)
	rest.Rows = append(page.Rows, rest.Rows...)
	return rest, nil
}

// legacyPageRows pages unfolded legacy rows already read.
func legacyPageRows(ctx context.Context, epoch string, rows []JobNotice, before, limit int, budget *pageBudget, vouch func(int) (JobNotice, bool), skip map[int]bool) ProcessPage {
	var page ProcessPage
	for _, n := range rows {
		if (before > 0 && n.ID >= before) || skip[n.ID] {
			continue
		}
		row := pageRow(n, vouch)
		if ctx.Err() != nil || len(page.Rows) == limit || !budget.admit(row) {
			page.Next = encodeProcessCursor(epoch, n.ID+1)
			return page
		}
		page.Rows = append(page.Rows, row)
	}
	return page
}

// readProcessRecord is one record by its number, from the store when there is
// one and from the unfolded legacy arrays, within their bounds, when there is
// not.
func (s processStore) readProcessRecord(id int) (JobNotice, bool, error) {
	root, err := s.openRoot(false)
	if err == nil {
		defer root.Close()
		if _, found, herr := readHead(root); herr == nil && found {
			return readRecord(root, id)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), processFoldPageTime)
	defer cancel()
	rows, _ := s.readLegacyRows(ctx)
	for _, n := range rows {
		if n.ID == id {
			return n, true, nil
		}
	}
	return JobNotice{}, false, nil
}

// ReadProcessRecord is one record of a conversation's history by job number,
// for a reader that does not hold it. Nothing is written.
func ReadProcessRecord(file string, id int) (JobNotice, bool, error) {
	if file == "" || id <= 0 {
		return JobNotice{}, false, nil
	}
	return processStore{dir: filepath.Dir(file)}.readProcessRecord(id)
}

// ── who is live ────────────────────────────────────────────────────────────

// liveProcesses is which agent in this engine runs each saved-as-running job,
// per conversation folder. It is how the conversation's own page vouches for a
// TASK WORKER's running job: the worker is a separate agent with its own
// registry, and its jobs are written into the conversation's history.
var liveProcesses = struct {
	sync.Mutex
	byDir map[string]map[int]*Agent
}{byDir: map[string]map[int]*Agent{}}

func liveProcessSet(dir string, id int, a *Agent, running bool) {
	liveProcesses.Lock()
	defer liveProcesses.Unlock()
	set := liveProcesses.byDir[dir]
	if running {
		if set == nil {
			set = map[int]*Agent{}
			liveProcesses.byDir[dir] = set
		}
		set[id] = a
		return
	}
	if set != nil && set[id] == a {
		delete(set, id)
		if len(set) == 0 {
			delete(liveProcesses.byDir, dir)
		}
	}
}

// liveProcessesOf is every live job this engine runs for one conversation
// folder, read from each owning agent's registry now.
func liveProcessesOf(dir string) map[int]JobNotice {
	liveProcesses.Lock()
	owners := make(map[int]*Agent, len(liveProcesses.byDir[dir]))
	for id, a := range liveProcesses.byDir[dir] {
		owners[id] = a
	}
	liveProcesses.Unlock()
	out := make(map[int]JobNotice, len(owners))
	for id, a := range owners {
		if a == nil || a.jobs == nil {
			continue
		}
		j := a.jobs.find(id)
		if j == nil {
			continue
		}
		n := noticeOf(j.info())
		if n.State != JobRunning || n.Kind == JobKindTask {
			continue
		}
		n.StartedBy, n.Task = a.processOwner()
		out[id] = n
	}
	return out
}

// liveProcessOwner is the agent that marked job id live in dir, or nil.
func liveProcessOwner(dir string, id int) *Agent {
	liveProcesses.Lock()
	defer liveProcesses.Unlock()
	return liveProcesses.byDir[dir][id]
}

// liveProcessOwnersOf is up to limit of the jobs marked live in dir, by
// number, and how many were marked in all. It copies no more than limit, so
// a stop's enumeration is bounded by its own cap and not by the map.
func liveProcessOwnersOf(dir string, limit int) (map[int]*Agent, int) {
	liveProcesses.Lock()
	defer liveProcesses.Unlock()
	set := liveProcesses.byDir[dir]
	out := make(map[int]*Agent, min(len(set), limit))
	for id, a := range set {
		if len(out) == limit {
			break
		}
		out[id] = a
	}
	return out, len(set)
}

// liveProcessesOfAgent is the numbers one agent has marked live.
func liveProcessesOfAgent(dir string, a *Agent) []int {
	liveProcesses.Lock()
	defer liveProcesses.Unlock()
	var ids []int
	for id, owner := range liveProcesses.byDir[dir] {
		if owner == a {
			ids = append(ids, id)
		}
	}
	return ids
}

// ProcessCursorLive is the cursor a page would give to continue the live rows
// below id, before the saved history begins.
func ProcessCursorLive(revision string, id int) string {
	if revision == "" || id < 1 {
		return ""
	}
	return encodeProcessPosition(revision, "L"+strconv.Itoa(id+1))
}

// ProcessCursorBefore is the cursor a page would give to continue below id in
// the history named by revision. A door that has to cut a page shorter than
// the walk did (its own reply budget) continues from the first row it cut.
func ProcessCursorBefore(revision string, id int) string {
	if revision == "" || id < 1 {
		return ""
	}
	return encodeProcessCursor(revision, id+1)
}
