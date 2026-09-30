package cellsync

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
)

// fetcherFor is a second device's fetcher over store, with its own engine.
func fetcherFor(t *testing.T, store blobstore.Store) (*Fetcher, cell.Cell) {
	t.Helper()
	c := cell.Cell{ID: cellID, Root: t.TempDir()}
	inbox := t.TempDir()
	return &Fetcher{
		Engine: NewFakeEngine(t.TempDir()),
		Store:  store,
		Inbox:  func(c cell.Cell) string { return filepath.Join(inbox, c.ID) },
	}, c
}

func TestFetchMaterializesHead(t *testing.T) {
	r := newRig(t)
	r.publishFirst(map[string]string{"a": "one"})
	head := r.seal(map[string]string{"a": "one", "sub/b": "two"})
	if err := r.pub.Publish(context.Background(), r.drv, head, r.info()); err != nil {
		t.Fatal(err)
	}
	f, c := fetcherFor(t, r.mem)
	if err := f.Fetch(context.Background(), c, head); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a": "one", "sub/b": "two"}
	if got := readFiles(t, c.Root); !reflect.DeepEqual(got, want) {
		t.Fatalf("materialized %v, want %v", got, want)
	}
}

func TestFetchNamesMissingRid(t *testing.T) {
	r := newRig(t)
	head := r.publishFirst(map[string]string{"a": "one"})
	f, c := fetcherFor(t, blobstore.NewMemory()) // a store that never got the objects
	err := f.Fetch(context.Background(), c, head)
	if !errors.Is(err, blobstore.ErrNotFound) || !strings.Contains(err.Error(), head) {
		t.Fatalf("fetch = %v, want ErrNotFound naming %s", err, head)
	}
}

func TestFetchReportsProgress(t *testing.T) {
	r := newRig(t)
	r.publishFirst(map[string]string{"a": "one"})
	head := r.seal(map[string]string{"a": "one", "b": "two"})
	if err := r.pub.Publish(context.Background(), r.drv, head, r.info()); err != nil {
		t.Fatal(err)
	}
	f, c := fetcherFor(t, r.mem)
	var have, want []int
	f.OnProgress = func(h, w int) { have, want = append(have, h), append(want, w) }
	if err := f.Fetch(context.Background(), c, head); err != nil {
		t.Fatal(err)
	}
	if len(have) < 2 {
		t.Fatalf("expected one report per round, got %v of %v", have, want)
	}
	for i := 1; i < len(have); i++ {
		if have[i] <= have[i-1] || want[i] < want[i-1] {
			t.Fatalf("progress went backwards: have %v want %v", have, want)
		}
	}
	if last := len(have) - 1; have[last] != want[last] {
		t.Fatalf("finished with %d of %d", have[last], want[last])
	}
}

// A takeover asks the store once per frame's worth of objects, not once per
// object: three hundred small files are a handful of requests.
func TestFetchAsksOncePerBatchNotOncePerObject(t *testing.T) {
	r := newRig(t)
	files := map[string]string{}
	for i := range 300 {
		files[fmt.Sprintf("f%03d", i)] = fmt.Sprint("body ", i)
	}
	head := r.publishFirst(files)
	f, c := fetcherFor(t, r.mem)
	before := gets(r.mem)
	if err := f.Fetch(context.Background(), c, head); err != nil {
		t.Fatal(err)
	}
	objects := 301 // a blob for each file, and the snapshot
	if got, most := gets(r.mem)-before, 2*(objects/blobstore.MaxGetMany+2); got > most {
		t.Fatalf("%d store requests for %d objects, want at most %d", got, objects, most)
	}
	if got := readFiles(t, c.Root); !reflect.DeepEqual(got, files) {
		t.Fatalf("materialized %d files, want %d", len(got), len(files))
	}
}

// gets counts the get requests a Memory store has answered.
func gets(m *blobstore.Memory) int {
	n := 0
	for _, op := range m.Log() {
		if op.Kind == "get" {
			n++
		}
	}
	return n
}
