//go:build !windows

// Package projectors applies session, message and part events to the SQLite
// tables that mirror flat storage. Each Apply call is one transaction.
package projectors

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

const (
	EventSessionCreated     = "session.created"
	EventSessionUpdated     = "session.updated"
	EventSessionDeleted     = "session.deleted"
	EventMessageUpdated     = "message.updated"
	EventMessageRemoved     = "message.removed"
	EventMessagePartRemoved = "message.part.removed"
	EventMessagePartUpdated = "message.part.updated"
)

// Event is the serialized subset consumed by a projector.
type Event struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Warning is emitted for the two deliberately ignored late-write cases.
type Warning struct {
	Message string
	Fields  PartialRow
}

// StoreOptions supplies the clock and the warning sink.
type StoreOptions struct {
	Now  func() int64
	Warn func(Warning)
}

// Store applies the ordered projector registry to a SQLite database.
type Store struct {
	db   *sql.DB
	now  func() int64
	warn func(Warning)
}

// NewStore binds the projector chain to an initialized database.
func NewStore(db *sql.DB, options StoreOptions) *Store {
	now := func() int64 { return time.Now().UnixMilli() }
	if options.Now != nil {
		now = options.Now
	}
	warn := func(Warning) {}
	if options.Warn != nil {
		warn = options.Warn
	}
	return &Store{db: db, now: now, warn: warn}
}

// NotFoundError is returned when session.updated names a session that does
// not exist. The useful detail is in Message.
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string { return "NotFoundError" }

// PartialRow is a set of column values keyed by column name.
type PartialRow map[string]any

// row is a decoded JSON object.
type row = map[string]any

// ToPartialRow maps a JSON session patch onto the snake_case update columns.
// A nested field whose parent is null yields a null column.
func ToPartialRow(info json.RawMessage) (PartialRow, error) {
	value, err := decodeObject(info)
	if err != nil {
		return nil, errors.New("projectors: session patch must be an object")
	}
	out := PartialRow{}
	fields := []struct {
		source string
		column string
		nested string
	}{
		{"id", "id", ""},
		{"projectID", "project_id", ""},
		{"workspaceID", "workspace_id", ""},
		{"parentID", "parent_id", ""},
		{"slug", "slug", ""},
		{"directory", "directory", ""},
		{"path", "path", ""},
		{"title", "title", ""},
		{"version", "version", ""},
		{"share", "share_url", "url"},
		{"summary", "summary_additions", "additions"},
		{"summary", "summary_deletions", "deletions"},
		{"summary", "summary_files", "files"},
		{"summary", "summary_diffs", "diffs"},
		{"revert", "revert", ""},
		{"permission", "permission", ""},
		{"time", "time_created", "created"},
		{"time", "time_updated", "updated"},
		{"time", "time_compacting", "compacting"},
		{"time", "time_archived", "archived"},
	}
	for _, field := range fields {
		if item, ok := grab(value, field.source, field.nested); ok {
			out[field.column] = item
		}
	}
	return out, nil
}

func grab(object row, field, nested string) (any, bool) {
	value, ok := object[field]
	if !ok {
		return nil, false
	}
	if nested == "" {
		return value, true
	}
	switch typed := value.(type) {
	case row:
		item, ok := typed[nested]
		return item, ok
	case []any:
		return nil, false
	}
	return value, true
}

