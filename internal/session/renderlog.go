package session

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE RENDER LEDGER is the durable, typed account of every video and music
// render: when it started, how it ended, whether a file was ever committed, and
// what the provider reported it cost. It exists so no surface has to read prose
// notes to know any of that. The contract is
// the render contract (RENDER-CONTRACT.md in the media design notes).
//
// APPEND-ONLY, FULL HISTORY, BOUNDED MEMORY. Every change appends one full
// snapshot row (`start`, then ONE `end`, then at most one `charge` receipt), and
// the last row for an id is the record. The file is NEVER read whole into
// memory: a page is read backward in bounded chunks from a byte-offset cursor,
// and one id is found by a bounded reverse scan under a deadline. There is no
// fixed number of records beyond which history stops resolving.
//
// EXACTLY ONE TERMINAL. The first end wins under the ledger's lock, so a Stop
// that is accepted seals `cancelled` at once and a provider that ignores the
// cancel and answers later cannot write a second ending. What it CAN still do
// is report what it charged: that arrives as a `charge` receipt on the SAME
// record — same id, same state — never as a new ending.

type RenderState string

const (
	RenderRunning     RenderState = "running"
	RenderFinished    RenderState = "finished"
	RenderFailed      RenderState = "failed"
	RenderCancelled   RenderState = "cancelled"
	RenderInterrupted RenderState = "interrupted"
	// RenderUnknown is a render the last row of which says running, read by
	// something that does not own the session: it may have ended since.
	RenderUnknown RenderState = "unknown"
)

type RenderCharge string

const (
	RenderCharged       RenderCharge = "charged"
	RenderNotCharged    RenderCharge = "not_charged"
	RenderChargeUnknown RenderCharge = "unknown"
)

// RenderRecord is one render as every reader sees it.
type RenderRecord struct {
	ID         string       `json:"id"`
	Job        int          `json:"job"`
	Session    string       `json:"session,omitempty"`
	Kind       string       `json:"kind"`
	State      RenderState  `json:"state"`
	Model      string       `json:"model"`
	Started    time.Time    `json:"started"`
	Ended      *time.Time   `json:"ended,omitempty"`
	DurationMS *int64       `json:"duration_ms,omitempty"`
	Run        string       `json:"run,omitempty"`
	Call       string       `json:"call,omitempty"`
	Committed  bool         `json:"committed"`
	Path       string       `json:"path,omitempty"`
	Charge     RenderCharge `json:"charge"`
	CostUSD    *float64     `json:"cost_usd,omitempty"`
	// Upstream is "unknown" on a cancelled render: only local waiting stopped,
	// and nothing here tells the provider to cancel.
	Upstream string   `json:"upstream,omitempty"`
	Error    string   `json:"error,omitempty"`
	Refusal  string   `json:"refusal,omitempty"`
	Options  []string `json:"options,omitempty"`
	// Persistence is unavailable when the latest fact is retained in memory because its journal write failed.
	Persistence string `json:"persistence,omitempty"`
}

// renderPatch is what a terminal row adds to the started one.
type renderPatch struct {
	state     RenderState
	committed bool
	path      string
	usage     *ai.Usage
	err       string
	refusal   string
	options   []string
}

const (
	// Bounds of one read. A page reads at most renderScanBudget bytes backward
	// and a lookup runs at most renderScanDeadline; running out of either is
	// reported honestly (a continuation cursor, or busy), never as "not found".
	renderScanBudget    = 16 << 20
	renderResolveBudget = 16 << 20
	renderScanDeadline  = 8 * time.Second
	renderChunk         = 64 << 10
	renderMaxLine       = 256 << 10
	renderErrorRunes    = 300
	renderListDefault   = 50
	renderListMax       = 100
)

const placeRenders = "renders.jsonl"

const (
	renderEvStart  = "start"
	renderEvEnd    = "end"
	renderEvCharge = "charge"
)

