package automation

import (
	"context"
	"database/sql"
	"fmt"
)

// The walk revision is what lets a reader of a crew's orders know that a walk
// of several pages saw one store: it moves when an automation is created, when
// one is deleted, and when an automation's id or recorded creation team
// changes, and at no other write. A run, a status, a wake time, a seen mark or
// any other edit leaves it alone, so the clock's ordinary work never makes a
// walk start again.
//
// IT IS ADDITIVE AND LIVES IN THE FILE. The table and its triggers are created
// if missing on every open, inside the schema transaction, and user_version is
// NOT moved: a build that predates them still opens the store, and because the
// triggers are SQLite's own, its creates, deletes and rewrites move the
// revision too — an older process that drops the creation team from a row
// makes every open walk stale rather than letting it end complete.
const originRevisionSchema = `
CREATE TABLE IF NOT EXISTS origin_revision (id INTEGER PRIMARY KEY CHECK (id = 1), rev INTEGER NOT NULL);
INSERT OR IGNORE INTO origin_revision (id, rev) VALUES (1, 0);
CREATE TRIGGER IF NOT EXISTS origin_revision_insert AFTER INSERT ON automations
BEGIN UPDATE origin_revision SET rev = rev + 1 WHERE id = 1; END;
CREATE TRIGGER IF NOT EXISTS origin_revision_delete AFTER DELETE ON automations
BEGIN UPDATE origin_revision SET rev = rev + 1 WHERE id = 1; END;
CREATE TRIGGER IF NOT EXISTS origin_revision_origin AFTER UPDATE OF id, doc ON automations
WHEN OLD.id IS NOT NEW.id
 OR (CASE WHEN json_valid(OLD.doc) THEN json_extract(OLD.doc, '$.origin.team') ELSE 'not json' END)
 IS NOT (CASE WHEN json_valid(NEW.doc) THEN json_extract(NEW.doc, '$.origin.team') ELSE 'not json' END)
BEGIN UPDATE origin_revision SET rev = rev + 1 WHERE id = 1; END;
`

// ensureOriginRevision creates the walk revision and its triggers when they
// are missing, inside the caller's schema transaction.
func ensureOriginRevision(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, originRevisionSchema); err != nil {
		return fmt.Errorf("automations: the walk revision: %w", err)
	}
	return nil
}
