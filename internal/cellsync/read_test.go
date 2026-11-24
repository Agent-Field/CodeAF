package cellsync

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
)

// asking is a store that remembers every rid a GetMany asked for, which is
// what a read cost on the wire.
type asking struct {
	blobstore.Store
	mu   sync.Mutex
	rids []string
}

func (a *asking) GetMany(ctx context.Context, rids []string) ([]blobstore.Object, error) {
	got, err := a.Store.GetMany(ctx, rids)
	a.mu.Lock()
	for _, o := range got {
		a.rids = append(a.rids, o.RID)
	}
	a.mu.Unlock()
	return got, err
}

func (a *asking) take() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.rids
	a.rids = nil
	return out
}

// readRig is a holder (A) that seals and publishes a chat, and a reader (B)
// with its own engine over the same relay.
type readRig struct {
	t      *testing.T
	a, b   *FakeEngine
	ca, cb cell.Cell
	mem    *blobstore.Memory
	store  *asking
	reader *Reader
	pager  *Pager
	files  map[string]string
}

const readChunk = 256

func newReadRig(t *testing.T) *readRig {
	t.Helper()
	r := &readRig{t: t, mem: blobstore.NewMemory(), files: map[string]string{}}
	r.store = &asking{Store: r.mem}
	r.a, r.b = NewFakeEngine(t.TempDir()), NewFakeEngine(t.TempDir())
	r.a.ChunkSize = readChunk
	// The reader's chat folder is never created: a read must not need it.
	r.ca = cell.Cell{ID: cellID, Root: t.TempDir()}
	r.cb = cell.Cell{ID: cellID, Root: filepath.Join(t.TempDir(), "never-made")}
	inbox := t.TempDir()
	r.reader = &Reader{Engine: r.b, Store: r.store, Inbox: func(c cell.Cell) string { return filepath.Join(inbox, c.ID) }}
	r.pager = &Pager{Reader: r.reader}
	return r
}

// publish seals the files and sends what the relay lacks.
func (r *readRig) publish() string {
	r.t.Helper()
	head := r.a.Seal(r.ca, r.files)
	ex, err := r.a.Export(context.Background(), r.ca, head)
	if err != nil {
		r.t.Fatal(err)
	}
	var paths []string
	for _, f := range ex.Frames {
		raw, err := os.ReadFile(f.Path)
		if err != nil {
			r.t.Fatal(err)
		}
		if _, err := r.mem.PutFrame(context.Background(), raw); err != nil {
			r.t.Fatal(err)
		}
		paths = append(paths, f.Path)
	}
	if err := r.a.Published(context.Background(), r.ca, paths); err != nil {
		r.t.Fatal(err)
	}
	return head
}

// transcript is a journal of n turns: a header, then per turn a person's line,
// an answer with a call, the call's result and a long answer that spans chunks.
func transcript(from, n int) string {
	var b strings.Builder
	if from == 0 {
		b.WriteString(`{"type":"session","version":1,"id":"abc"}` + "\n")
	}
	for i := from; i < from+n; i++ {
		fmt.Fprintf(&b, `{"type":"message","role":"user","content":"question %d"}`+"\n", i)
		fmt.Fprintf(&b, `{"type":"message","role":"assistant","content":"looking","toolCalls":[{"id":"bash:%d"}]}`+"\n", i)
		fmt.Fprintf(&b, `{"type":"message","role":"tool","toolCallId":"bash:%d","content":"%s"}`+"\n", i, strings.Repeat("x", 40+i%7*60))
		fmt.Fprintf(&b, `{"type":"message","role":"assistant","content":"answer %d %s"}`+"\n", i, strings.Repeat("y", 300))
	}
	return b.String()
}

// project is a folder far larger than the transcript, which a read must never
// touch.
func (r *readRig) project() {
	r.files["work/big.bin"] = strings.Repeat("0123456789abcdef", 4096)
	r.files["work/src/main.go"] = "package main\n"
}

// rids the holder's head reaches for one path: its index and its chunks.
func (r *readRig) pathRIDs(head, path string) (index string, chunks map[string]bool) {
	r.a.mu.Lock()
	defer r.a.mu.Unlock()
	fc := r.a.cell(r.ca)
	snap, _, _ := fc.snapAt(head)
	index = snap.Files[path]
	idx, _ := fc.indexAt(head, path)
	chunks = map[string]bool{}
	for _, ch := range idx.Chunks {
		chunks[ch.RID] = true
	}
	return index, chunks
}