// renderRow is one line of the file: a full snapshot and what kind of change
// produced it.
type renderRow struct {
	Ev string `json:"ev"`
	RenderRecord
}

var (
	ErrRenderCursor   = errors.New("the render cursor is not valid; list again from the start")
	ErrRenderNotFound = errors.New("there is no such render")
	// ErrRenderBusy says the lookup ran out of time before it could tell; the
	// render may exist.
	ErrRenderBusy = errors.New("the render history is too large to search right now; try again")
	errRenderLine = errors.New("a render history line is over the size limit")
)

// renderStore reads a ledger file. It owns no state but the path and the bounds.
type renderStore struct {
	path string
	// budget bounds the bytes one page reads backward; resolveBudget bounds the
	// bytes read forward to find the latest row of every entry on the page.
	budget        int64
	resolveBudget int64
	deadline      time.Duration
	// now is the clock every deadline is judged by; a test replaces it.
	now func() time.Time
	// beforeResolve, when set, runs between a page's two phases (tests only).
	beforeResolve func()
}

func newRenderStore(path string) renderStore {
	return renderStore{path: path, budget: renderScanBudget, resolveBudget: renderResolveBudget, deadline: renderScanDeadline, now: time.Now}
}

// expiry answers the moment a read that starts now must be over by.
func (s renderStore) expiry() func() bool {
	end := s.now().Add(s.deadline)
	return func() bool { return s.now().After(end) }
}

// reverseScan visits the file's complete lines whose end is at or before upper,
// newest first, reading at most budget bytes and stopping at the deadline. It
// returns the start of the lowest line visited (upper when none), whether the
// whole range down to offset 0 was covered, and whether it stopped for time.
func reverseScan(file *os.File, upper, budget int64, expired func() bool, visit func(start int64, line []byte) bool) (low int64, covered, timedOut bool, err error) {
	low = upper
	pos := upper
	var carry []byte
	read := int64(0)
	for pos > 0 {
		if expired() {
			return low, false, true, nil
		}
		if read >= budget {
			return low, false, false, nil
		}
		n := int64(renderChunk)
		if n > pos {
			n = pos
		}
		chunk := make([]byte, n+int64(len(carry)))
		if _, err := file.ReadAt(chunk[:n], pos-n); err != nil && !errors.Is(err, io.EOF) {
			return low, false, false, err
		}
		copy(chunk[n:], carry)
		read += n
		pos -= n
		data := chunk
		for {
			i := bytes.LastIndexByte(data, '\n')
			if i < 0 {
				break
			}
			if seg := data[i+1:]; len(seg) > 0 {
				if len(seg) > renderMaxLine {
					return low, false, false, errRenderLine
				}
				start := pos + int64(i) + 1
				low = start
				if !visit(start, seg) {
					return low, false, false, nil
				}
			}
			data = data[:i]
		}
		if len(data) > renderMaxLine {
			return low, false, false, errRenderLine
		}
		carry = data
	}
	if len(carry) > 0 {
		low = 0
		if !visit(0, carry) {
			return low, false, false, nil
		}
	}
	return low, true, false, nil
}

// forwardScan visits complete lines from offset from up to offset until, oldest
// first, and reads nothing beyond until. A line over the size limit is an error,
// never skipped: a skipped row could be the latest one.
func forwardScan(file *os.File, from, until int64, expired func() bool, visit func(start int64, line []byte)) (timedOut bool, err error) {
	reader := bufio.NewReaderSize(io.NewSectionReader(file, from, until-from), renderChunk)
	offset := from
	for {
		if expired() {
			return true, nil
		}
		line, readErr := reader.ReadSlice('\n')
		if errors.Is(readErr, bufio.ErrBufferFull) {
			return false, errRenderLine
		}
		if len(bytes.TrimSpace(line)) > 0 && len(line) <= renderMaxLine {
			visit(offset, line)
		}
		offset += int64(len(line))
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return false, nil
			}
			return false, readErr
		}
	}
}

