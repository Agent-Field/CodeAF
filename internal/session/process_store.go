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
	// processLegacyCap bounds how much of one legacy array the owner streams.
	// A larger array is folded up to the cap and the rest counted as lost.
	processLegacyCap = 256 << 20
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
type legacyMark struct {
	File  string `json:"file"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
}

// processLegacyFiles are the legacy arrays a conversation folder may hold, by
// their fixed names: the conversation's own, and the one every task worker of
// it wrote because its SessionFile was a task journal. They are named, never
// searched for.
func processLegacyFiles(dir string) []string {
	return []string{filepath.Join(dir, "processes.json"), filepath.Join(dir, placeNodeJournals, "processes.json")}
}

// processStore is one conversation's history, rooted at the conversation's
// folder.
type processStore struct{ dir string }

func (s processStore) root() string { return filepath.Join(s.dir, processStoreName) }

func processFileName(id int) string { return strconv.Itoa(id) + ".json" }

// readSmall reads one regular file under the store of at most limit bytes. A
// link, a special file or a larger file is refused rather than followed or
// read to the end.
func readSmall(root *os.Root, name string, limit int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errProcessRecordBad
	}
	file, err := root.OpenFile(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
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
	return os.OpenRoot(path)
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
	old, found, err := readRecord(root, n.ID)
	switch {
	case err != nil && !errors.Is(err, errProcessRecordBad):
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
		return true, writeHead(root, *head)
	}
	headChanged := false
	if n.ID > head.High {
		head.High, headChanged = n.ID, true
	}
	if found && !sameJob(old, n) {
		headChanged = true
	}
	if headChanged {
		if err := writeHead(root, *head); err != nil {
			return false, err
		}
	}
	return true, writeSmall(root, processFileName(n.ID), raw, durable)
}

// legacyMarkOf is a legacy file's mark as it stands now, and false when there
// is no regular file there.
func legacyMarkOf(path string) (legacyMark, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return legacyMark{}, false
	}
	return legacyMark{File: filepath.Base(filepath.Dir(path)) + "/" + filepath.Base(path), Size: info.Size(), MTime: info.ModTime().UnixNano()}, true
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
// as it now stands.
func (s processStore) legacyPending(head processHead) bool {
	for _, path := range processLegacyFiles(s.dir) {
		now, ok := legacyMarkOf(path)
		if !ok {
			continue
		}
		if was, folded := head.markFor(now.File); !folded || was != now {
			return true
		}
	}
	return false
}

// errLegacyElement ends a legacy stream whose one element outgrew its window.
var errLegacyElement = errors.New("legacy process element is too large")

// windowReader refuses to hand a decoder more than max bytes between resets,
// so one enormous element cannot make the decoder buffer it whole.
type windowReader struct {
	r      io.Reader
	n, max int64
}

func (w *windowReader) Read(p []byte) (int, error) {
	if w.n >= w.max {
		return 0, errLegacyElement
	}
	if int64(len(p)) > w.max-w.n {
		p = p[:w.max-w.n]
	}
	n, err := w.r.Read(p)
	w.n += int64(n)
	return n, err
}

// streamLegacy reads a legacy array element by element, at most total bytes,
// and hands each readable element to fn. It answers how many elements it saw
// and could not keep, and whether the stream ended before the array did.
func streamLegacy(path string, total int64, fn func(JobNotice) error) (lost int, cut bool, err error) {
	file, err := openRegularNoFollow(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, true, nil
	}
	defer file.Close()
	window := &windowReader{r: io.LimitReader(file, total), max: 2*processRecordCap + 64<<10}
	dec := json.NewDecoder(window)
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return 0, true, nil
	}
	for dec.More() {
		window.n = 0
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return lost + 1, true, nil
		}
		var n JobNotice
		if len(raw) > processRecordCap || json.Unmarshal(raw, &n) != nil || n.ID <= 0 || n.Kind == JobKindTask {
			if n.Kind != JobKindTask {
				lost++
			}
			continue
		}
		if err := fn(n); err != nil {
			return lost, false, err
		}
	}
	return lost, false, nil
}

// migrate folds every legacy array that is not folded in as it now stands.
// Only the conversation's owner calls it. A legacy row never overwrites a
// record of the same run unless the record is still running and the legacy
// row has settled; owner fields are never taken from a legacy row, because no
// legacy row was written by anyone who said who started it.
func (s processStore) migrate(root *os.Root, head *processHead) error {
	for _, path := range processLegacyFiles(s.dir) {
		now, ok := legacyMarkOf(path)
		if !ok {
			continue
		}
		if was, folded := head.markFor(now.File); folded && was == now {
			continue
		}
		lost, cut, err := streamLegacy(path, processLegacyCap, func(n JobNotice) error {
			n.StartedBy, n.Task = "", 0
			old, found, err := readRecord(root, n.ID)
			if err != nil && !errors.Is(err, errProcessRecordBad) {
				return err
			}
			if found {
				if !sameJob(old, n) || old.Over() || !n.Over() {
					return nil
				}
				n.StartedBy, n.Task, n.Dir = old.StartedBy, old.Task, old.Dir
			}
			_, err = s.put(root, head, n, false)
			return err
		})
		if err != nil {
			return err
		}
		head.Lost += lost
		if cut {
			head.Lost++
		}
		kept := head.Migrated[:0]
		for _, m := range head.Migrated {
			if m.File != now.File {
				kept = append(kept, m)
			}
		}
		head.Migrated = append(kept, now)
		if err := writeHead(root, *head); err != nil {
			return err
		}
	}
	return nil
}

// open is the owner's door: the store, created when missing, with every
// legacy array folded in.
func (s processStore) open() (*os.Root, processHead, error) {
	root, err := s.openRoot(true)
	if err != nil {
		return nil, processHead{}, err
	}
	head, found, err := readHead(root)
	if err != nil {
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
		if err := s.migrate(root, &head); err != nil {
			root.Close()
			return nil, processHead{}, err
		}
	}
	return root, head, nil
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

func encodeProcessCursor(epoch string, before int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("p1:" + epoch + ":" + strconv.Itoa(before)))
}

func decodeProcessCursor(cursor string) (string, int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", 0, errProcessCursor
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 3 || parts[0] != "p1" || parts[1] == "" {
		return "", 0, errProcessCursor
	}
	before, err := strconv.Atoi(parts[2])
	if err != nil || before < 1 {
		return "", 0, errProcessCursor
	}
	return parts[1], before, nil
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
	page := ProcessPage{Revision: head.Epoch, Incomplete: head.Lost > 0 || s.legacyPending(head)}
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
		return ProcessPageRow{JobNotice: live, Live: true}
	}
	return ProcessPageRow{JobNotice: n, Unknown: ProcessUnknownOwnerLost}
}

// legacyEpoch names an unfolded legacy history by the files' marks, so a
// cursor into it goes stale the moment either file changes.
func (s processStore) legacyEpoch() string {
	var parts []string
	for _, path := range processLegacyFiles(s.dir) {
		if m, ok := legacyMarkOf(path); ok {
			parts = append(parts, strconv.FormatInt(m.Size, 36)+"."+strconv.FormatInt(m.MTime, 36))
		}
	}
	return "L" + strings.Join(parts, "-")
}

// readLegacyRows is every readable row of the unfolded legacy arrays, read
// within [processLegacyReadCap] each, newest first, and whether any could not
// be read. It writes nothing.
func (s processStore) readLegacyRows() ([]JobNotice, bool) {
	byID := map[int]JobNotice{}
	incomplete := false
	for _, path := range processLegacyFiles(s.dir) {
		lost, cut, _ := streamLegacy(path, processLegacyReadCap, func(n JobNotice) error {
			n.StartedBy, n.Task = "", 0
			if old, ok := byID[n.ID]; ok && (old.Over() || !n.Over()) {
				return nil
			}
			byID[n.ID] = n
			return nil
		})
		if lost > 0 || cut {
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
// within their cap, and a saved running record is unknown.
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

// page is the shared walk. live rows (already admitted by the caller) fill
// skip; vouch is nil for a reader that holds nothing.
func (s processStore) page(ctx context.Context, ask ProcessPageAsk, limit int, vouch func(int) (JobNotice, bool), live []ProcessPageRow, owner bool) (ProcessPage, error) {
	var epoch string
	before := 0
	if ask.Cursor != "" {
		var err error
		if epoch, before, err = decodeProcessCursor(ask.Cursor); err != nil {
			return ProcessPage{}, err
		}
	}
	budget := newPageBudget()
	skip := map[int]bool{}
	var first []ProcessPageRow
	if ask.Cursor == "" {
		for _, row := range live {
			if budget.admit(row) {
				first = append(first, row)
				skip[row.ID] = true
			}
		}
	}
	var root *os.Root
	var head processHead
	var found bool
	var err error
	if owner {
		root, head, err = s.open()
		found = err == nil
	} else {
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
	if !found {
		return s.legacyPage(ctx, epoch, before, limit, budget, vouch, skip, first)
	}
	if epoch != "" && epoch != head.Epoch {
		return ProcessPage{}, ErrProcessStale
	}
	page := s.readPage(ctx, root, head, before, limit, budget, vouch, skip)
	page.Rows = append(first, page.Rows...)
	return page, nil
}

// legacyPage pages the unfolded legacy arrays for a reader with no store.
func (s processStore) legacyPage(ctx context.Context, epoch string, before, limit int, budget *pageBudget, vouch func(int) (JobNotice, bool), skip map[int]bool, first []ProcessPageRow) (ProcessPage, error) {
	now := s.legacyEpoch()
	if epoch != "" && epoch != now {
		return ProcessPage{}, ErrProcessStale
	}
	rows, incomplete := s.readLegacyRows()
	page := ProcessPage{Rows: first, Incomplete: incomplete, Revision: now}
	count := 0
	for _, n := range rows {
		if (before > 0 && n.ID >= before) || skip[n.ID] {
			continue
		}
		if ctx.Err() != nil {
			page.Next = encodeProcessCursor(now, n.ID+1)
			return page, nil
		}
		row := pageRow(n, vouch)
		if count == limit || !budget.admit(row) {
			page.Next = encodeProcessCursor(now, n.ID+1)
			return page, nil
		}
		page.Rows = append(page.Rows, row)
		count++
	}
	return page, nil
}

// readProcessRecord is one record by its number, from the store when there is
// one and from the unfolded legacy arrays when there is not.
func (s processStore) readProcessRecord(id int) (JobNotice, bool, error) {
	root, err := s.openRoot(false)
	if err == nil {
		defer root.Close()
		if _, found, herr := readHead(root); herr == nil && found {
			return readRecord(root, id)
		}
	}
	rows, _ := s.readLegacyRows()
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
