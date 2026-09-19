package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// EnqueueJob records work for the tick. A second enqueue with the same type and
// coalesce key while the first is still pending or leased is a no-op success:
// the organizer must not run twice for one source revision.
func (s *Store) EnqueueJob(ctx context.Context, job Job) (Job, error) {
	job, err := normalizeEnqueue(job)
	if err != nil {
		return Job{}, storeError(err)
	}
	if err := s.writeReady(ctx); err != nil {
		return Job{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, storeError(err)
	}
	defer tx.Rollback()
	stored, err := storeJob(ctx, tx, job, s.stamp())
	if err != nil {
		return Job{}, storeError(err)
	}
	return stored, storeError(tx.Commit())
}

func normalizeEnqueue(job Job) (Job, error) {
	if job.Type == "" {
		job.Type = JobOrganize
	}
	if job.State == "" {
		job.State = JobPending
	}
	if job.State != JobPending {
		return Job{}, fmt.Errorf("%w: jobs enqueue as pending", ErrInvalid)
	}
	return job, nil
}

func storeJob(ctx context.Context, tx *sql.Tx, job Job, now string) (Job, error) {
	if job.CoalesceKey != "" {
		existing, err := lookupCoalesced(ctx, tx, job.Type, job.CoalesceKey)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Job{}, err
		}
	}
	if job.ID == "" {
		id, err := mintID()
		if err != nil {
			return Job{}, err
		}
		job.ID = id
	}
	job.CreatedAt, job.UpdatedAt = now, now
	if err := insertJob(ctx, tx, job); err != nil {
		if job.CoalesceKey != "" && isUniqueConstraint(err) {
			return lookupCoalesced(ctx, tx, job.Type, job.CoalesceKey)
		}
		return Job{}, err
	}
	return job, nil
}

// LeaseJob moves the oldest matching pending row to leased and mints a fencing
// token. Expired leases return to pending first: expiry is not “worker dead”
// and does not mark external effects done.
func (s *Store) LeaseJob(ctx context.Context, types []string, owner, until string) (Job, error) {
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(until) == "" {
		return Job{}, storeError(fmt.Errorf("%w: lease needs an owner and a deadline", ErrInvalid))
	}
	if err := s.writeReady(ctx); err != nil {
		return Job{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, storeError(err)
	}
	defer tx.Rollback()
	now := s.stamp()
	if err := expireLeases(ctx, tx, now); err != nil {
		return Job{}, storeError(err)
	}
	picked, err := pickPendingJob(ctx, tx, types)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, storeError(ErrNotFound)
	}
	if err != nil {
		return Job{}, storeError(err)
	}
	fence, err := mintID()
	if err != nil {
		return Job{}, storeError(err)
	}
	job, err := takeLease(ctx, tx, picked.ID, owner, fence, until, now)
	if err != nil {
		return Job{}, storeError(err)
	}
	return job, storeError(tx.Commit())
}

// FinishJob requires the current fence. Only completed, deferred, failed and
// cancelled are terminal; there is no other job state.
func (s *Store) FinishJob(ctx context.Context, id, fence, state, detail string) error {
	if !validJobFinish(state) {
		return storeError(fmt.Errorf("%w: job cannot finish as %q", ErrInvalid, state))
	}
	return s.withHeldJob(ctx, id, fence, func(tx *sql.Tx, job Job, now string) error {
		_, err := tx.ExecContext(ctx, `UPDATE jobs SET state=?, error=?, updated_at=? WHERE id=?`,
			state, detail, now, job.ID)
		return err
	})
}

// HeartbeatJob extends a live lease. A mismatched or expired fence refuses.
func (s *Store) HeartbeatJob(ctx context.Context, id, fence, until string) error {
	if strings.TrimSpace(until) == "" {
		return storeError(fmt.Errorf("%w: heartbeat needs a deadline", ErrInvalid))
	}
	return s.withHeldJob(ctx, id, fence, func(tx *sql.Tx, job Job, now string) error {
		_, err := tx.ExecContext(ctx, `UPDATE jobs SET lease_until=?, updated_at=? WHERE id=?`,
			until, now, job.ID)
		return err
	})
}

func (s *Store) withHeldJob(ctx context.Context, id, fence string, apply func(*sql.Tx, Job, string) error) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(fence) == "" {
		return storeError(fmt.Errorf("%w: job fence does not match", ErrConflict))
	}
	if err := s.writeReady(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	now := s.stamp()
	job, err := loadJob(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return storeError(ErrNotFound)
	}
	if err != nil {
		return storeError(err)
	}
	if !holdsFence(job, fence, now) {
		return storeError(fenceConflict())
	}
	if err := apply(tx, job, now); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

func holdsFence(job Job, fence, now string) bool {
	if job.State != JobLeased || job.Fence == "" || job.Fence != fence {
		return false
	}
	if job.LeaseUntil != "" && job.LeaseUntil < now {
		return false
	}
	return true
}

func expireLeases(ctx context.Context, tx *sql.Tx, now string) error {
	_, err := tx.ExecContext(ctx, `UPDATE jobs SET state=?, owner='', fence='', lease_until='', updated_at=?
 WHERE state=? AND lease_until!='' AND lease_until<?`, JobPending, now, JobLeased, now)
	return err
}

func pickPendingJob(ctx context.Context, tx *sql.Tx, types []string) (Job, error) {
	query := `SELECT ` + jobColumns + ` FROM jobs WHERE state=?`
	args := []any{JobPending}
	if len(types) > 0 {
		query += ` AND type IN (` + placeholders(len(types)) + `)`
		for _, kind := range types {
			args = append(args, kind)
		}
	}
	query += ` ORDER BY seq LIMIT 1`
	return scanJob(tx.QueryRowContext(ctx, query, args...))
}

func takeLease(ctx context.Context, tx *sql.Tx, id, owner, fence, until, now string) (Job, error) {
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET state=?, owner=?, fence=?, lease_until=?, attempt=attempt+1, updated_at=?
 WHERE id=? AND state=?`, JobLeased, owner, fence, until, now, id, JobPending)
	if err != nil {
		return Job{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Job{}, err
	}
	if n == 0 {
		return Job{}, ErrNotFound
	}
	return loadJob(ctx, tx, id)
}

func loadJob(ctx context.Context, tx *sql.Tx, id string) (Job, error) {
	return scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id=?`, id))
}

func lookupCoalesced(ctx context.Context, tx *sql.Tx, jobType, key string) (Job, error) {
	return scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs
 WHERE type=? AND coalesce_key=? AND state IN (?,?) ORDER BY seq LIMIT 1`,
		jobType, key, JobPending, JobLeased))
}

func insertJob(ctx context.Context, tx *sql.Tx, job Job) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO jobs(`+jobColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.Type, job.State, job.Owner, job.Fence, job.CauseID, job.CoalesceKey, job.ChatID,
		job.SourceRev, job.Error, job.Attempt, job.LeaseUntil, job.CreatedAt, job.UpdatedAt)
	return err
}

func scanJob(row eventScanner) (Job, error) {
	var job Job
	err := row.Scan(&job.ID, &job.Type, &job.State, &job.Owner, &job.Fence, &job.CauseID, &job.CoalesceKey,
		&job.ChatID, &job.SourceRev, &job.Error, &job.Attempt, &job.LeaseUntil, &job.CreatedAt, &job.UpdatedAt)
	return job, err
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

func isUniqueConstraint(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	return sqliteErr.Code()&0xff == sqlite3.SQLITE_CONSTRAINT
}
