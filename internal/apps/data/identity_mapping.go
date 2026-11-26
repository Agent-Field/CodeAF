package data

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const identityMigration = `
	CREATE TABLE identity_mappings (
		binding TEXT NOT NULL, object TEXT NOT NULL, epoch INTEGER NOT NULL,
		type_id TEXT NOT NULL REFERENCES types(id), definition TEXT NOT NULL,
		PRIMARY KEY(binding, object, epoch)
	);
	CREATE TABLE identity_aliases (
		alias TEXT PRIMARY KEY, canonical TEXT NOT NULL, merge_id TEXT NOT NULL UNIQUE
	);
	CREATE INDEX identity_aliases_canonical ON identity_aliases(canonical);
	CREATE TABLE identity_merges (
		id TEXT PRIMARY KEY, type_id TEXT NOT NULL, canonical TEXT NOT NULL, alias TEXT NOT NULL,
		before_json TEXT NOT NULL, canonical_rev INTEGER NOT NULL, alias_rev INTEGER NOT NULL,
		evidence TEXT NOT NULL, actor TEXT NOT NULL, at TEXT NOT NULL,
		split_by TEXT NOT NULL DEFAULT '', split_at TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE identity_attestations (
		record TEXT PRIMARY KEY, rev INTEGER NOT NULL
	);
	CREATE TABLE identity_matches (
		id TEXT PRIMARY KEY REFERENCES work_items(id), type_id TEXT NOT NULL,
		left_id TEXT NOT NULL, right_id TEXT NOT NULL, left_rev INTEGER NOT NULL, right_rev INTEGER NOT NULL,
		evidence TEXT NOT NULL, mapping_epoch INTEGER NOT NULL, decision TEXT NOT NULL DEFAULT '', merge_id TEXT NOT NULL DEFAULT ''
	);
	CREATE UNIQUE INDEX identity_match_current ON identity_matches(left_id, right_id) WHERE decision = '';
	CREATE INDEX identity_match_rejections ON identity_matches(left_id, right_id, decision);
`

// MappingField describes a stable target field, its source column, and handling
// of explicit nulls. Missing source columns leave the previous claim alone.
type MappingField struct {
	Source     string             `json:"source"`
	Target     string             `json:"target"`
	Transforms []MappingTransform `json:"transforms,omitempty"`
	Clear      bool               `json:"clear,omitempty"`
	Class      string             `json:"class,omitempty"`
}

// SourceMapping pins a connection object to a common type and the fields that
// may be used to look for duplicates. Identity roles can name local aliases.
type SourceMapping struct {
	Binding string         `json:"binding"`
	Object  string         `json:"object"`
	Common  CommonTypeKey  `json:"common"`
	Fields  []MappingField `json:"fields"`
	Match   IdentityFields `json:"match,omitzero"`
}

type MappingTransform struct {
	Op  string `json:"op"`
	Arg string `json:"arg,omitempty"`
}

