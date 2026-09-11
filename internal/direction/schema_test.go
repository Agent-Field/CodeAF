package direction

import (
	"context"
	"database/sql"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// THE DIRECTION TABLES HOLD DIRECTION AND NOTHING ELSE (design §5). Execution
// state, grants, attention and payloads have their own owners; a column for
// any of them here is the start of a second owner. So the column set is
// checked in, and adding a column means editing this list — with the reason on
// the line — in the change that adds it.
var directionColumns = map[string][]string{
	"direction_records": {"seq", "id", "revision", "created_at"},
	"direction_revisions": {"seq", "record_id", "revision", "kind", "state", "state_reason", "title", "text",
		"text_sha256", "quote", "quote_origin", "source_class", "source_id", "source_session", "source_hint",
		"source_sha256", "author_class", "author_ref", "receipt_actor", "receipt_door", "receipt_ref",
		"receipt_at", "written_at"},
	"direction_targets":     {"record_id", "revision", "position", "target_kind", "ref_id", "session_id", "reach"},
	"direction_exclusions":  {"record_id", "revision", "target_kind", "ref_id", "session_id", "at"},
	"direction_links":       {"record_id", "revision", "link_kind", "to_ref", "to_revision"},
	"direction_legacy":      {"source_store", "source_id", "source_version", "source_sha256", "record_id", "revision", "import_run"},
	"direction_import_runs": {"id", "mode", "binary", "report_sha256", "started_at", "finished_at", "counts"},
	"direction_live": {"target_kind", "ref_id", "session_id", "lane", "record_id", "revision", "reach",
		"has_exclusions", "has_links", "written_at"},
}

func TestTheDirectionTablesHoldOnlyTheDesignedColumns(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	var tables []string
	err := s.ws.ReadSnapshot(ctx, func(tx *sql.Tx) error {
		names, err := collect(ctx, tx, "SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'direction%' ORDER BY name", nil,
			func(row scanner) (string, error) { var n string; return n, row.Scan(&n) })
		tables = names
		if err != nil {
			return err
		}
		for _, table := range names {
			cols, err := collect(ctx, tx, "SELECT name FROM pragma_table_info(?) ORDER BY cid", []any{table},
				func(row scanner) (string, error) { var n string; return n, row.Scan(&n) })
			if err != nil {
				return err
			}
			if want, ok := directionColumns[table]; !ok || !reflect.DeepEqual(cols, want) {
				t.Errorf("%s has columns %v; the checked-in set is %v", table, cols, want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != len(directionColumns) {
		t.Fatalf("direction tables %v, checked in %d", tables, len(directionColumns))
	}
}

// This package reaches the organization store and the standard library, and
// nothing that runs work: not standing's runtime, not the session, not the
// memory store (design §5).
func TestTheDirectionPackageImportsOnlyTheOrganizationStore(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if strings.Contains(path, ".") && path != workspaceImport {
				t.Errorf("%s imports %s", name, path)
			}
		}
	}
}

// OpenExisting never creates a store, like the workspace door of that name.
func TestOpenExistingDoesNotCreateAStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent", "collections.db")
	if _, err := OpenExisting(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("opening an absent store: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a read created the directory")
	}
}
