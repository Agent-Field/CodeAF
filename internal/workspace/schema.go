package workspace

import (
	"context"
	"fmt"
)

const minSchemaVersion = 1

// schemaVersion is the latest collections schema this binary writes.
// Wave 1 owns v2 (purpose, provenance, root_state). Wave 2 owns v3
// (guidance, jobs, observations, placement_suppressions, proposed_actions).
// Wave 3 owns v4 (participants, deliveries). Grant and execution tables
// stay absent until Wave 4.
const schemaVersion = 4

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

const v3DDL = `
CREATE TABLE guidance (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 scope_id TEXT NOT NULL DEFAULT '',
 text TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL CHECK(status IN ('active','superseded')),
 origin TEXT NOT NULL CHECK(origin IN ('person','system_fallback','organizer')),
 actor TEXT NOT NULL DEFAULT '',
 source_ref TEXT NOT NULL DEFAULT '',
 supersedes TEXT NOT NULL DEFAULT '',
 revision INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE INDEX guidance_scope ON guidance(scope_id, seq);
CREATE TABLE jobs (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 type TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','leased','completed','deferred','failed','cancelled')),
 owner TEXT NOT NULL DEFAULT '',
 fence TEXT NOT NULL DEFAULT '',
 cause_id TEXT NOT NULL DEFAULT '',
 coalesce_key TEXT NOT NULL DEFAULT '',
 chat_id TEXT NOT NULL DEFAULT '',
 source_rev TEXT NOT NULL DEFAULT '',
 error TEXT NOT NULL DEFAULT '',
 attempt INTEGER NOT NULL DEFAULT 0,
 lease_until TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE INDEX jobs_lease ON jobs(state, type, seq);
CREATE UNIQUE INDEX jobs_coalesce ON jobs(type, coalesce_key) WHERE state IN ('pending','leased') AND coalesce_key != '';
CREATE TABLE observations (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 chat_id TEXT NOT NULL DEFAULT '',
 source_rev TEXT NOT NULL DEFAULT '',
 purpose TEXT NOT NULL DEFAULT '',
 body TEXT NOT NULL DEFAULT '',
 evidence TEXT NOT NULL DEFAULT '',
 model TEXT NOT NULL DEFAULT '',
 prompt_version TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL
);
CREATE TABLE placement_suppressions (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 collection_id TEXT NOT NULL REFERENCES collections(id),
 kind TEXT NOT NULL,
 ref_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 evidence_hash TEXT NOT NULL,
 actor TEXT NOT NULL DEFAULT '',
 at TEXT NOT NULL,
 UNIQUE(collection_id, kind, ref_id, session_id, evidence_hash)
);
CREATE INDEX suppressions_edge ON placement_suppressions(collection_id, kind, ref_id, session_id);
CREATE TABLE proposed_actions (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 chat_id TEXT NOT NULL DEFAULT '',
 source_rev TEXT NOT NULL DEFAULT '',
 plan_json TEXT NOT NULL DEFAULT '',
 result TEXT NOT NULL DEFAULT '',
 idempotency_key TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX proposed_actions_idempotency ON proposed_actions(idempotency_key) WHERE idempotency_key != '';
`

const v4DDL = `
CREATE TABLE participants (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 discussion_id TEXT NOT NULL,
 actor_id TEXT NOT NULL UNIQUE,
 kind TEXT NOT NULL CHECK(kind IN ('chat','role','folder')),
 role TEXT NOT NULL DEFAULT '',
 source_chat_id TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL CHECK(status IN ('active','paused','archived')),
 scope_kind TEXT NOT NULL DEFAULT '',
 folder_id TEXT NOT NULL DEFAULT '',
 snapshot_json TEXT NOT NULL DEFAULT '',
 origin TEXT NOT NULL CHECK(origin IN ('person','system_fallback','organizer','agent')),
 actor TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE INDEX participants_discussion ON participants(discussion_id, seq);
CREATE UNIQUE INDEX participants_source ON participants(discussion_id, source_chat_id) WHERE source_chat_id != '' AND status='active';
CREATE TABLE deliveries (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 cause_id TEXT NOT NULL DEFAULT '',
 from_chat_id TEXT NOT NULL DEFAULT '',
 to_chat_id TEXT NOT NULL,
 pattern TEXT NOT NULL CHECK(pattern IN ('direct','fan-out','discussion')),
 state TEXT NOT NULL CHECK(state IN ('pending','accepted','recorded','processed')),
 body TEXT NOT NULL DEFAULT '',
 origin TEXT NOT NULL CHECK(origin IN ('person','system_fallback','organizer','agent')),
 actor_id TEXT NOT NULL DEFAULT '',
 discussion_id TEXT NOT NULL DEFAULT '',
 idempotency_key TEXT NOT NULL DEFAULT '',
 attempt INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL,
 accepted_at TEXT NOT NULL DEFAULT '',
 recorded_at TEXT NOT NULL DEFAULT '',
 processed_at TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL
);
CREATE INDEX deliveries_cause ON deliveries(cause_id, seq);
CREATE INDEX deliveries_pending ON deliveries(to_chat_id, state, seq);
CREATE UNIQUE INDEX deliveries_idempotency ON deliveries(idempotency_key) WHERE idempotency_key != '';
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
	if _, err := q.ExecContext(ctx, "SELECT id,purpose,revision FROM root_state LIMIT 0"); err != nil {
		return err
	}
	if version < 3 {
		return nil
	}
	if _, err := q.ExecContext(ctx, "SELECT id,scope_id,text,status FROM guidance LIMIT 0"); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "SELECT id,type,state,owner,fence FROM jobs LIMIT 0"); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "SELECT id,chat_id,body FROM observations LIMIT 0"); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "SELECT collection_id,kind,ref_id,evidence_hash FROM placement_suppressions LIMIT 0"); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "SELECT id,plan_json FROM proposed_actions LIMIT 0"); err != nil {
		return err
	}
	if version < 4 {
		return nil
	}
	if _, err := q.ExecContext(ctx, "SELECT id,actor_id,discussion_id,kind,scope_kind FROM participants LIMIT 0"); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, "SELECT id,cause_id,state,accepted_at,recorded_at,processed_at FROM deliveries LIMIT 0")
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
	if from == 2 {
		if err := migrateV2ToV3(ctx, tx); err != nil {
			return err
		}
		from = 3
	}
	if from == 3 {
		if err := migrateV3ToV4(ctx, tx); err != nil {
			return err
		}
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

func migrateV2ToV3(ctx context.Context, tx schemaQuerier) error {
	_, err := tx.ExecContext(ctx, v3DDL)
	return err
}

func migrateV3ToV4(ctx context.Context, tx schemaQuerier) error {
	_, err := tx.ExecContext(ctx, v4DDL)
	return err
}

func createV2(ctx context.Context, tx schemaQuerier, now string) error {
	if _, err := tx.ExecContext(ctx, v2CollectionsDDL+v2HistoryDDL); err != nil {
		return err
	}
	// Root is virtual: root_state holds its metadata, and no collections row is minted for it.
	_, err := tx.ExecContext(ctx, "INSERT INTO root_state(id,purpose,revision,updated_at) VALUES (1,'',1,?)", now)
	return err
}

func createCurrent(ctx context.Context, tx schemaQuerier, now string) error {
	if err := createV2(ctx, tx, now); err != nil {
		return err
	}
	if err := migrateV2ToV3(ctx, tx); err != nil {
		return err
	}
	if err := migrateV3ToV4(ctx, tx); err != nil {
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