var (
	decimalShape  = regexp.MustCompile(`^[+-]?[0-9]+(?:\.[0-9]+)?$`)
	currencyShape = regexp.MustCompile(`^[A-Z]{3}$`)
	dialShape     = regexp.MustCompile(`^[1-9][0-9]{0,2}$`)
	domainShape   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$`)
)

func (x MappingTransform) valid() bool {
	switch x.Op {
	case "trim", "lower", "upper", "email", "domain", "currency":
		return x.Arg == ""
	case "phone":
		return x.Arg == "" || dialShape.MatchString(x.Arg)
	case "money":
		return currencyShape.MatchString(x.Arg)
	case "date", "datetime":
		return len(x.Arg) <= 80
	}
	return false
}

func (x MappingTransform) apply(input any) (any, error) {
	value, ok := input.(string)
	if !ok || len(value) > 4096 || !x.valid() {
		return nil, fmt.Errorf("%s requires a supported transform and a short text value", x.Op)
	}
	switch x.Op {
	case "trim":
		return strings.TrimSpace(value), nil
	case "lower":
		return strings.ToLower(value), nil
	case "upper":
		return strings.ToUpper(value), nil
	case "email":
		if value = normalizedEmail(value); value != "" {
			return value, nil
		}
	case "phone":
		if value = normalizedPhone(value, x.Arg); value != "" {
			return value, nil
		}
	case "domain":
		if value = normalizedDomain(value); value != "" {
			return value, nil
		}
	case "currency":
		if value = strings.ToUpper(strings.TrimSpace(value)); currencyShape.MatchString(value) {
			return value, nil
		}
	case "money":
		parts := strings.Fields(value)
		if len(parts) == 2 && strings.EqualFold(parts[0], x.Arg) {
			parts = parts[1:]
		}
		if len(parts) == 1 && len(parts[0]) <= 100 && decimalShape.MatchString(parts[0]) {
			return parts[0], nil
		}
	case "date", "datetime":
		layout := x.Arg
		if layout == "" {
			layout = time.DateOnly
			if x.Op == "datetime" {
				layout = time.RFC3339Nano
			}
		}
		if at, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			if x.Op == "date" {
				return at.Format(time.DateOnly), nil
			}
			return at.UTC().Format(time.RFC3339Nano), nil
		}
	}
	return nil, fmt.Errorf("value cannot be converted by %s", x.Op)
}

func normalizedEmail(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if addr, err := mail.ParseAddress(value); err == nil && addr.Address == value && addr.Name == "" {
		return value
	}
	return ""
}

func normalizedDomain(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	u, err := url.Parse(value)
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	host := strings.TrimPrefix(strings.TrimSuffix(u.Hostname(), "."), "www.")
	if len(host) <= 253 && domainShape.MatchString(host) {
		return host
	}
	return ""
}

func normalizedPhone(value, dial string) string {
	value = strings.TrimSpace(value)
	var digits strings.Builder
	for _, c := range strings.TrimPrefix(value, "+") {
		switch {
		case c >= '0' && c <= '9':
			digits.WriteRune(c)
		case strings.ContainsRune(" -().\u00a0", c):
		default:
			return ""
		}
	}
	number := digits.String()
	if !strings.HasPrefix(value, "+") {
		if dial == "" || number == "" || number[0] == '0' {
			return ""
		}
		number = dial + number
	}
	if len(number) < 8 || len(number) > 15 || number[0] == '0' {
		return ""
	}
	return "+" + number
}

// InstallSourceMapping commits the rule and its mapping epoch together so an
// observation cannot see the epoch without the exact rule it fences.
func (s *Store) InstallSourceMapping(m SourceMapping, expected TypeEpochs) (TypeEpochs, error) {
	next := expected
	next.Mapping++
	err := s.write(func(w *writeTx) error {
		t := w.schema.common[m.Common]
		key := ExternalKey{Binding: m.Binding, Object: m.Object, Key: "mapping", Generation: 1}
		if w.s.draft != "" || t == nil || !key.valid() || len(m.Fields) == 0 || len(m.Fields) > 200 {
			return errorf(CodeInvalid, "a mapping needs a published common type, source object and fields")
		}
		m.Fields = append([]MappingField(nil), m.Fields...)
		targets := map[string]bool{}
		for i, rule := range m.Fields {
			f := t.field(rule.Target)
			if f == nil || f.Computed != "" || f.Sequence != nil || t.Lifecycle != nil && t.Lifecycle.Field == f.Name ||
				rule.Source == "" || len(rule.Source) > 1024 || len(rule.Class) > 200 || len(rule.Transforms) > 8 || targets[f.ID] {
				return fieldErr(rule.Target, "mapping needs a unique writable field and a source column")
			}
			targets[f.ID] = true
			m.Fields[i].Target = f.ID
			for _, transform := range rule.Transforms {
				if !transform.valid() || transform.Op == "money" && f.Type == TypeMoney && transform.Arg != f.Currency {
					return fieldErr(f.Name, "invalid transform or currency")
				}
			}
		}
		if err := m.Match.bind(t); err != nil {
			return err
		}
		var priorType string
		err := w.tx.QueryRow(`SELECT type_id FROM identity_mappings WHERE binding = ? AND object = ? ORDER BY epoch DESC LIMIT 1`, m.Binding, m.Object).Scan(&priorType)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if priorType != "" && priorType != t.ID {
			return errorf(CodeInvalid, "a source object cannot change its common type")
		}
		if err := w.advanceEpochs(t.ID, expected, next); err != nil {
			return err
		}
		_, err = w.tx.Exec(`INSERT INTO identity_mappings VALUES (?, ?, ?, ?, ?)`, m.Binding, m.Object, next.Mapping, t.ID, canonicalJSON(m))
		return err
	})
	if err != nil {
		return TypeEpochs{}, err
	}
	return next, nil
}

func (w *writeTx) sourceMapping(key ExternalKey, epochs TypeEpochs) (SourceMapping, int64, error) {
	var m SourceMapping
	var raw, typeID string
	var version int64
	err := w.tx.QueryRow(`SELECT definition, epoch, type_id FROM identity_mappings
		WHERE binding = ? AND object = ? ORDER BY epoch DESC LIMIT 1`, key.Binding, key.Object).Scan(&raw, &version, &typeID)
	if errors.Is(err, sql.ErrNoRows) {
		return m, 0, errorf(CodeNotFound, "there is no mapping for this source object")
	}
	if err != nil {
		return m, 0, err
	}
	current, err := readEpochs(w.tx, typeID)
	if err != nil {
		return m, 0, err
	}
	if current != epochs {
		return m, 0, errorf(CodeStale, "the source observation uses old type epochs")
	}
	return m, version, json.Unmarshal([]byte(raw), &m)
}

func (m SourceMapping) claims(values map[string]any) (map[string]SourceFieldClaim, error) {
	claims := map[string]SourceFieldClaim{}
	for _, rule := range m.Fields {
		v, present := values[rule.Source]
		if !present || v == nil && !rule.Clear {
			continue
		}
		if v != nil {
			for _, transform := range rule.Transforms {
				var err error
				v, err = transform.apply(v)
				if err != nil {
					return nil, fieldErr(rule.Target, "%v", err)
				}
			}
		}
		claims[rule.Target] = SourceFieldClaim{Path: rule.Source, Value: v, Class: rule.Class}
	}
	return claims, nil
}

// MappedSourceObservation is the input from a trusted source adapter. Live
// and ACL-bound readers leave NativeValues false and do not run matching.
type MappedSourceObservation struct {
	SourceObservation
	Values            map[string]any
	VerifiedWorkEmail bool
}

// ObserveMappedSource uses the source's typed key to commit a mapping, its
// claims and a first match assessment under the same writer transaction.
func (s *Store) ObserveMappedSource(input MappedSourceObservation) (string, error) {
	o := input.SourceObservation
	if err := o.validate(); err != nil {
		return "", err
	}
	if s.draft != "" || len(o.Claims) != 0 {
		return "", errorf(CodeNotAllowed, "mapped observations require a published source and raw source values")
	}
	var canonical string
	err := s.write(func(w *writeTx) error {
		var replay bool
		if err := w.tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM source_events
			WHERE binding = ? AND object = ? AND key = ? AND generation = ? AND event = ?)`,
			append(o.Key.args(), o.Event)...).Scan(&replay); err != nil {
			return err
		}
		if replay {
			var err error
			canonical, _, err = findIdentity(w.tx, o.Key)
			return err
		}
		m, version, err := w.sourceMapping(o.Key, o.Epochs)
		if err != nil {
			return err
		}
		o.mappingVersion = version
		if o.Claims, err = m.claims(input.Values); err != nil {
			return err
		}
		id, err := w.linkIdentity(m.Common, o.Key, "")
		if err != nil {
			return err
		}
		t := w.schema.common[m.Common]
		before, err := loadRecord(w.tx, w.schema, id)
		if err != nil {
			return err
		}
		if canonical, _, err = w.observeSource(o); err != nil {
			return err
		}
		if !o.NativeValues || m.Match == (IdentityFields{}) || m.Common != CommonPerson && m.Common != CommonOrganization {
			return nil
		}
		after, err := loadRecord(w.tx, w.schema, id)
		if err != nil || after == nil {
			return err
		}
		if err := w.attestMappedIdentity(t, after, m.Match, o, input.VerifiedWorkEmail); err != nil {
			return err
		}
		if before != nil {
			return nil
		}
		scope := Scope{Actor: "engine", sync: true, CauseChain: o.CauseChain, sourceEvent: o.Event}
		canonical, err = w.matchIdentity(scope, t, after, m.Match, true)
		return err
	})
	if err != nil {
		return "", err
	}
	return canonical, nil
}

func (w *writeTx) attestMappedIdentity(t *Type, r *record, fields IdentityFields, o SourceObservation, verified bool) error {
	email := t.byID[fields.Email]
	org := t.byID[fields.Organization]
	if !verified || email == nil || org == nil || r.Meta.Sources[email.Name] != o.Key || r.Meta.Sources[org.Name] != o.Key {
		return nil
	}
	// Both fields must be in this observation so a partial refresh cannot
	// attest a work address or a relationship from an old revision.
	if o.Claims[fields.Email].Value == nil || o.Claims[fields.Organization].Value == nil {
		return nil
	}
	_, err := w.tx.Exec(`INSERT INTO identity_attestations VALUES (?, ?)
		ON CONFLICT(record) DO UPDATE SET rev = excluded.rev`, r.ID, r.Rev)
	return err
}
