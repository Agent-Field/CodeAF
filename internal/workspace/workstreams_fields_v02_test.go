package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// WP-F2 (d), Storage: missing proposal columns refuse at Open without repair.
func TestWorkstreamSchemaThreeRequiresGoAheadRecordColumns(t *testing.T) {
	for _, column := range []string{"goahead", "goahead_id"} {
		t.Run(column, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "obsolete.db")
			s := openTestStore(t, path)
			createTestCollection(t, s, "Schema three")
			if userVersion(t, s) != 3 {
				t.Fatal("fixture is not schema 3")
			}
			if _, err := s.db.Exec("ALTER TABLE workstream_records DROP COLUMN " + column); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			opened, err := Open(path)
			if opened != nil {
				_ = opened.Close()
			}
			if err == nil || !strings.Contains(err.Error(), column) {
				t.Fatalf("obsolete schema opened: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(before) != string(after) {
				t.Fatalf("refusal changed file: %v", err)
			}
		})
	}
}
