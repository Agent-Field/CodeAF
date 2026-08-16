package projectors

import (
	"context"
	"database/sql"
	"io"
	"reflect"
	"testing"
)

func TestApplySchemaExactTablesColumnsAndIndexes(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:", BusyRetryOptions{Log: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE project (id text PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}

	wantColumns := map[string][]string{
		"session": {
			"id", "project_id", "workspace_id", "parent_id", "slug", "directory", "path",
			"title", "version", "share_url", "summary_additions", "summary_deletions",
			"summary_files", "summary_diffs", "revert", "permission", "agent", "model",
			"time_created", "time_updated", "time_compacting", "time_archived",
		},
		"message":         {"id", "session_id", "time_created", "time_updated", "data"},
		"part":            {"id", "message_id", "session_id", "time_created", "time_updated", "data"},
		"todo":            {"session_id", "content", "status", "priority", "position", "time_created", "time_updated"},
		"session_message": {"id", "session_id", "type", "time_created", "time_updated", "data"},
		"permission":      {"project_id", "time_created", "time_updated", "data"},
	}
	for table, want := range wantColumns {
		if got := tableColumns(t, db, table); !reflect.DeepEqual(got, want) {
			t.Errorf("%s columns:\n%v\nwant:\n%v", table, got, want)
		}
	}

	wantIndexes := []string{
		"message_session_time_created_id_idx",
		"part_message_id_id_idx",
		"part_session_idx",
		"session_message_session_idx",
		"session_message_session_type_idx",
		"session_message_time_created_idx",
		"session_parent_idx",
		"session_project_idx",
		"session_workspace_idx",
		"todo_session_idx",
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master
		WHERE type = 'index' AND name NOT LIKE 'sqlite_autoindex_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	gotIndexes := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		gotIndexes = append(gotIndexes, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotIndexes, wantIndexes) {
		t.Fatalf("indexes:\n%v\nwant:\n%v", gotIndexes, wantIndexes)
	}
}

func TestSchemaForeignKeyCascades(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:", BusyRetryOptions{Log: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE project (id text PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO project VALUES ('p1')`,
		`INSERT INTO session
			(id, project_id, slug, directory, title, version, time_created, time_updated)
			VALUES ('s1', 'p1', 'slug', '/tmp', 'title', 'v', 1, 1)`,
		`INSERT INTO message VALUES ('m1', 's1', 1, 1, '{}')`,
		`INSERT INTO part VALUES ('pt1', 'm1', 's1', 1, 1, '{}')`,
		`INSERT INTO todo VALUES ('s1', 'x', 'pending', 'high', 0, 1, 1)`,
		`INSERT INTO session_message VALUES ('e1', 's1', 'user', 1, 1, '{}')`,
		`INSERT INTO permission VALUES ('p1', 1, 1, '{}')`,
		`DELETE FROM session WHERE id = 's1'`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	for _, table := range []string{"message", "part", "todo", "session_message"} {
		if got := rowCount(t, db, table); got != 0 {
			t.Errorf("%s rows after session delete = %d", table, got)
		}
	}
	if got := rowCount(t, db, "permission"); got != 1 {
		t.Fatalf("permission rows after session delete = %d", got)
	}
	if _, err := db.Exec(`DELETE FROM project WHERE id = 'p1'`); err != nil {
		t.Fatal(err)
	}
	if got := rowCount(t, db, "permission"); got != 0 {
		t.Fatalf("permission rows after project delete = %d", got)
	}
}

func tableColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func rowCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
