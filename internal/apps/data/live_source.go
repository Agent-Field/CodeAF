package data

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// LiveBinding resolves the grants and type from the same schema before a
// trusted connector uses the crosswalk. Drafts have their own sample store.
func (s *Store) LiveBinding(sc Scope, alias string, write bool) (TypeBinding, *Type, error) {
	sch := s.schema.Load()
	binding, ok := sch.bindings[sc.App][alias]
	if !ok || binding.Common == "" || sc.App == "" || s.draft != "" {
		return TypeBinding{}, nil, errorf(CodeNotAllowed, "a live read needs a published type binding")
	}
	p, err := readPublication(s.rdb, sc.App)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TypeBinding{}, nil, err
	}
	if p.Archived {
		return TypeBinding{}, nil, errorf(CodeNotAllowed, "this app is archived")
	}
	t, err := sch.allow(sc, alias, write)
	if err != nil {
		return TypeBinding{}, nil, err
	}
	if write && (t.Ledger || t.Lifecycle != nil || len(t.Freeze) != 0) {
		return TypeBinding{}, nil, errorf(CodeNotAllowed, "this record type needs its native write rules")
	}
	return binding, t, nil
}

// LiveField limits the first source mapping to ordinary scalar fields. The
// source's schema owns constraints such as uniqueness for those fields.
func (t *Type) LiveField(name string, write bool) (*Field, error) {
	f := t.field(name)
	if f == nil || f.Computed != "" || (write && (f.AI != "" || f.Sequence != nil || f.Masked)) {
		return nil, fieldErr(name, "cannot be mapped to this source operation")
	}
	switch f.Type {
	case TypeText, TypeLongtext, TypeNumber, TypeBool, TypeEnum, TypeEmail, TypePhone, TypeURL, TypeDate, TypeDatetime:
		return f, nil
	default:
		return nil, fieldErr(name, "needs a source mapping for its field type")
	}
}

// LiveValue applies the same scalar rules and mask as local record reads.
func (f *Field) LiveValue(value any, write bool) (any, error) {
	if f.Masked && !write && value != nil {
		return masked, nil
	}
	return f.liveValue(value, write)
}

// LiveReadValue normalizes a source value before the connection evaluates policy.
func (f *Field) LiveReadValue(value any) (any, error) {
	return f.liveValue(value, false)
}

func (f *Field) liveValue(value any, write bool) (any, error) {
	// Relative dates describe a person's entry, never a value held by a source.
	if text, ok := value.(string); ok && (f.Type == TypeDate || f.Type == TypeDatetime) {
		if v := strings.ToLower(strings.TrimSpace(text)); v == "$now" || v == "$today" || v == "today" || v == "now" {
			return nil, fieldErr(f.Name, "needs an absolute source date")
		}
	}
	out, err := f.coerce(value, coercion{now: time.Now(), lenient: !write})
	if err != nil || write && out == nil && f.Required {
		return nil, fieldErr(f.Name, "does not match the bound field")
	}
	return out, nil
}
