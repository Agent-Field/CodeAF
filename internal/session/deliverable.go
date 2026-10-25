package session

// deliverable.go is the typed deliverable fact on the deliverables index and
// the reader that projects it.
//
// ── ONE LEDGER ──
//
// There is no second store. A deliverable is a row of the existing index
// (artifacts.go) that carries an optional [DeliverableFact]. A row written
// before this file existed has none, still parses and still lists, and its
// turn, task, inputs and state are UNKNOWN: nothing is ever inferred from a
// title, the current turn, a time, or where the file happens to sit today.
//
// ── UPDATES ARE FULL SNAPSHOTS ──
//
// Keep, Remove and the like append a row with the same id and Update true. The
// update restates the whole deliverable (same path, title, session, kind and
// Created as its base), so the NEWEST row of an id alone is the projection and
// a reader never has to join two rows that a page boundary could separate. An
// older row of an id that has a newer one is superseded and never lists. The
// index is never rewritten, so a file that has gone missing keeps its row.
//
// ── PAGING IS OVER THE WHOLE HISTORY ──
//
// [ReadArtifacts] keeps the newest 2000 rows, which is right for a picker and
// wrong for a complete listing. These readers walk the index backward by byte
// offset (mediafact.go's scanBackward) with a cursor, so a page either ends at
// the start of history (no Next) or hands back where to resume.

