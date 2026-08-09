// Legacy SQLite projector chain — port of src/session/projectors.ts:1-143
// (swe-pro 3b25a1a). Each Apply call is transactional, matching
// SyncEvent.process rather than exposing the individual SQL statements.
package projectors

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
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

// StoreOptions supplies the only ambient effects read by the source module.
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

func projectorTypes() []string {
	types := []string{
		EventSessionCreated,
		EventSessionUpdated,
		EventSessionDeleted,
		EventMessageUpdated,
		EventMessageRemoved,
		EventMessagePartRemoved,
		EventMessagePartUpdated,
	}
	return append(types, nextProjectorTypes()...)
}

// NotFoundError mirrors the NamedError thrown by session.updated. Its
// JavaScript Error.message is the name, while the useful detail is a property.
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string { return "NotFoundError" }

// ToPartialRow maps a JSON Session.Patch into the snake_case update row. Field
// order and whole-nested-null expansion match the grab/Object.fromEntries
// implementation in projectors.ts.
func ToPartialRow(info json.RawMessage) (PartialRow, error) {
	value, err := parseOrderedJSON(info)
	if err != nil {
		return PartialRow{}, err
	}
	if value.kind != jsonObject {
		return PartialRow{}, errors.New("projectors: session patch must be an object")
	}
	out := newJSONObject()
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
		item, ok := grab(value.o, field.source, field.nested)
		if ok {
			out.set(field.column, item)
		}
	}
	return PartialRow{value: objectJSON(out)}, nil
}

