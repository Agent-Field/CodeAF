package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v3", "history.jsonl")
	store := New(path)
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

func texts(entries []Entry) []string {
	out := make([]string, len(entries))
	for index, entry := range entries {
		out[index] = entry.Text
	}
	return out
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func TestAppendAndRecentAreNewestFirst(t *testing.T) {
	store, _ := newTestStore(t)

	store.Append("first", "/work")
	store.Append("second", "/work")
	store.Append("third", "/work")

	if got, want := texts(store.Recent(0)), []string{"third", "second", "first"}; !equal(got, want) {
		t.Fatalf("Recent(0) = %v, want %v", got, want)
	}
	if got, want := texts(store.Recent(2)), []string{"third", "second"}; !equal(got, want) {
		t.Fatalf("Recent(2) = %v, want %v", got, want)
	}
	if got := store.Recent(99); len(got) != 3 {
		t.Fatalf("Recent(99) returned %d entries, want 3", len(got))
	}
}

func TestAppendDedupesConsecutiveAndIgnoresBlank(t *testing.T) {
	store, _ := newTestStore(t)

	store.Append("make check", "/work")
	store.Append("make check", "/work")
	store.Append("", "/work")
	store.Append("   \n\t ", "/work")
	store.Append("go test ./...", "/work")
	store.Append("make check", "/work") // not consecutive: kept

	if got, want := texts(store.Recent(0)), []string{"make check", "go test ./...", "make check"}; !equal(got, want) {
		t.Fatalf("Recent(0) = %v, want %v", got, want)
	}
}

func TestRecentForFiltersByCwd(t *testing.T) {
	store, _ := newTestStore(t)

	store.Append("a1", "/alpha")
	store.Append("b1", "/beta")
	store.Append("a2", "/alpha")
	store.Append("b2", "/beta")

	if got, want := texts(store.RecentFor("/alpha", 0)), []string{"a2", "a1"}; !equal(got, want) {
		t.Fatalf("RecentFor(/alpha) = %v, want %v", got, want)
	}
	if got, want := texts(store.RecentFor("/beta", 1)), []string{"b2"}; !equal(got, want) {
		t.Fatalf("RecentFor(/beta, 1) = %v, want %v", got, want)
	}
	if got := store.RecentFor("/gamma", 0); got != nil {
		t.Fatalf("RecentFor(/gamma) = %v, want nil", got)
	}
}

func TestLoadSkipsCorruptLines(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "history.jsonl")

	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	lines := []string{
		fmt.Sprintf(`{"text":"good one","cwd":"/work","ts":%q}`, stamp),
		`{"text":"half written`,     // truncated, as a crash leaves it
		`not json at all`,           // garbage
		``,                          // blank
		`{"text":"","cwd":"/work"}`, // no text to recall
		fmt.Sprintf(`{"text":"good two","cwd":"/work","ts":%q}`, stamp),
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := New(path)
	defer store.Close()

	if got, want := texts(store.Recent(0)), []string{"good two", "good one"}; !equal(got, want) {
		t.Fatalf("Recent(0) = %v, want %v", got, want)
	}

	// A new append lands after the surviving lines, not on top of them.
	store.Append("good three", "/work")
	if got, want := texts(store.Recent(1)), []string{"good three"}; !equal(got, want) {
		t.Fatalf("Recent(1) = %v, want %v", got, want)
	}
}

func TestRotateKeepsNewestHalf(t *testing.T) {
	store, path := newTestStore(t)

	for index := 0; index < maxEntries; index++ {
		store.Append(fmt.Sprintf("entry %d", index), "/work")
	}
	if err := store.flushForTest(); err != nil {
		t.Fatal(err)
	}

	onDisk := readEntries(t, path)
	if len(onDisk) != keepEntries {
		t.Fatalf("file holds %d lines after rotation, want %d", len(onDisk), keepEntries)
	}
	if got, want := onDisk[0].Text, fmt.Sprintf("entry %d", maxEntries-keepEntries); got != want {
		t.Fatalf("oldest kept line = %q, want %q", got, want)
	}
	if got, want := onDisk[len(onDisk)-1].Text, fmt.Sprintf("entry %d", maxEntries-1); got != want {
		t.Fatalf("newest kept line = %q, want %q", got, want)
	}

	// The live snapshot rotated with the file rather than drifting from it.
	recent := store.Recent(0)
	if len(recent) != keepEntries {
		t.Fatalf("Recent(0) returned %d entries after rotation, want %d", len(recent), keepEntries)
	}
	if got, want := recent[0].Text, fmt.Sprintf("entry %d", maxEntries-1); got != want {
		t.Fatalf("newest recalled = %q, want %q", got, want)
	}

	// A store opened fresh on the rotated file agrees with it.
	reopened := New(path)
	defer reopened.Close()
	if got := len(reopened.Recent(0)); got != keepEntries {
		t.Fatalf("reopened Recent(0) returned %d entries, want %d", got, keepEntries)
	}
}

func TestCloseFlushesQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")

	store := New(path)
	store.Append("typed just before exit", "/work")
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got, want := texts(readEntries(t, path)), []string{"typed just before exit"}; !equal(got, want) {
		t.Fatalf("file after Close = %v, want %v", got, want)
	}

	// Close is idempotent, and appends after it are dropped rather than queued
	// for a goroutine that will never run again.
	if err := store.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	store.Append("after close", "/work")
	if err := store.flushForTest(); err != nil {
		t.Fatal(err)
	}
	if got := len(readEntries(t, path)); got != 1 {
		t.Fatalf("file holds %d entries after a post-Close append, want 1", got)
	}

	// What was flushed is what the next session recalls.
	next := New(path)
	defer next.Close()
	if got, want := texts(next.Recent(0)), []string{"typed just before exit"}; !equal(got, want) {
		t.Fatalf("next session Recent(0) = %v, want %v", got, want)
	}
}

func TestBackgroundDrainWritesWithoutClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	store := New(path)
	defer store.Close()

	store.Append("drained by the ticker", "/work")

	// The goroutine wakes every flushInterval; poll rather than sleep out a
	// fixed budget, so the test costs one tick and not a worst case.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if entries := readEntries(t, path); len(entries) == 1 {
			if entries[0].Text != "drained by the ticker" {
				t.Fatalf("drained %q", entries[0].Text)
			}
			if entries[0].Cwd != "/work" || entries[0].Ts.IsZero() {
				t.Fatalf("drained entry lost its cwd or stamp: %+v", entries[0])
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("queue was not drained by the background goroutine")
}

func TestConcurrentAppendAndRecent(t *testing.T) {
	store, _ := newTestStore(t)

	var waiting sync.WaitGroup
	for writer := 0; writer < 4; writer++ {
		waiting.Add(1)
		go func(writer int) {
			defer waiting.Done()
			for index := 0; index < 50; index++ {
				store.Append(fmt.Sprintf("w%d-%d", writer, index), fmt.Sprintf("/cwd%d", writer))
			}
		}(writer)
	}
	for reader := 0; reader < 2; reader++ {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			for index := 0; index < 50; index++ {
				store.Recent(10)
				store.RecentFor("/cwd0", 10)
			}
		}()
	}
	waiting.Wait()

	if got := len(store.Recent(0)); got != 200 {
		t.Fatalf("Recent(0) returned %d entries, want 200", got)
	}
}

func readEntries(t *testing.T, path string) []Entry {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var entries []Entry
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("unreadable line %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}
