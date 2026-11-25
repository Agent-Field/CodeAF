package cellsync

// ── READING A CHAT THAT LIVES ON ANOTHER COMPUTER ───────────────────────────
//
// The desktop shows the transcript of a chat another paired computer holds,
// newest messages first, older ones as the person scrolls. The only way this
// package had to look inside a remote chat was a take — Fetcher.Complete, the
// whole head — and the whole head is the chat's PROJECT FOLDER, which can be
// gigabytes the person never asked for. Reading what was said must not cost
// the project folder.
//
// So a Reader fetches by PATH and by RANGE (cellstore/pathread.go has the
// engine half):
//
//	open    snapshot + the trees down to .cell/ + the transcript's index   a few KB
//	        + the chunks under the newest page                              ~256 KiB–2 MiB
//	        + the file's first chunk once, for its header line              never changes
//	more    the chunks before the cursor
//	update  a new head: its small index objects and its new tail chunk. The
//	        engine cuts files by their content, so a transcript that grew by a
//	        turn keeps every earlier chunk byte for byte and they are all held.
//
// THE SAME STORE AS A TAKE. A Reader is built over the same Engine a take of the
// chat uses, so what it imports lands where the take's whole-graph want looks:
// continuing a chat after reading it downloads only what the read did not, and
// no byte is paid for twice. (Its INBOX is its own, beside the take's, because a
// strict import refuses files no pass of its own wanted and a take or a watching
// daemon may be filling the shared one at the same moment.)
//
// NO LEASE, NO MATERIALIZE, NO AGENT, NO WORKSPACE FOLDER. A read is a spectator:
// the computer holding the chat is not displaced, nothing is restored, and the
// relay and its encryption are untouched. Whoever is tempted to "simplify" this
// into Fetcher.Complete or a take: that is precisely the cost it exists to avoid
// (docs/design/remote-read/DESIGN.md has the numbers).

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
)

// Peek and PeekChunk are cellstore's types under the seam's names, as Export is.
type (
	Peek      = cellstore.Peek
	PeekChunk = cellstore.PeekChunk
)

// ErrIncomplete and ErrNoPath are the engine's two answers a reader acts on.
var (
	ErrIncomplete = cellstore.ErrIncomplete
	ErrNoPath     = cellstore.ErrNoPath
)

// PathEngine is the device-local half of a read: the engine verbs that walk ONE
// path of a head rather than all of it, plus the import a take uses, so read
// objects land in the take's store. cellstore.SyncEngine is the real one and
// FakeEngine stands in for it in tests.
type PathEngine interface {
	// WantPath names the rids the store lacks on the way to path and, when
	// ranged, the chunks of the file overlapping [start, end).
	WantPath(ctx context.Context, c cell.Cell, head, path string, ranged bool, start, end int64) ([]string, error)
	// Peek says what the store holds of path, without the network.
	Peek(ctx context.Context, c cell.Cell, head, path string) (Peek, error)
	// ReadRange writes bytes [start, end) of path to out from held chunks.
	ReadRange(ctx context.Context, c cell.Cell, head, path string, start, end int64, out string) (int64, error)
	// ImportPath stores the wanted objects the inbox holds: the take's own
	// import verb into the take's own store, run without the chat's folder.
	ImportPath(ctx context.Context, c cell.Cell, head, inbox string) (int, error)
}

// Reader brings single files of a chat's head, or ranges of one, from the relay
// into the chat's own store, and reads them back. See the top of this file for
// why it must never become a whole-head fetch.
type Reader struct {
	Engine PathEngine
	Store  blobstore.Store
	// Inbox is the take's inbox for a cell; the Reader works in a folder of its
	// own beside it (ReadInboxSuffix).
	Inbox func(c cell.Cell) string
	// ManyMax bounds the rids one GetMany asks for; 0 is blobstore.MaxGetMany.
	ManyMax int
	// Fetched counts the bytes of every object this Reader pulled from the
	// relay. A page reports what it cost through it.
	Fetched int64
}

