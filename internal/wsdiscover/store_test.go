package wsdiscover

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "v3", "discovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestDefaultPathIsHomeV3Discovery(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	got := DefaultPath()
	want := home.Join("v3", "discovery.db")
	if got != want || !strings.HasSuffix(got, filepath.Join("v3", "discovery.db")) {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestCursorIdentityIsNotAByteOffset(t *testing.T) {
	typ := reflect.TypeOf(Cursor{})
	if _, ok := typ.FieldByName("Offset"); ok {
		t.Fatal("Cursor must not carry a byte offset; identity is session+generation+ordinal+hash")
	}
	if _, ok := typ.FieldByName("ByteOffset"); ok {
		t.Fatal("Cursor must not carry a byte offset field")
	}
	for _, name := range []string{"SessionID", "Generation", "Ordinal", "ContentHash"} {
		if _, ok := typ.FieldByName(name); !ok {
			t.Fatalf("Cursor is missing %s", name)
		}
	}
}

func TestOpenRefusesAForeignDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collections.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA application_id=1094796108`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version=2`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err := Open(path); !errors.Is(err, ErrForeign) {
		t.Fatalf("foreign open: %v", err)
	}
}

func TestOpenRefusesAnEmptyPath(t *testing.T) {
	if _, err := Open(""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty path: %v", err)
	}
}

func TestIngestRefusesAZeroOrdinal(t *testing.T) {
	s := openTestStore(t)
	err := s.Ingest(context.Background(), []Record{{
		SessionID: "chat-a", Generation: 1, Ordinal: 0, Text: "offset-shaped",
	}}, nil)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("ordinal 0 must not be a cursor: %v", err)
	}
}

func rec(session string, gen int, ord int64, text string) Record {
	return Record{SessionID: session, SourceRef: "src:" + session, Speaker: "user", Text: text, Generation: gen, Ordinal: ord}
}

func TestIngestIndexesPassagesAndLexicalSearch(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	embed := &FakeEmbedder{Model: "fake", Version: "v1", Dim: 4}
	if err := s.Ingest(ctx, []Record{
		rec("chat-a", 1, 1, "customers must authenticate receipt links"),
		rec("chat-a", 1, 2, "mailing raw URLs was rejected"),
	}, embed); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchLexical(ctx, "authenticate", 10)
	if err != nil || len(hits) != 1 || hits[0].Ordinal != 1 {
		t.Fatalf("lexical %v %v", hits, err)
	}
	if hits[0].ContentHash == "" || hits[0].ContentHash != contentHash("customers must authenticate receipt links") {
		t.Fatalf("content hash %q", hits[0].ContentHash)
	}
	head, err := s.Head(ctx, "chat-a")
	if err != nil || head.SessionID != "chat-a" || head.Generation != 1 || head.Ordinal != 2 || head.ContentHash == "" {
		t.Fatalf("head %+v %v", head, err)
	}
	prog, err := s.Progress(ctx)
	if err != nil || prog.Passages != 2 || prog.Vectors != 2 || prog.Delayed || prog.Detail != "" {
		t.Fatalf("progress %+v %v", prog, err)
	}
}

func TestNilEmbedderStoresPassagesAndMarksDelayed(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.Ingest(ctx, []Record{rec("chat-a", 1, 1, "receipt authentication")}, nil); err != nil {
		t.Fatal(err)
	}
	prog, err := s.Progress(ctx)
	if err != nil || prog.Passages != 1 || prog.Vectors != 0 || !prog.Delayed || prog.Detail != delayedDetail {
		t.Fatalf("progress %+v %v", prog, err)
	}
	hits, err := s.SearchLexical(ctx, "authentication", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("lexical still works when delayed: %v %v", hits, err)
	}
	if len(hits[0].Vector) != 0 {
		t.Fatalf("nil embedder stored a vector: %v", hits[0].Vector)
	}
}

func TestDownEmbedderDoesNotStoreEmptyVectorsAsSuccess(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.Ingest(ctx, []Record{rec("chat-a", 1, 1, "receipt authentication")}, &FakeEmbedder{Down: true}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Passages(ctx, "chat-a")
	if err != nil || len(rows) != 1 || len(rows[0].Vector) != 0 || rows[0].Model != "" {
		t.Fatalf("down embedder leaked a vector: %+v %v", rows, err)
	}
	prog, _ := s.Progress(ctx)
	if !prog.Delayed || prog.Detail != "discovery delayed" {
		t.Fatalf("down embedder progress %+v", prog)
	}
}

func TestUnusableVectorsAreRefused(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	bad := &stubEmbedder{vecs: [][]float32{{}}, model: "x", version: "1", dim: 4}
	if err := s.Ingest(ctx, []Record{rec("chat-a", 1, 1, "alpha")}, bad); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Passages(ctx, "chat-a")
	if len(rows) != 1 || len(rows[0].Vector) != 0 {
		t.Fatalf("empty vector success leaked: %+v", rows)
	}
}

type stubEmbedder struct {
	vecs           [][]float32
	model, version string
	dim            int
	err            error
}

func (s *stubEmbedder) Available(context.Context) (string, bool, error) {
	return s.model, true, s.err
}

func (s *stubEmbedder) Embed(context.Context, []string) ([][]float32, string, string, int, error) {
	return s.vecs, s.model, s.version, s.dim, s.err
}