func TestReadFetchesOnlyTheTranscriptsPathAndTheNewestChunks(t *testing.T) {
	r := newReadRig(t)
	r.project()
	r.files[cell.TranscriptPath] = transcript(0, 3)
	r.publish()
	r.files[cell.TranscriptPath] = transcript(0, 40)
	head := r.publish()

	page, err := r.pager.Page(context.Background(), r.cb, head, "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !page.More() || page.Start == 0 {
		t.Fatalf("a 40-turn chat's newest page reached the start: %+v", page.Start)
	}
	index, chunks := r.pathRIDs(head, cell.TranscriptPath)
	allowed := map[string]bool{head: true, index: true}
	for rid := range chunks {
		allowed[rid] = true
	}
	got := r.store.take()
	tail := 0
	for _, rid := range got {
		if !allowed[rid] {
			t.Fatalf("the read fetched %s, which is not on the transcript's path (the project folder or the history)", rid)
		}
		if chunks[rid] {
			tail++
		}
	}
	// The newest page and the header's first chunk, never the whole file.
	if tail >= len(chunks)/2 {
		t.Fatalf("fetched %d of the transcript's %d chunks for a 3-entry page", tail, len(chunks))
	}
	if r.reader.Fetched <= 0 {
		t.Fatal("the read reports it fetched nothing")
	}
	if r.b.Materialized != 0 {
		t.Fatal("a read materialized the chat")
	}
	if _, err := os.Stat(r.cb.Root); !os.IsNotExist(err) {
		t.Fatalf("a read created the chat's folder: %v", err)
	}
	if !bytes.HasPrefix(page.Segment, []byte(`{"type":"session"`)) {
		t.Fatalf("a page cut from the middle lost the header line: %.80q", page.Segment)
	}
	if !bytes.Contains(page.Segment, []byte("question 39")) {
		t.Fatal("the newest page does not hold the newest turn")
	}
}

func TestPagesWalkBackToTheStartAcrossSplitLinesWithoutGapOrOverlap(t *testing.T) {
	r := newReadRig(t)
	whole := transcript(0, 24) // a whole number of pages: the oldest one ends on the first message
	r.files[cell.TranscriptPath] = whole
	head := r.publish()
	header := `{"type":"session","version":1,"id":"abc"}` + "\n"

	var parts []string
	before, end := "", int64(len(whole))
	for pages := 0; ; pages++ {
		if pages > 100 {
			t.Fatal("paging never reached the start")
		}
		page, err := r.pager.Page(context.Background(), r.cb, head, before, 4)
		if err != nil {
			t.Fatal(err)
		}
		if page.End != end {
			t.Fatalf("page ends at %d, want %d (the previous page's start)", page.End, end)
		}
		seg := string(page.Segment)
		if page.Start > 0 {
			if !strings.HasPrefix(seg, header) {
				t.Fatalf("page at %d has no header", page.Start)
			}
			seg = strings.TrimPrefix(seg, header)
			if !strings.HasPrefix(seg, `{"type":"message","role":"user"`) {
				t.Fatalf("page at %d does not open on a person's line: %.60q", page.Start, seg)
			}
		}
		if seg != whole[page.Start:page.End] {
			t.Fatalf("page [%d,%d) is not those bytes of the file", page.Start, page.End)
		}
		// NO PAGE IS EMPTY: a page that reaches the first message also takes the
		// header before it, rather than leaving it as one more page to fetch.
		if strings.Count(seg, `"role":"user"`) == 0 {
			t.Fatalf("page [%d,%d) holds no message", page.Start, page.End)
		}
		if n := strings.Count(seg, `"role":"user"`); page.More() && n < 4 {
			t.Fatalf("page at %d holds %d turns, want at least the limit", page.Start, n)
		}
		parts = append([]string{seg}, parts...)
		if !page.More() {
			if page.Start != 0 {
				t.Fatalf("the last page starts at %d", page.Start)
			}
			break
		}
		before, end = page.Before, page.Start
	}
	if strings.Join(parts, "") != whole {
		t.Fatal("the pages together are not the transcript")
	}
	// Every chunk was fetched once across all the pages.
	_, chunks := r.pathRIDs(head, cell.TranscriptPath)
	seen := map[string]int{}
	for _, rid := range r.store.take() {
		seen[rid]++
		if seen[rid] > 1 {
			t.Fatalf("%s was fetched twice", rid)
		}
	}
	for rid := range chunks {
		if seen[rid] != 1 {
			t.Fatalf("chunk %s fetched %d times", rid, seen[rid])
		}
	}
}