func parseRenderRow(line []byte) (renderRow, bool) {
	var row renderRow
	if json.Unmarshal(line, &row) != nil || row.ID == "" || row.Ev == "" {
		return renderRow{}, false
	}
	return row, true
}

// list reads one page, newest started first. cursor is "" or an offset token
// from a previous page.
func (s renderStore) list(limit int, cursor string) (RenderPage, error) {
	if limit <= 0 {
		limit = renderListDefault
	}
	if limit > renderListMax {
		limit = renderListMax
	}
	page := RenderPage{Renders: []RenderRecord{}}
	file, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return page, nil
		}
		return page, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return page, err
	}
	size := info.Size()
	upper := size
	if cursor != "" {
		offset, ok := parseRenderCursor(cursor)
		if !ok || offset > size {
			return page, ErrRenderCursor
		}
		if offset > 0 {
			var prior [1]byte
			if _, err := file.ReadAt(prior[:], offset-1); err != nil || prior[0] != '\n' {
				return page, ErrRenderCursor
			}
		}
		upper = offset
	}
	expired := s.expiry()
	type anchor struct {
		off int64
		rec RenderRecord
	}
	var anchors []anchor
	low, covered, timedOut, err := reverseScan(file, upper, s.budget, expired, func(off int64, line []byte) bool {
		if row, ok := parseRenderRow(line); ok && row.Ev == renderEvStart {
			anchors = append(anchors, anchor{off, row.RenderRecord})
			return len(anchors) < limit
		}
		return true
	})
	if err != nil {
		return page, err
	}
	if timedOut && len(anchors) == 0 {
		return page, ErrRenderBusy
	}
	if !covered && low > 0 {
		page.Next = fmt.Sprintf("r2.%d", low)
		page.Incomplete = len(anchors) < limit
	}
	if len(anchors) == 0 {
		return page, nil
	}
	// RESOLVE EACH ENTRY TO ITS LATEST ROW, EXACTLY OR NOT AT ALL. The later rows
	// are after the entry, so one forward pass from the oldest entry to the size
	// read at the start covers them, holding one record per page entry. If that
	// span is over the resolve budget, or time or a read runs out before the pass
	// is done, the page is refused: a start row is only ever the truth when the
	// pass proved nothing newer exists, and a stale "running" or "uncharged"
	// answer is worse than a busy one.
	oldest := anchors[len(anchors)-1].off
	if size-oldest > s.resolveBudget {
		return RenderPage{Renders: []RenderRecord{}}, ErrRenderBusy
	}
	if s.beforeResolve != nil {
		s.beforeResolve()
	}
	byID := make(map[string]int, len(anchors))
	resolved := make([]RenderRecord, len(anchors))
	for index, a := range anchors {
		resolved[index] = a.rec
		byID[a.rec.ID] = index
	}
	timedOut, err = forwardScan(file, oldest, size, expired, func(_ int64, line []byte) {
		if row, ok := parseRenderRow(line); ok {
			if index, held := byID[row.ID]; held {
				resolved[index] = row.RenderRecord
			}
		}
	})
	switch {
	case err != nil:
		return RenderPage{Renders: []RenderRecord{}}, err
	case timedOut:
		return RenderPage{Renders: []RenderRecord{}}, ErrRenderBusy
	}
	page.Renders = resolved
	return page, nil
}

// get finds one id by a bounded reverse scan of the whole file.
func (s renderStore) get(id string) (RenderRecord, error) {
	file, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return RenderRecord{}, ErrRenderNotFound
		}
		return RenderRecord{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return RenderRecord{}, err
	}
	var latest *RenderRecord
	_, covered, timedOut, err := reverseScan(file, info.Size(), 1<<62, s.expiry(), func(_ int64, line []byte) bool {
		row, ok := parseRenderRow(line)
		if !ok || row.ID != id {
			return true
		}
		if latest == nil {
			rec := row.RenderRecord
			latest = &rec
		}
		return row.Ev != renderEvStart
	})
	switch {
	case err != nil:
		return RenderRecord{}, err
	case latest != nil:
		return *latest, nil
	case timedOut && !covered:
		return RenderRecord{}, ErrRenderBusy
	}
	return RenderRecord{}, ErrRenderNotFound
}

