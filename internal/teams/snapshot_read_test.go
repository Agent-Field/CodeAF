package teams

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type migrationContext struct {
	context.Context
	checks  int
	migrate func()
}

func (c *migrationContext) Err() error {
	c.checks++
	if c.checks == 2 {
		c.migrate()
	}
	return c.Context.Err()
}

func TestSnapshotSeesMigrationBetweenThePrimaryAndLegacyOpen(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, LegacyFileName)
	body := []byte(`{"version":2,"teams":[{"id":"present","name":"Present"}]}`)
	if err := os.WriteFile(legacy, []byte(`{"spaces":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := &migrationContext{Context: context.Background(), migrate: func() {
		// The writer publishes teams.json before renaming spaces.json. There is
		// never an instant with neither source, even though both old opens fail.
		if err := os.WriteFile(filepath.Join(dir, FileName), body, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(legacy, legacy+".migrated"); err != nil {
			t.Fatal(err)
		}
	}}
	f, _, err := ReadSnapshot(ctx, dir, 0)
	if err != nil || len(f.Teams) != 1 || f.Teams[0].ID != "present" {
		t.Fatalf("migration = %+v, %v", f, err)
	}
}

func TestSnapshotMissingIsEmptyAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	f, rev, err := ReadSnapshot(context.Background(), dir, 0)
	if err != nil || len(f.Teams) != 0 || rev != revisionOf(nil) {
		t.Fatalf("missing = %+v %q %v", f, rev, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("the read left %v", entries)
	}
}

func TestSnapshotRefusesWhatItCannotReadHonestly(t *testing.T) {
	put := func(body string) string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	if _, _, err := ReadSnapshot(context.Background(), put(`{"version":2,"teams":[`), 0); !errors.Is(err, ErrSnapshotUnreadable) {
		t.Fatalf("malformed = %v", err)
	}
	if _, _, err := ReadSnapshot(context.Background(), put(""), 0); !errors.Is(err, ErrSnapshotUnreadable) {
		t.Fatalf("empty = %v", err)
	}
	if _, _, err := ReadSnapshot(context.Background(), put(" \nnull\t"), 0); !errors.Is(err, ErrSnapshotUnreadable) {
		t.Fatalf("null is not a missing file: %v", err)
	}
	// Older empty objects and null lists keep the existing decoder's semantics.
	for _, body := range []string{`{}`, `{"version":2,"teams":null}`} {
		if f, _, err := ReadSnapshot(context.Background(), put(body), 0); err != nil || len(f.Teams) != 0 {
			t.Fatalf("legacy empty %q = %+v, %v", body, f, err)
		}
	}
	if _, _, err := ReadSnapshot(context.Background(), put(`{"version":3,"teams":[]}`), 0); !errors.Is(err, ErrSnapshotVersion) {
		t.Fatalf("future = %v", err)
	}
	exact := `{"version":2,"teams":[],"p":"` + strings.Repeat("x", 100) + `"}`
	if _, _, err := ReadSnapshot(context.Background(), put(exact), int64(len(exact))); err != nil {
		t.Fatalf("a file of exactly the limit = %v", err)
	}
	if _, _, err := ReadSnapshot(context.Background(), put(exact), int64(len(exact))-1); !errors.Is(err, ErrSnapshotTooLarge) {
		t.Fatalf("one byte over = %v", err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, FileName), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadSnapshot(context.Background(), dir, 0); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a directory in the file's place = %v", err)
	}
}

// A BIG FILE IS READ IN CHUNKS AND A CANCELLED CONTEXT STOPS IT.
func TestSnapshotLooksAtItsContextBetweenChunks(t *testing.T) {
	dir := t.TempDir()
	body := `{"version":2,"teams":[],"p":"` + strings.Repeat("x", 3*snapshotChunk) + `"}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadSnapshot(context.Background(), dir, 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ReadSnapshot(ctx, dir, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled = %v", err)
	}
}
