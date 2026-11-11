package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A CONVERSATION READ THROUGH ITS PINNED FOLDER STILL ANSWERS FROM ITS LEGACY
// ARRAYS, WITH NOTHING FOLDED OR WRITTEN, AND ONE WITH NO HISTORY IS EMPTY
// RATHER THAN AN ERROR.
func TestReadProcessPageWithinRootReadsLegacyArraysAndWritesNothing(t *testing.T) {
	places := t.TempDir()
	dir := filepath.Join(places, "b", "c")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, placeTranscript), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	page, err := ReadProcessPageWithinRoot(ctx, places, "b", "c", ProcessPageAsk{})
	if err != nil || len(page.Rows) != 0 {
		t.Fatalf("empty: %+v %v", page, err)
	}
	legacy := `[{"ID":1,"Kind":"command","Command":"a","State":"done"},{"ID":2,"Kind":"command","Command":"b","State":"done"}]`
	if err := os.WriteFile(filepath.Join(dir, processLegacyNames[0]), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	page, err = ReadProcessPageWithinRoot(ctx, places, "b", "c", ProcessPageAsk{Limit: 1})
	if err != nil || len(page.Rows) != 1 || page.Rows[0].ID != 2 || page.Next == "" {
		t.Fatalf("legacy: %+v %v", page, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, processStoreName)); err == nil {
		t.Fatal("a read created the store")
	}
	// A legacy array that is a link is not read.
	outside := filepath.Join(t.TempDir(), "x.json")
	_ = os.WriteFile(outside, []byte(legacy), 0o600)
	_ = os.Remove(filepath.Join(dir, processLegacyNames[0]))
	_ = os.Symlink(outside, filepath.Join(dir, processLegacyNames[0]))
	page, err = ReadProcessPageWithinRoot(ctx, places, "b", "c", ProcessPageAsk{})
	if err == nil && len(page.Rows) != 0 {
		t.Fatalf("a linked array was read: %+v", page)
	}
	// A transcript that is a directory, or missing, is not a conversation.
	_ = os.Remove(filepath.Join(dir, placeTranscript))
	if _, err := ReadProcessPageWithinRoot(ctx, places, "b", "c", ProcessPageAsk{}); err == nil {
		t.Fatal("a folder with no transcript was read")
	}
}
