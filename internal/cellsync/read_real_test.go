package cellsync

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	storetest "github.com/Agent-Field/codeaf/internal/cellstore/celltest"
	"github.com/Agent-Field/codeaf/internal/home"
)

// realDevice is one computer's real engine over its own store.
type realDevice struct {
	eng  cellstore.Engine
	sync cellstore.SyncEngine
}

func newRealDevice(t *testing.T, bin string, daemon bool) realDevice {
	eng := cellstore.Engine{Binary: bin, DataRoot: t.TempDir(), Transport: cellstore.Spawn{Binary: bin}}
	if daemon {
		eng.Transport = nil // the app's transport: the managed daemon, then a spawn
	}
	keys := cellstore.SyncKeys{CellKey: bytes.Repeat([]byte{7}, 32), Dedup: bytes.Repeat([]byte{9}, 32)}
	return realDevice{eng: eng, sync: eng.Sync(keys, cellstore.LedgerName("http://relay", "id_read"))}
}

// sendTo exports what the relay lacks of head and puts the frames.
func (d realDevice) sendTo(t *testing.T, c cell.Cell, head string, mem *blobstore.Memory) {
	t.Helper()
	ex, err := d.sync.Export(context.Background(), c, head)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range ex.Frames {
		raw, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mem.PutFrame(context.Background(), raw); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, f.Path)
	}
	if err := d.sync.Published(context.Background(), c, paths); err != nil {
		t.Fatal(err)
	}
}

// The read against a real furrow: a chat with a large project folder and a
// multi-megabyte transcript is read on a second computer for a fraction of its
// size, a new turn costs about one chunk, and a take afterwards fetches no
// object the read already brought. It skips green with an engine that cannot
// read a path (the pinned one, until furrow's path reads ship): point
// CODEAF_FURROW at a build of Agent-Field/furrow feat/path-range-read.
//
// It runs twice: over spawned engines, and over the managed daemon the app
// drives the sync verbs through, whose JSON arguments are their own spelling.
func TestReadARemoteChatRealEngine(t *testing.T) {
	bin := celltest.Engine(t)
	if !cellstore.ReadsPathsAt(bin) {
		t.Skipf("engine %s cannot read one path of a snapshot (furrow --json capabilities lacks read)", bin)
	}
	for _, daemon := range []bool{false, true} {
		t.Run(map[bool]string{false: "spawn", true: "daemon"}[daemon], func(t *testing.T) {
			// The daemon's socket lives under the codeaf home: never the real one.
			// EngineDir stops every spawned engine before its folder is removed.
			t.Setenv(home.EnvVar, storetest.EngineDir(t))
			t.Setenv("CODEAF_ENGINE_DAEMON", map[bool]string{false: "0", true: "1"}[daemon])
			readARemoteChat(t, bin, daemon)
		})
	}
}

func readARemoteChat(t *testing.T, bin string, daemon bool) {
	holder, reader := newRealDevice(t, bin, daemon), newRealDevice(t, bin, daemon)
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	project := make([]byte, 12<<20)
	if _, err := rand.Read(project); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Root, "big.bin"), project, 0o600); err != nil {
		t.Fatal(err)
	}
	journal := transcriptOfSize(0, 4<<20)
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(c.Root, cell.TranscriptPath), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(journal)
	sealed, err := holder.eng.Seal(context.Background(), c, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	head := sealed.Turn.ID
	mem := blobstore.NewMemory()
	holder.sendTo(t, c, head, mem)

	store := &asking{Store: mem}
	there := cell.Cell{ID: c.ID, Root: filepath.Join(t.TempDir(), "never-made")}
	r := &Reader{Engine: reader.sync, Store: store, Inbox: reader.sync.Inbox}
	pager := &Pager{Reader: r}
	page, err := pager.Page(context.Background(), there, head, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	total := int64(len(project) + len(journal))
	t.Logf("first page: %d bytes of transcript [%d,%d), fetched %d of %d (%.1f%%)", len(page.Segment), page.Start, page.End, r.Fetched, total, 100*float64(r.Fetched)/float64(total))
	if !page.More() || !bytes.Contains(page.Segment, []byte(`"type":"session"`)) {
		t.Fatalf("newest page reached the start or lost the header: start %d", page.Start)
	}
	if r.Fetched > 4<<20 {
		t.Fatalf("the first page fetched %d bytes; a 12 MiB folder must not be paid for", r.Fetched)
	}
	if _, err := os.Stat(there.Root); !os.IsNotExist(err) {
		t.Fatalf("the read created the chat's folder: %v", err)
	}
	older, err := pager.Page(context.Background(), there, head, page.Before, 20)
	if err != nil {
		t.Fatal(err)
	}
	if older.End != page.Start || older.Start >= older.End {
		t.Fatalf("the older page is [%d,%d), want it to end at %d", older.Start, older.End, page.Start)
	}
	read := map[string]bool{}
	for _, rid := range store.take() {
		read[rid] = true
	}

	// One more turn: the read pays its index objects and its new tail.
	journal += transcriptOfSize(1000, 6<<10)
	write(journal)
	next, err := holder.eng.Seal(context.Background(), c, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	holder.sendTo(t, c, next.Turn.ID, mem)
	r.Fetched = 0
	newer, err := pager.Page(context.Background(), there, next.Turn.ID, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("after a new turn: fetched %d bytes", r.Fetched)
	if !bytes.Contains(newer.Segment, []byte("question 1000")) {
		t.Fatal("the new turn is not on the newest page")
	}
	if r.Fetched > 2<<20 {
		t.Fatalf("a new turn cost %d bytes", r.Fetched)
	}
	for _, rid := range store.take() {
		if read[rid] {
			t.Fatalf("%s fetched twice", rid)
		}
		read[rid] = true
	}

	// The take afterwards: the same store, so no object is fetched again. A
	// take makes the chat's folder before it fetches (syncsetup's landing);
	// the read never did.
	if err := os.MkdirAll(there.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	f := &Fetcher{Engine: reader.sync, Store: store, Inbox: reader.sync.Inbox}
	if err := f.Complete(context.Background(), there, next.Turn.ID); err != nil {
		t.Fatal(err)
	}
	took := store.take()
	for _, rid := range took {
		if read[rid] {
			t.Fatalf("the take fetched %s again, which the read already held", rid)
		}
	}
	t.Logf("the take then fetched %d objects, none of the read's %d", len(took), len(read))
}

// transcriptOfSize is turns from `from` on until the journal is at least size
// bytes; turn 0 carries the header.
func transcriptOfSize(from, size int) string {
	var b strings.Builder
	for i := from; b.Len() < size; i++ {
		b.WriteString(transcript(i, 1))
	}
	return b.String()
}
