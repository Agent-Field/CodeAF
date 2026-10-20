package filememo

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestStampSharesReadsAndDetectsReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int64
	memo := New(func(_ string, data []byte, _ bool) (string, error) { reads.Add(1); return string(data), nil })
	check := func(want string, count int64) {
		t.Helper()
		var wg sync.WaitGroup
		for range 32 {
			wg.Go(func() {
				got, err := memo.Read(path)
				if err != nil || got != want {
					t.Errorf("read=%q %v, want %q", got, err, want)
				}
			})
		}
		wg.Wait()
		if reads.Load() != count {
			t.Fatalf("reads=%d, want %d", reads.Load(), count)
		}
	}
	check("first", 1)
	check("first", 1)
	info, _ := os.Stat(path)
	next := path + ".new"
	if err := os.WriteFile(next, []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(next, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(next, path); err != nil {
		t.Fatal(err)
	}
	check("other", 2)
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	check("x", 3)
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	check("changed", 4)
}

func TestMemoBoundsRetainedPaths(t *testing.T) {
	memo := New(func(_ string, data []byte, _ bool) (string, error) { return string(data), nil })
	for i := range 100 {
		if _, err := memo.Read(filepath.Join(t.TempDir(), fmt.Sprint(i))); err != nil {
			t.Fatal(err)
		}
	}
	if len(memo.held) != 64 {
		t.Fatalf("held=%d, want 64", len(memo.held))
	}
}

func TestMemoReadErrorIsNeitherMissingNorCachedContent(t *testing.T) {
	path := t.TempDir()
	calls := 0
	memo := New(func(_ string, data []byte, missing bool) (string, error) {
		calls++
		return string(data), nil
	})
	for range 2 {
		if _, err := memo.Read(path); err == nil || os.IsNotExist(err) {
			t.Fatalf("directory read error became absence: %v", err)
		}
	}
	if calls != 0 || len(memo.held) != 0 || memo.Reads() != 2 {
		t.Fatalf("failed reads derived=%d held=%d reads=%d", calls, len(memo.held), memo.Reads())
	}
}