// unresolved is every render whose last row is its start: the renders a dead
// process left running. It streams the whole file and holds only the unresolved
// ones. A scan that could not finish returns an error and NO partial answer: an
// incomplete set must never be acted on as though it were the whole one.
func (s renderStore) unresolved() ([]RenderRecord, error) {
	file, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	open := map[string]RenderRecord{}
	timedOut, err := forwardScan(file, 0, info.Size(), s.expiry(), func(_ int64, line []byte) {
		if row, ok := parseRenderRow(line); ok {
			if row.Ev == renderEvStart {
				open[row.ID] = row.RenderRecord
			} else {
				delete(open, row.ID)
			}
		}
	})
	if err != nil {
		return nil, err
	}
	if timedOut {
		return nil, ErrRenderBusy
	}
	out := make([]RenderRecord, 0, len(open))
	for _, rec := range open {
		out = append(out, rec)
	}
	return out, nil
}

func parseRenderCursor(cursor string) (int64, bool) {
	rest, ok := strings.CutPrefix(cursor, "r2.")
	if !ok {
		return 0, false
	}
	offset, err := strconv.ParseInt(rest, 10, 64)
	return offset, err == nil && offset >= 0
}

// RenderPage is one window of renders, newest first. Next is absent only when
// the whole history has been read; Incomplete says the page is short because
// the read budget ran out, so keep following Next.
type RenderPage struct {
	Renders    []RenderRecord `json:"renders"`
	Next       string         `json:"next,omitempty"`
	Incomplete bool           `json:"incomplete,omitempty"`
}

// ── the owner's ledger ──────────────────────────────────────────────────────

// renderLive is a render this process still holds a goroutine for. It is kept
// until that goroutine returns, because a provider that ignored a cancel may
// still report what it charged.
type renderLive struct {
	rec   RenderRecord
	usage *ai.Usage // a charge reported before the ending, carried into it
}

type renderLedger struct {
	store   renderStore
	owner   *renderRetention
	session string
	mu      sync.Mutex
	file    *os.File
	live    map[string]*renderLive
	// notify is written once, before the ledger is published
	// ([jobRegistry.renderLedger]), and only read afterwards.
	notify  func(RenderRecord)
	scratch bool // the file is a temporary one, removed at close
	// restoreErr is why the restore at open could not finish; nil when it did.
	restoreErr error
	closed     bool // a late append after close is dropped, never reopens the file
}

func newRenderID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}
	return "rnd_" + hex.EncodeToString(buffer)
}

// openRenderLedger opens the ledger for a session the caller OWNS. A render the
// last process left running is recorded as interrupted — once, here, durably.
func openRenderLedger(path, session string, now time.Time) *renderLedger {
	return openRenderLedgerOn(newRenderStore(path), session, now)
}

// openRenderLedgerOn is [openRenderLedger] over a store with its own bounds.
func openRenderLedgerOn(store renderStore, session string, now time.Time) *renderLedger {
	return openRenderLedgerUsing(store, session, now, processRenderHistory)
}

