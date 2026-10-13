package records

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type reader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func get(ctx context.Context, db reader, typ, id string) (bridge.Record, error) {
	table, err := query.Table(typ)
	if err != nil {
		return bridge.Record{}, invalid(err.Error())
	}
	var data string
	err = db.QueryRowContext(ctx, "SELECT record FROM "+table+" WHERE id = ?", id).Scan(&data)
	if err == sql.ErrNoRows {
		return bridge.Record{}, bridge.Errorf(bridge.CodeNotFound, "no "+typ+" "+id)
	}
	if err != nil {
		return bridge.Record{}, err
	}
	var row bridge.Record
	err = json.Unmarshal([]byte(data), &row)
	return row, err
}
func (s *Service) Get(ctx context.Context, scope svc.Scope, p bridge.GetRecordParams) (bridge.Record, error) {
	c, err := s.open(ctx, scope, false, false)
	if err != nil {
		return bridge.Record{}, err
	}
	defer c.close()
	if _, err := recordType(c.def, p.Type); err != nil {
		return bridge.Record{}, err
	}
	return get(ctx, c.db, p.Type, p.ID)
}
func (s *Service) Query(ctx context.Context, scope svc.Scope, p bridge.QueryParams) (bridge.QueryResult, error) {
	result := bridge.QueryResult{Rows: []bridge.Record{}}
	c, err := s.open(ctx, scope, false, false)
	if err != nil {
		return result, err
	}
	defer c.close()
	typ, err := recordType(c.def, p.Type)
	if err != nil {
		return result, err
	}
	ast, err := query.Parse(typ, p.Query, scope.Caller.Member)
	if err != nil {
		return result, invalid(err.Error())
	}
	plan, err := query.Compile(typ, ast, query.Identity)
	if err != nil {
		return result, invalid(err.Error())
	}
	statement, args, err := plan.Page(p.Type, p.After, p.Limit)
	if err != nil {
		return result, invalid(err.Error())
	}
	// The page, total and meter counts come from one read snapshot.
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, statement, args...)
	if err != nil {
		return result, err
	}
	var values []any
	for rows.Next() {
		var data string
		var value any
		if err := rows.Scan(&data, &value); err != nil {
			rows.Close()
			return result, err
		}
		var row bridge.Record
		if err := json.Unmarshal([]byte(data), &row); err != nil {
			rows.Close()
			return result, err
		}
		result.Rows = append(result.Rows, row)
		values = append(values, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	limit := p.Limit
	if limit == 0 {
		limit = query.MaxLimit
	}
	if len(result.Rows) > limit {
		result.Rows = result.Rows[:limit]
		result.Next, err = plan.Cursor(p.Type, result.Rows[limit-1].ID, values[limit-1])
		if err != nil {
			return result, err
		}
	}
	table, _ := query.Table(p.Type)
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+plan.Where, plan.Args...).Scan(&result.Total); err != nil {
		return result, err
	}
	if p.CountBy != "" {
		f := typ.Fields[p.CountBy]
		if f == nil || f.Type != "enum" {
			return result, invalid("count_by must name an enum field")
		}
		expr, _ := query.Expression(typ, p.CountBy)
		result.Counts, err = counts(ctx, tx, table, expr, plan)
		if err != nil {
			return result, err
		}
	}
	return result, tx.Commit()
}
func counts(ctx context.Context, tx *sql.Tx, table, expr string, plan query.Plan) (map[string]int, error) {
	rows, err := tx.QueryContext(ctx, "SELECT "+expr+", COUNT(*) FROM "+table+" WHERE "+plan.Where+" GROUP BY "+expr, plan.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]int{}
	for rows.Next() {
		var value any
		var count int
		if err := rows.Scan(&value, &count); err != nil {
			return nil, err
		}
		if value != nil {
			result[fmt.Sprint(value)] = count
		}
	}
	return result, rows.Err()
}
