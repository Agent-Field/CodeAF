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
//
// Append-only also means the file grows a line for every row ever judged, and
// a long-lived run would read the whole of that back on every Send and every
// Pending. So a Send whose file has grown mostly into rows it can no longer
// need rewrites it to the pending rows alone — through a temporary file in the
// same directory, never in place — and the markers for rows that are gone go
// with the rows.
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
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultMaxPending is the cap on pending rows when Outbox.MaxPending is zero
// or below.
const DefaultMaxPending = 5000

// compactFloor is the number of lines below which a Send does not bother to
// rewrite the file: a handful of lines costs less to read than to copy, and
// the markers of a run that has judged a few thousand rows are what a rewrite
// is worth. See [Outbox.compactLocked] for what a rewrite does and when.
const compactFloor = 1024

const (
	rowSchema  = 1
	nonceLen   = 16
	maxPayload = 64 << 10 // 64 KiB, measured on the compacted payload
	maxBatch   = 200      // rows per POST
	// maxReason caps the reason a dropped marker carries, so one refusal
	// cannot grow a marker past a short line.
	maxReason = 200
	// capDropReason is the reason a cap drop carries: the row was refused by
	// no destination, it simply aged out.
	capDropReason = "over cap"
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
// batch arrived or because a drop retired it — the pending cap, or a
// destination that refused it by line. A marker is a line of its own in the
// same file, which is what keeps the outbox append-only. Reason is written
// only on a drop, so a file written before reasons were kept still parses and
// reads back with the reason empty.
type mark struct {
	Sent    string `json:"sent,omitempty"`
	Dropped string `json:"dropped,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Drop is one row a dropped marker retired without sending it: the row's nonce
// and the reason it was dropped — a destination's refusal text, or the cap's
// own word. A file written before reasons were kept answers with the reason
// empty.
type Drop struct {
	Nonce  string
	Reason string
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
	// Install is the install's own nonce, carried as the X-Codeaf-Install
	// header on every batch sent over http; empty sends no header. A
	// destination that requires the header names it in its error, so a batch
	// sent without one is a batch the destination refuses — the caller sets
	// this from its own store before the first Send.
	Install string

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
	rows, _ := o.load()
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
	rows, _ := o.load()
	return rows
}

// Dropped answers the rows a dropped marker retired without sending them, in
// the order they were dropped — oldest first, so the last is the most recent
// drop and the reason beside it is the one a reading form shows. It never
// fails: a line that does not parse is skipped, and a file that cannot be read
// answers as no drops at all. A file that has been compacted holds only its
// pending rows, so the dropped markers of rows a rewrite took go with them.
func (o *Outbox) Dropped() []Drop {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.loadDropped()
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
// transmitted and is not counted. A batch the destination refused by line —
// a 400 naming `line N` — is refused on one row it has said it will never
// accept: the named row is marked dropped, the rows around it are sent
// within the same call, and Send counts the sent ones. Rows whose batch did
// not arrive, and every row after it, stay pending, and Send returns an
// error that says what failed.
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
	rows, lines := o.load()
	pend := make([]string, len(rows))
	for i, r := range rows {
		pend[i] = r.Nonce
	}
	o.pend = pend
	// The file carried every row the outbox ever judged, so a run that sends
	// often grows it without bound and every Send reads the whole of it. Send is
	// where that is worth undoing: it is the read that would pay for the whole
	// file, sendMu holds every other Send out while this one runs its first
	// POST, and the file is rewritten only once it is mostly dead — more lines
	// than twice the rows still pending — so each rewrite is paid for by the
	// rows it drops and a run's own path meets one at most once per floor's
	// worth of rows. A rewrite that fails is not a failed Send: the rows stay
	// pending and the next Send tries again.
	if lines > compactFloor && lines > 2*len(pend) {
		_ = o.compactLocked(rows)
	}
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
	if o.writeMarksLocked(droppedMarks(dropped, capDropReason)) != nil {
		// The cap is kept by the markers on disk, so a cap this Append could
		// not record is a cap that is not kept. The rows stay pending and the
		// next Append tries the trim again, which is the same answer the
		// failed row write itself gives.
		return
	}
	o.pend = append(o.pend[:0], o.pend[drop:]...)
}

// retire appends one marker per nonce and takes those rows out of the pending
// set. A marker that could not be written leaves its rows pending, where the
// next Send tries them again rather than losing them — which is why the drop
// happens only once the write has gone through. The alternative, a row gone
// from the pending set and present in the file, is a row that comes back at
// the next Open having been reported delivered.
func (o *Outbox) retire(marks []mark) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.writeMarksLocked(marks) != nil {
		return
	}
	o.dropPendingLocked(marks)
}

// writeMarksLocked appends one marker line per mark, and answers whether the
// line reached the file. The caller holds mu.
func (o *Outbox) writeMarksLocked(marks []mark) error {
	if len(marks) == 0 {
		return nil
	}
	if o.f == nil {
		return errors.New("outbox: outbox is closed")
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
	_, err := o.f.Write(buf)
	return err
}

// dropPendingLocked removes the marked rows from the pending list, ONE ROW PER
// MARKER and oldest first. A nonce is not an identity: [Outbox.Rand] is the
// caller's to set, and a source that answers with the same bytes twice writes
// the same nonce twice. Treating the marks as a set of names would then take
// every row sharing a name — the rows a failed batch left pending, the row a
// trim was told to keep — out of the outbox with it. Markers are only ever
// written for the oldest rows, so counting them off the front is what they
// mean. The caller holds mu.
func (o *Outbox) dropPendingLocked(marks []mark) {
	gone := make(map[string]int, len(marks))
	for _, m := range marks {
		if m.Sent != "" {
			gone[m.Sent]++
		}
		if m.Dropped != "" {
			gone[m.Dropped]++
		}
	}
	kept := o.pend[:0]
	for _, n := range o.pend {
		if gone[n] > 0 {
			gone[n]--
			continue
		}
		kept = append(kept, n)
	}
	o.pend = kept
}

// load reads the outbox file and answers the rows still pending, in file
// order, and the number of lines the file held — rows, markers and lines that
// parsed as neither. A line that does not parse, or that is not a row, is
// skipped, and a file that cannot be read answers as no rows and no lines at
// all. The caller holds mu.
//
// EACH MARKER RETIRES ONE ROW, oldest first, wherever the marker sits — see
// [Outbox.dropPendingLocked] for why the count rather than the name: a nonce
// is what the caller's own [Outbox.Rand] made of sixteen bytes, and a reader
// that took a marker as a name would answer that a file holding two rows under
// one nonce and one marker holds nothing at all.
func (o *Outbox) load() ([]Row, int) {
	b, err := os.ReadFile(o.path)
	if err != nil {
		return nil, 0
	}
	var rows []Row
	retired := make(map[string]int)
	lines := 0
	for len(b) > 0 {
		lines++
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
				retired[m.Sent]++
				continue
			}
			if m.Dropped != "" {
				retired[m.Dropped]++
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
		if retired[r.Nonce] > 0 {
			retired[r.Nonce]--
			continue
		}
		kept = append(kept, r)
	}
	return kept, lines
}

// loadDropped reads the outbox file and answers its dropped markers, in file
// order, oldest first. A line that does not parse, or that is not a dropped
// marker, is skipped, and a file that cannot be read answers as no drops at
// all. A reason is absent in a file written before reasons were kept, which
// the reading leaves empty. The caller holds mu.
func (o *Outbox) loadDropped() []Drop {
	b, err := os.ReadFile(o.path)
	if err != nil {
		return nil
	}
	var drops []Drop
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
		if json.Unmarshal(line, &m) == nil && m.Dropped != "" {
			drops = append(drops, Drop{Nonce: m.Dropped, Reason: m.Reason})
		}
	}
	return drops
}

// compactLocked rewrites the outbox file to hold exactly the pending rows,
// dropping every marker and every row a marker retired with them — a marker
// means nothing once its row is gone. It writes a temporary file in the same
// directory, fsyncs it, and renames it over the original before reopening the
// handle, never truncating in place: a crash before the rename leaves the old
// file whole, and a crash after it leaves a file holding the same pending
// rows, so a row is never lost and a retired row never comes back to be sent
// twice. The caller holds mu, and Send calls it before its first POST, so no
// Send of this process is between a POST and its markers. Its error is Send's
// to ignore — the file is either untouched or already compacted.
//
// A codeaf that already holds the old file keeps writing to the inode the
// rename unlinked, and those appends are lost. The profile is one writer's to
// hold across processes, which is the assumption the append-only file already
// rests on; nothing here reaches past it.
func (o *Outbox) compactLocked(rows []Row) error {
	tmp, err := os.CreateTemp(filepath.Dir(o.path), filepath.Base(o.path)+".compact-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	for _, r := range rows {
		line, err := rowLine(r)
		if err != nil {
			return err
		}
		if _, err := tmp.Write(line); err != nil {
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, o.path); err != nil {
		return err
	}
	renamed = true
	nf, err := os.OpenFile(o.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, fileMode)
	if err != nil {
		// The file on disk is the compacted one and holds every pending row, so
		// the work is done; only the handle cannot be had. Refuse writes rather
		// than write to the inode the rename unlinked.
		o.f.Close()
		o.f = nil
		return err
	}
	o.f.Close()
	o.f = nf
	return nil
}

// sendHTTP posts the rows to dest, at most maxBatch per POST, and marks each
// batch that came back 2xx. A batch refused by line — a 400 whose reply names
// `line N` — is refused on one row the destination will never take: that row
// is marked dropped, so it stays gone across a reopen, and the rest of the
// batch is posted again in the same call. Each retry retires a row, so a
// batch of refused rows costs one POST per row at the most and never a spin.
// The first batch that fails any other way ends the call, its rows and every
// row after it staying pending: a 400 that names no line and a 413 name no
// row at all, so retiring one would be a guess, and 429 and 5xx are about
// the destination rather than any row.
func (o *Outbox) sendHTTP(ctx context.Context, dest string, rows []Row) (int, error) {
	client := o.Client
	if client == nil {
		client = defaultClient
	}
	n := 0
	for start := 0; start < len(rows); {
		end := start + maxBatch
		if end > len(rows) {
			end = len(rows)
		}
		if err := ctx.Err(); err != nil {
			return n, err
		}
		sent, err := o.postBatch(ctx, client, dest, rows[start:end])
		if err != nil {
			return n, err
		}
		if len(sent) > 0 {
			o.retire(sentMarks(rowNonces(sent)))
			n += len(sent)
		}
		start = end
	}
	return n, nil
}

// postBatch posts one batch and answers the rows of it that arrived. A reply
// that names a line takes that row out and the batch is posted again; a batch
// every row of which was refused answers no rows and no error, the way a
// Keep that declines everything does.
func (o *Outbox) postBatch(ctx context.Context, client *http.Client, dest string, batch []Row) ([]Row, error) {
	for len(batch) > 0 {
		body := make([]byte, 0, len(batch)*128)
		for _, r := range batch {
			line, err := rowLine(r)
			if err != nil {
				return nil, err
			}
			body = append(body, line...)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-ndjson")
		if o.Install != "" {
			req.Header.Set("X-Codeaf-Install", o.Install)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		reply, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return batch, nil
		}
		if line, reason := refusedText(reply, batch); line >= 0 {
			o.retire([]mark{{Dropped: batch[line].Nonce, Reason: reason}})
			rest := make([]Row, 0, len(batch)-1)
			rest = append(rest, batch[:line]...)
			rest = append(rest, batch[line+1:]...)
			batch = rest
			continue
		}
		return nil, fmt.Errorf("outbox: %s replied %s", dest, resp.Status)
	}
	return nil, nil
}

// refusedText reads a refusal reply and answers which row of the batch it
// refuses and the reason it gives, or -1 and "" when it names no line of the
// batch. The relay refuses the first row that fails its schema and answers
// `line N: <why>`, N the 1-based number of the line as posted; a batch is one
// row per line with no blank lines, so the named line is batch[N-1] and the
// reason is what followed the colon.
func refusedText(reply []byte, batch []Row) (int, string) {
	var refused struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(reply, &refused) != nil || refused.Error == "" {
		return -1, ""
	}
	num, ok := strings.CutPrefix(refused.Error, "line ")
	if !ok {
		return -1, ""
	}
	colon := strings.IndexByte(num, ':')
	if colon < 1 {
		return -1, ""
	}
	line, err := strconv.Atoi(num[:colon])
	if err != nil || line < 1 || line > len(batch) {
		return -1, ""
	}
	return line - 1, dropReason(num[colon+1:])
}

// dropReason is a refusal's reason as a dropped marker stores it: the text
// after `line N:` trimmed of the space around it, and capped at maxReason so
// one destination cannot grow a marker past a short line.
func dropReason(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > maxReason {
		text = text[:maxReason]
	}
	return text
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
	marks := make([]mark, len(nonces))
	for i, n := range nonces {
		marks[i] = mark{Sent: n}
	}
	return marks
}

// droppedMarks retires rows the outbox will not send again, each carrying the
// reason it was dropped: a destination's refusal text, or the cap's own word.
func droppedMarks(nonces []string, reason string) []mark {
	marks := make([]mark, len(nonces))
	for i, n := range nonces {
		marks[i] = mark{Dropped: n, Reason: reason}
	}
	return marks
}