func grab(object *jsonObjectValue, field, nested string) (jsonValue, bool) {
	value, ok := object.get(field)
	if !ok {
		return jsonValue{}, false
	}
	if nested != "" && (value.kind == jsonObject || value.kind == jsonArray) {
		if value.kind == jsonObject {
			return value.o.get(nested)
		}
		return jsonValue{}, false
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
	data, err := parseOrderedJSON(event.Data)
	if err != nil {
		return err
	}
	if data.kind != jsonObject {
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
		if isNextProjector(event.Type) {
			err = s.projectNext(ctx, tx, event, data)
		} else {
			err = fmt.Errorf("Projector not found for event: %s", event.Type)
		}
	}
	if err != nil {
		return normalizeSQLiteError(err)
	}
	return nil
}

// ApplyReconcileTx upserts every authoritative field from a flat-storage
// event. Unlike the live TS projector, startup reconciliation must repair
// stale key and timestamp columns as well as JSON payloads.
func (s *Store) ApplyReconcileTx(ctx context.Context, tx *sql.Tx, event Event) error {
	data, err := parseOrderedJSON(event.Data)
	if err != nil {
		return err
	}
	if data.kind != jsonObject {
		return errors.New("projectors: event data must be an object")
	}
	switch event.Type {
	case EventSessionCreated:
		info, ok := objectField(data, "info")
		if !ok || info.kind != jsonObject {
			return errors.New("projectors: session.created info must be an object")
		}
		row := sessionInsertRow(info)
		for _, column := range []string{
			"workspace_id", "parent_id", "path", "share_url",
			"summary_additions", "summary_deletions", "summary_files", "summary_diffs",
			"revert", "permission", "agent", "model", "time_compacting", "time_archived",
		} {
			if _, exists := row.get(column); !exists {
				row.set(column, nullJSON())
			}
		}
		err = upsertObjectRow(ctx, tx, "session", row, "id", sessionJSONColumns)
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

func (s *Store) reconcileMessageUpdated(ctx context.Context, tx *sql.Tx, data jsonValue) error {
	info, ok := objectField(data, "info")
	if !ok || info.kind != jsonObject {
		return errors.New("projectors: message.updated info must be an object")
	}
	id, _ := stringField(info, "id")
	sessionID, _ := stringField(info, "sessionID")
	timeCreated, _ := nestedField(info, "time", "created")
	rest := info.o.clone()
	rest.delete("id")
	rest.delete("sessionID")
	restJSON, err := objectJSON(rest).compactString()
	if err != nil {
		return err
	}
	row := newJSONObject()
	row.set("id", stringJSON(id))
	row.set("session_id", stringJSON(sessionID))
	row.set("time_created", timeCreated)
	row.set("time_updated", numberJSON(float64(s.now())))
	row.set("data", stringJSON(restJSON))
	return upsertObjectRow(ctx, tx, "message", row, "id", nil)
}

func (s *Store) reconcilePartUpdated(ctx context.Context, tx *sql.Tx, data jsonValue) error {
	part, ok := objectField(data, "part")
	if !ok || part.kind != jsonObject {
		return errors.New("projectors: message.part.updated part must be an object")
	}
	id, _ := stringField(part, "id")
	messageID, _ := stringField(part, "messageID")
	sessionID, _ := stringField(part, "sessionID")
	timeCreated, _ := objectField(data, "time")
	rest := part.o.clone()
	rest.delete("id")
	rest.delete("messageID")
	rest.delete("sessionID")
	restJSON, err := objectJSON(rest).compactString()
	if err != nil {
		return err
	}
	row := newJSONObject()
	row.set("id", stringJSON(id))
	row.set("message_id", stringJSON(messageID))
	row.set("session_id", stringJSON(sessionID))
	row.set("time_created", timeCreated)
	row.set("time_updated", numberJSON(float64(s.now())))
	row.set("data", stringJSON(restJSON))
	return upsertObjectRow(ctx, tx, "part", row, "id", nil)
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

func (s *Store) projectSessionCreated(ctx context.Context, tx *sql.Tx, data jsonValue) error {
	info, ok := objectField(data, "info")
	if !ok || info.kind != jsonObject {
		return errors.New("projectors: session.created info must be an object")
	}
	row := sessionInsertRow(info)
	return insertObjectRow(ctx, tx, "session", row, sessionJSONColumns)
}

func sessionInsertRow(info jsonValue) *jsonObjectValue {
	row := newJSONObject()
	copyField(row, "id", info, "id")
	copyField(row, "project_id", info, "projectID")
	copyField(row, "workspace_id", info, "workspaceID")
	copyField(row, "parent_id", info, "parentID")
	copyField(row, "slug", info, "slug")
	copyField(row, "directory", info, "directory")
	copyField(row, "path", info, "path")
	copyField(row, "title", info, "title")
	copyField(row, "agent", info, "agent")
	copyField(row, "model", info, "model")
	copyField(row, "version", info, "version")
	copyNestedField(row, "share_url", info, "share", "url")
	copyNestedField(row, "summary_additions", info, "summary", "additions")
	copyNestedField(row, "summary_deletions", info, "summary", "deletions")
	copyNestedField(row, "summary_files", info, "summary", "files")
	copyNestedField(row, "summary_diffs", info, "summary", "diffs")
	if revert, ok := objectField(info, "revert"); ok && revert.kind != jsonNull {
		row.set("revert", revert)
	} else {
		row.set("revert", nullJSON())
	}
	copyField(row, "permission", info, "permission")
	copyNestedField(row, "time_created", info, "time", "created")
	copyNestedField(row, "time_updated", info, "time", "updated")
	copyNestedField(row, "time_compacting", info, "time", "compacting")
	copyNestedField(row, "time_archived", info, "time", "archived")
	return row
}

func copyField(row *jsonObjectValue, column string, object jsonValue, field string) {
	if value, ok := objectField(object, field); ok {
		row.set(column, value)
	}
}

func copyNestedField(row *jsonObjectValue, column string, object jsonValue, parent, field string) {
	if value, ok := nestedField(object, parent, field); ok {
		row.set(column, value)
	}
}

var sessionJSONColumns = map[string]bool{
	"summary_diffs": true,
	"revert":        true,
	"permission":    true,
	"model":         true,
}

func (s *Store) projectSessionUpdated(ctx context.Context, tx *sql.Tx, data jsonValue) error {
	info, ok := objectField(data, "info")
	if !ok {
		return errors.New("projectors: session.updated info is required")
	}
	encoded, err := info.MarshalJSON()
	if err != nil {
		return err
	}
	partial, err := ToPartialRow(encoded)
	if err != nil {
		return err
	}
	row := partial.object().clone()
	if len(row.keys) == 0 {
		return errors.New("No values to set")
	}
	if _, explicit := row.get("time_updated"); !explicit {
		row.set("time_updated", numberJSON(float64(s.now())))
	}
	sessionID, _ := stringField(data, "sessionID")
	affected, err := updateObjectRow(ctx, tx, "session", row, "id", sessionID, sessionJSONColumns)
	if err != nil {
		return err
	}
	if affected == 0 {
		return &NotFoundError{Message: "Session not found: " + sessionID}
	}
	return nil
}

func (s *Store) projectSessionDeleted(ctx context.Context, tx *sql.Tx, data jsonValue) error {
	sessionID, _ := stringField(data, "sessionID")
	_, err := tx.ExecContext(ctx, "DELETE FROM session WHERE id = ?", sessionID)
	return err
}

func (s *Store) projectMessageUpdated(ctx context.Context, tx *sql.Tx, data jsonValue) error {
	info, ok := objectField(data, "info")
	if !ok || info.kind != jsonObject {
		return errors.New("projectors: message.updated info must be an object")
	}
	id, _ := stringField(info, "id")
	sessionID, _ := stringField(info, "sessionID")
	timeCreated, _ := nestedField(info, "time", "created")
	rest := info.o.clone()
	rest.delete("id")
	rest.delete("sessionID")
	restJSON, err := objectJSON(rest).compactString()
	if err != nil {
		return err
	}
	created, err := sqlValue(timeCreated, false)
	if err != nil {
		return err
	}
	now := s.now()
	_, err = tx.ExecContext(ctx, `INSERT INTO message
		(id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data, time_updated = ?`,
		id, sessionID, created, now, restJSON, now)
	if err != nil && isForeignKeyError(err) {
		fields := newJSONObject()
		fields.set("messageID", stringJSON(id))
		fields.set("sessionID", stringJSON(sessionID))
		s.warn(Warning{
			Message: "ignored late message update",
			Fields:  PartialRow{value: objectJSON(fields)},
		})
		return nil
	}
	return err
}

func (s *Store) projectMessageRemoved(ctx context.Context, tx *sql.Tx, data jsonValue) error {
	messageID, _ := stringField(data, "messageID")
	sessionID, _ := stringField(data, "sessionID")
	_, err := tx.ExecContext(ctx, "DELETE FROM message WHERE id = ? AND session_id = ?", messageID, sessionID)
	return err
}

func (s *Store) projectPartRemoved(ctx context.Context, tx *sql.Tx, data jsonValue) error {
	partID, _ := stringField(data, "partID")
	sessionID, _ := stringField(data, "sessionID")
	_, err := tx.ExecContext(ctx, "DELETE FROM part WHERE id = ? AND session_id = ?", partID, sessionID)
	return err
}

func (s *Store) projectPartUpdated(ctx context.Context, tx *sql.Tx, data jsonValue) error {
	part, ok := objectField(data, "part")
	if !ok || part.kind != jsonObject {
		return errors.New("projectors: message.part.updated part must be an object")
	}
	id, _ := stringField(part, "id")
	messageID, _ := stringField(part, "messageID")
	sessionID, _ := stringField(part, "sessionID")
	timeCreated, _ := objectField(data, "time")
	rest := part.o.clone()
	rest.delete("id")
	rest.delete("messageID")
	rest.delete("sessionID")
	restJSON, err := objectJSON(rest).compactString()
	if err != nil {
		return err
	}
	created, err := sqlValue(timeCreated, false)
	if err != nil {
		return err
	}
	now := s.now()
	_, err = tx.ExecContext(ctx, `INSERT INTO part
		(id, message_id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data, time_updated = ?`,
		id, messageID, sessionID, created, now, restJSON, now)
	if err != nil && isForeignKeyError(err) {
		fields := newJSONObject()
		fields.set("partID", stringJSON(id))
		fields.set("messageID", stringJSON(messageID))
		fields.set("sessionID", stringJSON(sessionID))
		s.warn(Warning{
			Message: "ignored late part update",
			Fields:  PartialRow{value: objectJSON(fields)},
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

func insertObjectRow(
	ctx context.Context,
	tx *sql.Tx,
	table string,
	row *jsonObjectValue,
	jsonColumns map[string]bool,
) error {
	columns := append([]string(nil), row.keys...)
	placeholders := make([]string, len(columns))
	values := make([]any, len(columns))
	for index, column := range columns {
		placeholders[index] = "?"
		value, err := sqlValue(row.vals[column], jsonColumns[column])
		if err != nil {
			return err
		}
		values[index] = value
	}
	statement := "INSERT INTO " + table + " (" + strings.Join(columns, ", ") + ") VALUES (" +
		strings.Join(placeholders, ", ") + ")"
	_, err := tx.ExecContext(ctx, statement, values...)
	return err
}

func upsertObjectRow(
	ctx context.Context,
	tx *sql.Tx,
	table string,
	row *jsonObjectValue,
	conflictColumn string,
	jsonColumns map[string]bool,
) error {
	columns := append([]string(nil), row.keys...)
	placeholders := make([]string, len(columns))
	updates := make([]string, 0, len(columns)-1)
	values := make([]any, len(columns))
	for index, column := range columns {
		placeholders[index] = "?"
		value, err := sqlValue(row.vals[column], jsonColumns[column])
		if err != nil {
			return err
		}
		values[index] = value
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
	_, err := tx.ExecContext(ctx, statement, values...)
	return err
}

func updateObjectRow(
	ctx context.Context,
	tx *sql.Tx,
	table string,
	row *jsonObjectValue,
	whereColumn string,
	whereValue any,
	jsonColumns map[string]bool,
) (int64, error) {
	sets := make([]string, len(row.keys))
	values := make([]any, 0, len(row.keys)+1)
	for index, column := range row.keys {
		sets[index] = column + " = ?"
		value, err := sqlValue(row.vals[column], jsonColumns[column])
		if err != nil {
			return 0, err
		}
		values = append(values, value)
	}
	values = append(values, whereValue)
	result, err := tx.ExecContext(ctx,
		"UPDATE "+table+" SET "+strings.Join(sets, ", ")+" WHERE "+whereColumn+" = ?",
		values...,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