// ReadInboxSuffix names the Reader's inbox beside the take's.
const ReadInboxSuffix = ".read"

// Bring makes the store hold everything path needs at head — the index objects
// on the way to it and, when ranged, the chunks overlapping [start, end) — by
// the want → gather → import loop a take runs, scoped to that path. Objects
// already held are never asked for again.
func (r *Reader) Bring(ctx context.Context, c cell.Cell, head, path string, ranged bool, start, end int64) error {
	inbox := r.Inbox(c) + ReadInboxSuffix
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		want, err := r.Engine.WantPath(ctx, c, head, path, ranged, start, end)
		if err != nil {
			return err
		}
		if len(want) == 0 {
			return nil
		}
		if err := r.gather(ctx, inbox, want); err != nil {
			return err
		}
		n, err := r.Engine.ImportPath(ctx, c, head, inbox)
		if err != nil {
			return err
		}
		if n == 0 {
			// An import that takes nothing would repeat forever.
			return errors.New("cellsync: import took none of the wanted objects")
		}
	}
}

// gather is Fetcher.gather that also counts the bytes it pulled.
func (r *Reader) gather(ctx context.Context, inbox string, want []string) error {
	max := r.ManyMax
	if max <= 0 || max > blobstore.MaxGetMany {
		max = blobstore.MaxGetMany
	}
	for len(want) > 0 {
		batch := want[:min(len(want), max)]
		objects, err := r.Store.GetMany(ctx, batch)
		if err != nil {
			return fmt.Errorf("cellsync: fetch object %s: %w", batch[0], err)
		}
		if len(objects) == 0 {
			return fmt.Errorf("cellsync: the store answered no object for %s", batch[0])
		}
		if err := keep(inbox, objects); err != nil {
			return err
		}
		for _, o := range objects {
			r.Fetched += int64(len(o.Bytes))
		}
		want = want[len(objects):]
	}
	return nil
}

// Peek brings path's index objects and says what the store holds of it.
func (r *Reader) Peek(ctx context.Context, c cell.Cell, head, path string) (Peek, error) {
	if err := r.Bring(ctx, c, head, path, false, 0, 0); err != nil {
		return Peek{}, err
	}
	return r.Engine.Peek(ctx, c, head, path)
}

// Bytes brings and reads bytes [start, end) of path at head.
func (r *Reader) Bytes(ctx context.Context, c cell.Cell, head, path string, start, end int64) ([]byte, error) {
	if end <= start {
		return nil, nil
	}
	if err := r.Bring(ctx, c, head, path, true, start, end); err != nil {
		return nil, err
	}
	// The bytes are written beside the inbox, never in it: an import reads
	// every file in its inbox as an object it was sent.
	inbox := r.Inbox(c) + ReadInboxSuffix
	file, err := os.CreateTemp(filepath.Dir(inbox), filepath.Base(inbox)+".range-*")
	if err != nil {
		return nil, err
	}
	out := file.Name()
	file.Close()
	defer os.Remove(out)
	if _, err := r.Engine.ReadRange(ctx, c, head, path, start, end, out); err != nil {
		return nil, err
	}
	return os.ReadFile(out)
}

// ── PAGES OF A TRANSCRIPT ───────────────────────────────────────────────────

// TranscriptPage is one page of a remote chat's transcript as bytes ready for
// the session's tail reader (session.ReadTranscriptBytes).
type TranscriptPage struct {
	// Segment is the file's header line (when the page does not start the file)
	// followed by bytes [Start, End) of it. Start is always the start of a
	// person's message line, so a page never opens in the middle of a turn.
	Segment    []byte
	Start, End int64
	// Before is the cursor of the next older page, "" when this page starts
	// the file.
	Before string
	Head   string
}

// More reports whether an older page exists.
func (p TranscriptPage) More() bool { return p.Before != "" }

