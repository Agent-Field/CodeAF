package cellsync

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
)

// failAfter is a store that takes n frames and then refuses every other one.
// put names every frame it took.
type failAfter struct {
	blobstore.Store
	n   int
	put []string
}

var errCutShort = errors.New("upload cut short")

func (s *failAfter) PutFrame(ctx context.Context, frame []byte) (blobstore.FrameID, error) {
	if s.n == 0 {
		return "", errCutShort
	}
	s.n--
	id, err := s.Store.PutFrame(ctx, frame)
	if err == nil {
		s.put = append(s.put, string(id))
	}
	return id, err
}

// TestAnInterruptedUploadKeepsTheFramesItSent: a large chat whose upload runs
// out of time must not start again from nothing. The frames that went up are in
// the store whatever happens to the rest, so the next export packs only the
// objects still missing, and the head still does not move.
func TestAnInterruptedUploadKeepsTheFramesItSent(t *testing.T) {
	ctx := context.Background()
	c := cell.Cell{ID: cellID, Root: t.TempDir()}
	eng := NewFakeEngine(t.TempDir())
	eng.MaxObjects = 2
	files := map[string]string{}
	for i := range 6 {
		files[fmt.Sprintf("file-%d.txt", i)] = fmt.Sprintf("content of file %d", i)
	}
	head := eng.Seal(c, files)
	whole, err := eng.Ledger(t.TempDir()).Export(ctx, c, head)
	if err != nil || len(whole.Frames) < 3 {
		t.Fatalf("fixture needs at least three frames: %d, %v", len(whole.Frames), err)
	}
	store := &failAfter{Store: blobstore.NewMemory(), n: 2}
	pub := &Publisher{Engine: eng, Store: store}
	if _, err := pub.Upload(ctx, c, head); !errors.Is(err, errCutShort) {
		t.Fatalf("upload = %v, want the store's refusal", err)
	}
	again, err := eng.Export(ctx, c, head)
	if err != nil {
		t.Fatal(err)
	}
	sent := whole.Frames[0].Objects + whole.Frames[1].Objects
	if again.Objects != whole.Objects-sent {
		t.Fatalf("after two of %d frames went up the next export packs %d of %d objects, want %d", len(whole.Frames), again.Objects, whole.Objects, whole.Objects-sent)
	}
	store.n = len(again.Frames)
	if _, err := pub.Upload(ctx, c, head); err != nil {
		t.Fatalf("resumed upload: %v", err)
	}
	if rest, err := eng.Export(ctx, c, head); err != nil || rest.Objects != 0 {
		t.Fatalf("after the resumed upload %d objects are still unsent (%v)", rest.Objects, err)
	}
}

// TestABranchCutShortStillNamesEveryFrameItPut: the branch's own upload is the
// only writer of its orphan frames, so its plan must name them. Keeping the
// frames of an attempt cut short left them out of the retry's export and so out
// of the plan; only Upload, which names no plan, keeps them.
func TestABranchCutShortStillNamesEveryFrameItPut(t *testing.T) {
	r := newRig(t)
	r.publishFirst(map[string]string{"a": "0"})
	r.engA.MaxObjects = 2
	files := map[string]string{}
	for i := range 6 {
		files[fmt.Sprintf("file-%d.txt", i)] = fmt.Sprintf("branch content %d", i)
	}
	kept := r.seal(files)
	store := &failAfter{Store: r.store, n: 1}
	r.pub.Store = store
	b := Brancher{Dir: r.dirA, Publisher: r.pub, NewID: r.newID}
	if _, err := b.Branch(context.Background(), r.drv, kept, 1, r.info()); !errors.Is(err, errCutShort) {
		t.Fatalf("branch = %v, want the store's refusal", err)
	}
	if len(store.put) != 1 {
		t.Fatalf("fixture put %d frames before the refusal, want 1", len(store.put))
	}
	store.n = 1 << 10
	id, err := b.Branch(context.Background(), r.drv, kept, 1, r.info())
	if err != nil {
		t.Fatal(err)
	}
	plan := map[string]bool{}
	for _, f := range r.head(id).Frames {
		plan[f] = true
	}
	for _, f := range store.put {
		if !plan[f] {
			t.Fatalf("branch plan %v leaves out frame %s it put", r.head(id).Frames, f)
		}
	}
}
