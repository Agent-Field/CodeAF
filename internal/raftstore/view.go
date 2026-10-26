package raftstore

import (
	"bytes"
	"context"
	"database/sql"
)

// ApplicationView supplies only committed metadata and keyed byte accounting.
// It is a local dependency read; the actor must first pass ReadIndex before
// exposing any of this view to a client. No identity-sized buffer is created.
func (s *SQLite) ApplicationView(ctx context.Context) (Application, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var app Application
	var size uint64
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		app, err = getApplication(ctx, tx)
		if err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT coalesce(sum(12+length(CAST(namespace AS BLOB))+length(CAST(key AS BLOB))+length(data)),0) FROM records`).Scan(&size)
	})
	return app, size, err
}

// ScanPage bounds listings at one immutable applied view. Callers serialize
// the complete multi-page read with application and discard it on CAS failure.
// A page carries at most MaxRecordBytes payload bytes, including keys.
func (s *SQLite) ScanPage(ctx context.Context, expected uint64, namespace Namespace, after string) ([]Write, Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var app Application
	out := []Write{}
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		app, err = getApplication(ctx, tx)
		if err != nil {
			return err
		}
		if app.Applied != expected {
			return ErrConflict
		}
		rows, err := tx.QueryContext(ctx, `SELECT key,data,checksum FROM records WHERE namespace=? AND key>? ORDER BY key`, namespace, after)
		if err != nil {
			return err
		}
		defer rows.Close()
		size := 0
		for rows.Next() {
			var k string
			var data, hash []byte
			if err := rows.Scan(&k, &data, &hash); err != nil {
				return err
			}
			if !bytes.Equal(hash, checksum([]byte("records"), []byte(namespace), []byte(k), data)) {
				return ErrCorrupt
			}
			cost := len(k) + len(data)
			if size+cost > MaxRecordBytes {
				if len(out) == 0 {
					return ErrBatch
				}
				break
			}
			size += cost
			out = append(out, Write{Key: Key{Namespace: namespace, Key: k}, Value: data})
		}
		return rows.Err()
	})
	return out, app, err
}