// Apply projects one event inside a SQLite transaction.
func (s *Store) Apply(ctx context.Context, event Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := s.ApplyTx(ctx, tx, event); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ApplyTx projects one event into an existing transaction.
func (s *Store) ApplyTx(ctx context.Context, tx *sql.Tx, event Event) error {
	data, err := decodeObject(event.Data)
	if err != nil {
		return errors.New("projectors: event data must be an object")
	}
	switch event.Type {
	case EventSessionCreated:
		err = s.projectSessionCreated(ctx, tx, data)
	case EventSessionUpdated:
		err = s.projectSessionUpdated(ctx, tx, data)
	case EventSessionDeleted:
		err = s.projectSessionDeleted(ctx, tx, data)
	case EventMessageUpdated:
		err = s.projectMessageUpdated(ctx, tx, data)
	case EventMessageRemoved:
		err = s.projectMessageRemoved(ctx, tx, data)
	case EventMessagePartRemoved:
		err = s.projectPartRemoved(ctx, tx, data)
	case EventMessagePartUpdated:
		err = s.projectPartUpdated(ctx, tx, data)
	default:
		err = fmt.Errorf("Projector not found for event: %s", event.Type)
	}
	if err != nil {
		return normalizeSQLiteError(err)
	}
	return nil
}

// ApplyReconcileTx upserts every authoritative field from a flat-storage
// event. Startup reconciliation must repair stale key and timestamp columns as
// well as JSON payloads, so it does not go through the live projectors.
func (s *Store) ApplyReconcileTx(ctx context.Context, tx *sql.Tx, event Event) error {
	data, err := decodeObject(event.Data)
	if err != nil {
		return errors.New("projectors: event data must be an object")
	}
	switch event.Type {
	case EventSessionCreated:
		info, ok := data["info"].(row)
		if !ok {
			return errors.New("projectors: session.created info must be an object")
		}
		values := sessionInsertRow(info)
		for _, column := range []string{
			"workspace_id", "parent_id", "path", "share_url",
			"summary_additions", "summary_deletions", "summary_files", "summary_diffs",
			"revert", "permission", "agent", "model", "time_compacting", "time_archived",
		} {
			if _, exists := values[column]; !exists {
				values[column] = nil
			}
		}
		err = upsertRow(ctx, tx, "session", values, "id", sessionJSONColumns)
	case EventMessageUpdated:
		err = s.reconcileMessageUpdated(ctx, tx, data)
	case EventMessagePartUpdated:
		err = s.reconcilePartUpdated(ctx, tx, data)
	default:
		err = s.ApplyTx(ctx, tx, event)
	}
	if err != nil {
		return normalizeSQLiteError(err)
	}
	return nil
}

func (s *Store) reconcileMessageUpdated(ctx context.Context, tx *sql.Tx, data row) error {
	info, ok := data["info"].(row)
	if !ok {
		return errors.New("projectors: message.updated info must be an object")
	}
	restJSON, err := restJSON(info, "id", "sessionID")
	if err != nil {
		return err
	}
	values := row{
		"id":           stringOf(info["id"]),
		"session_id":   stringOf(info["sessionID"]),
		"time_created": nested(info, "time", "created"),
		"time_updated": float64(s.now()),
		"data":         restJSON,
	}
	return upsertRow(ctx, tx, "message", values, "id", nil)
}

func (s *Store) reconcilePartUpdated(ctx context.Context, tx *sql.Tx, data row) error {
	part, ok := data["part"].(row)
	if !ok {
		return errors.New("projectors: message.part.updated part must be an object")
	}
	restJSON, err := restJSON(part, "id", "messageID", "sessionID")
	if err != nil {
		return err
	}
	values := row{
		"id":           stringOf(part["id"]),
		"message_id":   stringOf(part["messageID"]),
		"session_id":   stringOf(part["sessionID"]),
		"time_created": data["time"],
		"time_updated": float64(s.now()),
		"data":         restJSON,
	}
	return upsertRow(ctx, tx, "part", values, "id", nil)
}

func normalizeSQLiteError(err error) error {
	coded, ok := err.(sqliteCodeError)
	if !ok || coded.Code()&0xff != 19 {
		return err
	}
	message := err.Error()
	const prefix = "constraint failed: "
	if strings.HasPrefix(message, prefix) {
		message = strings.TrimPrefix(message, prefix)
		if open := strings.LastIndex(message, " ("); open >= 0 && strings.HasSuffix(message, ")") {
			message = message[:open]
		}
		return errors.New(message)
	}
	return err
}

func (s *Store) projectSessionCreated(ctx context.Context, tx *sql.Tx, data row) error {
	info, ok := data["info"].(row)
	if !ok {
		return errors.New("projectors: session.created info must be an object")
	}
	return insertRow(ctx, tx, "session", sessionInsertRow(info), sessionJSONColumns)
}

func sessionInsertRow(info row) row {
	values := row{}
	copyField(values, "id", info, "id")
	copyField(values, "project_id", info, "projectID")
	copyField(values, "workspace_id", info, "workspaceID")
	copyField(values, "parent_id", info, "parentID")
	copyField(values, "slug", info, "slug")
	copyField(values, "directory", info, "directory")
	copyField(values, "path", info, "path")
	copyField(values, "title", info, "title")
	copyField(values, "agent", info, "agent")
	copyField(values, "model", info, "model")
	copyField(values, "version", info, "version")
	copyNestedField(values, "share_url", info, "share", "url")
	copyNestedField(values, "summary_additions", info, "summary", "additions")
	copyNestedField(values, "summary_deletions", info, "summary", "deletions")
	copyNestedField(values, "summary_files", info, "summary", "files")
	copyNestedField(values, "summary_diffs", info, "summary", "diffs")
	values["revert"] = info["revert"]
	copyField(values, "permission", info, "permission")
	copyNestedField(values, "time_created", info, "time", "created")
	copyNestedField(values, "time_updated", info, "time", "updated")
	copyNestedField(values, "time_compacting", info, "time", "compacting")
	copyNestedField(values, "time_archived", info, "time", "archived")
	return values
}

func copyField(values row, column string, object row, field string) {
	if value, ok := object[field]; ok {
		values[column] = value
	}
}

func copyNestedField(values row, column string, object row, parent, field string) {
	if inner, ok := object[parent].(row); ok {
		if value, ok := inner[field]; ok {
			values[column] = value
		}
	}
}

// nested returns object[parent][field], or nil when either level is absent.
func nested(object row, parent, field string) any {
	if inner, ok := object[parent].(row); ok {
		return inner[field]
	}
	return nil
}

func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

var sessionJSONColumns = map[string]bool{
	"summary_diffs": true,
	"revert":        true,
	"permission":    true,
	"model":         true,
}

func (s *Store) projectSessionUpdated(ctx context.Context, tx *sql.Tx, data row) error {
	info, ok := data["info"]
	if !ok {
		return errors.New("projectors: session.updated info is required")
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		return err
	}
	partial, err := ToPartialRow(encoded)
	if err != nil {
		return err
	}
	values := row(partial)
	if len(values) == 0 {
		return errors.New("No values to set")
	}
	if _, explicit := values["time_updated"]; !explicit {
		values["time_updated"] = float64(s.now())
	}
	sessionID := stringOf(data["sessionID"])
	affected, err := updateRow(ctx, tx, "session", values, "id", sessionID, sessionJSONColumns)
	if err != nil {
		return err
	}
	if affected == 0 {
		return &NotFoundError{Message: "Session not found: " + sessionID}
	}
	return nil
}

func (s *Store) projectSessionDeleted(ctx context.Context, tx *sql.Tx, data row) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM session WHERE id = ?", stringOf(data["sessionID"]))
	return err
}

