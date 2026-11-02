package directory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
)

func retainedRows(t *testing.T, db *sql.DB) []string {
	t.Helper()
	var result []string
	for _, table := range []string{"identity", "devices", "cells", "version"} {
		column := "hex(rec)"
		if table == "version" {
			column = "quote(n)"
		}
		rows, err := db.Query(`SELECT quote(id),` + column + ` FROM ` + table + ` ORDER BY id`)
		legacyMust(t, err)
		for rows.Next() {
			var id, value string
			legacyMust(t, rows.Scan(&id, &value))
			result = append(result, table+":"+id+":"+value)
		}
		legacyMust(t, rows.Err())
		legacyMust(t, rows.Close())
	}
	return result
}

func TestR6_1FormerSQLCannotChangeFrozenSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.db")
	s, _ := legacySeed(t, path)
	defer s.Close()
	old, err := sql.Open("sqlite", path)
	legacyMust(t, err)
	defer old.Close()
	original := retainedRows(t, old)
	legacyMust(t, s.FreezeLegacy(context.Background(), legacyConfig()))
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT OR IGNORE INTO identity (id, rec) VALUES (1, ?)`, []any{`{"V":1}`}},
		{`INSERT OR IGNORE INTO version (id, n) VALUES (1, 0)`, nil},
		{`UPDATE version SET n = n + 1 WHERE id = 1`, nil},
	}
	for _, row := range []struct{ table, id string }{{"identity", "1"}, {"devices", "b"}, {"cells", "chat"}, {"devices", "new"}, {"cells", "new"}} {
		statements = append(statements, struct {
			query string
			args  []any
		}{`INSERT INTO ` + row.table + ` (id, rec) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET rec = excluded.rec`, []any{row.id, `{"V":1}`}})
	}
	// The former writer has no DELETE statement, but all retained SQL operations
	// must refuse changes, including writes through an already open connection.
	for _, table := range []string{"identity", "devices", "cells", "version"} {
		column := "rec = '{}'"
		if table == "version" {
			column = "n = 99"
		}
		statements = append(statements, struct {
			query string
			args  []any
		}{`UPDATE ` + table + ` SET ` + column, nil}, struct {
			query string
			args  []any
		}{`DELETE FROM ` + table, nil})
	}
	for i, statement := range statements {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			_, err := old.Exec(statement.query, statement.args...)
			if err == nil || !strings.Contains(err.Error(), "directory upgraded") {
				t.Errorf("former write must report the upgrade: %s: %v", statement.query, err)
			}
			if got := retainedRows(t, old); !reflect.DeepEqual(got, original) {
				t.Errorf("retained source changed after %s", statement.query)
			}
		})
	}
}

func TestR6_3SourceCrashCommitsGuardWithFreeze(t *testing.T) {
	for _, phase := range []string{"after-freeze-gate", "before-commit", "after-commit"} {
		t.Run(phase, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source.db")
			s, _ := legacySeed(t, path)
			legacyMust(t, s.Close())
			cmd := exec.Command(os.Args[0], "-test.run=^TestR24SourceCrashHelper$", "-test.count=1")
			cmd.Env = append(os.Environ(), "P11_SOURCE="+path, "P11_STAGE=freeze", "P11_PHASE="+phase)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 81 {
				t.Fatalf("cut: %v %s", err, out)
			}
			db, err := sql.Open("sqlite", path)
			legacyMust(t, err)
			defer db.Close()
			var gates, guards int
			legacyMust(t, db.QueryRow(`SELECT count(*) FROM legacy_gate`).Scan(&gates))
			legacyMust(t, db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND tbl_name IN ('identity','devices','cells','version')`).Scan(&guards))
			_, err = db.Exec(`UPDATE version SET n = n + 1 WHERE id = 1`)
			if phase == "after-commit" {
				if gates != 1 || guards != 12 || err == nil {
					t.Fatalf("committed freeze lost guards: gates=%d guards=%d write=%v", gates, guards, err)
				}
			} else if gates != 0 || guards != 0 || err != nil {
				t.Fatalf("uncommitted freeze blocked former writer: gates=%d guards=%d write=%v", gates, guards, err)
			}
		})
	}
}

func TestR6_4FrozenSourceReopensForInventoryAndResume(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "source.db")
	s, _ := legacySeed(t, path)
	original := retainedRows(t, s.db)
	legacyMust(t, s.FreezeLegacy(ctx, legacyConfig()))
	if _, err := s.db.Exec(`UPDATE version SET n = n + 1 WHERE id = 1`); err == nil {
		t.Error("frozen source permits a second writer")
	}
	legacyMust(t, s.Close())
	s, err := OpenSQLite(path, time.Now)
	legacyMust(t, err)
	defer s.Close()
	legacyMust(t, s.FreezeLegacy(ctx, legacyConfig()))
	manifest, err := s.InventoryLegacy(ctx)
	legacyMust(t, err)
	_, err = s.ExportLegacy(ctx, func(_ clusterstate.RecordWrite) error { return nil })
	legacyMust(t, err)
	legacyMust(t, s.OwnLegacy(ctx, []LegacyReceipt{{Node: 1, Manifest: manifest, Applied: 2}, {Node: 2, Manifest: manifest, Applied: 2}}))
	again, err := s.InventoryLegacy(ctx)
	legacyMust(t, err)
	if !reflect.DeepEqual(manifest, again) || !reflect.DeepEqual(original, retainedRows(t, s.db)) {
		t.Fatal("resume changed the retained source or inventory")
	}
}
