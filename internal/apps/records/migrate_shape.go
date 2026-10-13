package records

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// ShapeAliases reads the annotation without widening the wave-1 file types.
func ShapeAliases(files spec.Files) (map[string]map[string]string, error) {
	var app struct {
		Records map[string]struct {
			Fields map[string]struct{ Alias string }
		}
	}
	if err := json.Unmarshal(files[spec.AppFile], &app); err != nil {
		return nil, err
	}
	aliases := map[string]map[string]string{}
	for typ, def := range app.Records {
		aliases[typ] = map[string]string{}
		for field, f := range def.Fields {
			if f.Alias == "" {
				continue
			}
			if !query.ValidName(f.Alias) || !query.ValidName(field) || f.Alias == field {
				return nil, invalid("invalid field alias")
			}
			if _, exists := def.Fields[f.Alias]; exists {
				return nil, invalid("alias names a live field")
			}
			if _, exists := aliases[typ][f.Alias]; exists {
				return nil, invalid("alias has two targets")
			}
			aliases[typ][f.Alias] = field
		}
	}
	return aliases, nil
}

func shapeDB(path string, readOnly bool) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("_pragma", "busy_timeout(5000)")
	if readOnly {
		q.Set("mode", "ro")
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
}

// ShapeSnapshot reads every real row, including hidden values, in one snapshot.
func ShapeSnapshot(ctx context.Context, path string) (map[string][]bridge.Record, error) {
	result := map[string][]bridge.Record{}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return result, nil
	} else if err != nil {
		return nil, err
	}
	db, err := shapeDB(path, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tables, err := shapeTables(ctx, tx)
	if err != nil {
		return nil, err
	}
	for _, name := range tables {
		rows, err := tx.QueryContext(ctx, "SELECT record FROM "+name)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var data []byte
			if err := rows.Scan(&data); err != nil {
				rows.Close()
				return nil, err
			}
			var row bridge.Record
			if err := json.Unmarshal(data, &row); err != nil {
				rows.Close()
				return nil, err
			}
			result[row.Type] = append(result[row.Type], row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return result, tx.Commit()
}

func shapeTables(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND (name GLOB 'records_*' OR name GLOB 'shape_records_*') ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		typ := strings.TrimPrefix(strings.TrimPrefix(name, "shape_"), "records_")
		table, err := query.Table(typ)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(name, "shape_") {
			table = "\"" + name + "\""
		}
		names = append(names, table)
	}
	return names, rows.Err()
}

// WithShapeMigration keeps real writers out between the dry run, version
// commit and migration. VACUUM INTO includes committed WAL pages in the copy.
func (s *Service) WithShapeMigration(ctx context.Context, scope svc.Scope, files spec.Files, commit func() error) error {
	scope.Data = svc.DataReal
	lock := s.writer(scope)
	lock.Lock()
	defer lock.Unlock()
	path := filepath.Join(s.cfg.Root, scope.App, "records.db")
	db, err := shapeDB(path, false)
	if err != nil {
		return err
	}
	defer db.Close()
	dir, err := os.MkdirTemp(filepath.Dir(path), ".migration-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	copyPath := filepath.Join(dir, "records.db")
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", copyPath); err != nil {
		return err
	}
	copyDB, err := shapeDB(copyPath, false)
	if err != nil {
		return err
	}
	err = migrateShape(ctx, copyDB, files)
	closeErr := copyDB.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := commit(); err != nil {
		return err
	}
	return migrateShape(context.WithoutCancel(ctx), db, files)
}

func migrateShape(ctx context.Context, db *sql.DB, files spec.Files) error {
	b, err := spec.Decode(files)
	if err != nil {
		return err
	}
	aliases, err := ShapeAliases(files)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS _schema (id INTEGER PRIMARY KEY CHECK(id=1), definition TEXT NOT NULL)"); err != nil {
		return err
	}
	for _, typ := range sorted(b.App.Records) {
		table, err := query.Table(typ)
		if err != nil {
			return err
		}
		var backed bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)", "shape_records_"+typ).Scan(&backed); err != nil {
			return err
		}
		if backed {
			table = shapeTable(typ)
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)", strings.Trim(table, "\"")).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if err := createTable(ctx, tx, typ, b.App.Records[typ]); err != nil {
				return err
			}
		}
		for _, old := range sorted(aliases[typ]) {
			target := aliases[typ][old]
			sourcePath, targetPath := fieldPath(old), fieldPath(target)
			// Populate the new key without removing its original value or audit history.
			if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET record=json_set(record, ?, json_extract(record, ?)) WHERE json_type(record, ?) IS NULL AND json_type(record, ?) IS NOT NULL", targetPath, sourcePath, targetPath, sourcePath); err != nil {
				return err
			}
		}
		if err := shapeIndexes(ctx, tx, table, typ, b.App.Records[typ]); err != nil {
			return err
		}
		if !backed {
			if _, err := tx.ExecContext(ctx, "ALTER TABLE "+table+" RENAME TO "+shapeTable(typ)); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS _history (seq INTEGER PRIMARY KEY, record_type TEXT NOT NULL, id TEXT NOT NULL, op TEXT NOT NULL, by TEXT NOT NULL, at TEXT NOT NULL, before TEXT, after TEXT)"); err != nil {
		return err
	}
	data, err := json.Marshal(b.App.Records)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO _schema(id,definition) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET definition=excluded.definition", string(data)); err != nil {
		return err
	}
	return tx.Commit()
}

