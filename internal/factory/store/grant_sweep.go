package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
)

// GrantLapse identifies the first grant to lapse on one resource in a sweep window.
type GrantLapse struct {
	acl.Resource
	Earliest time.Time
}

// GrantSweepWatermark reports the latest millisecond already announced.
func (st *Store) GrantSweepWatermark() (int64, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return time.Time{}.UnixMilli(), err
	}
	return grantSweepWatermark(db)
}

func grantSweepWatermark(q rowQuerier) (int64, error) {
	var through int64
	err := q.QueryRow(`SELECT covered_ms FROM grant_sweep WHERE id = 1`).Scan(&through)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}.UnixMilli(), nil
	}
	return through, err
}

// SweepGrantLapses serializes expiry with grant writes, including writes by
// another process. The watermark advances only after the events are durable.
func (st *Store) SweepGrantLapses(now time.Time, emit func([]GrantLapse) error) error {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return err
	}
	return st.underLock(filepath.Join(st.root, workstreamsLock), func() error {
		last, err := grantSweepWatermark(db)
		next := now.UnixMilli()
		if err != nil || next <= last {
			return err
		}
		lapses, err := grantLapses(db, last, next)
		if err != nil {
			return err
		}
		if len(lapses) > 0 {
			if err := emit(lapses); err != nil {
				return err
			}
		}
		_, err = db.Exec(`INSERT INTO grant_sweep (id, covered_ms) VALUES (1, ?)
			ON CONFLICT (id) DO UPDATE SET covered_ms=excluded.covered_ms
			WHERE excluded.covered_ms > grant_sweep.covered_ms`, next)
		return err
	})
}

func grantLapses(db *sql.DB, after, through int64) ([]GrantLapse, error) {
	rows, err := db.Query(`SELECT resource_kind, resource_id, MIN(expires_at) AS earliest
		FROM grants WHERE expires_at > ? AND expires_at <= ?
		GROUP BY resource_kind, resource_id ORDER BY earliest, resource_kind, resource_id`,
		accessTime(time.UnixMilli(after)), accessTime(time.UnixMilli(through)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []GrantLapse
	for rows.Next() {
		var lapse GrantLapse
		var at string
		if err := rows.Scan(&lapse.Kind, &lapse.ID, &at); err != nil {
			return nil, err
		}
		lapse.Earliest, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		result = append(result, lapse)
	}
	return result, rows.Err()
}

// NextGrantExpiry reads the first expiry after the given instant.
func (st *Store) NextGrantExpiry(after time.Time) (time.Time, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return time.Time{}, err
	}
	var at string
	err = db.QueryRow(`SELECT expires_at FROM grants WHERE expires_at > ? ORDER BY expires_at LIMIT 1`, accessTime(after)).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, at)
}
