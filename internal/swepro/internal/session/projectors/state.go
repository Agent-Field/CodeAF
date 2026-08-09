package projectors

import (
	"context"
	"database/sql"
	"fmt"
)

// RowImage is an insertion-ordered SQLite row suitable for parity snapshots.
type RowImage struct {
	value jsonValue
}

func (r RowImage) MarshalJSON() ([]byte, error) { return r.value.MarshalJSON() }

// Snapshot is the complete session-owned working-memory image.
type Snapshot struct {
	Sessions       []RowImage `json:"sessions"`
	Messages       []RowImage `json:"messages"`
	Parts          []RowImage `json:"parts"`
	SessionMessage []RowImage `json:"session_messages"`
}

// Snapshot reads projector tables in deterministic primary-key order.
func (s *Store) Snapshot(ctx context.Context) (Snapshot, error) {
	out := Snapshot{
		Sessions:       []RowImage{},
		Messages:       []RowImage{},
		Parts:          []RowImage{},
		SessionMessage: []RowImage{},
	}
	var err error
	if out.Sessions, err = queryRowImages(ctx, s.db, "session", sessionJSONColumns); err != nil {
		return Snapshot{}, err
	}
	if out.Messages, err = queryRowImages(ctx, s.db, "message", map[string]bool{"data": true}); err != nil {
		return Snapshot{}, err
	}
	if out.Parts, err = queryRowImages(ctx, s.db, "part", map[string]bool{"data": true}); err != nil {
		return Snapshot{}, err
	}
	if out.SessionMessage, err = queryRowImages(
		ctx,
		s.db,
		"session_message",
		map[string]bool{"data": true},
	); err != nil {
		return Snapshot{}, err
	}
	return out, nil
}

func queryRowImages(
	ctx context.Context,
	db *sql.DB,
	table string,
	jsonColumns map[string]bool,
) ([]RowImage, error) {
	rows, err := db.QueryContext(ctx, "SELECT * FROM "+table+" ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []RowImage{}
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}
		object := newJSONObject()
		for index, column := range columns {
			value, err := databaseJSONValue(values[index], jsonColumns[column])
			if err != nil {
				return nil, fmt.Errorf("projectors: decode %s.%s: %w", table, column, err)
			}
			object.set(column, value)
		}
		out = append(out, RowImage{value: objectJSON(object)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func databaseJSONValue(value any, parseJSON bool) (jsonValue, error) {
	if value == nil {
		return nullJSON(), nil
	}
	if parseJSON {
		switch typed := value.(type) {
		case string:
			return parseOrderedJSON([]byte(typed))
		case []byte:
			return parseOrderedJSON(typed)
		}
	}
	switch typed := value.(type) {
	case bool:
		return boolJSON(typed), nil
	case int64:
		return numberJSON(float64(typed)), nil
	case float64:
		return numberJSON(typed), nil
	case string:
		return stringJSON(typed), nil
	case []byte:
		return stringJSON(string(typed)), nil
	default:
		return jsonValue{}, fmt.Errorf("unsupported SQLite value %T", value)
	}
}