func openRenderLedgerUsing(store renderStore, session string, now time.Time, owner *renderRetention) *renderLedger {
	path := store.path
	ledger := &renderLedger{store: store, owner: owner, session: strings.Clone(session), live: map[string]*renderLive{}}
	if path == "" {
		// A session with no folder (the legacy layout) has no durable home for
		// its history, so it gets a private temporary file that lives as long as
		// the session does and is removed when the ledger closes. The reads are
		// the same reads.
		if temp, err := os.CreateTemp("", "codeaf-renders-*.jsonl"); err == nil {
			ledger.store.path = temp.Name()
			ledger.scratch = true
			_ = temp.Close()
		}
		return ledger
	}
	// Recover actual receipts from a former owner before interpreting its start
	// records. A failed retry leaves the process-owned facts intact.
	if _, err := ledger.flushPendingLocked(); err != nil {
		return ledger
	}
	// The left-running renders are marked interrupted only when the whole file
	// was read. If it could not be, nothing is marked and restoreErr says why:
	// those renders keep reading as unknown, never as running, and the next open
	// tries again.
	left, err := ledger.store.unresolved()
	if err != nil {
		ledger.restoreErr = err
		return ledger
	}
	for _, rec := range left {
		rec := rec
		ledger.applyEnd(&rec, renderPatch{state: RenderInterrupted}, now)
		if err := ledger.append(renderEvEnd, rec); err != nil {
			ledger.restoreErr = err
			break
		}
	}
	return ledger
}