import (
	"bytes"
	"container/list"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The kinds a deliverable has. "export" and "document" are legacy rows only;
// other authored files use the shared file kind in share_file.go.
const (
	DeliverableMarkdown = "markdown"
	DeliverableMermaid  = "mermaid"
	DeliverableExport   = "export"
	deliverableDocument = "document"
)

// Who made a deliverable.
const (
	DeliverOriginAgent      = "agent"
	DeliverOriginSavedByYou = "saved_by_you"
	// DeliverOriginSharedByAgent is a file the agent explicitly handed over with
	// share_deliverable. It says who SHARED the file, never who made it: such a
	// row is never Authored and its state stays unknown (share_publish.go).
	DeliverOriginSharedByAgent = "shared_by_agent"
)

// A deliverable's recorded state. Absent is unknown, and stays unknown.
const (
	DeliverDraft = "draft"
	DeliverKept  = "kept"
)

// How a kept deliverable came to be kept.
const (
	KeptCopied        = "copied"
	KeptCreatedInRepo = "created_in_repo"
)

const (
	deliverIDPrefix     = "dlv_"
	deliverLegacyPrefix = "dll_"
	// deliverFromMax bounds the inputs one row keeps, and deliverFromBytes one of them.
	deliverFromMax   = 16
	deliverFromBytes = 1024
)

// DeliverableFile is the identity of the bytes as the writer saw them. It is
// the same shape [MediaFact] keeps, for the kinds that are not media.
type DeliverableFile struct {
	Canonical string `json:"canonical,omitempty"`
	Bytes     int64  `json:"bytes,omitempty"`
	ModNS     int64  `json:"mod_ns,omitempty"`
	CtimeNS   int64  `json:"ctime_ns,omitempty"`
	Dev       uint64 `json:"dev,omitempty"`
	Ino       uint64 `json:"ino,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	MIME      string `json:"mime,omitempty"`
}

// DeliverableKept is where a copy was taken out to, recorded only after the
// copy completed.
type DeliverableKept struct {
	Path   string    `json:"path"`
	At     time.Time `json:"at"`
	Bytes  int64     `json:"bytes,omitempty"`
	SHA256 string    `json:"sha256,omitempty"`
	How    string    `json:"how"`
	// File is the kept copy as its own descriptor reported it when the copy was
	// complete: the authority that lets the copy outlive the folder it came from.
	File *DeliverableFile `json:"file,omitempty"`
}

// DeliverableFact is what the index remembers about one deliverable.
type DeliverableFact struct {
	ID     string    `json:"id"`
	Update bool      `json:"update,omitempty"`
	At     time.Time `json:"at"`
	// MediaUpdate publishes a fresh media source on this update row. A state
	// update clears it, so restating a source never lists that source twice.
	MediaUpdate bool `json:"media_update,omitempty"`
	// Turn is the authored MESSAGE the work answered (TaskSource.ID form), and
	// is set only from authoritative causality. Run is the producing turn's
	// immutable RunID, the join to DisplayEntry.RunID. Either may be absent.
	Turn    string   `json:"turn,omitempty"`
	Run     string   `json:"run,omitempty"`
	Task    string   `json:"task,omitempty"`
	From    []string `json:"from,omitempty"`
	State   string   `json:"state,omitempty"`
	Profile string   `json:"profile,omitempty"`
	Origin  string   `json:"origin,omitempty"`
	// Authored says a writer seam observed the bytes written by the agent.
	Authored bool `json:"authored,omitempty"`
	// Call is the provider tool call the row was recorded under, where the
	// recording seam knew it. It is an attribution, never an idempotency key.
	Call    string           `json:"call,omitempty"`
	Branch  string           `json:"branch,omitempty"`
	Rel     string           `json:"rel,omitempty"`
	Version string           `json:"version,omitempty"`
	File    *DeliverableFile `json:"file,omitempty"`
	Kept    *DeliverableKept `json:"kept,omitempty"`
	Removed bool             `json:"removed,omitempty"`
	// Source is the original file a stable engine-owned copy was taken from,
	// as its own descriptor reported it while every byte was read. It is set
	// only on a copy, and it is NOT a claim that the original was created.
	Source *DeliverableFile `json:"source,omitempty"`
}

// NewDeliverableID mints a deliverable id: random, written once with the row.
func NewDeliverableID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		sum := sha256.Sum256([]byte(time.Now().String()))
		copy(raw[:], sum[:])
	}
	return deliverIDPrefix + hex.EncodeToString(raw[:])
}

// LegacyDeliverableID is the id of a row recorded before deliverable ids were:
// a function of its own immutable columns, so it is the same on every read.
func LegacyDeliverableID(sessionID, kind, path string, created time.Time) string {
	sum := sha256.Sum256([]byte("deliverable/legacy/v1\x00" + sessionID + "\x00" + kind + "\x00" + path + "\x00" + created.UTC().Format(time.RFC3339Nano)))
	return deliverLegacyPrefix + hex.EncodeToString(sum[:12])
}

// ValidDeliverableID says whether id has a shape this engine issues.
func ValidDeliverableID(id string) bool {
	switch {
	case strings.HasPrefix(id, deliverIDPrefix):
		return len(id) == len(deliverIDPrefix)+32 && isHex(id[len(deliverIDPrefix):])
	case strings.HasPrefix(id, deliverLegacyPrefix):
		return len(id) == len(deliverLegacyPrefix)+24 && isHex(id[len(deliverLegacyPrefix):])
	}
	return false
}

// DeliverableRow is one deliverable as the index remembers it, newest row of
// its id.
type DeliverableRow struct {
	Artifact Artifact
	Fact     DeliverableFact
	// Recorded says the row carries a deliverable fact; false is a legacy row.
	Recorded bool
	ID       string
	Kind     string
	Start    int64
	Len      int
}

// deliverableKindExt is the kind a file name says, "" for none. Media kinds are
// confirmed by content elsewhere; this only decides what is worth looking at.
func deliverableKindExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return DeliverableMarkdown
	case ".mmd", ".mermaid":
		return DeliverableMermaid
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return "image"
	case ".mp3", ".wav", ".m4a", ".ogg", ".flac":
		return "audio"
	case ".mp4", ".mov", ".webm", ".m4v":
		return "video"
	}
	return ""
}

// deliverableRowOf settles one index row into a deliverable, or reports that
// it is not one. Attachments, evidence and other read-only rows never are.
func deliverableRowOf(row Artifact) (DeliverableRow, bool) {
	out := DeliverableRow{Artifact: row, Kind: row.Kind}
	switch {
	case row.Deliver != nil && row.Deliver.ID != "":
		if !ValidDeliverableID(row.Deliver.ID) {
			// A fact under an id this engine never issues is malformed: it is not
			// a deliverable, and no update of it is tracked, so the base and
			// its updates are refused alike and nothing half of it is shown.
			return DeliverableRow{}, false
		}
		out.Fact, out.Recorded, out.ID = *row.Deliver, true, row.Deliver.ID
		out.Fact.From = append([]string(nil), row.Deliver.From...)
	case row.Media != nil:
		if row.Media.Origin != "" && row.Media.Origin != MediaGenerated {
			return DeliverableRow{}, false
		}
	}
	if row.Deliver == nil || row.Deliver.ID == "" {
		switch row.Kind {
		case "image", "audio", "video", DeliverableExport, DeliverableMarkdown, DeliverableMermaid:
		case deliverableDocument:
			// An evidence "document" is a PDF the agent read, not a deliverable.
			if row.Media != nil {
				return DeliverableRow{}, false
			}
		default:
			return DeliverableRow{}, false
		}
		out.ID = LegacyDeliverableID(row.Session, row.Kind, row.Path, row.Created)
		out.Fact.ID = out.ID
	}
	return out, true
}

// ── the update tracker ──────────────────────────────────────────────────────

// The bounds of the update tracker. They are variables only so a test can make
// a small index exercise them.
var (
	// deliverTrackerMax is the most updates the PROCESS tracks, across all
	// indexes. Each is an offset and a length (12 bytes, 16 in a slice) under
	// an id the tracker accepts only in a shape this engine issues
	// ([ValidDeliverableID], at most 36 bytes), so the whole is about 32 MiB
	// at 127 bytes measured per update and cannot be grown by a long id. Past
	// it every listing is the typed [ErrDeliverIndexTooLarge]: exact, never a
	// list that may show a superseded or removed row.
	deliverTrackerMax int64 = 1 << 18
	// deliverSyncBudget is the most index bytes one call reads to catch the
	// tracker up. A call that spends it answers [ErrMediaBusy]; the progress it
	// made is kept, so the next call resumes and the work is never repeated.
	deliverSyncBudget int64 = 64 << 20
	// deliverSyncChunk is one read of that catch-up.
	deliverSyncChunk int64 = 256 << 10
)

// deliverRowMax is the longest deliverable row this engine writes. The tracker
// and the listing both read lines up to mediaRowMax, so a row ours can never be
// one they skip.
const deliverRowMax = 64 << 10

// ErrDeliverPageEnd is what a listing's fit answers to end the page before a
// row it could not describe in time; the page then resumes at that row.
var ErrDeliverPageEnd = errors.New("the page ends here and continues")

// ErrDeliverIndexTooLarge is an index with more updates than the tracker keeps.
// It is a refusal with a reason, not an answer that might be wrong.
var ErrDeliverIndexTooLarge = errors.New("the deliverables history has more changes than can be listed exactly")

type deliverUpdate struct {
	off int64
	n   int32
}

// deliverTracker knows, for every deliverable, where each of its updates sits
// in the index, up to `scan.pos`. Updates are appended, so each id's offsets
// arrive ascending. It is rebuilt from the index (never persisted) and caught
// up incrementally.
type deliverTracker struct {
	sem     chan struct{} // one catch-up at a time, taken with the caller's deadline
	mu      sync.RWMutex
	dev     uint64
	ino     uint64
	scan    deliverScan
	count   int
	updates map[string][]deliverUpdate

	// written is the path lookup index, guarded by artifactsMu.
	written *writtenPaths

	// The registry's own fields, guarded by deliverRegistry.mu and never by mu.
	key    string        // the index path it is registered under
	leases int           // callers using it now; one with leases is never evicted
	idleAt *list.Element // its place in the idle list while leases is zero
}

// deliverScan is where a catch-up stands BETWEEN calls. A line may begin in one
// call and end in a later one, so the pending line travels with the offset: its
// head when it is short enough to be a row, or only the fact that it is being
// skipped when it is not. KEEPING THE SKIP IS THE LAW: a run with no newline
// longer than one call's budget must be passed a budget at a time, never begun
// again from its first byte, and the bytes after it must never be read as the
// start of a line.
type deliverScan struct {
	pos      int64  // every byte before here has been read
	carry    []byte // the pending line's head, when it can still be a row
	line     int64  // where the pending line began
	overlong bool   // the pending line is longer than any row and is skipped
}

// pending says a line has begun and has not yet ended.
func (s deliverScan) pending() bool { return s.carry != nil || s.overlong }

type deliverFound struct {
	id string
	u  deliverUpdate
}

var deliverUpdateNeedle = []byte(`"update":true`)

// deliverUpdateIn is the update a complete line records, if it records one.
func deliverUpdateIn(line []byte, off int64) (deliverFound, bool) {
	if len(line) > mediaRowMax || !bytes.Contains(line, deliverUpdateNeedle) {
		return deliverFound{}, false
	}
	var row struct {
		Deliver *struct {
			ID string `json:"id"`
		} `json:"deliver"`
	}
	if json.Unmarshal(line, &row) != nil || row.Deliver == nil || !ValidDeliverableID(row.Deliver.ID) {
		// An id of no shape this engine issues is a row no creation carries
		// (deliverableRowOf refuses one too), so it cannot supersede anything.
		return deliverFound{}, false
	}
	return deliverFound{row.Deliver.ID, deliverUpdate{off: off, n: int32(len(line))}}, true
}

// feed reads one chunk that starts at s.pos and answers the updates on the
// lines it ENDED. A line ends only at its newline: every row is written with
// one, so bytes after the last newline are a row still being written (or not a
// row) and wait as the pending line, never parsed as though the file ended.
func (s *deliverScan) feed(buf []byte) []deliverFound {
	var batch []deliverFound
	s.feedLines(buf, func(off int64, line []byte) {
		if f, ok := deliverUpdateIn(line, off); ok {
			batch = append(batch, f)
		}
	})
	return batch
}

func (s *deliverScan) feedLines(buf []byte, visit func(int64, []byte)) {
	start := 0
	for {
		nl := bytes.IndexByte(buf[start:], '\n')
		if nl < 0 {
			break
		}
		end := start + nl
		off, line, skip := s.pos+int64(start), buf[start:end], s.overlong
		if s.pending() {
			off = s.line
		}
		if s.carry != nil {
			line = append(s.carry, line...)
		}
		s.carry, s.overlong = nil, false
		if !skip && len(line) <= mediaRowMax {
			visit(off, line)
		}
		start = end + 1
	}
	if tail := buf[start:]; len(tail) > 0 {
		if !s.pending() {
			s.line = s.pos + int64(start)
		}
		if s.overlong || len(s.carry)+len(tail) > mediaRowMax {
			s.overlong, s.carry = true, nil
		} else {
			s.carry = append(s.carry, tail...)
		}
	}
	s.pos += int64(len(buf))
}

// commit applies one chunk's updates and its new place TOGETHER, or neither.
// The process-wide bound is taken for the whole batch at once, so a refusal
// leaves the tracker exactly as it was and the same chunk is read again on a
// later call; an offset at or below one already held for that id is the same
// row seen twice and is not added again, so each list stays ascending.
func (t *deliverTracker) commit(next deliverScan, batch []deliverFound) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if n := int64(len(batch)); n > 0 && deliverTracked.Add(n) > deliverTrackerMax {
		deliverTracked.Add(-n)
		return ErrDeliverIndexTooLarge
	}
	for _, f := range batch {
		list := t.updates[f.id]
		if k := len(list); k > 0 && list[k-1].off >= f.u.off {
			deliverTracked.Add(-1)
			continue
		}
		t.updates[f.id] = append(list, f.u)
		t.count++
	}
	t.scan = next
	return nil
}

// deliverTracked counts every update remembered by every tracker in the
// process, so the bound is the PROCESS'S memory and not one index's.
var deliverTracked atomic.Int64

// latestBefore is the newest update of id whose row starts before end.
func (t *deliverTracker) latestBefore(id string, end int64) (deliverUpdate, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	list := t.updates[id]
	i := sort.Search(len(list), func(i int) bool { return list[i].off >= end })
	if i == 0 {
		return deliverUpdate{}, false
	}
	return list[i-1], true
}

// sync catches the tracker up to target (a point at or below the index's end).
// It holds no lock while it reads, gives up at the deadline or when the call's
// byte budget is spent, and keeps what it learned either way.
func (t *deliverTracker) sync(file *os.File, now mediaCursorBody, target int64, deadline time.Time) error {
	var expire <-chan time.Time
	if !deadline.IsZero() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		expire = timer.C
	}
	select {
	case t.sem <- struct{}{}:
		defer func() { <-t.sem }()
	case <-expire:
		return ErrMediaBusy
	}
	scan := t.start(now)
	if scan.pos >= target {
		return nil
	}
	budget := deliverSyncBudget
	for scan.pos < target {
		if budget <= 0 || (!deadline.IsZero() && time.Now().After(deadline)) {
			return ErrMediaBusy
		}
		size := min(deliverSyncChunk, target-scan.pos)
		buf := make([]byte, size)
		if _, err := file.ReadAt(buf, scan.pos); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		budget -= size
		next := scan
		batch := next.feed(buf)
		if err := t.commit(next, batch); err != nil {
			return err
		}
		scan = next
	}
	return nil
}

// start is the place a catch-up resumes from, after forgetting everything when
// the index is no longer the file the tracker read (replaced, or cut shorter).
// The pending head is copied, so a catch-up that stops part way leaves the
// committed one untouched.
func (t *deliverTracker) start(now mediaCursorBody) deliverScan {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.updates == nil || t.dev != now.Dev || t.ino != now.Ino || t.scan.pos > now.End {
		deliverTracked.Add(-int64(t.count))
		t.updates, t.dev, t.ino, t.scan, t.count = map[string][]deliverUpdate{}, now.Dev, now.Ino, deliverScan{}, 0
	}
	scan := t.scan
	if scan.carry != nil {
		scan.carry = bytes.Clone(scan.carry)
	}
	return scan
}

// ── cursors ─────────────────────────────────────────────────────────────────

// deliverCursorBody is a listing's place: the index's identity, the offset
// below which older rows remain (End) and the point the listing is a SNAPSHOT
// of (Snap), fixed by its first page. An update appended after Snap is not seen
// by any page of that listing, so no page can disagree with another.
type deliverCursorBody struct {
	Dev  uint64 `json:"d"`
	Ino  uint64 `json:"i"`
	End  int64  `json:"e"`
	Snap int64  `json:"s"`
}

func encodeDeliverCursor(body deliverCursorBody) string {
	raw, _ := json.Marshal(body)
	return "d1." + base64.RawURLEncoding.EncodeToString(raw)
}

func decodeDeliverCursor(cursor string) (deliverCursorBody, error) {
	var body deliverCursorBody
	text, ok := strings.CutPrefix(cursor, "d1.")
	if !ok {
		return body, ErrMediaCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil || json.Unmarshal(raw, &body) != nil || body.End < 0 || body.Snap < body.End {
		return deliverCursorBody{}, ErrMediaCursor
	}
	return body, nil
}

// ── listing ─────────────────────────────────────────────────────────────────

// DeliverableQuery narrows a listing. Everything is code: no model ranks.
type DeliverableQuery struct {
	// Sessions limits to these conversations; nil is every conversation.
	Sessions map[string]bool
	Kinds    map[string]bool
	// States matches the RECORDED state; "unknown" matches a row with none.
	States map[string]bool
	// Text is a case-insensitive substring over the file name, title and kind.
	Text string
	// CountOnly counts the rows a page would take and keeps none of them; the
	// page is then bounded by the read budget, not by a row limit.
	CountOnly bool
}

func (q DeliverableQuery) matches(row DeliverableRow) bool {
	if q.Sessions != nil && !q.Sessions[row.Artifact.Session] {
		return false
	}
	if len(q.Kinds) > 0 && !q.Kinds[row.Kind] {
		return false
	}
	if len(q.States) > 0 {
		state := row.Fact.State
		if state == "" {
			state = "unknown"
		}
		if !q.States[state] {
			return false
		}
	}
	if text := strings.ToLower(strings.TrimSpace(q.Text)); text != "" {
		if !strings.Contains(strings.ToLower(filepath.Base(row.Artifact.Path)), text) &&
			!strings.Contains(strings.ToLower(row.Artifact.Title), text) &&
			!strings.Contains(strings.ToLower(row.Kind), text) {
			return false
		}
	}
	return true
}

// DeliverableScan is one page of a listing.
type DeliverableScan struct {
	Rows []DeliverableRow
	// Count is the rows taken, kept or not (see DeliverableQuery.CountOnly).
	Count int
	// Next resumes below this page; empty is the start of history.
	Next string
	// Incomplete says the page is short because the read budget or the
	// deadline ended it, not because history did. It is set ONLY together with
	// a Next that continues: a page that cannot say how to go on is an error.
	Incomplete bool
}

// projectDeliverable is the newest state of a deliverable as of the snapshot:
// its latest update before snap, else the creation row. A row that no longer
// matches the creation it restates means the index changed under the reader.
func projectDeliverable(file *os.File, tracker *deliverTracker, base DeliverableRow, snap int64) (DeliverableRow, error) {
	upd, ok := tracker.latestBefore(base.ID, snap)
	if !ok {
		return base, nil
	}
	line := make([]byte, upd.n)
	if _, err := file.ReadAt(line, upd.off); err != nil {
		return DeliverableRow{}, ErrMediaCursor
	}
	var raw Artifact
	if json.Unmarshal(line, &raw) != nil || raw.Deliver == nil || !raw.Deliver.Update {
		return DeliverableRow{}, ErrMediaCursor
	}
	row, ok := deliverableRowOf(raw)
	if !ok || row.ID != base.ID {
		return DeliverableRow{}, ErrMediaCursor
	}
	row.Start, row.Len = base.Start, base.Len
	return row, nil
}

// ListDeliverables is one page of deliverables, NEWEST CREATION FIRST: the
// order is the index position of each deliverable's creation row, and an update
// (Keep, Remove) never moves it. Each is shown as its latest state as of the
// listing's snapshot. fit answers what a row costs to send and whether the
// caller takes it (a live filter); it may be nil. A row that alone exceeds
// budget is [ErrMediaTooLarge]. A fit that answers [ErrDeliverPageEnd] ends the
// page BEFORE that row, with a Next that resumes at it: a page short of time,
// never a row left out.
func ListDeliverables(index string, q DeliverableQuery, cursor string, limit int, deadline time.Time, budget int, fit func(DeliverableRow) (cost int, take bool, err error)) (DeliverableScan, error) {
	if strings.TrimSpace(index) == "" {
		return DeliverableScan{}, nil
	}
	limit = min(max(limit, 1), MediaPageMax)
	if q.CountOnly {
		limit = int(^uint(0) >> 1)
	}
	file, now, err := openIndex(index)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DeliverableScan{}, nil
		}
		return DeliverableScan{}, err
	}
	defer file.Close()
	end, snap := now.End, now.End
	if cursor != "" {
		held, err := decodeDeliverCursor(cursor)
		if err != nil || held.Dev != now.Dev || held.Ino != now.Ino || held.Snap > now.End {
			return DeliverableScan{}, ErrMediaCursor
		}
		end, snap = held.End, held.Snap
	}
	tracker, release, err := leaseTracker(index, deadline)
	if err != nil {
		return DeliverableScan{}, err
	}
	defer release()
	if err := tracker.sync(file, now, snap, deadline); err != nil {
		return DeliverableScan{}, err
	}
	var needles [][]byte
	var values map[string]bool
	byValue := len(q.Sessions) > deliverNeedleMax
	if byValue {
		values = sessionValues(q.Sessions)
	} else if q.Sessions != nil {
		for id := range q.Sessions {
			needles = append(needles, mediaNeedle(id))
		}
	}
	var scan DeliverableScan
	spent, tooLarge, rejected := 0, false, false
	// endedAt is where a page a fit ended resumes: just past the line of the
	// row it did not describe, so that row is the first the next page reads.
	var endedAt int64 = -1
	var projectErr error
	boundary, err := scanBackward(file, end, mediaScanBudget, deadline, func(start int64, line []byte) bool {
		if byValue && !lineNamesSession(line, values) {
			return true
		}
		if needles != nil {
			hit := false
			for _, needle := range needles {
				if bytes.Contains(line, needle) {
					hit = true
					break
				}
			}
			if !hit {
				return true
			}
		}
		var raw Artifact
		if json.Unmarshal(line, &raw) != nil || strings.TrimSpace(raw.Path) == "" || strings.TrimSpace(raw.Title) == "" {
			return true
		}
		if raw.Deliver != nil && raw.Deliver.Update {
			return true // an update is a state, not a position
		}
		row, ok := deliverableRowOf(raw)
		if !ok {
			return true
		}
		row.Start, row.Len = start, len(line)
		row, projectErr = projectDeliverable(file, tracker, row, snap)
		if projectErr != nil {
			return false
		}
		if row.Fact.Removed || !q.matches(row) {
			return true
		}
		if fit != nil {
			cost, take, err := fit(row)
			if errors.Is(err, ErrDeliverPageEnd) {
				endedAt = start + int64(len(line)) + 1
				return false
			}
			if err != nil || (take && cost > budget-spent) {
				if scan.Count == 0 {
					tooLarge = true
				}
				rejected = true
				return false
			}
			if !take {
				return true
			}
			spent += cost
		}
		scan.Count++
		if !q.CountOnly {
			scan.Rows = append(scan.Rows, row)
		}
		return scan.Count < limit
	})
	if err != nil {
		return DeliverableScan{}, err
	}
	if projectErr != nil {
		return DeliverableScan{}, projectErr
	}
	if tooLarge {
		return DeliverableScan{}, ErrMediaTooLarge
	}
	if endedAt >= 0 {
		// A page that made no progress at all is busy, not a cursor that
		// would be asked the same question again.
		if endedAt >= end {
			return DeliverableScan{}, ErrMediaBusy
		}
		scan.Next = encodeDeliverCursor(deliverCursorBody{Dev: now.Dev, Ino: now.Ino, End: endedAt, Snap: snap})
		scan.Incomplete = true
		return scan, nil
	}
	if rejected && len(scan.Rows) > 0 {
		boundary = scan.Rows[len(scan.Rows)-1].Start
	}
	if boundary > 0 {
		scan.Next = encodeDeliverCursor(deliverCursorBody{Dev: now.Dev, Ino: now.Ino, End: boundary, Snap: snap})
		scan.Incomplete = scan.Count < limit && !rejected
	}
	return scan, nil
}

// FindDeliverable is the session's deliverable with id as of now: its creation
// row projected through its latest update, found by reading the whole index
// backward in bounded memory. It answers not-found only after the whole history
// was read; running out of time is [ErrMediaBusy].
func FindDeliverable(index, sessionID, id string, deadline time.Time) (DeliverableRow, error) {
	if !ValidDeliverableID(id) || sessionID == "" {
		return DeliverableRow{}, ErrMediaNotFound
	}
	file, now, err := openIndex(index)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DeliverableRow{}, ErrMediaNotFound
		}
		return DeliverableRow{}, err
	}
	defer file.Close()
	tracker, release, err := leaseTracker(index, deadline)
	if err != nil {
		return DeliverableRow{}, err
	}
	defer release()
	if err := tracker.sync(file, now, now.End, deadline); err != nil {
		return DeliverableRow{}, err
	}
	needle := mediaNeedle(sessionID)
	var found DeliverableRow
	hit := false
	var projectErr error
	boundary, err := scanBackward(file, now.End, 1<<62, deadline, func(start int64, line []byte) bool {
		if !bytes.Contains(line, needle) {
			return true
		}
		var raw Artifact
		if json.Unmarshal(line, &raw) != nil || raw.Session != sessionID || strings.TrimSpace(raw.Path) == "" {
			return true
		}
		if raw.Deliver != nil && raw.Deliver.Update {
			return true
		}
		row, ok := deliverableRowOf(raw)
		if !ok || row.ID != id {
			return true
		}
		row.Start, row.Len = start, len(line)
		found, projectErr = projectDeliverable(file, tracker, row, now.End)
		hit = projectErr == nil
		return false
	})
	switch {
	case err != nil:
		return DeliverableRow{}, err
	case projectErr != nil:
		return DeliverableRow{}, projectErr
	case hit:
		return found, nil
	case boundary > 0:
		return DeliverableRow{}, ErrMediaBusy
	}
	return DeliverableRow{}, ErrMediaNotFound
}

// ── appending ───────────────────────────────────────────────────────────────

// AppendDeliverableUpdate appends a full-snapshot update of base with change
// applied, and answers whether the row was written. It copies the base's path,
// title, session, kind and Created so older readers keep their ordering.
func AppendDeliverableUpdate(index string, base DeliverableRow, change func(*DeliverableFact)) bool {
	_, ok := appendDeliverableUpdate(index, base, change)
	return ok
}

// appendDeliverableUpdate is [AppendDeliverableUpdate] that answers the
// deliverable as the row it wrote now states it.
func appendDeliverableUpdate(index string, base DeliverableRow, change func(*DeliverableFact)) (DeliverableRow, bool) {
	row := base.Artifact
	fact := base.Fact
	fact.ID, fact.Update, fact.At = base.ID, true, time.Now().UTC()
	fact.MediaUpdate = false
	fact.From = append([]string(nil), base.Fact.From...)
	change(&fact)
	row.Deliver = &fact
	written, ok := recordArtifactRow(index, row)
	if !ok {
		return DeliverableRow{}, false
	}
	return DeliverableRow{Artifact: written, Fact: fact, Recorded: true, ID: base.ID, Kind: base.Kind, Start: base.Start, Len: base.Len}, true
}

// deliverRowFits says whether a row is within what this engine writes for a
// deliverable, so the tracker and the listing can never skip one of ours.
func deliverRowFits(line []byte) bool { return len(line) <= deliverRowMax }

// boundedFrom keeps at most deliverFromMax inputs, each within bounds.
func boundedFrom(from []string) []string {
	var out []string
	for _, item := range from {
		item = strings.TrimSpace(item)
		if item == "" || len(item) > deliverFromBytes || len(out) >= deliverFromMax {
			continue
		}
		out = append(out, item)
	}
	return out
}

// ── liveness ────────────────────────────────────────────────────────────────

// DeliverableLiveOnBranch is the live state of a deliverable whose file exists
// only on a task branch that has not merged.
const DeliverableLiveOnBranch = "on_branch"

// ResolveDeliverable checks a deliverable's file against the disk with the same
// law media sources use: the live canonical path must equal the recorded one
// and must lie inside roots. A recorded identity is compared when the writer
// kept one; a legacy row with none is never called changed on a guess.
func ResolveDeliverable(row DeliverableRow, roots []string) MediaLive {
	fact := MediaFact{Origin: MediaGenerated}
	switch {
	case row.Fact.File != nil && row.Fact.File.Canonical != "":
		f := row.Fact.File
		fact.Canonical, fact.Bytes, fact.ModNS, fact.CtimeNS, fact.Dev, fact.Ino = f.Canonical, f.Bytes, f.ModNS, f.CtimeNS, f.Dev, f.Ino
	case row.Artifact.Media != nil && row.Artifact.Media.Canonical != "":
		fact = *row.Artifact.Media
		fact.Origin = MediaGenerated
	default:
		fact.Canonical = canonicalFile(row.Artifact.Path)
	}
	live := ResolveMedia(MediaRow{Fact: fact}, roots)
	if live.State == MediaAvailable || live.State == MediaChanged {
		return live
	}
	// THE SOURCE IS GONE, but a copy this engine made and recorded may not be:
	// a temporary folder swept after seven days must not take with it a file the
	// person kept. The copy is judged by its own recorded identity.
	if kept := row.Fact.Kept; kept != nil && kept.How == KeptCopied && kept.File != nil && kept.File.Canonical != "" {
		if copy := resolveKeptCopy(kept); copy.State == MediaAvailable || copy.State == MediaChanged {
			copy.Copy = true
			return copy
		}
	}
	return live
}

// resolveKeptCopy checks a kept copy against the disk: a regular file at the
// canonical path that was recorded (not a link retargeted since), and its
// identity compared to the one recorded when the copy completed. There is no
// scope law here: the person chose where it lives.
func resolveKeptCopy(kept *DeliverableKept) MediaLive {
	recorded := kept.File.Canonical
	resolved, err := filepath.EvalSymlinks(recorded)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return MediaLive{State: MediaMissing}
	case err != nil:
		return MediaLive{State: MediaUnreadable}
	case resolved != recorded:
		return MediaLive{State: MediaOutside}
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return MediaLive{State: MediaUnreadable}
	}
	ctime, dev, ino := fileIdentity(info)
	live := MediaLive{State: MediaAvailable, Version: fileVersion(info.Size(), info.ModTime().UnixNano(), ctime, dev, ino), Bytes: info.Size(), HasBytes: true, Path: resolved}
	f := kept.File
	if recordedIdentityDiffers(MediaFact{ModNS: f.ModNS, CtimeNS: f.CtimeNS, Dev: f.Dev, Ino: f.Ino, Bytes: f.Bytes}, info.Size(), info.ModTime().UnixNano(), ctime, dev, ino) {
		live.State = MediaChanged
	}
	return live
}

// DeliverableVersion is the recorded version: the content hash where the writer
// held one, else the recorded file identity, else nothing (a legacy row).
func DeliverableVersion(row DeliverableRow) string {
	if row.Fact.Version != "" {
		return row.Fact.Version
	}
	if f := row.Fact.File; f != nil && f.SHA256 != "" {
		return "sha256:" + f.SHA256
	}
	if f := row.Fact.File; f != nil && f.ModNS != 0 {
		return fileVersion(f.Bytes, f.ModNS, f.CtimeNS, f.Dev, f.Ino)
	}
	if row.Artifact.Media != nil {
		return MediaRow{Fact: *row.Artifact.Media, Recorded: true}.Version()
	}
	return ""
}
