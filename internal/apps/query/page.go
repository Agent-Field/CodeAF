package query

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const MaxLimit = 100

type cursor struct {
	Query string `json:"q"`
	ID    string `json:"id"`
	Value any    `json:"value"`
}

func fingerprint(p Plan, recordType string) string {
	sum := sha256.Sum256([]byte(p.Fingerprint(recordType)))
	return hex.EncodeToString(sum[:])
}
func (p Plan) Cursor(recordType, id string, value any) (string, error) {
	data, err := json.Marshal(cursor{fingerprint(p, recordType), id, value})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
func (p Plan) Page(recordType, after string, limit int) (string, []any, error) {
	table, err := Table(recordType)
	if err != nil {
		return "", nil, err
	}
	if limit < 0 || limit > MaxLimit {
		return "", nil, fmt.Errorf("limit must be between 1 and %d", MaxLimit)
	}
	if limit == 0 {
		limit = MaxLimit
	}
	where, args := p.Where, append([]any(nil), p.Args...)
	if after != "" {
		condition, values, err := p.seek(recordType, after)
		if err != nil {
			return "", nil, err
		}
		where += " AND (" + condition + ")"
		args = append(args, values...)
	}
	direction := "ASC"
	if p.Sort.Desc {
		direction = "DESC"
	}
	order := "(" + p.Expr + " IS NOT NULL) " + direction + ", " + p.Expr + " " + direction + ", id " + direction
	args = append(args, limit+1)
	return "SELECT record, " + p.Expr + " FROM " + table + " WHERE " + where + " ORDER BY " + order + " LIMIT ?", args, nil
}
func (p Plan) seek(recordType, after string) (string, []any, error) {
	if len(after) > 16384 {
		return "", nil, fmt.Errorf("invalid cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(after)
	var c cursor
	if err != nil || json.Unmarshal(data, &c) != nil || c.ID == "" || c.Query != fingerprint(p, recordType) {
		return "", nil, fmt.Errorf("invalid cursor for query")
	}
	switch c.Value.(type) {
	case nil, string, float64, bool:
	default:
		return "", nil, fmt.Errorf("invalid cursor value")
	}
	op := ">"
	if p.Sort.Desc {
		op = "<"
	}
	expr := p.Expr
	if c.Value == nil {
		sql := "(" + expr + " IS NULL AND id " + op + " ?)"
		if !p.Sort.Desc {
			sql += " OR " + expr + " IS NOT NULL"
		}
		return sql, []any{c.ID}, nil
	}
	sql := "(" + expr + " " + op + " ? OR (" + expr + " = ? AND id " + op + " ?))"
	if p.Sort.Desc {
		sql += " OR " + expr + " IS NULL"
	}
	return sql, []any{c.Value, c.Value, c.ID}, nil
}
