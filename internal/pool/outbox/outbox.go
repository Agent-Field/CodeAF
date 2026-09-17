// Package outbox keeps small measurement rows in a local append-only file and
// hands them to a destination: an HTTP endpoint that takes newline-delimited
// JSON, or another file. One line is one row, so the rows survive a restart
// and Pending() is answered by reading the file back. Nothing here gets in the
// caller's way — a full disk, a dead server, a destination that is nonsense
// all surface as error values — and a row whose batch arrived is marked in the
// file, so it is never transmitted twice.
//
// The file holds rows and, interleaved with them, one-line sent and dropped
// markers that retire rows; a line that is not a row is skipped on read. That
// is what keeps the file append-only while a cap on pending rows still drops
// from the old end and a mark outlives closing the outbox and opening the same
// path again.
package outbox

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultMaxPending is the cap on pending rows when Outbox.MaxPending is zero
// or below.
const DefaultMaxPending = 5000

const (
	rowSchema  = 1
	nonceLen   = 16
	maxPayload = 64 << 10 // 64 KiB, measured on the compacted payload
	maxBatch   = 200      // rows per POST
	// defaultBudget bounds one Send when Outbox.Budget is zero.
	defaultBudget = 2 * time.Second
	dirMode       = 0700
	fileMode      = 0600
)

// Row is one measurement as it is stored in the outbox file, one row per line.
type Row struct {
	Schema  int             `json:"schema"` // always 1
	Day     string          `json:"day"`    // "YYYY-MM-DD", UTC
	Nonce   string          `json:"nonce"`  // 16 random bytes, lowercase hex
	Payload json.RawMessage `json:"payload"`
}

// mark retires rows: a retired row is no longer pending, either because its
// batch arrived or because the pending cap dropped it. A marker is a line of
// its own in the same file, which is what keeps the outbox append-only.
type mark struct {
	Sent    string `json:"sent,omitempty"`
	Dropped string `json:"dropped,omitempty"`
}

// Outbox keeps rows in one append-only file until they are sent. The six
// exported fields are read every time they are used, so a caller may set them
// after Open and before the first Append or Send.
type Outbox struct {
	// Now answers the time a row was appended; nil means time.Now.
	Now func() time.Time
	// Rand is read for nonce bytes; nil means crypto/rand.Reader.
	Rand io.Reader
	// Keep decides, at Send time, which payloads are transmitted; nil keeps
	// every row. A payload Keep declines leaves the outbox without reaching
	// the destination.
	Keep func(payload json.RawMessage) bool
	// Budget bounds one Send call as a whole; zero means 2s.
	Budget time.Duration
	// Client sends the batches; nil means a client the package makes.
	Client *http.Client
	// MaxPending caps the pending rows, dropping from the old end; zero or
	// below means DefaultMaxPending.
	MaxPending int

	path   string
	sendMu sync.Mutex // one Send at a time, so two never carry the same rows
	mu     sync.Mutex // guards f and pend
	f      *os.File
	pend   []string // nonces of the pending rows, oldest first
}

// defaultClient is shared by every outbox that does not name its own. Its
// requests are bounded by the Send budget, so no client-level timeout.
var defaultClient = &http.Client{}