func (s *Store) projectMessageUpdated(ctx context.Context, tx *sql.Tx, data row) error {
	info, ok := data["info"].(row)
	if !ok {
		return errors.New("projectors: message.updated info must be an object")
	}
	id := stringOf(info["id"])
	sessionID := stringOf(info["sessionID"])
	restJSON, err := restJSON(info, "id", "sessionID")
	if err != nil {
		return err
	}
	created, err := sqlValue(nested(info, "time", "created"), false)
	if err != nil {
		return err
	}
	now := s.now()
	_, err = tx.ExecContext(ctx, `INSERT INTO message
		(id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data, time_updated = ?`,
		id, sessionID, created, now, restJSON, now)
	if err != nil && isForeignKeyError(err) {
		s.warn(Warning{
			Message: "ignored late message update",
			Fields:  PartialRow{"messageID": id, "sessionID": sessionID},
		})
		return nil
	}
	return err
}

func (s *Store) projectMessageRemoved(ctx context.Context, tx *sql.Tx, data row) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM message WHERE id = ? AND session_id = ?",
		stringOf(data["messageID"]), stringOf(data["sessionID"]))
	return err
}

func (s *Store) projectPartRemoved(ctx context.Context, tx *sql.Tx, data row) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM part WHERE id = ? AND session_id = ?",
		stringOf(data["partID"]), stringOf(data["sessionID"]))
	return err
}