func fieldPath(field string) string { return "$.fields.\"" + field + "\"" }

func shapeIndexes(ctx context.Context, tx *sql.Tx, table, typ string, def *spec.RecordType) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_xinfo("+table+")")
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk, hidden int
		var name, kind string
		var dflt any
		if err := rows.Scan(&cid, &name, &kind, &notnull, &dflt, &pk, &hidden); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, field := range sorted(def.Fields) {
		f := def.Fields[field]
		if !query.Indexed(f) {
			continue
		}
		if !columns["field_"+field] {
			if _, err := tx.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+query.Column(field)+" GENERATED ALWAYS AS (json_extract(record, '"+fieldPath(field)+"')) VIRTUAL"); err != nil {
				return err
			}
		}
		index := "\"index_" + typ + "_" + field + "\""
		if _, err := tx.ExecContext(ctx, "DROP INDEX IF EXISTS "+index); err != nil {
			return err
		}
		unique := ""
		if f.Unique {
			unique = "UNIQUE "
		}
		if _, err := tx.ExecContext(ctx, "CREATE "+unique+"INDEX "+index+" ON "+table+" ("+query.Column(field)+")"); err != nil {
			return err
		}
	}
	return nil
}

// ensureShapeSchema is the single runtime hook. Temporary views expose the
// current shape while their write triggers preserve hidden stored fields.
func ensureShapeSchema(ctx context.Context, c *connection, scope svc.Scope, dir string) error {
	if err := ensureSchema(ctx, c.db, c.def, scope, dir); err != nil {
		return err
	}
	aliases, err := ShapeAliases(c.def.Files)
	if err != nil {
		return err
	}
	for _, typ := range sorted(c.def.App.Records) {
		var backed bool
		if err := c.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)", "shape_records_"+typ).Scan(&backed); err != nil {
			return err
		}
		if !backed {
			continue
		}
		def := c.def.App.Records[typ]
		for old, target := range aliases[typ] {
			field := *def.Fields[target]
			field.Required, field.Unique, field.Index = false, false, false
			field.Computed = ""
			def.Fields[old] = &field
		}
		if err := shapeView(ctx, c.db, typ, def, aliases[typ], scope.Caller.Member); err != nil {
			return err
		}
	}
	return nil
}

