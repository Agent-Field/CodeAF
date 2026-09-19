package wsdiscover

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestReplayDoesNotDuplicateAfterCrash(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "discovery.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	embed := &FakeEmbedder{}
	first := []Record{
		rec("chat-a", 1, 1, "authenticate receipt links"),
		rec("chat-a", 1, 2, "reject mailing raw URLs"),
	}
	if err := s.Ingest(ctx, first, embed); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	replay := append(append([]Record{}, first...), rec("chat-a", 1, 3, "keep the rejection"))
	if err := s.Ingest(ctx, replay, embed); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Passages(ctx, "chat-a")
	if err != nil || len(rows) != 3 {
		t.Fatalf("replay duplicated or dropped rows: %d %v", len(rows), err)
	}
	head, err := s.Head(ctx, "chat-a")
	if err != nil || head.Ordinal != 3 || head.Generation != 1 {
		t.Fatalf("head after replay %+v %v", head, err)
	}
}

func TestSamePositionDifferentHashReplacesDerivedRow(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	embed := &FakeEmbedder{}
	if err := s.Ingest(ctx, []Record{rec("chat-a", 1, 1, "old wording")}, embed); err != nil {
		t.Fatal(err)
	}
	if err := s.Ingest(ctx, []Record{rec("chat-a", 1, 1, "rewritten wording")}, embed); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Passages(ctx, "chat-a")
	if err != nil || len(rows) != 1 || rows[0].Text != "rewritten wording" {
		t.Fatalf("rewrite left stale derived rows: %+v %v", rows, err)
	}
	hits, _ := s.SearchLexical(ctx, "old", 5)
	if len(hits) != 0 {
		t.Fatalf("rewritten text still searchable: %+v", hits)
	}
}

func TestRewindInvalidatesThatGeneration(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	embed := &FakeEmbedder{}
	if err := s.Ingest(ctx, []Record{
		rec("chat-a", 1, 1, "first generation authenticate"),
		rec("chat-a", 2, 1, "rewound generation espresso"),
	}, embed); err != nil {
		t.Fatal(err)
	}
	if err := s.Rewind(ctx, "chat-a", 1); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Passages(ctx, "chat-a")
	if err != nil || len(rows) != 1 || rows[0].Generation != 2 {
		t.Fatalf("rewind left generation 1: %+v %v", rows, err)
	}
	hits, _ := s.SearchLexical(ctx, "authenticate", 5)
	if len(hits) != 0 {
		t.Fatalf("rewound generation still searchable: %+v", hits)
	}
	hits, _ = s.SearchLexical(ctx, "espresso", 5)
	if len(hits) != 1 {
		t.Fatalf("later generation should remain: %+v", hits)
	}
}

func TestDeleteSourceInvalidatesDerivedRows(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	embed := &FakeEmbedder{}
	if err := s.Ingest(ctx, []Record{rec("chat-a", 1, 1, "authenticate receipt links")}, embed); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSource(ctx, "chat-a"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Passages(ctx, "chat-a")
	if err != nil || len(rows) != 0 {
		t.Fatalf("deleted source still has passages: %+v %v", rows, err)
	}
	hits, _ := s.SearchLexical(ctx, "authenticate", 5)
	if len(hits) != 0 {
		t.Fatalf("deleted material resurfaced: %+v", hits)
	}
	if _, err := s.Head(ctx, "chat-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted head: %v", err)
	}
	state, _, err := s.Source(ctx, "chat-a")
	if err != nil || state != SourceDeleted {
		t.Fatalf("deleted state %q %v", state, err)
	}
}

