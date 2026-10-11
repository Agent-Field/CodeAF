package vectors

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// fake is an Embedder whose vector depends on the text and the space, counts
// the texts it was asked for, and can be told to fail on its nth batch.
type fake struct {
	space   placeorg.SpaceID
	calls   atomic.Int32
	texts   atomic.Int32
	failOn  int32 // 1-based batch number, 0 never
	mu      sync.Mutex
	seenTxt []string
}

func (f *fake) Space() placeorg.SpaceID { return f.space }
func (f *fake) Dims() int               { return placeorg.PrimaryDims }
func (f *fake) Embed(_ context.Context, texts []string) ([][]float32, error) {
	n := f.calls.Add(1)
	if f.failOn != 0 && n == f.failOn {
		return nil, errors.New("provider outage")
	}
	f.texts.Add(int32(len(texts)))
	f.mu.Lock()
	f.seenTxt = append(f.seenTxt, texts...)
	f.mu.Unlock()
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, placeorg.PrimaryDims)
		v[len(t)%placeorg.PrimaryDims] = 1
		out[i] = v
	}
	return out, nil
}

func corpus(n int) *placeorg.Corpus {
	c := &placeorg.Corpus{}
	for i := 0; i < n; i++ {
		c.Chats = append(c.Chats, placeorg.Chat{ID: fmt.Sprintf("c%d", i), Text: fmt.Sprintf("title %d %s", i, "x")})
	}
	return c
}

func TestAnUnchangedTextIsNeverRefetched(t *testing.T) {
	dir, c := t.TempDir(), corpus(150)
	p := &fake{space: placeorg.SpacePrimary}
	_, _, fell, err := New(dir).Vectorize(context.Background(), c, p, &fake{space: placeorg.SpaceLocal})
	if err != nil || fell || p.texts.Load() != 150 {
		t.Fatalf("first run: err=%v fell=%v texts=%d", err, fell, p.texts.Load())
	}
	p2 := &fake{space: placeorg.SpacePrimary}
	_, _, fell, err = New(dir).Vectorize(context.Background(), c, p2, &fake{space: placeorg.SpaceLocal})
	if err != nil || fell || p2.calls.Load() != 0 {
		t.Fatalf("second run called the embedder %d times (err=%v fell=%v)", p2.calls.Load(), err, fell)
	}
}

func TestAChangedTitleReEmbedsExactlyThatChat(t *testing.T) {
	dir, c := t.TempDir(), corpus(10)
	vz := New(dir)
	if _, _, _, err := vz.Vectorize(context.Background(), c, &fake{space: placeorg.SpacePrimary}, nil); err != nil {
		t.Fatal(err)
	}
	c.Chats[4].Text = "a brand new title"
	p := &fake{space: placeorg.SpacePrimary}
	_, vecs, _, err := vz.Vectorize(context.Background(), c, p, nil)
	if err != nil || p.texts.Load() != 1 || p.seenTxt[0] != "a brand new title" {
		t.Fatalf("err=%v texts=%d seen=%v", err, p.texts.Load(), p.seenTxt)
	}
	if vecs[4][len("a brand new title")] != 1 {
		t.Fatal("the changed chat did not get its new vector")
	}
}

func TestAnOutageOnBatchThreeYieldsACompleteLocalResult(t *testing.T) {
	dir, c := t.TempDir(), corpus(64*5)
	p := &fake{space: placeorg.SpacePrimary, failOn: 3}
	l := &fake{space: placeorg.SpaceLocal}
	space, vecs, fell, err := New(dir).Vectorize(context.Background(), c, p, l)
	if err != nil || !fell || space != placeorg.SpaceLocal {
		t.Fatalf("err=%v fell=%v space=%s", err, fell, space)
	}
	if len(vecs) != len(c.Chats) {
		t.Fatalf("got %d vectors", len(vecs))
	}
	for i, v := range vecs {
		if v == nil {
			t.Fatalf("chat %d has no vector", i)
		}
	}
	if int(l.texts.Load()) != len(c.Chats) {
		t.Fatalf("local embedded %d, want all %d", l.texts.Load(), len(c.Chats))
	}
}

func TestNoPrimaryFallsToLocal(t *testing.T) {
	space, vecs, fell, err := New(t.TempDir()).Vectorize(context.Background(), corpus(3), nil, &fake{space: placeorg.SpaceLocal})
	if err != nil || !fell || space != placeorg.SpaceLocal || len(vecs) != 3 {
		t.Fatalf("err=%v fell=%v space=%s", err, fell, space)
	}
}

func TestACrashMidRecordKeepsEverythingBefore(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, placeorg.SpacePrimary, placeorg.PrimaryDims)
	if err != nil {
		t.Fatal(err)
	}
	v := make([]float32, placeorg.PrimaryDims)
	v[7] = 1
	_ = s.Put("a", "h", v)
	_ = s.Put("b", "h", v)
	path := s.path
	s.Close()
	fi, _ := os.Stat(path)
	if err := os.Truncate(path, fi.Size()-100); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, placeorg.SpacePrimary, placeorg.PrimaryDims)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, ok := s.Get("a", "h"); !ok || got[7] != 1 {
		t.Fatal("intact record lost")
	}
	if _, ok := s.Get("b", "h"); ok {
		t.Fatal("torn record served")
	}
	if err := s.Put("c", "h", v); err != nil {
		t.Fatal(err)
	}
}

func TestCompactionWhenHalfIsStale(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir, placeorg.SpacePrimary, placeorg.PrimaryDims)
	v := make([]float32, placeorg.PrimaryDims)
	for i := 0; i < 4; i++ {
		_ = s.Put("a", fmt.Sprint("h", i), v) // three stale, one live
	}
	did, err := s.Compact()
	if err != nil || !did {
		t.Fatalf("did=%v err=%v", did, err)
	}
	_ = s.Put("b", "h", v)
	s.Close()
	s, _ = Open(dir, placeorg.SpacePrimary, placeorg.PrimaryDims)
	defer s.Close()
	if _, ok := s.Get("a", "h3"); !ok || s.Len() != 2 || s.records != 2 {
		t.Fatalf("len=%d records=%d", s.Len(), s.records)
	}
}

func TestTenThousandVectorsLoadUnder200ms(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir, placeorg.SpacePrimary, placeorg.PrimaryDims)
	rng := rand.New(rand.NewSource(1))
	v := make([]float32, placeorg.PrimaryDims)
	for i := 0; i < 10000; i++ {
		for j := range v {
			v[j] = rng.Float32()
		}
		if err := s.Put(fmt.Sprint("chat-", i), "hash", v); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	start := time.Now()
	s, err := Open(dir, placeorg.SpacePrimary, placeorg.PrimaryDims)
	took := time.Since(start)
	if err != nil || s.Len() != 10000 {
		t.Fatalf("err=%v len=%d", err, s.Len())
	}
	s.Close()
	if took > 200*time.Millisecond {
		t.Fatalf("load took %v", took)
	}
}
