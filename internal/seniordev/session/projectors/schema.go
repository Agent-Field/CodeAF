//go:build !windows

// The session SQLite schema. The project table that two foreign keys
// reference is owned by the caller.
package projectors

import (
	"context"
	"database/sql"
	"fmt"
)

// SchemaStatements creates the session-owned tables and indexes.
var SchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS session (
		id text PRIMARY KEY,
		project_id text NOT NULL,
		workspace_id text,
		parent_id text,
		slug text NOT NULL,
		directory text NOT NULL,
		path text,
		title text NOT NULL,
		version text NOT NULL,
		share_url text,
		summary_additions integer,
		summary_deletions integer,
		summary_files integer,
		summary_diffs text,
		revert text,
		permission text,
		agent text,
		model text,
		time_created integer NOT NULL,
		time_updated integer NOT NULL,
		time_compacting integer,
		time_archived integer,
		CONSTRAINT fk_session_project_id_project_id_fk
			FOREIGN KEY (project_id) REFERENCES project(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS session_project_idx ON session (project_id)`,
	`CREATE INDEX IF NOT EXISTS session_workspace_idx ON session (workspace_id)`,
	`CREATE INDEX IF NOT EXISTS session_parent_idx ON session (parent_id)`,
	`CREATE TABLE IF NOT EXISTS message (
		id text PRIMARY KEY,
		session_id text NOT NULL,
		time_created integer NOT NULL,
		time_updated integer NOT NULL,
		data text NOT NULL,
		CONSTRAINT fk_message_session_id_session_id_fk
			FOREIGN KEY (session_id) REFERENCES session(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS message_session_time_created_id_idx
		ON message (session_id, time_created, id)`,
	`CREATE TABLE IF NOT EXISTS part (
		id text PRIMARY KEY,
		message_id text NOT NULL,
		session_id text NOT NULL,
		time_created integer NOT NULL,
		time_updated integer NOT NULL,
		data text NOT NULL,
		CONSTRAINT fk_part_message_id_message_id_fk
			FOREIGN KEY (message_id) REFERENCES message(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS part_message_id_id_idx ON part (message_id, id)`,
	`CREATE INDEX IF NOT EXISTS part_session_idx ON part (session_id)`,
	`CREATE TABLE IF NOT EXISTS todo (
		session_id text NOT NULL,
		content text NOT NULL,
		status text NOT NULL,
		priority text NOT NULL,
		position integer NOT NULL,
		time_created integer NOT NULL,
		time_updated integer NOT NULL,
		CONSTRAINT todo_pk PRIMARY KEY (session_id, position),
		CONSTRAINT fk_todo_session_id_session_id_fk
			FOREIGN KEY (session_id) REFERENCES session(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS todo_session_idx ON todo (session_id)`,
	`CREATE TABLE IF NOT EXISTS session_message (
		id text PRIMARY KEY,
		session_id text NOT NULL,
		type text NOT NULL,
		time_created integer NOT NULL,
		time_updated integer NOT NULL,
		data text NOT NULL,
		CONSTRAINT fk_session_message_session_id_session_id_fk
			FOREIGN KEY (session_id) REFERENCES session(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS session_message_session_idx
		ON session_message (session_id)`,
	`CREATE INDEX IF NOT EXISTS session_message_session_type_idx
		ON session_message (session_id, type)`,
	`CREATE INDEX IF NOT EXISTS session_message_time_created_idx
		ON session_message (time_created)`,
	`CREATE TABLE IF NOT EXISTS permission (
		project_id text PRIMARY KEY,
		time_created integer NOT NULL,
		time_updated integer NOT NULL,
		data text NOT NULL,
		CONSTRAINT fk_permission_project_id_project_id_fk
			FOREIGN KEY (project_id) REFERENCES project(id) ON DELETE CASCADE
	)`,
}

// ApplySchema installs the session-owned tables and indexes. The project table
// referenced by two foreign keys must be installed by the caller.
func ApplySchema(ctx context.Context, db *sql.DB) error {
	for _, statement := range SchemaStatements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("projectors: apply schema: %w", err)
		}
	}
	return nil
}
