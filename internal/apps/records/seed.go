package records

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Seed replaces only sample.db, including its schema, in one transaction.
func (s *Service) Seed(ctx context.Context, app string, rows map[string][]bridge.Record) error {
	scope := svc.Scope{App: app, Data: svc.DataSample, Caller: svc.Caller{Member: svc.Owner}}
	c, err := s.open(ctx, scope, true, true)
	if err != nil {
		return err
	}
	defer c.close()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	old, err := resetSample(ctx, tx)
	if err != nil {
		return err
	}
	if err := schema(ctx, tx, c.def.App); err != nil {
		return err
	}
	seeded, err := seedRows(ctx, tx, c.def, rows)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, row := range old {
		s.publish(scope, bridge.RecordDiff{Op: bridge.DiffDelete, Type: row.Type, ID: row.ID, By: svc.Owner})
	}
	for i := range seeded {
		row := seeded[i]
		s.publish(scope, bridge.RecordDiff{Op: bridge.DiffUpsert, Type: row.Type, ID: row.ID, Record: &row, By: svc.Owner})
	}
	return nil
}
func resetSample(ctx context.Context, tx *sql.Tx) ([]bridge.Record, error) {
	tables, err := tx.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name GLOB 'records_*'`)
	if err != nil {
		return nil, err
	}
	var names []string
	for tables.Next() {
		var name string
		if err := tables.Scan(&name); err != nil {
			tables.Close()
			return nil, err
		}
		names = append(names, name)
	}
	err = tables.Err()
	tables.Close()
	if err != nil {
		return nil, err
	}
	var old []bridge.Record
	for _, name := range names {
		// Tables are created from slugs. Validate again before interpolating stored metadata.
		typ := name[len("records_"):]
		table, err := query.Table(typ)
		if err != nil {
			return nil, err
		}
		rows, err := tx.QueryContext(ctx, "SELECT record FROM "+table+" ORDER BY id")
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var data string
			if err := rows.Scan(&data); err != nil {
				rows.Close()
				return nil, err
			}
			var row bridge.Record
			if err := json.Unmarshal([]byte(data), &row); err != nil {
				rows.Close()
				return nil, err
			}
			old = append(old, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "DROP TABLE "+table); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"_schema", "_history"} {
		if _, err := tx.ExecContext(ctx, "DROP TABLE IF EXISTS "+name); err != nil {
			return nil, err
		}
	}
	return old, nil
}
func seedRows(ctx context.Context, tx *sql.Tx, def *spec.Bundle, rows map[string][]bridge.Record) ([]bridge.Record, error) {
	for name := range rows {
		if _, err := recordType(def, name); err != nil {
			return nil, err
		}
	}
	var seeded []bridge.Record
	at := time.Now().UTC()
	for _, name := range sorted(def.App.Records) {
		rules, err := NewRules(def.App.Records[name])
		if err != nil {
			return nil, err
		}
		for _, input := range rows[name] {
			if input.ID == "" {
				return nil, invalid("sample record needs an id")
			}
			row := input
			row.Type, row.Version = name, max(row.Version, 1)
			if row.CreatedAt == "" {
				row.CreatedAt = stamp(at)
			}
			if row.UpdatedAt == "" {
				row.UpdatedAt = row.CreatedAt
			}
			created, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
			if err != nil {
				return nil, invalid("invalid sample created_at")
			}
			updated, err := time.Parse(time.RFC3339Nano, row.UpdatedAt)
			if err != nil {
				return nil, invalid("invalid sample updated_at")
			}
			row.CreatedAt, row.UpdatedAt = stamp(created), stamp(updated)
			if row.CreatedBy == "" {
				row.CreatedBy = svc.Owner
			}
			if row.UpdatedBy == "" {
				row.UpdatedBy = svc.Owner
			}
			row, err = rules.Prepare(row, svc.Owner, at, true)
			if err != nil {
				return nil, fmt.Errorf("sample %s %s: %w", name, row.ID, err)
			}
			if err := save(ctx, tx, nil, &row, svc.Owner); err != nil {
				return nil, err
			}
			seeded = append(seeded, row)
		}
	}
	// Two passes allow forward references and cycles without weakening referential validation.
	for _, row := range seeded {
		if err := relations(ctx, tx, def.App, def.App.Records[row.Type], row.Fields); err != nil {
			return nil, err
		}
	}
	return seeded, nil
}
