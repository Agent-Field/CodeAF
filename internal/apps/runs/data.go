package runs

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/Agent-Field/codeaf/internal/apps/data"
)

// Data connects a run to either the person's store or a draft sample store.
// Both go through the same app capability, mask and record-rule checks.
type Data struct {
	Store func() (*data.Store, error)
}

func (d Data) Write(ctx context.Context, call RecordCall) (Reply, error) {
	if err := ctx.Err(); err != nil {
		return Reply{}, err
	}
	store, err := d.Store()
	if err != nil {
		return Reply{}, err
	}
	scope := data.Scope{App: call.App, Flow: true, Actor: "run:" + call.ID, Cause: call.Step}
	var result *data.WriteResult
	switch call.Name {
	case "records.create":
		result, err = store.Create(scope, call.Type, []map[string]any{call.Set})
	case "records.update":
		result, err = store.Update(scope, call.Type, call.IDs, call.Set, data.UpdateOptions{})
	default:
		err = errors.New("unknown run record action")
	}
	if err != nil {
		var refused *data.Error
		if !errors.As(err, &refused) {
			err = errors.Join(ErrOutcomeUnknown, err)
		}
		return Reply{}, err
	}
	return Reply{Output: map[string]any{"batch": result.Batch, "rows": result.Rows}}, nil
}

// ResolveParams resolves declared record references through the run's app
// scope. References always come from the selected store, including when a
// client sent a row object instead of an id.
func (d Data) ResolveParams(app string, declared map[string]Parameter, provided map[string]any) (map[string]any, error) {
	out := maps.Clone(provided)
	scope := data.Scope{App: app, Agent: true}
	for name, parameter := range declared {
		value, ok := provided[name]
		if parameter.To == "" || !ok {
			continue
		}
		store, err := d.Store()
		if err != nil {
			return nil, err
		}
		reader := refReader{store: store, scope: scope, remaining: maxReferenceReads}
		switch parameter.Type {
		case data.TypeRef:
			out[name], err = reader.get(parameter.To, value, refDepth)
		case data.TypeRefs:
			out[name], err = reader.many(parameter.To, value)
		}
		if err != nil {
			return nil, fmt.Errorf("parameter %s: %w", name, err)
		}
	}
	return out, nil
}

// ReadRecords exposes the app-scoped row list used by the records expression.
func (d Data) ReadRecords(app string, recordType string) ([]any, error) {
	store, err := d.Store()
	if err != nil {
		return nil, err
	}
	reader := refReader{store: store, scope: data.Scope{App: app, Agent: true}, remaining: maxReferenceReads}
	return reader.many(recordType, "")
}

// RecordInput resolves a durable event's scoped history before evaluating the
// trigger or saving its input. Live callers may still supply their own rows.
func (d Data) RecordInput(app string, change Change) (Change, error) {
	store, err := d.Store()
	if err != nil {
		return Change{}, err
	}
	scope := data.Scope{App: app, Agent: true}
	typ, err := store.Type(scope, change.Type)
	if err != nil {
		var e *data.Error
		if errors.As(err, &e) && e.Code == data.CodeNotAllowed {
			return Change{}, ErrNotFound
		}
		return Change{}, err
	}
	if change.TypeID != "" {
		if typ.ID != change.TypeID {
			return Change{}, ErrNotFound
		}
		change.Before, change.After, err = store.RecordEventValues(scope, change.TypeID, change.ID, change.Batch)
		if err != nil {
			if e, ok := data.AsError(err); ok && (e.Code == data.CodeNotAllowed || e.Code == data.CodeNotFound) {
				return Change{}, ErrNotFound
			}
			return Change{}, err
		}
	}
	filter := func(row map[string]any) (map[string]any, error) {
		if row == nil {
			return nil, nil
		}
		reader := refReader{store: store, scope: scope, remaining: maxReferenceReads}
		return reader.project(typ, row, refDepth)
	}
	change.Before, err = filter(change.Before)
	if err == nil {
		change.After, err = filter(change.After)
	}
	return change, err
}