func TestANewTurnFetchesOnlyTheIndexAndTheNewTail(t *testing.T) {
	r := newReadRig(t)
	r.project()
	r.files[cell.TranscriptPath] = transcript(0, 30)
	first := r.publish()
	if _, err := r.pager.Page(context.Background(), r.cb, first, "", 3); err != nil {
		t.Fatal(err)
	}
	before := map[string]bool{}
	for _, rid := range r.store.take() {
		before[rid] = true
	}
	r.files[cell.TranscriptPath] = transcript(0, 30) + transcript(30, 1)
	second := r.publish()
	r.reader.Fetched = 0
	page, err := r.pager.Page(context.Background(), r.cb, second, "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(page.Segment, []byte("question 30")) {
		t.Fatal("the new turn is not on the newest page")
	}
	index, chunks := r.pathRIDs(second, cell.TranscriptPath)
	got := r.store.take()
	newChunks := 0
	for _, rid := range got {
		if before[rid] {
			t.Fatalf("%s was fetched again after the new turn", rid)
		}
		switch {
		case rid == second, rid == index:
		case chunks[rid]:
			newChunks++
		default:
			t.Fatalf("the update fetched %s, which is not the new head's transcript", rid)
		}
	}
	// The turn adds ~ a turn of bytes: the rewritten last chunk and the new ones.
	turn := len(transcript(30, 1))
	if max := turn/readChunk + 2; newChunks > max {
		t.Fatalf("the update fetched %d chunks for a %d-byte turn (at most %d)", newChunks, turn, max)
	}
	// An older page on the cursor keeps reading the snapshot paging began at.
	older, err := r.pager.Page(context.Background(), r.cb, second, page.Before, 3)
	if err != nil {
		t.Fatal(err)
	}
	if older.Head != second || older.End != page.Start {
		t.Fatalf("older page at head %s ends %d, want %s ending %d", older.Head, older.End, second, page.Start)
	}
}

func TestATakeAfterAReadFetchesOnlyWhatTheReadDidNot(t *testing.T) {
	r := newReadRig(t)
	r.project()
	r.files[cell.TranscriptPath] = transcript(0, 20)
	head := r.publish()
	if _, err := r.pager.Page(context.Background(), r.cb, head, "", 3); err != nil {
		t.Fatal(err)
	}
	read := map[string]bool{}
	for _, rid := range r.store.take() {
		read[rid] = true
	}
	// The take runs over the same engine (the same store) the read used.
	f := &Fetcher{Engine: r.b, Store: r.store, Inbox: r.reader.Inbox}
	if err := f.Complete(context.Background(), r.cb, head); err != nil {
		t.Fatal(err)
	}
	for _, rid := range r.store.take() {
		if read[rid] {
			t.Fatalf("the take fetched %s again, which the read had already brought", rid)
		}
	}
	if want, _ := r.b.Want(context.Background(), r.cb, head); len(want) != 0 {
		t.Fatalf("after the take the store still wants %d objects", len(want))
	}
}

func TestReadOfAChatWithNoTranscriptIsAnEmptyPage(t *testing.T) {
	r := newReadRig(t)
	r.project()
	head := r.publish()
	page, err := r.pager.Page(context.Background(), r.cb, head, "", 10)
	if err != nil || len(page.Segment) != 0 || page.More() {
		t.Fatalf("page = %+v, %v; want empty", page, err)
	}
}

func TestABeforeThisEngineDidNotWriteIsRefused(t *testing.T) {
	r := newReadRig(t)
	r.files[cell.TranscriptPath] = transcript(0, 2)
	head := r.publish()
	if _, err := r.pager.Page(context.Background(), r.cb, head, "not-a-cursor", 3); err != ErrBadCursor {
		t.Fatalf("err = %v, want ErrBadCursor", err)
	}
}
