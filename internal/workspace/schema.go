package workspace

import (
	"context"
	"fmt"
)

const minSchemaVersion = 1

// schemaVersion is the latest collections schema this binary writes.
// Issue 1 owns v2 (purpose, provenance, root_state). Later issues bump this
// with their own tables; extra empty tables are not created here.
const schemaVersion = 2

const v2CollectionsDDL = `
CREATE TABLE collections (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL,
 purpose TEXT NOT NULL DEFAULT '',
 lifecycle TEXT NOT NULL DEFAULT 'active',
 revision INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE memberships (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 collection_id TEXT NOT NULL REFERENCES collections(id),
 kind TEXT NOT NULL CHECK(kind IN ('collection','conversation','task','standing','artifact')),
 ref_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 target_collection TEXT REFERENCES collections(id),
 CHECK ((kind='collection' AND target_collection IS NOT NULL AND target_collection=ref_id)
     OR (kind!='collection' AND target_collection IS NULL)),
 UNIQUE(collection_id,kind,ref_id,session_id)
);
CREATE INDEX memberships_reference ON memberships(kind,ref_id,session_id);
`

const v2HistoryDDL = `
CREATE TABLE membership_events (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 collection_id TEXT NOT NULL,
 kind TEXT NOT NULL,
 ref_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 action TEXT NOT NULL CHECK(action IN ('add','remove')),
 origin TEXT NOT NULL CHECK(origin IN ('person','system_fallback','organizer')),
 reason TEXT NOT NULL DEFAULT '',
 actor TEXT NOT NULL DEFAULT '',
 evidence TEXT NOT NULL DEFAULT '',
 at TEXT NOT NULL,
 idempotency_key TEXT NOT NULL DEFAULT ''
);
CREATE INDEX membership_events_edge ON membership_events(collection_id,kind,ref_id,session_id,seq);
CREATE UNIQUE INDEX membership_events_idempotency ON membership_events(idempotency_key) WHERE idempotency_key != '';
CREATE TABLE root_state (
 id INTEGER PRIMARY KEY CHECK(id=1),
 purpose TEXT NOT NULL DEFAULT '',
 revision INTEGER NOT NULL DEFAULT 1,
 updated_at TEXT NOT NULL DEFAULT ''
);
`

func verifyVersionTables(ctx context.Context, q schemaQuerier, version int) error {
	if _, err := q.ExecContext(ctx, "SELECT seq,id,name FROM collections LIMIT 0"); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "SELECT seq,collection_id,kind,ref_id,session_id,target_collection FROM memberships LIMIT 0"); err != nil {
		return err
	}
	if version < 2 {
		return nil
	}
	if _, err := q.ExecContext(ctx, "SELECT purpose,lifecycle,revision,created_at,updated_at FROM collections LIMIT 0"); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "SELECT seq,origin,reason,actor,at FROM membership_events LIMIT 0"); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, "SELECT id,purpose,revision FROM root_state LIMIT 0")
	return err
}

func migrateToCurrent(ctx context.Context, tx schemaQuerier, from int, now string) error {
	if from >= schemaVersion {
		return nil
	}
	if from < 1 {
		return fmt.Errorf("cannot migrate collections schema from version %d", from)
	}
	if from == 1 {
		if err := migrateV1ToV2(ctx, tx, now); err != nil {
			return err
		}
		from = 2
	}
	_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", schemaVersion))
	return err
}

func migrateV1ToV2(ctx context.Context, tx schemaQuerier, now string) error {
	alters := []string{
		"ALTER TABLE collections ADD COLUMN purpose TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE collections ADD COLUMN lifecycle TEXT NOT NULL DEFAULT 'active'",
		"ALTER TABLE collections ADD COLUMN revision INTEGER NOT NULL DEFAULT 1",
		"ALTER TABLE collections ADD COLUMN created_at TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE collections ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''",
	}
	for _, stmt := range alters {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, v2HistoryDDL); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO root_state(id,purpose,revision,updated_at) VALUES (1,'',1,?)", now)
	return err
}

func createV2(ctx context.Context, tx schemaQuerier, now string) error {
	if _, err := tx.ExecContext(ctx, v2CollectionsDDL+v2HistoryDDL); err != nil {
		return err
	}
	// Root is virtual: root_state holds its metadata, and no collections row is minted for it.
	if _, err := tx.ExecContext(ctx, "INSERT INTO root_state(id,purpose,revision,updated_at) VALUES (1,'',1,?)", now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d", applicationID, schemaVersion))
	return err
}

func readPragmas(ctx context.Context, q schemaQuerier) (app, version int, err error) {
	if err = q.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil {
		return 0, 0, err
	}
	err = q.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	return app, version, err
}
