package automation

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// WHEN A RUN BEGAN TO WAIT ON THE PERSON is kept beside it, because its slot
// does not say: a run the clock parks late, after codeaf was away past its
// slot, asks long after it was due, and a window that tells the person what
// is new must not date the question to a slot they may have looked past.
//
// IT IS ADDITIVE AND LIVES IN THE FILE, as the walk revision is
// (store_revision.go): created if missing on every open, user_version not
// moved. A build that predates it still opens the store and parks runs
// without a row, and those read their slot; its deletes of runs drop their
// rows too, through the trigger.
const runAskedSchema = `
CREATE TABLE IF NOT EXISTS run_asked (run_id INTEGER PRIMARY KEY, asked_ms INTEGER NOT NULL);
CREATE TRIGGER IF NOT EXISTS run_asked_delete AFTER DELETE ON runs
BEGIN DELETE FROM run_asked WHERE run_id = OLD.id; END;
`

// ensureRunAsked creates the asked table and its trigger when they are
// missing, inside the caller's schema transaction.
func ensureRunAsked(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, runAskedSchema); err != nil {
		return fmt.Errorf("automations: when runs asked: %w", err)
	}
	return nil
}

// noteAsked keeps when a run began to wait on the person, inside the write
// that parked it.
func noteAsked(tx *sql.Tx, run int64, at time.Time) error {
	_, err := tx.Exec(`INSERT OR REPLACE INTO run_asked (run_id, asked_ms) VALUES (?, ?)`, run, ms(at))
	return err
}