// Pager cuts a remote chat's transcript into pages, newest first, by fetching
// chunks from the end backwards until a page holds at least Limit entries.
type Pager struct {
	Reader *Reader
	// Path is the transcript's path in the head; "" is cell.TranscriptPath.
	Path string
	// Count says how many entries a person would see in a segment (the app's
	// shaping, over session.ReadTranscriptBytes). Nil counts person lines.
	Count func(segment []byte) int
}

// cursor is the opaque before: the head the paging started at and the byte
// offset the next older page ends at. The head rides along so that paging back
// through a chat stays on ONE snapshot while new turns land: offsets are
// offsets into that snapshot's transcript.
type cursor struct {
	Head string `json:"h"`
	At   int64  `json:"o"`
}

func encodeCursor(c cursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// ErrBadCursor is a before this engine did not write.
var ErrBadCursor = errors.New("cellsync: that page cursor is not one this engine wrote")

func decodeCursor(s string) (cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	var c cursor
	if err != nil || json.Unmarshal(raw, &c) != nil || c.Head == "" || c.At <= 0 {
		return cursor{}, ErrBadCursor
	}
	return c, nil
}

// CursorHead is the head a before cursor pages through, so a caller reads the
// page at the snapshot the paging began at.
func CursorHead(before string) (string, error) {
	c, err := decodeCursor(before)
	return c.Head, err
}

// Page reads the page ending at before ("" is the newest page at head). It holds
// at least limit entries unless it reaches the start of the file; a turn is never
// split, so a page can hold more.
func (p *Pager) Page(ctx context.Context, c cell.Cell, head, before string, limit int) (TranscriptPage, error) {
	path := p.Path
	if path == "" {
		path = cell.TranscriptPath
	}
	if limit <= 0 {
		limit = 1
	}
	end := int64(-1)
	if before != "" {
		cur, err := decodeCursor(before)
		if err != nil {
			return TranscriptPage{}, err
		}
		head, end = cur.Head, cur.At
	}
	page := TranscriptPage{Head: head}
	peek, err := p.Reader.Peek(ctx, c, head, path)
	if errors.Is(err, ErrNoPath) {
		return page, nil
	}
	if err != nil {
		return page, err
	}
	if peek.Kind != "" && peek.Kind != "file" {
		return page, fmt.Errorf("cellsync: %s is not a file in %s", path, head)
	}
	if end < 0 || end > peek.Len {
		end = peek.Len
	}
	page.End = end
	if end == 0 {
		return page, nil
	}
	starts := chunkStarts(peek, end)
	header, err := p.header(ctx, c, head, path, peek)
	if err != nil {
		return page, err
	}
	// Walk back a chunk at a time. Each step reads [from, end) — the bytes
	// already held come from the store, only the new chunk from the relay — and
	// looks for the latest place to cut that still gives limit entries.
	for i := len(starts) - 1; i >= 0; i-- {
		from := starts[i]
		buf, err := p.Reader.Bytes(ctx, c, head, path, from, end)
		if err != nil {
			return page, err
		}
		cuts := personLines(buf, from)
		if from == 0 {
			cuts = append([]int64{0}, cuts...) // the file's own start is always a cut
		}
		if cut, ok := p.latestCut(header, buf, from, cuts, limit); ok || from == 0 {
			// THE OLDEST PAGE TAKES THE FILE'S OPENING LINES WITH IT. When the cut
			// lands on the earliest person line, all that lies before it is the
			// session header and whatever the chat wrote before its first message:
			// no entry a person would page back for. Offering it as one more page
			// cost the viewer a round trip that answered nothing (seen on two GCP
			// machines, 2026-11-16), so the page reaches byte 0 and says there is
			// no older page.
			if !ok || from == 0 && len(cuts) > 1 && cut <= cuts[1] {
				cut = 0
			}
			page.Start = cut
			page.Segment = withHeader(header, buf[cut-from:], cut)
			if cut > 0 {
				page.Before = encodeCursor(cursor{Head: head, At: cut})
			}
			return page, nil
		}
	}
	return page, nil // unreachable: the first chunk starts at 0
}

// latestCut is the largest cut whose segment [cut, end) counts at least limit
// entries. The count only grows as the cut moves back, so it is a binary search
// over the cuts: a few shapings, not one per line.
func (p *Pager) latestCut(header, buf []byte, from int64, cuts []int64, limit int) (int64, bool) {
	count := func(cut int64) int {
		seg := withHeader(header, buf[cut-from:], cut)
		if p.Count != nil {
			return p.Count(seg)
		}
		return len(personLines(seg, 0))
	}
	// cuts ascend; find the last index whose count >= limit.
	i := sort.Search(len(cuts), func(i int) bool { return count(cuts[i]) < limit })
	if i == 0 {
		return 0, false
	}
	return cuts[i-1], true
}

// header is the file's first line when it is the session header, which carries
// the format version a reading must check: a page cut from the middle of a file
// written by a newer codeaf must say so, not show a shortened copy. It costs the
// file's first chunk once; that chunk never changes, so it is never fetched again.
func (p *Pager) header(ctx context.Context, c cell.Cell, head, path string, peek Peek) ([]byte, error) {
	if peek.Len == 0 {
		return nil, nil
	}
	first := peek.Len
	if len(peek.Chunks) > 0 {
		first = peek.Chunks[0].End
	}
	raw, err := p.Reader.Bytes(ctx, c, head, path, 0, min(first, peek.Len))
	if err != nil {
		return nil, err
	}
	line, _, found := bytes.Cut(raw, []byte("\n"))
	if !found || !isLine(line, "session", "") {
		return nil, nil
	}
	return append(bytes.Clone(line), '\n'), nil
}

// withHeader prepends the header line to a segment that does not start the file.
func withHeader(header, seg []byte, at int64) []byte {
	if at == 0 || len(header) == 0 {
		return bytes.Clone(seg)
	}
	return append(bytes.Clone(header), seg...)
}

// chunkStarts are the offsets of the chunks that begin before end, ascending.
// A file the engine reports with no chunk list is one piece.
func chunkStarts(peek Peek, end int64) []int64 {
	if len(peek.Chunks) == 0 {
		return []int64{0}
	}
	var out []int64
	for _, ch := range peek.Chunks {
		if ch.Start < end {
			out = append(out, ch.Start)
		}
	}
	if len(out) == 0 || out[0] != 0 {
		out = append([]int64{0}, out...)
	}
	return out
}

// personLines are the offsets (from base) of every line in buf that is a
// message the person wrote. The first line of a buffer that does not start the
// file is skipped: it may be the tail of a line split across a chunk boundary.
// A page cuts only at these, so a tool call is never separated from its result.
func personLines(buf []byte, base int64) []int64 {
	var out []int64
	at := 0
	if base > 0 {
		nl := bytes.IndexByte(buf, '\n')
		if nl < 0 {
			return nil
		}
		at = nl + 1
	}
	for at < len(buf) {
		nl := bytes.IndexByte(buf[at:], '\n')
		line := buf[at:]
		if nl >= 0 {
			line = buf[at : at+nl]
		}
		if isLine(line, "message", "user") {
			out = append(out, base+int64(at))
		}
		if nl < 0 {
			break
		}
		at += nl + 1
	}
	return out
}

// isLine reports whether a journal line is of the given type (and role). The
// cheap prefix test keeps a long tool result from being decoded just to learn
// it is not a person's message.
func isLine(line []byte, typ, role string) bool {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' || !bytes.Contains(line, []byte(`"type":"`+typ+`"`)) {
		return false
	}
	var head struct {
		Type string `json:"type"`
		Role string `json:"role"`
	}
	if json.Unmarshal(line, &head) != nil {
		return false
	}
	return head.Type == typ && (role == "" || strings.EqualFold(head.Role, role))
}
