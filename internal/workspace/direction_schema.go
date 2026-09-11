package workspace

import (
	"context"
	"database/sql"
)

// The version 4 addition: the one record for direction (design-t03b §1.5).
// Every table is new, so an upgrade only adds. No vocabulary is held in a CHECK
// constraint — kinds, states, source classes, target kinds and link kinds are
// open lists validated in Go by internal/direction (L5), because a CHECK list is
// a table rebuild the day the list grows.
//
// Revisions, targets, exclusions and links are written once and never edited;
// direction_records.revision is the pointer that moves. direction_live is
// DERIVED: one row per target of every current revision that sits in a live
// lane, maintained inside the same transaction that moves the pointer and
// rebuildable from the revisions at any time. It is the only table the
// resolver range-scans, which is what keeps a resolve independent of the
// lifetime number of records (L6).
const directionSchema = `
CREATE TABLE direction_records (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 revision INTEGER NOT NULL,
 created_at TEXT NOT NULL
);
CREATE TABLE direction_revisions (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 record_id TEXT NOT NULL REFERENCES direction_records(id),
 revision INTEGER NOT NULL,
 kind TEXT NOT NULL, state TEXT NOT NULL, state_reason TEXT NOT NULL DEFAULT '',
 title TEXT NOT NULL, text TEXT NOT NULL, text_sha256 TEXT NOT NULL,
 quote TEXT NOT NULL DEFAULT '', quote_origin TEXT NOT NULL,
 source_class TEXT NOT NULL, source_id TEXT NOT NULL DEFAULT '',
 source_session TEXT NOT NULL DEFAULT '', source_hint TEXT NOT NULL DEFAULT '',
 source_sha256 TEXT NOT NULL DEFAULT '',
 author_class TEXT NOT NULL, author_ref TEXT NOT NULL DEFAULT '',
 receipt_actor TEXT NOT NULL DEFAULT '', receipt_door TEXT NOT NULL DEFAULT '',
 receipt_ref TEXT NOT NULL DEFAULT '', receipt_at TEXT NOT NULL DEFAULT '',
 written_at TEXT NOT NULL,
 UNIQUE(record_id, revision)
);
CREATE INDEX direction_revisions_rejected ON direction_revisions(text_sha256) WHERE state='rejected';
CREATE TABLE direction_targets (
 record_id TEXT NOT NULL, revision INTEGER NOT NULL, position INTEGER NOT NULL,
 target_kind TEXT NOT NULL, ref_id TEXT NOT NULL, session_id TEXT NOT NULL DEFAULT '',
 reach TEXT NOT NULL DEFAULT 'direct',
 PRIMARY KEY(record_id, revision, target_kind, ref_id, session_id),
 FOREIGN KEY(record_id, revision) REFERENCES direction_revisions(record_id, revision)
);
CREATE TABLE direction_exclusions (
 record_id TEXT NOT NULL, revision INTEGER NOT NULL,
 target_kind TEXT NOT NULL, ref_id TEXT NOT NULL, session_id TEXT NOT NULL DEFAULT '',
 at TEXT NOT NULL,
 PRIMARY KEY(record_id, revision, target_kind, ref_id, session_id)
);
CREATE TABLE direction_links (
 record_id TEXT NOT NULL, revision INTEGER NOT NULL, link_kind TEXT NOT NULL,
 to_ref TEXT NOT NULL, to_revision INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(record_id, revision, link_kind, to_ref)
);
CREATE INDEX direction_links_to ON direction_links(to_ref, link_kind);
CREATE TABLE direction_legacy (
 source_store TEXT NOT NULL,
 source_id TEXT NOT NULL, source_version TEXT NOT NULL, source_sha256 TEXT NOT NULL,
 record_id TEXT NOT NULL, revision INTEGER NOT NULL, import_run TEXT NOT NULL,
 PRIMARY KEY(source_store, source_id, source_sha256)
);
CREATE INDEX direction_legacy_record ON direction_legacy(record_id);
CREATE TABLE direction_import_runs (
 id TEXT PRIMARY KEY, mode TEXT NOT NULL, binary TEXT NOT NULL, report_sha256 TEXT NOT NULL,
 started_at TEXT NOT NULL, finished_at TEXT, counts TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE direction_live (
 target_kind TEXT NOT NULL, ref_id TEXT NOT NULL, session_id TEXT NOT NULL,
 lane TEXT NOT NULL, record_id TEXT NOT NULL, revision INTEGER NOT NULL,
 reach TEXT NOT NULL, has_exclusions INTEGER NOT NULL, has_links INTEGER NOT NULL,
 written_at TEXT NOT NULL,
 PRIMARY KEY(target_kind, ref_id, session_id, lane, record_id)
) WITHOUT ROWID;
CREATE INDEX direction_live_record ON direction_live(record_id);
`

// verifyDirectionSchema reads every version 4 table with every column the
// direction package uses, so a store that claims version 4 and lost a table is
// refused rather than read as having no direction.
func verifyDirectionSchema(q schemaReader) error {
	for _, read := range []string{
		"SELECT key,value FROM " + storeMetaTable + " LIMIT 0",
		"SELECT seq,id,revision,created_at FROM direction_records LIMIT 0",
		`SELECT seq,record_id,revision,kind,state,state_reason,title,text,text_sha256,quote,quote_origin,
 source_class,source_id,source_session,source_hint,source_sha256,author_class,author_ref,
 receipt_actor,receipt_door,receipt_ref,receipt_at,written_at FROM direction_revisions LIMIT 0`,
		"SELECT record_id,revision,position,target_kind,ref_id,session_id,reach FROM direction_targets LIMIT 0",
		"SELECT record_id,revision,target_kind,ref_id,session_id,at FROM direction_exclusions LIMIT 0",
		"SELECT record_id,revision,link_kind,to_ref,to_revision FROM direction_links LIMIT 0",
		"SELECT source_store,source_id,source_version,source_sha256,record_id,revision,import_run FROM direction_legacy LIMIT 0",
		"SELECT id,mode,binary,report_sha256,started_at,finished_at,counts FROM direction_import_runs LIMIT 0",
		`SELECT target_kind,ref_id,session_id,lane,record_id,revision,reach,has_exclusions,has_links,written_at
 FROM direction_live LIMIT 0`,
	} {
		if _, err := q.Exec(read); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDirection brings the store to version 4, adding the direction tables
// in one immediate transaction that re-reads the stamp. A store already at
// version 4 is verified and nothing is written. This is the only door that
// creates the tables; see ordinaryVersion for why an ordinary open does not.
func (s *Store) EnsureDirection() error { return s.initialize(schemaVersion) }

// THE TWO RAW TRANSACTION DOORS belong to the direction package, which keeps its
// records in this store because the resolver must read placements and
// direction in one snapshot, and a second database file cannot give it one. A
// law in internal/direction pins them to that package, so nothing else writes
// this store around the doors that validate it.
//
// ReadSnapshot runs fn in one deferred read transaction: under WAL it sees one
// consistent state of every table and never takes the writer, so a per-turn
// read cannot make a peer's commit wait. The transaction is always rolled back;
// fn cannot write through it.
func (s *Store) ReadSnapshot(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return fn(tx)
}

// WriteImmediate runs fn in one BEGIN IMMEDIATE transaction and commits only
// if fn returns nil. Holding the writer from the first read makes every
// read-then-write inside fn a fenced one (L1): no peer can commit between them.
func (s *Store) WriteImmediate(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
