package cellsync

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
)

// stubFurrow is a cellstore.Transport that does what furrow's export and
// published verbs do to the disk, and counts the work: export seals every
// object of a head the ledger does not list into frames written afresh to the
// outbox (furrow writes each frame to a .part file and renames it, so every
// frame is born again), and published records a frame's rids and deletes it.
// Sealed counts objects sealed, which is the CPU an export costs.
type stubFurrow struct {
	objects                  map[string][]blobstore.Object // head -> its objects, sealed form
	perFrame                 int
	exports, sealed, written int
}

func (s *stubFurrow) Do(_ context.Context, t cellstore.Target, op cellstore.Op) ([]byte, error) {
	args := op.Args()
	switch op.Verb() {
	case "export":
		s.exports++
		head, outbox, ledger := flag(args, "--head"), flag(args, "--outbox"), flag(args, "--ledger")
		sent := readLedger(filepath.Join(t.DataDir, "published."+ledger))
		var unsent []blobstore.Object
		for _, o := range s.objects[head] {
			if !sent[o.RID] {
				unsent = append(unsent, o)
			}
		}
		if err := os.MkdirAll(outbox, 0o700); err != nil {
			return nil, err
		}
		out := cellstore.Export{HeadRID: head}
		for len(unsent) > 0 {
			n := min(s.perFrame, len(unsent))
			frame, err := blobstore.Encode(strings.Repeat("ab", 16), unsent[:n])
			if err != nil {
				return nil, err
			}
			s.sealed += n
			path := filepath.Join(outbox, string(blobstore.IDOf(frame))+".agef")
			if err := os.WriteFile(path+".part", frame, 0o600); err != nil {
				return nil, err
			}
			if err := os.Rename(path+".part", path); err != nil {
				return nil, err
			}
			s.written++
			out.Frames = append(out.Frames, cellstore.FrameFile{Path: path, Objects: n, Bytes: int64(len(frame))})
			out.Objects += n
			out.Bytes += int64(len(frame))
			unsent = unsent[n:]
		}
		return json.Marshal(out)
	case "published":
		ledger := flag(args, "--ledger")
		var rids []string
		var frames []string
		for i, a := range args {
			if strings.HasSuffix(a, ".agef") && i > 0 {
				frames = append(frames, a)
			}
		}
		for _, path := range frames {
			raw, err := os.ReadFile(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			h, _, err := blobstore.Decode(raw)
			if err != nil {
				return nil, err
			}
			for _, o := range h.Objects {
				rids = append(rids, o.RID)
			}
		}
		f, err := os.OpenFile(filepath.Join(t.DataDir, "published."+ledger), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, err
		}
		for _, r := range rids {
			fmt.Fprintln(f, r)
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		for _, path := range frames {
			_ = os.Remove(path)
		}
		return json.Marshal(map[string]int{"recorded": len(rids)})
	}
	return nil, fmt.Errorf("stub furrow has no verb %q", op.Verb())
}

func flag(args []string, name string) string {
	for i := range len(args) - 1 {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}

func readLedger(path string) map[string]bool {
	sent := map[string]bool{}
	f, err := os.Open(path)
	if err != nil {
		return sent
	}
	defer f.Close()
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		sent[lines.Text()] = true
	}
	return sent
}

// objectsOf makes n distinct sealed objects of about size bytes each.
func objectsOf(tag string, n, size int) []blobstore.Object {
	out := make([]blobstore.Object, n)
	for i := range out {
		unit := fmt.Sprintf("%s-%d|", tag, i)
		rid, raw := pack(kindBlob, []byte(strings.Repeat(unit, size/len(unit)+1))[:size])
		out[i] = blobstore.Object{RID: rid, Bytes: raw}
	}
	return out
}

// refusingStore is a relay with storage turned off: it answers every question
// and refuses every frame, as the hosted relay's 403 "storage_off" does.
type refusingStore struct {
	blobstore.Store
	puts int
}

var errStorageOff = errors.New(`blobstore: server refused with 403 "storage_off"`)

func (s *refusingStore) PutFrame(context.Context, []byte) (blobstore.FrameID, error) {
	s.puts++
	return "", errStorageOff
}

// outboxRig is one chat whose export goes through the real cellstore
// SyncEngine to stubFurrow.
func outboxRig(t *testing.T) (*stubFurrow, cellstore.SyncEngine, cell.Cell, string) {
	t.Helper()
	data, outbox := t.TempDir(), filepath.Join(t.TempDir(), "chat.outbox")
	stub := &stubFurrow{objects: map[string][]blobstore.Object{}, perFrame: 8}
	eng := cellstore.SyncEngine{
		Transport: stub,
		Keys:      cellstore.SyncKeys{CellKey: make([]byte, 32), Dedup: make([]byte, 32)},
		Ledger:    cellstore.LedgerName("http://relay", "id_test"),
		Target:    func(cell.Cell) cellstore.Target { return cellstore.Target{DataDir: data} },
		Outbox:    func(cell.Cell) string { return outbox },
	}
	return stub, eng, cell.Cell{ID: cellID, Root: t.TempDir()}, outbox
}

// TestARefusedUploadIsNotSealedAgainOnTheNextPass is the 2026-11-09 laptop at
// 400% CPU: a relay refused every put, the background share pass tried each
// chat again a minute later, and every attempt sealed and rewrote the chat's
// whole outbox although nothing had changed. A second pass over the same head
// must reuse the frames still waiting, not export them again.
func TestARefusedUploadIsNotSealedAgainOnTheNextPass(t *testing.T) {
	ctx := context.Background()
	stub, eng, c, outbox := outboxRig(t)
	const head = "head-1"
	stub.objects[head] = objectsOf("a", 64, 2<<10) // eight frames, well over askFloor
	store := &refusingStore{Store: blobstore.NewMemory()}
	pub := &Publisher{Engine: eng, Store: store}

	const passes = 5
	for range passes {
		if _, err := pub.Upload(ctx, c, head); !errors.Is(err, errStorageOff) {
			t.Fatalf("upload = %v, want the relay's refusal", err)
		}
	}
	t.Logf("%d passes: %d exports, %d objects sealed, %d frames written, %d puts",
		passes, stub.exports, stub.sealed, stub.written, store.puts)
	if stub.exports != 1 || stub.sealed != 64 || stub.written != 8 {
		t.Fatalf("%d passes with no change ran %d exports sealing %d objects into %d frames; want one export of 64 objects in 8 frames",
			passes, stub.exports, stub.sealed, stub.written)
	}
	if frames, _ := filepath.Glob(filepath.Join(outbox, "*.agef")); len(frames) != 8 {
		t.Fatalf("outbox holds %d frames, want the 8 still waiting", len(frames))
	}
}

// TestANewTurnReplacesTheWaitingFrames: once the head moves, the waiting export
// is sealed again for the new head, and the frames only the old export named
// are deleted rather than left in the outbox for ever.
func TestANewTurnReplacesTheWaitingFrames(t *testing.T) {
	ctx := context.Background()
	stub, eng, c, outbox := outboxRig(t)
	first := objectsOf("a", 16, 1<<10)
	stub.objects["head-1"] = first
	// The new turn's objects pack first, as a new tree does, so every frame
	// boundary moves and no file of the first export is written again.
	stub.objects["head-2"] = append(objectsOf("b", 3, 1<<10), first...)
	if _, err := eng.Export(ctx, c, "head-1"); err != nil {
		t.Fatal(err)
	}
	ex, err := eng.Export(ctx, c, "head-2")
	if err != nil {
		t.Fatal(err)
	}
	if stub.exports != 2 {
		t.Fatalf("a moved head ran %d exports, want 2", stub.exports)
	}
	frames, _ := filepath.Glob(filepath.Join(outbox, "*.agef"))
	if len(frames) != len(ex.Frames) {
		t.Fatalf("outbox holds %d frames, want only the %d of the newest export", len(frames), len(ex.Frames))
	}
}

// TestAPublishOrARecordEndsTheReuse: the kept export is answered only while it
// is exactly what the engine would write. A frame published since, or a ledger
// that grew under it, sends the next pass to the engine.
func TestAPublishOrARecordEndsTheReuse(t *testing.T) {
	ctx := context.Background()
	stub, eng, c, _ := outboxRig(t)
	const head = "head-1"
	stub.objects[head] = objectsOf("a", 24, 1<<10)
	ex, err := eng.Export(ctx, c, head)
	if err != nil || len(ex.Frames) != 3 {
		t.Fatalf("export = %d frames, %v", len(ex.Frames), err)
	}
	if err := eng.Published(ctx, c, []string{ex.Frames[0].Path}); err != nil {
		t.Fatal(err)
	}
	again, err := eng.Export(ctx, c, head)
	if err != nil {
		t.Fatal(err)
	}
	if stub.exports != 2 || again.Objects != 16 {
		t.Fatalf("after a publish: %d exports, %d objects; want a second export of the 16 unsent", stub.exports, again.Objects)
	}
	if _, err := eng.Export(ctx, c, head); err != nil || stub.exports != 2 {
		t.Fatalf("an unchanged pass ran export %d times, %v; want it kept", stub.exports, err)
	}
}