func shapeView(ctx context.Context, db *sql.DB, typ string, def *spec.RecordType, aliases map[string]string, member string) error {
	table, err := query.Table(typ)
	if err != nil {
		return err
	}
	projection := "'{}'"
	for _, field := range sorted(def.Fields) {
		source := field
		if target := aliases[field]; target != "" {
			source = target
		}
		path := fieldPath(source)
		value := "json_extract(record, '" + path + "')"
		if len(def.Fields[field].Default) > 0 {
			data := string(def.Fields[field].Default)
			if data == "\"$me\"" {
				encoded, _ := json.Marshal(member)
				data = string(encoded)
			}
			data = strings.ReplaceAll(data, "'", "''")
			value = "COALESCE(" + value + ", json_extract('" + data + "', '$'))"
		}
		projection = "json_set(" + projection + ", '$.\"" + field + "\"', " + value + ")"
	}
	expression := "json_set(record, '$.fields', json(" + projection + "))"
	columns := []string{"id", expression + " AS record"}
	for _, field := range sorted(def.Fields) {
		if query.Indexed(def.Fields[field]) {
			source := field
			if target := aliases[field]; target != "" {
				source = target
			}
			columns = append(columns, "json_extract(record, '"+fieldPath(source)+"') AS "+query.Column(field))
		}
	}
	if _, err := db.ExecContext(ctx, "CREATE TEMP VIEW "+table+" AS SELECT "+strings.Join(columns, ",")+" FROM main."+shapeTable(typ)); err != nil {
		return err
	}
	fields := "json_extract(NEW.record, '$.fields')"
	for _, old := range sorted(aliases) {
		target := aliases[old]
		oldValue := "json_extract(NEW.record, '" + fieldPath(old) + "')"
		newValue := "json_extract(NEW.record, '" + fieldPath(target) + "')"
		value := "CASE WHEN " + oldValue + " IS NOT json_extract(OLD.record, '" + fieldPath(old) + "') THEN " + oldValue + " ELSE " + newValue + " END"
		fields = "json_set(" + fields + ", '$.\"" + old + "\"', " + value + ", '$.\"" + target + "\"', " + value + ")"
	}
	// json_patch would erase explicit nulls; copy every exposed key with json_set.
	merged := "json_extract(record, '$.fields')"
	for _, field := range sorted(def.Fields) {
		merged = "json_set(" + merged + ", '$.\"" + field + "\"', json_extract(" + fields + ", '$.\"" + field + "\"'))"
	}
	update := "UPDATE " + shapeTable(typ) + " SET record=json_set(NEW.record, '$.fields', json(" + merged + ")) WHERE id=OLD.id;"
	insertRecord := "NEW.record"
	for _, old := range sorted(aliases) {
		target := aliases[old]
		value := "COALESCE(json_extract(NEW.record, '" + fieldPath(target) + "'), json_extract(NEW.record, '" + fieldPath(old) + "'))"
		insertRecord = "json_set(" + insertRecord + ", '" + fieldPath(old) + "', " + value + ", '" + fieldPath(target) + "', " + value + ")"
	}
	insert := "INSERT INTO " + shapeTable(typ) + "(id,record) VALUES(NEW.id," + insertRecord + ");"
	remove := "DELETE FROM " + shapeTable(typ) + " WHERE id=OLD.id;"
	for _, trigger := range []struct{ verb, body string }{{"UPDATE", update}, {"INSERT", insert}, {"DELETE", remove}} {
		statement := fmt.Sprintf("CREATE TEMP TRIGGER \"shape_%s_%s\" INSTEAD OF %s ON %s BEGIN %s END", typ, trigger.verb, trigger.verb, table, trigger.body)
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// ShapeCounts counts real stored values with SQL rather than preview samples.
func ShapeCounts(ctx context.Context, path, typ, field string) (int, error) {
	table, err := query.Table(typ)
	if err != nil || (field != "" && !query.ValidName(field)) {
		return 0, invalid("invalid migration field")
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	db, err := shapeDB(path, true)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var exists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)", "records_"+typ).Scan(&exists); err != nil {
		return 0, err
	}
	if !exists {
		if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)", "shape_records_"+typ).Scan(&exists); err != nil {
			return 0, err
		}
		if !exists {
			return 0, nil
		}
		table = shapeTable(typ)
	}
	statement := "SELECT COUNT(*) FROM " + table
	args := []any{}
	if field != "" {
		statement += " WHERE json_type(record, ?) IS NOT NULL AND json_type(record, ?) != 'null'"
		args = append(args, fieldPath(field), fieldPath(field))
	}
	var count int
	err = db.QueryRowContext(ctx, statement, args...).Scan(&count)
	return count, err
}

func shapeTable(typ string) string { return "\"shape_records_" + typ + "\"" }
