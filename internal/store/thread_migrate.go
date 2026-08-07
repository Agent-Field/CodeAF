package store

import (
	"database/sql"
	"strings"
)

// migrateThreadSchema keeps selectable questions, charter commands, and durable
// media attachments usable when an existing resident database is opened by the
// standing-aware, multimodal build.
func migrateThreadSchema(db *sql.DB) error {
	hasOptions, err := tableHasColumn(db, "messages", "options")
	if err != nil {
		return err
	}
	if !hasOptions {
		if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN options JSON NOT NULL DEFAULT '[]' CHECK (json_valid(options))`); err != nil {
			return err
		}
	}

	hasReflex, err := tableHasColumn(db, "commands", "reflex")
	if err != nil {
		return err
	}
	if !hasReflex {
		if _, err := db.Exec(`ALTER TABLE commands ADD COLUMN reflex INTEGER NOT NULL DEFAULT 0 CHECK (reflex IN (0, 1))`); err != nil {
			return err
		}
	}

	if err := addJSONColumn(db, "messages", "attachments"); err != nil {
		return err
	}
	if err := addJSONColumn(db, "commands", "attachments"); err != nil {
		return err
	}

	var definition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'commands'`).Scan(&definition); err != nil {
		return err
	}
	if !strings.Contains(definition, "kind IN") {
		return nil
	}
	// The rebuild lifts the legacy kind CHECK so charter commands replay. The
	// attachments column was added above, so it must survive the copy.
	_, err = db.Exec(`
		ALTER TABLE commands RENAME TO commands_legacy;
		CREATE TABLE commands (
		    seq         INTEGER PRIMARY KEY REFERENCES events(seq),
		    ts          TEXT NOT NULL,
		    session_id  TEXT NOT NULL DEFAULT '',
		    kind        TEXT NOT NULL,
		    reflex      INTEGER NOT NULL DEFAULT 0 CHECK (reflex IN (0, 1)),
		    target      TEXT NOT NULL DEFAULT '',
		    instruction TEXT NOT NULL,
		    attachments JSON NOT NULL DEFAULT '[]' CHECK (json_valid(attachments)),
		    status      TEXT NOT NULL CHECK (status IN ('pending', 'applied', 'rejected')),
		    result      TEXT NOT NULL DEFAULT '',
		    updated_seq INTEGER NOT NULL
		);
		INSERT INTO commands SELECT seq, ts, session_id, kind, reflex, target,
		    instruction, attachments, status, result, updated_seq FROM commands_legacy;
		DROP TABLE commands_legacy;
		CREATE INDEX commands_status_seq ON commands (status, seq);
	`)
	return err
}

func addJSONColumn(db *sql.DB, table, column string) error {
	found, err := tableHasColumn(db, table, column)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` JSON NOT NULL DEFAULT '[]' CHECK (json_valid(` + column + `))`)
	return err
}

func tableHasColumn(db *sql.DB, table, wanted string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == wanted {
			return true, nil
		}
	}
	return false, rows.Err()
}