func (l *renderLedger) append(ev string, rec RenderRecord) error {
	if l.store.path == "" || l.closed {
		return errors.New("the render journal is unavailable")
	}
	if l.file == nil {
		file, err := os.OpenFile(l.store.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		l.file = file
	}
	line, err := json.Marshal(renderRow{Ev: ev, RenderRecord: rec})
	if err != nil {
		return err
	}
	info, err := l.file.Stat()
	if err != nil {
		_ = l.file.Close()
		l.file = nil
		return err
	}
	line = append(line, '\n')
	n, err := l.file.Write(line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		// A failed append may have left a partial row. Roll back only this
		// writer's attempted row before another retry can append behind it.
		if trimErr := l.file.Truncate(info.Size()); trimErr != nil {
			l.restoreErr = trimErr
		}
		_ = l.file.Close()
		l.file = nil
	}
	return err
}

// flushPendingLocked retains every unflushed terminal fact. No new paid render
// starts while a previous outcome cannot be journaled. The active-render gate
// bounds the number that can become pending concurrently.
func (l *renderLedger) flushPendingLocked() ([]RenderRecord, error) {
	var saved []RenderRecord
	if l.restoreErr != nil {
		return nil, l.restoreErr
	}
	for _, pending := range l.owner.pending(l.store.path) {
		rec := pending.record
		rec.Persistence = ""
		if err := l.append(pending.event, rec); err != nil {
			return saved, err
		}
		if !l.owner.saved(pending.key, pending.revision, pending.event) {
			continue
		}
		if entry := l.live[rec.ID]; entry != nil {
			entry.rec = rec
		}
		saved = append(saved, rec)
	}
	if l.owner.hasPending(l.store.path) {
		return saved, ErrRenderBusy
	}
	return saved, nil
}

func (l *renderLedger) emit(rec RenderRecord) {
	rec = cloneRenderRecord(rec)
	if l.notify != nil {
		l.notify(rec)
	}
}

// begin records a render's start and returns its stable id.
func (l *renderLedger) begin(job int, kind, model, run, call string, now time.Time) string {
	id, _ := l.beginChecked(job, kind, model, run, call, now)
	return id
}

func (l *renderLedger) beginChecked(job int, kind, model, run, call string, now time.Time) (string, error) {
	l.mu.Lock()
	var notices []RenderRecord
	defer func() {
		l.mu.Unlock()
		for _, rec := range notices {
			l.emit(rec)
		}
	}()
	if l.closed {
		return "", errors.New("the render session has closed")
	}
	saved, err := l.flushPendingLocked()
	notices = append(notices, saved...)
	if err != nil {
		return "", fmt.Errorf("pending render facts must be saved before another render starts: %w", err)
	}
	rec := RenderRecord{
		ID: newRenderID(), Job: job, Session: l.session, Kind: kind, State: RenderRunning,
		Model: strings.Clone(model), Started: now, Run: strings.Clone(run), Call: strings.Clone(call), Charge: RenderChargeUnknown,
	}
	if rec.ID == "" || len(model) > 1024 || len(run) > 1024 || len(call) > 1024 || len(l.session) > 1024 {
		return "", errors.New("the render identity is unavailable or exceeds the metadata limit")
	}
	if !l.owner.reserve(l.store.path, rec.ID) {
		return "", errors.New("the process has no room to retain another render outcome; restore storage for unsaved renders and retry")
	}
	if err := l.append(renderEvStart, rec); err != nil {
		l.owner.release(l.store.path, rec.ID)
		return "", fmt.Errorf("the render start could not be saved: %w", err)
	}
	l.live[rec.ID] = &renderLive{rec: rec}
	notices = append(notices, rec)
	return rec.ID, nil
}

func (l *renderLedger) applyEnd(rec *RenderRecord, patch renderPatch, now time.Time) {
	rec.State = patch.state
	rec.Committed = patch.committed
	rec.Path = patch.path
	ended := now
	rec.Ended = &ended
	duration := now.Sub(rec.Started).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	rec.DurationMS = &duration
	if patch.usage != nil {
		rec.Charge, rec.CostUSD = renderCharge(patch.usage)
	}
	if patch.state == RenderCancelled {
		rec.Upstream = "unknown"
	}
	rec.Refusal = ""
	if len(patch.refusal) <= 128 {
		rec.Refusal = strings.Clone(patch.refusal)
	}
	rec.Options = nil
	for _, option := range patch.options[:min(len(patch.options), 8)] {
		if option != "" && len(option) <= 1024 {
			rec.Options = append(rec.Options, strings.Clone(option))
		}
	}
	rec.Error = strings.Join(strings.Fields(patch.err), " ")
	if runes := []rune(rec.Error); len(runes) > renderErrorRunes {
		rec.Error = string(runes[:renderErrorRunes])
	}
}

// settle writes the render's one ending and reports whether THIS call was it.
// A render already ended answers false and writes nothing.
func (l *renderLedger) settle(id string, patch renderPatch, now time.Time) bool {
	l.mu.Lock()
	entry := l.live[id]
	if entry == nil || entry.rec.State != RenderRunning {
		l.mu.Unlock()
		return false
	}
	if patch.usage == nil {
		patch.usage = entry.usage
	}
	l.applyEnd(&entry.rec, patch, now)
	entry.rec.Persistence = ""
	if err := l.append(renderEvEnd, entry.rec); err != nil {
		entry.rec.Persistence = "unavailable"
		l.owner.retain(l.store.path, entry.rec, renderEvEnd)
	}
	rec := entry.rec
	l.mu.Unlock()
	l.emit(rec)
	return true
}

// charge reports what the provider said it charged, whenever it says it. Before
// the ending it rides into the ending; after it, it is a receipt on the SAME
// record: same id, same state, one row, once. A charge the provider did not
// report is not recorded as anything.
func (l *renderLedger) charge(id string, usage *ai.Usage, now time.Time) {
	if known, _ := renderCharge(usage); known == RenderChargeUnknown {
		return
	}
	l.mu.Lock()
	entry := l.live[id]
	if entry == nil {
		l.mu.Unlock()
		return
	}
	if entry.rec.State == RenderRunning {
		entry.usage = usage
		l.mu.Unlock()
		return
	}
	if entry.rec.Charge != RenderChargeUnknown {
		l.mu.Unlock()
		return
	}
	entry.rec.Charge, entry.rec.CostUSD = renderCharge(usage)
	entry.rec.Persistence = ""
	if err := l.append(renderEvCharge, entry.rec); err != nil {
		entry.rec.Persistence = "unavailable"
		l.owner.retain(l.store.path, entry.rec, renderEvCharge)
	}
	rec := entry.rec
	l.mu.Unlock()
	l.emit(rec)
}

// release says the render's goroutine has returned: nothing more can arrive.
func (l *renderLedger) release(id string) {
	l.mu.Lock()
	delete(l.live, id)
	l.owner.release(l.store.path, id)
	l.mu.Unlock()
}

func (l *renderLedger) get(id string) (RenderRecord, error) {
	l.mu.Lock()
	if entry := l.live[id]; entry != nil {
		rec := cloneRenderRecord(entry.rec)
		l.mu.Unlock()
		return rec, nil
	}
	l.mu.Unlock()
	if rec, ok := l.owner.get(l.store.path, id); ok {
		return rec, nil
	}
	rec, err := l.store.get(id)
	return l.settleUnresolved(rec), err
}

func (l *renderLedger) list(limit int, cursor string) (RenderPage, error) {
	l.mu.Lock()
	saved, flushErr := l.flushPendingLocked()
	l.mu.Unlock()
	for _, rec := range saved {
		l.emit(rec)
	}
	if flushErr != nil {
		return RenderPage{}, ErrRenderBusy
	}
	page, err := l.store.list(limit, cursor)
	if err != nil {
		return page, err
	}
	l.mu.Lock()
	for index, rec := range page.Renders {
		if entry := l.live[rec.ID]; entry != nil {
			page.Renders[index] = cloneRenderRecord(entry.rec)
		}
	}
	l.mu.Unlock()
	for index := range page.Renders {
		l.settleUnresolvedInPlace(&page.Renders[index])
	}
	return page, nil
}

// settleUnresolved shows a render whose last row is its start, and which this
// process is not running, as unknown rather than running.
func (l *renderLedger) settleUnresolved(rec RenderRecord) RenderRecord {
	l.settleUnresolvedInPlace(&rec)
	return rec
}

func (l *renderLedger) settleUnresolvedInPlace(rec *RenderRecord) {
	if rec.State != RenderRunning {
		return
	}
	l.mu.Lock()
	_, mine := l.live[rec.ID]
	l.mu.Unlock()
	if !mine {
		rec.State = RenderUnknown
	}
}

func (l *renderLedger) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
	if l.scratch {
		_ = os.Remove(l.store.path)
	}
}

