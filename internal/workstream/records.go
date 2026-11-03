package workstream

import (
	"context"
	"database/sql"
	"strings"
)

const recordSelect = `SELECT id,workstream_id,kind,text,state,actor,source,created_at,decided_at,coalesce(supersedes,''),coalesce(superseded_by,'') FROM workstream_records`

func recordIn(ctx context.Context, tx *sql.Tx, id string) (Record, error) {
	var r Record
	var source sql.NullString
	var at int64
	var decided sql.NullInt64
	err := tx.QueryRowContext(ctx, recordSelect+" WHERE id=?", id).Scan(&r.ID, &r.WorkstreamID, &r.Kind, &r.Text, &r.State, &r.By, &source, &at, &decided, &r.Supersedes, &r.SupersededBy)
	if err != nil {
		return r, found(err)
	}
	r.CreatedAt, r.DecidedAt = instant(at), optionalInstant(decided)
	r.Source, err = readSource(source)
	return r, err
}
func recordText(kind, text string) error {
	if kind != "decision" && kind != "question" {
		return invalid("record kind must be decision or question")
	}
	if strings.TrimSpace(text) == "" || !textWithin(text, 2000) {
		return invalid("record text must be 1–2000 bytes")
	}
	return nil
}
func (s *Store) Records(ctx context.Context, id, kind, state string) ([]Record, error) {
	if kind != "" && kind != "decision" && kind != "question" {
		return nil, invalid("unknown record kind")
	}
	if state != "" && state != "proposed" && state != "kept" && state != "dropped" && state != "superseded" {
		return nil, invalid("unknown record state")
	}
	out := []Record{}
	err := s.workspace.Read(ctx, func(tx *sql.Tx, v int) error {
		if v < 3 {
			return ErrNotFound
		}
		if _, err := getIn(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id FROM workstream_records WHERE workstream_id=? AND (?='' OR kind=?) AND (?='' OR state=?) ORDER BY created_at DESC,rowid DESC`, id, kind, kind, state, state)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, id := range ids {
			r, err := recordIn(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return nil
	})
	return out, err
}
func (s *Store) ProposeRecord(ctx context.Context, p RecordPropose) (Record, Change, error) {
	if err := recordText(p.Kind, p.Text); err != nil {
		return Record{}, Change{}, err
	}
	if p.By == "" {
		if p.Kept {
			p.By = "you"
		} else {
			p.By = "agent"
		}
	}
	by, err := actor(p.By)
	if err != nil {
		return Record{}, Change{}, err
	}
	source, err := sourceJSON(p.Source)
	if err != nil {
		return Record{}, Change{}, err
	}
	var out Record
	var c Change
	err = s.workspace.Write(ctx, func(tx *sql.Tx) error {
		if _, err := getIn(ctx, tx, p.WorkstreamID); err != nil {
			return err
		}
		id, err := s.newID()
		if err != nil {
			return err
		}
		at := s.now().UnixMilli()
		state := "proposed"
		var decided any
		if p.Kept {
			state = "kept"
			decided = at
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO workstream_records(id,workstream_id,kind,text,state,actor,source,created_at,decided_at) VALUES (?,?,?,?,?,?,?,?,?)`, id, p.WorkstreamID, p.Kind, p.Text, state, by, source, at, decided); err != nil {
			return err
		}
		if err := touch(ctx, tx, p.WorkstreamID, at); err != nil {
			return err
		}
		out, err = recordIn(ctx, tx, id)
		c = changed(p.WorkstreamID, PartRecords)
		return err
	})
	if err != nil {
		return Record{}, Change{}, err
	}
	return out, c, nil
}
func (s *Store) KeepRecord(ctx context.Context, id string) (Record, Change, error) {
	return s.setRecordState(ctx, id, "kept")
}
func (s *Store) DropRecord(ctx context.Context, id string) (Record, Change, error) {
	return s.setRecordState(ctx, id, "dropped")
}
func (s *Store) setRecordState(ctx context.Context, id, state string) (Record, Change, error) {
	var out Record
	var c Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		r, err := recordIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.State == "superseded" {
			return ErrConflict
		}
		if r.State == state {
			out = r
			return nil
		}
		at := s.now().UnixMilli()
		if _, err := tx.ExecContext(ctx, "UPDATE workstream_records SET state=?,decided_at=? WHERE id=?", state, at, id); err != nil {
			return err
		}
		if err := touch(ctx, tx, r.WorkstreamID, at); err != nil {
			return err
		}
		out, err = recordIn(ctx, tx, id)
		c = changed(r.WorkstreamID, PartRecords)
		return err
	})
	if err != nil {
		return Record{}, Change{}, err
	}
	return out, c, nil
}
func (s *Store) SupersedeRecord(ctx context.Context, id string, p RecordReplacement) (Record, Record, Change, error) {
	by := p.By
	if by == "" {
		by = "agent"
	}
	by, err := actor(by)
	if err != nil {
		return Record{}, Record{}, Change{}, err
	}
	source, err := sourceJSON(p.Source)
	if err != nil {
		return Record{}, Record{}, Change{}, err
	}
	var out, old Record
	var c Change
	err = s.workspace.Write(ctx, func(tx *sql.Tx) error {
		r, err := recordIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.State == "superseded" {
			return ErrConflict
		}
		kind := p.Kind
		if kind == "" {
			kind = r.Kind
		}
		if err := recordText(kind, p.Text); err != nil {
			return err
		}
		replacement, err := s.newID()
		if err != nil {
			return err
		}
		at := s.now().UnixMilli()
		if _, err := tx.ExecContext(ctx, `INSERT INTO workstream_records(id,workstream_id,kind,text,state,actor,source,created_at,decided_at,supersedes) VALUES (?,?,?,?,'kept',?,?,?,?,?)`, replacement, r.WorkstreamID, kind, p.Text, by, source, at, at, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE workstream_records SET state='superseded',decided_at=?,superseded_by=? WHERE id=?", at, replacement, id); err != nil {
			return err
		}
		if err := touch(ctx, tx, r.WorkstreamID, at); err != nil {
			return err
		}
		out, err = recordIn(ctx, tx, replacement)
		if err != nil {
			return err
		}
		old, err = recordIn(ctx, tx, id)
		c = changed(r.WorkstreamID, PartRecords)
		return err
	})
	if err != nil {
		return Record{}, Record{}, Change{}, err
	}
	return out, old, c, nil
}