func TestUnavailableIsNotDeletedAndNotAbsent(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, _, err := s.Source(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absent: %v", err)
	}
	if err := s.Ingest(ctx, []Record{rec("chat-a", 1, 1, "authenticate receipt links")}, &FakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkUnavailable(ctx, "chat-a"); err != nil {
		t.Fatal(err)
	}
	state, gen, err := s.Source(ctx, "chat-a")
	if err != nil || state != SourceUnavailable || gen != 1 {
		t.Fatalf("unavailable %q gen=%d %v", state, gen, err)
	}
	rows, err := s.Passages(ctx, "chat-a")
	if err != nil || len(rows) != 1 {
		t.Fatalf("unavailable dropped derived rows: %+v %v", rows, err)
	}
	if err := s.DeleteSource(ctx, "chat-a"); err != nil {
		t.Fatal(err)
	}
	state, _, err = s.Source(ctx, "chat-a")
	if err != nil || state != SourceDeleted {
		t.Fatalf("after delete %q %v", state, err)
	}
	rows, _ = s.Passages(ctx, "chat-a")
	if len(rows) != 0 {
		t.Fatalf("deleted still current: %+v", rows)
	}
}

func TestVectorMismatchDoesNotCompare(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.Ingest(ctx, []Record{rec("chat-a", 1, 1, "authenticate receipt links")}, &FakeEmbedder{Model: "alpha", Version: "1", Dim: 4}); err != nil {
		t.Fatal(err)
	}
	query := fakeVector("authenticate receipt links", 4)
	hits, err := s.SearchSimilar(ctx, query, "beta", "1", 4, 5)
	if err != nil || len(hits) != 0 {
		t.Fatalf("model mismatch compared: %+v %v", hits, err)
	}
	hits, err = s.SearchSimilar(ctx, query, "alpha", "2", 4, 5)
	if err != nil || len(hits) != 0 {
		t.Fatalf("version mismatch compared: %+v %v", hits, err)
	}
	other := fakeVector("authenticate receipt links", 8)
	hits, err = s.SearchSimilar(ctx, other, "alpha", "1", 8, 5)
	if err != nil || len(hits) != 0 {
		t.Fatalf("dimension mismatch compared: %+v %v", hits, err)
	}
	hits, err = s.SearchSimilar(ctx, query, "alpha", "1", 4, 5)
	if err != nil || len(hits) != 1 || hits[0].SessionID != "chat-a" {
		t.Fatalf("matching space: %+v %v", hits, err)
	}
}

func TestRebuildRecreatesFTSAndVectors(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.Ingest(ctx, []Record{rec("chat-a", 1, 1, "authenticate receipt links")}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Rebuild(ctx, &FakeEmbedder{Model: "rebuilt", Version: "2", Dim: 4}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Passages(ctx, "chat-a")
	if err != nil || len(rows) != 1 || rows[0].Model != "rebuilt" || len(rows[0].Vector) != 4 {
		t.Fatalf("rebuild vectors: %+v %v", rows, err)
	}
	hits, err := s.SearchLexical(ctx, "authenticate", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("rebuild FTS: %+v %v", hits, err)
	}
}

func TestResetThenIngestRebuildsFromJournals(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	journal := []Record{rec("chat-a", 1, 1, "authenticate receipt links")}
	if err := s.Ingest(ctx, journal, &FakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	prog, _ := s.Progress(ctx)
	if prog.Passages != 0 || prog.Vectors != 0 {
		t.Fatalf("reset left derived rows %+v", prog)
	}
	if err := s.Ingest(ctx, journal, &FakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchLexical(ctx, "authenticate", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("rebuild from journal: %+v %v", hits, err)
	}
}

func TestSchemaHasNoByteOffsetColumn(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.Ingest(ctx, []Record{rec("chat-a", 1, 1, "word")}, nil); err != nil {
		t.Fatal(err)
	}
	rows, err := s.db.Query(`PRAGMA table_info(cursors)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "offset" || name == "byte_offset" || name == "position" {
			t.Fatalf("cursor table stored a byte offset as %s", name)
		}
	}
}

func TestHostileLexicalQueryIsAMiss(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.Ingest(ctx, []Record{rec("chat-a", 1, 1, "authenticate")}, &FakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchLexical(ctx, `"`, 5)
	if err != nil || len(hits) != 0 {
		t.Fatalf("hostile FTS: %+v %v", hits, err)
	}
}