// ReadRenders lists a session's renders from disk WITHOUT owning the session
// (the conversation is not open here). A render whose last row is its start is
// shown as unknown: it may have ended since, and claiming otherwise is a guess.
func ReadRenders(sessionDir string, limit int, cursor string) (RenderPage, error) {
	path := Place{Dir: sessionDir}.join(placeRenders)
	if path == "" {
		return RenderPage{Renders: []RenderRecord{}}, nil
	}
	if processRenderHistory.hasPending(path) {
		return RenderPage{}, ErrRenderBusy
	}
	page, err := newRenderStore(path).list(limit, cursor)
	for index := range page.Renders {
		if page.Renders[index].State == RenderRunning {
			page.Renders[index].State = RenderUnknown
		}
	}
	return page, err
}

// ReadRender is [ReadRenders] for one id; an id the history does not hold is
// [ErrRenderNotFound], and a lookup that ran out of time is [ErrRenderBusy].
func ReadRender(sessionDir, id string) (RenderRecord, error) {
	path := Place{Dir: sessionDir}.join(placeRenders)
	if path == "" {
		return RenderRecord{}, ErrRenderNotFound
	}
	if rec, ok := processRenderHistory.get(path, id); ok {
		return rec, nil
	}
	rec, err := newRenderStore(path).get(id)
	if rec.State == RenderRunning {
		rec.State = RenderUnknown
	}
	return rec, err
}

// renderCharge reads the provider's own usage: a positive finite cost is
// charged, an explicit zero is not charged, anything else is unknown. A missing
// figure is never turned into a zero.
func renderCharge(usage *ai.Usage) (RenderCharge, *float64) {
	if usage == nil || usage.Cost == nil {
		return RenderChargeUnknown, nil
	}
	cost := *usage.Cost
	switch {
	case math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0:
		return RenderChargeUnknown, nil
	case cost == 0:
		return RenderNotCharged, nil
	}
	return RenderCharged, &cost
}
