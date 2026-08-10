package store

import (
	"database/sql"
	"strings"
)

// migrateThreadSchema keeps selectable questions, charter commands, durable
// media attachments, reply-model attribution, and the span a reply answers
// usable when an existing resident database is opened by a newer build.
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
	hasBrief, err := tableHasColumn(db, "messages", "brief")
	if err != nil {
		return err
	}
	if !hasBrief {
		if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN brief JSON NOT NULL DEFAULT 'null' CHECK (json_valid(brief))`); err != nil {
			return err
		}
	}
	hasQuestionSeq, err := tableHasColumn(db, "messages", "question_seq")
	if err != nil {
		return err
	}
	if !hasQuestionSeq {
		if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN question_seq INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	// Answering a durable question checks, inside the write lock, that the
	// question was actually asked. The index lives here rather than beside the
	// table because question_seq itself arrives by migration.
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS messages_question_seq
		ON messages (question_seq, role, seq)`); err != nil {
		return err
	}

	hasAnswers, err := tableHasColumn(db, "messages", "answers_seq")
	if err != nil {
		return err
	}
	if !hasAnswers {
		if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN answers_seq INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}

	hasModel, err := tableHasColumn(db, "messages", "model")
	if err != nil {
		return err
	}
	if !hasModel {
		if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN model TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	hasProgress, err := tableHasColumn(db, "messages", "progress")
	if err != nil {
		return err
	}
	if !hasProgress {
		if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN progress JSON NOT NULL DEFAULT 'null' CHECK (json_valid(progress))`); err != nil {
			return err
		}
	}

	// Typed message parts. The default is 'null' rather than '[]' so an
	// existing row says the true thing — this message was written before parts
	// existed and has none — and so the read path's no-allocation legacy branch
	// matches on the same literal for old rows and new prose-only ones alike.
	hasParts, err := tableHasColumn(db, "messages", "parts")
	if err != nil {
		return err
	}
	if !hasParts {
		if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN parts JSON NOT NULL DEFAULT 'null' CHECK (json_valid(parts))`); err != nil {
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

	hasFresh, err := tableHasColumn(db, "commands", "fresh")
	if err != nil {
		return err
	}
	if !hasFresh {
		if _, err := db.Exec(`ALTER TABLE commands ADD COLUMN fresh INTEGER NOT NULL DEFAULT 0 CHECK (fresh IN (0, 1))`); err != nil {
			return err
		}
	}

	// An existing database's commands were all written by a person, and the
	// empty default is exactly how the issuer axis says so.
	hasIssuer, err := tableHasColumn(db, "commands", "issuer")
	if err != nil {
		return err
	}
	if !hasIssuer {
		if _, err := db.Exec(`ALTER TABLE commands ADD COLUMN issuer TEXT NOT NULL DEFAULT ''`); err != nil {
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
	// attachments and issuer columns were added above, so they must survive the
	// copy.
	_, err = db.Exec(`
		ALTER TABLE commands RENAME TO commands_legacy;
		CREATE TABLE commands (
		    seq         INTEGER PRIMARY KEY REFERENCES events(seq),
		    ts          TEXT NOT NULL,
		    session_id  TEXT NOT NULL DEFAULT '',
		    kind        TEXT NOT NULL,
		    issuer      TEXT NOT NULL DEFAULT '',
		    reflex      INTEGER NOT NULL DEFAULT 0 CHECK (reflex IN (0, 1)),
		    fresh       INTEGER NOT NULL DEFAULT 0 CHECK (fresh IN (0, 1)),
		    target      TEXT NOT NULL DEFAULT '',
		    instruction TEXT NOT NULL,
		    attachments JSON NOT NULL DEFAULT '[]' CHECK (json_valid(attachments)),
		    status      TEXT NOT NULL CHECK (status IN ('pending', 'applied', 'rejected')),
		    result      TEXT NOT NULL DEFAULT '',
		    updated_seq INTEGER NOT NULL
		);
		INSERT INTO commands SELECT seq, ts, session_id, kind, issuer, reflex, fresh, target,
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