const (
	maxReferenceReads = 512
	refDepth          = 2
)

type refReader struct {
	store     *data.Store
	scope     data.Scope
	remaining int
}

func (r *refReader) many(typ string, v any) ([]any, error) {
	rows := []any{}
	switch value := v.(type) {
	case string:
		cursor := ""
		for {
			page, err := r.store.Query(r.scope, typ, value, cursor, 100)
			if err != nil {
				return nil, err
			}
			meta, err := r.store.Type(r.scope, typ)
			if err != nil {
				return nil, err
			}
			if page.Total > maxReferenceReads || len(rows)+len(page.Rows) > maxReferenceReads {
				return nil, errors.New("record query returned too many rows for a run parameter")
			}
			for _, row := range page.Rows {
				projected, err := r.project(meta, row, refDepth)
				if err != nil {
					return nil, err
				}
				rows = append(rows, projected)
			}
			if page.Next == "" {
				return rows, nil
			}
			cursor = page.Next
		}
	case []any:
		for _, item := range value {
			row, err := r.get(typ, item, refDepth)
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		}
		return rows, nil
	default:
		return nil, errors.New("a refs parameter needs record ids or a record query")
	}
}

func (r *refReader) get(typ string, value any, depth int) (map[string]any, error) {
	id, _ := value.(string)
	if row, ok := value.(map[string]any); ok {
		id, _ = row["id"].(string)
	}
	if id == "" {
		return nil, errors.New("a reference needs a record id")
	}
	if r.remaining == 0 {
		return nil, errors.New("a run parameter follows too many record references")
	}
	r.remaining--
	meta, err := r.store.Type(r.scope, typ)
	if err != nil {
		return nil, err
	}
	row, err := r.store.Get(r.scope, typ, id, false)
	if err != nil {
		return nil, err
	}
	return r.project(meta, row, depth)
}

func (r *refReader) project(typ *data.Type, row map[string]any, depth int) (map[string]any, error) {
	fields := map[string]any{"id": row["id"], "type": typ.Name}
	for _, name := range []string{"created_at", "updated_at"} {
		if value, ok := row[name]; ok {
			fields[name] = value
		}
	}
	for _, name := range typ.Order {
		field := typ.Fields[name]
		value, found := row[name]
		if !found || field.Hidden || field.Masked {
			continue
		}
		fields[name] = value
		if depth <= 0 || value == nil || (field.Type != data.TypeRef && field.Type != data.TypeRefs) {
			continue
		}
		if _, err := r.store.Type(r.scope, field.To); err != nil {
			var e *data.Error
			if errors.As(err, &e) && e.Code == data.CodeNotAllowed {
				continue
			}
			return nil, err
		}
		if field.Type == data.TypeRef {
			if id, ok := value.(string); !ok || id == "" {
				continue
			}
			projected, err := r.dereference(field.To, value, depth-1)
			if err != nil {
				return nil, err
			}
			fields[name] = projected
		} else {
			var ids []any
			switch list := value.(type) {
			case []any:
				ids = list
			case []string:
				for _, id := range list {
					ids = append(ids, id)
				}
			}
			values := make([]any, 0, len(ids))
			for _, id := range ids {
				projected, err := r.dereference(field.To, id, depth-1)
				if err != nil {
					return nil, err
				}
				values = append(values, projected)
			}
			fields[name] = values
		}
	}
	return fields, nil
}

// Unrelated selectors can still use a row with a deleted reference. The
// dangling id stays a scalar, so dereferencing a field on it fails normally.
func (r *refReader) dereference(typ string, id any, depth int) (any, error) {
	row, err := r.get(typ, id, depth)
	var refused *data.Error
	if errors.As(err, &refused) && refused.Code == data.CodeNotFound {
		return id, nil
	}
	return row, err
}
