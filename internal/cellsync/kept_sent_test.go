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
type failAfter struct {
	blobstore.Store
	n int
}

var errCutShort = errors.New("upload cut short")

func (s *failAfter) PutFrame(ctx context.Context, frame []byte) (blobstore.FrameID, error) {
	if s.n == 0 {
		return "", errCutShort
	}
	s.n--
	return s.Store.PutFrame(ctx, frame)
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