// Open prepares the outbox file at path, creating the parent directory with
// mode 0700 if it is missing and the file itself with mode 0600. A path that
// already holds rows keeps them, markers included. An empty path is an error.
func Open(path string) (*Outbox, error) {
	if path == "" {
		return nil, errors.New("outbox: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, fileMode)
	if err != nil {
		return nil, err
	}
	o := &Outbox{path: path, f: f}
	o.mu.Lock()
	defer o.mu.Unlock()
	rows := o.load()
	o.pend = make([]string, 0, len(rows))
	for _, r := range rows {
		o.pend = append(o.pend, r.Nonce)
	}
	return o, nil
}

// Append writes one row holding payload. The row is refused, and nothing is
// written, when the payload is not valid JSON, when it is longer than 64 KiB
// once compacted, or when it still holds a newline once compacted. The payload
// is stored compacted, so one row is always one line.
func (o *Outbox) Append(payload json.RawMessage) error {
	compacted, err := compact(payload)
	if err != nil {
		return err
	}
	if len(compacted) > maxPayload {
		return errors.New("outbox: payload is longer than 64 KiB once compacted")
	}
	if bytes.IndexByte(compacted, '\n') >= 0 {
		return errors.New("outbox: payload holds a newline once compacted")
	}
	nonce := make([]byte, nonceLen)
	rand := o.Rand
	if rand == nil {
		rand = cryptorand.Reader
	}
	if _, err := io.ReadFull(rand, nonce); err != nil {
		return fmt.Errorf("outbox: read nonce: %w", err)
	}
	now := o.Now
	var t time.Time
	if now == nil {
		t = time.Now()
	} else {
		t = now()
	}
	row := Row{
		Schema:  rowSchema,
		Day:     t.UTC().Format("2006-01-02"),
		Nonce:   hex.EncodeToString(nonce),
		Payload: json.RawMessage(compacted),
	}
	line, err := rowLine(row)
	if err != nil {
		return err
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	if o.f == nil {
		return errors.New("outbox: outbox is closed")
	}
	if _, err := o.f.Write(line); err != nil {
		return err
	}
	o.pend = append(o.pend, row.Nonce)
	o.trimLocked()
	return nil
}

// Pending answers the rows that have not been sent, in the order they were
// appended. It never fails: a line that does not parse, or that is not a row,
// is skipped, and a file that cannot be read answers as no rows at all.
func (o *Outbox) Pending() []Row {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.load()
}

// Send hands the pending rows to dest and answers with how many rows it
// transmitted. A dest beginning https:// or http:// receives one POST per
// batch of at most 200 rows, content-type application/x-ndjson, the body one
// row per line in pending order, each line the same JSON object the file
// holds; a 2xx answer means that batch arrived. Any other dest is a
// filesystem path, plain or written file://, that the rows are appended to,
// created with mode 0600 and a parent directory of mode 0700 if either is
// missing. An empty dest, or a Send with nothing pending, is a no-op: 0 and a
// nil error, the rows staying pending.
//
// The whole call lives inside Budget, which is 2s when it is zero, and inside
// ctx as well, whichever ends first. Rows whose batch arrived are marked sent
// and leave the outbox; a row Keep declines is marked sent without being
// transmitted and is not counted. Rows whose batch did not arrive, and every
// row after it, stay pending, and Send returns an error that says what
// failed.
func (o *Outbox) Send(ctx context.Context, dest string) (int, error) {
	if dest == "" {
		return 0, nil
	}
	o.sendMu.Lock()
	defer o.sendMu.Unlock()

	o.mu.Lock()
	if o.f == nil {
		o.mu.Unlock()
		return 0, errors.New("outbox: outbox is closed")
	}
	rows := o.load()
	pend := make([]string, len(rows))
	for i, r := range rows {
		pend[i] = r.Nonce
	}
	o.pend = pend
	o.mu.Unlock()

	if len(rows) == 0 {
		return 0, nil
	}

	budget := o.Budget
	if budget <= 0 {
		budget = defaultBudget
	}
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(budget))
	defer cancel()

	// Rows the caller declines leave the outbox at once, whether or not any
	// later batch arrives.
	keep := o.Keep
	var deliver []Row
	if keep == nil {
		deliver = rows
	} else {
		deliver = make([]Row, 0, len(rows))
		var declined []string
		for _, r := range rows {
			if keep(r.Payload) {
				deliver = append(deliver, r)
			} else {
				declined = append(declined, r.Nonce)
			}
		}
		if len(declined) > 0 {
			o.retire(sentMarks(declined))
		}
	}
	if len(deliver) == 0 {
		return 0, nil
	}

	if strings.HasPrefix(dest, "https://") || strings.HasPrefix(dest, "http://") {
		return o.sendHTTP(ctx, dest, deliver)
	}
	return o.sendFile(ctx, strings.TrimPrefix(dest, "file://"), deliver)
}

// Close releases whatever the outbox holds open. Calling it more than once is
// not an error. A closed outbox refuses Append and Send; Pending still
// answers by reading the file.
func (o *Outbox) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.f == nil {
		return nil
	}
	err := o.f.Close()
	o.f = nil
	return err
}

// trimLocked drops the oldest pending rows until the cap holds, writing a
// dropped marker for each so they stay gone across a reopen. The row just
// appended is the last one, so it always survives. The caller holds mu.
func (o *Outbox) trimLocked() {
	limit := o.MaxPending
	if limit <= 0 {
		limit = DefaultMaxPending
	}
	drop := len(o.pend) - limit
	if drop <= 0 {
		return
	}
	dropped := make([]string, drop)
	copy(dropped, o.pend[:drop])
	o.writeMarksLocked(droppedMarks(dropped))
	o.pend = append(o.pend[:0], o.pend[drop:]...)
}

// retire appends one marker per nonce and takes those rows out of the pending
// set. A marker write is best effort: a failed write leaves the rows pending,
// where the next Send tries them again rather than losing them.
func (o *Outbox) retire(marks []mark) {
	gone := make(map[string]bool, len(marks))
	for _, m := range marks {
		if m.Sent != "" {
			gone[m.Sent] = true
		}
		if m.Dropped != "" {
			gone[m.Dropped] = true
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.writeMarksLocked(marks)
	o.dropPendingLocked(gone)
}

// writeMarksLocked appends one marker line per mark. The caller holds mu.
func (o *Outbox) writeMarksLocked(marks []mark) {
	if o.f == nil || len(marks) == 0 {
		return
	}
	buf := make([]byte, 0, 24*len(marks))
	for _, m := range marks {
		line, err := json.Marshal(m)
		if err != nil {
			continue // a struct of strings never fails to marshal
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	o.f.Write(buf) // best effort; see retire
}

// dropPendingLocked removes the named nonces from the pending list. The
// caller holds mu.
func (o *Outbox) dropPendingLocked(gone map[string]bool) {
	kept := o.pend[:0]
	for _, n := range o.pend {
		if !gone[n] {
			kept = append(kept, n)
		}
	}
	o.pend = kept
}

// load reads the outbox file and answers the rows still pending, in file
// order. A line that does not parse, or that is not a row, is skipped, and a
// file that cannot be read answers as no rows at all. A row whose nonce a
// sent or dropped marker retires is left out, wherever the marker sits. The
// caller holds mu.
func (o *Outbox) load() []Row {
	b, err := os.ReadFile(o.path)
	if err != nil {
		return nil
	}
	var rows []Row
	retired := make(map[string]bool)
	for len(b) > 0 {
		line := b
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			line, b = b[:i], b[i+1:]
		} else {
			b = nil
		}
		if len(line) == 0 {
			continue
		}
		var m mark
		if json.Unmarshal(line, &m) == nil {
			if m.Sent != "" {
				retired[m.Sent] = true
				continue
			}
			if m.Dropped != "" {
				retired[m.Dropped] = true
				continue
			}
		}
		var r Row
		if json.Unmarshal(line, &r) != nil || r.Nonce == "" {
			continue
		}
		rows = append(rows, r)
	}
	kept := rows[:0]
	for _, r := range rows {
		if !retired[r.Nonce] {
			kept = append(kept, r)
		}
	}
	return kept
}

// sendHTTP posts the rows to dest, at most maxBatch per POST, and marks each
// batch that came back 2xx. The first batch that fails ends the call; its
// rows and every row after it stay pending.
func (o *Outbox) sendHTTP(ctx context.Context, dest string, rows []Row) (int, error) {
	client := o.Client
	if client == nil {
		client = defaultClient
	}
	n := 0
	for start := 0; start < len(rows); start += maxBatch {
		end := start + maxBatch
		if end > len(rows) {
			end = len(rows)
		}
		if err := ctx.Err(); err != nil {
			return n, err
		}
		body := make([]byte, 0, (end-start)*128)
		for _, r := range rows[start:end] {
			line, err := rowLine(r)
			if err != nil {
				return n, err
			}
			body = append(body, line...)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest, bytes.NewReader(body))
		if err != nil {
			return n, err
		}
		req.Header.Set("Content-Type", "application/x-ndjson")
		resp, err := client.Do(req)
		if err != nil {
			return n, err
		}
		ok := resp.StatusCode >= 200 && resp.StatusCode < 300
		io.CopyN(io.Discard, resp.Body, 1<<16)
		resp.Body.Close()
		if !ok {
			return n, fmt.Errorf("outbox: %s replied %s", dest, resp.Status)
		}
		o.retire(sentMarks(rowNonces(rows[start:end])))
		n += end - start
	}
	return n, nil
}

// sendFile appends the rows to the file at path, creating it with mode 0600
// and its parent directory with mode 0700 if either is missing.
func (o *Outbox) sendFile(ctx context.Context, path string, rows []Row) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, fileMode)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	body := make([]byte, 0, len(rows)*128)
	for _, r := range rows {
		line, err := rowLine(r)
		if err != nil {
			return 0, err
		}
		body = append(body, line...)
	}
	if _, err := f.Write(body); err != nil {
		return 0, err
	}
	o.retire(sentMarks(rowNonces(rows)))
	return len(rows), nil
}

// compact checks that payload is valid JSON and answers it compacted, so a
// row is always one line.
func compact(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("outbox: payload is not valid JSON")
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, payload); err != nil {
		return nil, fmt.Errorf("outbox: payload is not valid JSON: %w", err)
	}
	return buf.Bytes(), nil
}

// rowLine answers the JSON object for one row as it is stored in the file,
// newline included. HTML escaping is off so a payload keeps the bytes compaction
// gave it, and every later read of the same row answers the same bytes.
func rowLine(row Row) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(row); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func rowNonces(rows []Row) []string {
	nonces := make([]string, len(rows))
	for i, r := range rows {
		nonces[i] = r.Nonce
	}
	return nonces
}

func sentMarks(nonces []string) []mark {
	return nonceMarks(nonces, true)
}

func droppedMarks(nonces []string) []mark {
	return nonceMarks(nonces, false)
}

func nonceMarks(nonces []string, sent bool) []mark {
	marks := make([]mark, len(nonces))
	for i, n := range nonces {
		if sent {
			marks[i] = mark{Sent: n}
		} else {
			marks[i] = mark{Dropped: n}
		}
	}
	return marks
}