func (s *Store) projectPartUpdated(ctx context.Context, tx *sql.Tx, data row) error {
	part, ok := data["part"].(row)
	if !ok {
		return errors.New("projectors: message.part.updated part must be an object")
	}
	id := stringOf(part["id"])
	messageID := stringOf(part["messageID"])
	sessionID := stringOf(part["sessionID"])
	restJSON, err := restJSON(part, "id", "messageID", "sessionID")
	if err != nil {
		return err
	}
	created, err := sqlValue(data["time"], false)
	if err != nil {
		return err
	}
	now := s.now()
	_, err = tx.ExecContext(ctx, `INSERT INTO part
		(id, message_id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data, time_updated = ?`,
		id, messageID, sessionID, created, now, restJSON, now)
	if err != nil && isForeignKeyError(err) {
		s.warn(Warning{
			Message: "ignored late part update",
			Fields:  PartialRow{"partID": id, "messageID": messageID, "sessionID": sessionID},
		})
		return nil
	}
	return err
}

func isForeignKeyError(err error) bool {
	if coded, ok := err.(sqliteCodeError); ok && coded.Code() == 787 {
		return true
	}
	return strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

// decodeObject decodes a JSON object into a map; anything else is an error.
func decodeObject(data []byte) (row, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	object, ok := value.(row)
	if !ok {
		return nil, errors.New("projectors: expected a JSON object")
	}
	return object, nil
}

// restJSON encodes object without the named keys, which live in their own
// columns.
func restJSON(object row, omit ...string) (string, error) {
	rest := make(row, len(object))
	for key, value := range object {
		rest[key] = value
	}
	for _, key := range omit {
		delete(rest, key)
	}
	encoded, err := jsonutil.Marshal(rest)
	return string(encoded), err
}

// sqlValue converts a decoded JSON value into a SQLite parameter. Objects and
// arrays are stored as compact JSON; asJSON forces that for scalars too.
func sqlValue(value any, asJSON bool) (any, error) {
	if value == nil {
		return nil, nil
	}
	if !asJSON {
		switch typed := value.(type) {
		case bool, float64, string:
			return typed, nil
		}
	}
	encoded, err := jsonutil.Marshal(value)
	return string(encoded), err
}

func sortedColumns(values row) []string {
	columns := make([]string, 0, len(values))
	for column := range values {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	return columns
}

func insertRow(ctx context.Context, tx *sql.Tx, table string, values row, jsonColumns map[string]bool) error {
	columns := sortedColumns(values)
	placeholders := make([]string, len(columns))
	args := make([]any, len(columns))
	for index, column := range columns {
		placeholders[index] = "?"
		value, err := sqlValue(values[column], jsonColumns[column])
		if err != nil {
			return err
		}
		args[index] = value
	}
	statement := "INSERT INTO " + table + " (" + strings.Join(columns, ", ") + ") VALUES (" +
		strings.Join(placeholders, ", ") + ")"
	_, err := tx.ExecContext(ctx, statement, args...)
	return err
}

func upsertRow(ctx context.Context, tx *sql.Tx, table string, values row, conflictColumn string, jsonColumns map[string]bool) error {
	columns := sortedColumns(values)
	placeholders := make([]string, len(columns))
	updates := make([]string, 0, len(columns))
	args := make([]any, len(columns))
	for index, column := range columns {
		placeholders[index] = "?"
		value, err := sqlValue(values[column], jsonColumns[column])
		if err != nil {
			return err
		}
		args[index] = value
		if column != conflictColumn {
			updates = append(updates, column+" = excluded."+column)
		}
	}
	statement := "INSERT INTO " + table + " (" + strings.Join(columns, ", ") + ") VALUES (" +
		strings.Join(placeholders, ", ") + ") ON CONFLICT(" + conflictColumn + ") "
	if len(updates) == 0 {
		statement += "DO NOTHING"
	} else {
		statement += "DO UPDATE SET " + strings.Join(updates, ", ")
	}
	_, err := tx.ExecContext(ctx, statement, args...)
	return err
}

func updateRow(ctx context.Context, tx *sql.Tx, table string, values row, whereColumn string, whereValue any, jsonColumns map[string]bool) (int64, error) {
	columns := sortedColumns(values)
	sets := make([]string, len(columns))
	args := make([]any, 0, len(columns)+1)
	for index, column := range columns {
		sets[index] = column + " = ?"
		value, err := sqlValue(values[column], jsonColumns[column])
		if err != nil {
			return 0, err
		}
		args = append(args, value)
	}
	args = append(args, whereValue)
	result, err := tx.ExecContext(ctx,
		"UPDATE "+table+" SET "+strings.Join(sets, ", ")+" WHERE "+whereColumn+" = ?",
		args...,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
