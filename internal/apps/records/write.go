package records

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/google/uuid"
	"modernc.org/sqlite"
)

func (s *Service) Create(ctx context.Context, scope svc.Scope, p bridge.CreateRecordParams) (bridge.Record, error) {
	c, err := s.open(ctx, scope, true, false)
	if err != nil {
		return bridge.Record{}, err
	}
	defer c.close()
	typ, err := recordType(c.def, p.Type)
	if err != nil {
		return bridge.Record{}, err
	}
	if err := writable(typ, p.Set); err != nil {
		return bridge.Record{}, err
	}
	rules, err := NewRules(typ)
	if err != nil {
		return bridge.Record{}, err
	}
	at := time.Now().UTC()
	row := bridge.Record{ID: p.Type + "_" + uuid.NewString(), Type: p.Type, Version: 1, Fields: p.Set, CreatedAt: stamp(at), UpdatedAt: stamp(at), CreatedBy: scope.Caller.Member, UpdatedBy: scope.Caller.Member}
	if scope.Caller.Agent {
		row.AI = sorted(p.Set)
	}
	row, err = rules.Prepare(row, scope.Caller.Member, at, true)
	if err != nil {
		return row, err
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return row, err
	}
	defer tx.Rollback()
	if err := relations(ctx, tx, c.def.App, typ, row.Fields); err != nil {
		return row, err
	}
	if err := save(ctx, tx, nil, &row, scope.Caller.Member); err != nil {
		return row, err
	}
	if err := tx.Commit(); err != nil {
		return row, err
	}
	s.publish(scope, bridge.RecordDiff{Op: bridge.DiffUpsert, Type: p.Type, ID: row.ID, Record: &row, By: scope.Caller.Member})
	return row, nil
}
func (s *Service) Update(ctx context.Context, scope svc.Scope, p bridge.UpdateRecordParams) (bridge.UpdateRecordResult, error) {
	result := bridge.UpdateRecordResult{Rows: []bridge.Record{}}
	ids, err := batchIDs(p.ID, p.IDs)
	if err != nil {
		return result, err
	}
	c, err := s.open(ctx, scope, true, false)
	if err != nil {
		return result, err
	}
	defer c.close()
	typ, err := recordType(c.def, p.Type)
	if err != nil {
		return result, err
	}
	if err := writable(typ, p.Set); err != nil {
		return result, err
	}
	rules, err := NewRules(typ)
	if err != nil {
		return result, err
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	at := time.Now().UTC()
	for _, id := range ids {
		row, err := updateOne(ctx, tx, c.def.App, typ, rules, scope, p.Type, id, p.Set, at)
		if err != nil {
			return bridge.UpdateRecordResult{}, err
		}
		result.Rows = append(result.Rows, row)
	}
	if err := tx.Commit(); err != nil {
		return bridge.UpdateRecordResult{}, err
	}
	for i := range result.Rows {
		row := result.Rows[i]
		s.publish(scope, bridge.RecordDiff{Op: bridge.DiffUpsert, Type: p.Type, ID: row.ID, Record: &row, By: scope.Caller.Member})
	}
	return result, nil
}
func updateOne(ctx context.Context, tx *sql.Tx, app *spec.App, typ *spec.RecordType, rules *Rules, scope svc.Scope, recordType, id string, set map[string]any, at time.Time) (bridge.Record, error) {
	before, err := get(ctx, tx, recordType, id)
	if err != nil {
		return bridge.Record{}, err
	}
	row := before
	row.Fields, err = copyFields(before.Fields)
	if err != nil {
		return row, err
	}
	row.AI = slices.Clone(before.AI)
	for name, value := range set {
		if scope.Caller.Agent && typ.Fields[name].AI != "" && before.Fields[name] != nil && !slices.Contains(before.AI, name) {
			continue
		}
		row.Fields[name] = value
		if scope.Caller.Agent && !slices.Contains(row.AI, name) {
			row.AI = append(row.AI, name)
		}
		if !scope.Caller.Agent {
			row.AI = slices.DeleteFunc(row.AI, func(s string) bool { return s == name })
		}
	}
	row.Version++
	row.UpdatedAt, row.UpdatedBy = stamp(at), scope.Caller.Member
	row, err = rules.Prepare(row, scope.Caller.Member, at, false)
	if err != nil {
		return row, err
	}
	if err := relations(ctx, tx, app, typ, row.Fields); err != nil {
		return row, err
	}
	return row, save(ctx, tx, &before, &row, scope.Caller.Member)
}
func (s *Service) Delete(ctx context.Context, scope svc.Scope, p bridge.DeleteRecordParams) (bridge.DeleteRecordResult, error) {
	ids, err := batchIDs(p.ID, p.IDs)
	if err != nil {
		return bridge.DeleteRecordResult{}, err
	}
	c, err := s.open(ctx, scope, true, false)
	if err != nil {
		return bridge.DeleteRecordResult{}, err
	}
	defer c.close()
	if _, err := recordType(c.def, p.Type); err != nil {
		return bridge.DeleteRecordResult{}, err
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return bridge.DeleteRecordResult{}, err
	}
	defer tx.Rollback()
	for _, id := range ids {
		before, err := get(ctx, tx, p.Type, id)
		if err != nil {
			return bridge.DeleteRecordResult{}, err
		}
		if err := referenced(ctx, tx, c.def.App, p.Type, id, ids); err != nil {
			return bridge.DeleteRecordResult{}, err
		}
		table, _ := query.Table(p.Type)
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE id = ?", id); err != nil {
			return bridge.DeleteRecordResult{}, err
		}
		if scope.Data == svc.DataReal {
			if err := history(ctx, tx, &before, nil, scope.Caller.Member); err != nil {
				return bridge.DeleteRecordResult{}, err
			}
		} else {
			if _, err := tx.ExecContext(ctx, "DELETE FROM _history WHERE record_type = ? AND id = ?", p.Type, id); err != nil {
				return bridge.DeleteRecordResult{}, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return bridge.DeleteRecordResult{}, err
	}
	for _, id := range ids {
		s.publish(scope, bridge.RecordDiff{Op: bridge.DiffDelete, Type: p.Type, ID: id, By: scope.Caller.Member})
	}
	return bridge.DeleteRecordResult{IDs: ids}, nil
}
func batchIDs(one string, many []string) ([]string, error) {
	if (one == "") == (len(many) == 0) {
		return nil, invalid("exactly one of id and ids is required")
	}
	if one != "" {
		return []string{one}, nil
	}
	ids := slices.Clone(many)
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			return nil, invalid("ids must be nonempty and distinct")
		}
		seen[id] = true
	}
	return ids, nil
}
func save(ctx context.Context, tx *sql.Tx, before, after *bridge.Record, member string) error {
	table, _ := query.Table(after.Type)
	data, err := json.Marshal(after)
	if err != nil {
		return invalid("record must contain JSON values")
	}
	if before == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO "+table+" (id,record) VALUES (?,?)", after.ID, string(data))
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE "+table+" SET record = ? WHERE id = ?", string(data), after.ID)
	}
	if err != nil {
		var sq *sqlite.Error
		if errors.As(err, &sq) && sq.Code()&0xff == 19 {
			return bridge.Errorf(bridge.CodeConflict, "record conflicts with a unique field or id")
		}
		return err
	}
	return history(ctx, tx, before, after, member)
}
func history(ctx context.Context, tx *sql.Tx, before, after *bridge.Record, member string) error {
	row, op, at := after, "create", ""
	if before != nil {
		op = "update"
	}
	if after == nil {
		row, op, at = before, "delete", stamp(time.Now())
	} else {
		at = after.UpdatedAt
	}
	var a, b any
	if before != nil {
		data, err := json.Marshal(before)
		if err != nil {
			return err
		}
		a = string(data)
	}
	if after != nil {
		data, err := json.Marshal(after)
		if err != nil {
			return err
		}
		b = string(data)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO _history (record_type,id,op,by,at,before,after) VALUES (?,?,?,?,?,?,?)`, row.Type, row.ID, op, member, at, a, b)
	return err
}
func relations(ctx context.Context, tx *sql.Tx, app *spec.App, typ *spec.RecordType, fields map[string]any) error {
	for _, name := range sorted(typ.Fields) {
		f, value := typ.Fields[name], fields[name]
		if value == nil || (f.Type != spec.FieldRef && f.Type != spec.FieldRefs) {
			continue
		}
		if app.Records[f.To] == nil {
			return invalid("unknown relation target " + f.To)
		}
		refs := []any{value}
		if f.Type == spec.FieldRefs {
			refs = value.([]any)
		}
		for _, id := range refs {
			if _, err := get(ctx, tx, f.To, id.(string)); err != nil {
				var e *bridge.Error
				if errors.As(err, &e) && e.Code == bridge.CodeNotFound {
					return invalid(name + ": related record does not exist")
				}
				return err
			}
		}
	}
	return nil
}

// Restrict deletion rather than leaving required relations pointing at absent rows.
func referenced(ctx context.Context, tx *sql.Tx, app *spec.App, typ, id string, deleting []string) error {
	for _, source := range sorted(app.Records) {
		schema := app.Records[source]
		table, _ := query.Table(source)
		for _, name := range sorted(schema.Fields) {
			f := schema.Fields[name]
			if f.To != typ || (f.Type != spec.FieldRef && f.Type != spec.FieldRefs) {
				continue
			}
			expr, _ := query.Expression(schema, name)
			condition := expr + " = ?"
			if f.Type == spec.FieldRefs {
				condition = "EXISTS (SELECT 1 FROM json_each(" + expr + ") WHERE value = ?)"
			}
			args := []any{id}
			if source == typ {
				for _, removed := range deleting {
					condition += " AND id != ?"
					args = append(args, removed)
				}
			}
			var count int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+condition, args...).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return bridge.Errorf(bridge.CodeConflict, "record is referenced by "+source+"."+name)
			}
		}
	}
	return nil
}
