package data

import (
	"database/sql"
	"encoding/json"
	"errors"
)

const runWriteMigration = `
	CREATE TABLE run_write_receipts (
		app TEXT NOT NULL,
		step_key TEXT NOT NULL,
		type_id TEXT NOT NULL,
		op TEXT NOT NULL,
		result_json TEXT NOT NULL,
		PRIMARY KEY(app, step_key)
	) WITHOUT ROWID;
`

type recordBatch struct {
	batch string
	rows  []*record
	held  []Held
}

type runWriteResult struct {
	Batch string        `json:"batch"`
	Rows  []runWriteRow `json:"rows"`
	Held  []Held        `json:"held,omitempty"`
}

type runWriteRow struct {
	ID        string         `json:"id"`
	Data      map[string]any `json:"data"`
	Meta      recordMeta     `json:"meta"`
	Rev       int64          `json:"rev"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

func (s *Store) writeRecordBatch(sc Scope, typ, op string, apply func(*writeTx, Scope, *Type, *recordBatch) error) (*WriteResult, error) {
	sc, flushAudit := s.WithPolicyAudit(sc)
	defer flushAudit()
	var t *Type
	var result recordBatch
	err := s.writeAs(sc, func(w *writeTx) error {
		var err error
		if t, err = w.schema.allow(sc, typ, true); err != nil {
			return err
		}
		if sc.RunWriteKey != "" {
			found, err := w.loadRunWrite(sc, t, op, &result)
			if found || err != nil {
				return err
			}
		}
		if err := apply(w, sc, t, &result); err != nil {
			return err
		}
		if sc.RunWriteKey != "" {
			return w.saveRunWrite(sc, t, op, result)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := &WriteResult{Batch: result.batch, Rows: make([]Row, 0, len(result.rows)), Held: append([]Held{}, result.held...)}
	for _, r := range result.rows {
		if s.LiveRowAllowed(sc, t, r.Data) {
			out.Rows = append(out.Rows, s.row(t, r, sc, false))
		}
	}
	return out, nil
}

// The receipt is in the mutation's transaction. Its snapshots keep the
// original output even if another writer edits the records before recovery.
func (w *writeTx) saveRunWrite(sc Scope, t *Type, op string, batch recordBatch) error {
	result := runWriteResult{Batch: batch.batch, Held: batch.held, Rows: make([]runWriteRow, 0, len(batch.rows))}
	for _, r := range batch.rows {
		result.Rows = append(result.Rows, runWriteRow{
			ID: r.ID, Data: t.encode(r.Data, r.unknown), Meta: t.encodeMeta(r.Meta),
			Rev: r.Rev, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		})
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = w.tx.Exec(`INSERT INTO run_write_receipts(app, step_key, type_id, op, result_json)
		VALUES(?, ?, ?, ?, ?)`, sc.App, sc.RunWriteKey, t.ID, op, string(raw))
	return err
}

// Both new and recovered writes project their results through record policy
// so the durable receipt never grants an app extra access.
func (w *writeTx) loadRunWrite(sc Scope, t *Type, op string, batch *recordBatch) (bool, error) {
	var typeID, savedOp, raw string
	err := w.tx.QueryRow(`SELECT type_id, op, result_json FROM run_write_receipts WHERE app = ? AND step_key = ?`,
		sc.App, sc.RunWriteKey).Scan(&typeID, &savedOp, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if typeID != t.ID || savedOp != op {
		return false, errors.New("the run write key already belongs to another record type or action")
	}
	var result runWriteResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return false, err
	}
	batch.batch, batch.held = result.Batch, result.Held
	for _, row := range result.Rows {
		if t.rowRule != nil {
			// A receipt is a historical snapshot, so visibility must also
			// hold on the current record, just as it does for history.
			current, err := loadRecord(w.tx, w.schema, row.ID)
			if err != nil {
				return false, err
			}
			if current == nil || current.Type != t.Name || !w.s.LiveRowAllowed(sc, t, current.Data) {
				continue
			}
		}
		values, unknown := t.decode(row.Data)
		batch.rows = append(batch.rows, &record{
			ID: row.ID, Type: t.Name, Rev: row.Rev, Data: values, unknown: unknown,
			Meta: t.decodeMeta(row.Meta), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}
	return true, nil
}
