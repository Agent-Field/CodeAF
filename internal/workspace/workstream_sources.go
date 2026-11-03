package workspace

import (
	"context"
	"database/sql"
)

// WorkstreamSourcesSchemaVersion is the first schema with source pointers.
const WorkstreamSourcesSchemaVersion = 4

// Sources are pointers with their own lifetime, separate from memberships.
const workstreamSourcesSchema = `
CREATE TABLE workstream_sources (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 workstream_id TEXT NOT NULL REFERENCES workstreams(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('file','repo','issue','link','kept')),
 target TEXT NOT NULL,
 title TEXT NOT NULL,
 path TEXT, repo TEXT, number INTEGER, url TEXT, ref TEXT, why TEXT,
 read_by_jobs INTEGER NOT NULL DEFAULT 0,
 proposed INTEGER NOT NULL DEFAULT 0,
 added_by TEXT NOT NULL CHECK(added_by IN ('you','agent')),
 added_at INTEGER NOT NULL,
 UNIQUE(workstream_id,kind,target)
);
CREATE INDEX workstream_sources_order ON workstream_sources(workstream_id,seq);
`

const workstreamSourcesRead = "SELECT seq,id,workstream_id,kind,target,title,path,repo,number,url,ref,why,read_by_jobs,proposed,added_by,added_at FROM workstream_sources LIMIT 0"

// migrateWorkstreamSources is additive so existing rows and schema text stay intact.
func migrateWorkstreamSources(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, workstreamSourcesSchema)
	return err
}
