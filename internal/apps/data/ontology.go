package data

import (
	"database/sql"
	"errors"
	"strings"

	appschema "github.com/Agent-Field/codeaf/internal/apps/schema"
)

// Data-plane migration also removes redundant values from old outbox rows.
// Scoped record values remain available in the existing history table.
const dataPlaneMigration = `
	ALTER TABLE outbox ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
	ALTER TABLE outbox ADD COLUMN cause_chain TEXT NOT NULL DEFAULT '[]';
	ALTER TABLE outbox ADD COLUMN source_event TEXT NOT NULL DEFAULT '';
	ALTER TABLE outbox ADD COLUMN binding TEXT NOT NULL DEFAULT '';
	UPDATE outbox SET event = 'freshness.changed', payload_json = 'null'
		WHERE event = 'record.updated' AND batch = '' AND cause = 'clock';
	UPDATE history AS h
		SET before_json = json_set(h.before_json,
				'$.created_at', json_extract(o.payload_json, '$.before.created_at'),
				'$.updated_at', json_extract(o.payload_json, '$.before.updated_at')),
			after_json = json_set(h.after_json,
				'$.created_at', json_extract(o.payload_json, '$.after.created_at'),
				'$.updated_at', json_extract(o.payload_json, '$.after.updated_at'))
		FROM outbox AS o
		WHERE o.event IN ('record.created', 'record.updated', 'record.deleted')
			AND o.batch = h.batch AND o.record = h.record AND o.type_id = h.type_id;
	UPDATE outbox SET payload_json = '{}' WHERE event IN ('record.created', 'record.updated', 'record.deleted');
	UPDATE outbox SET payload_json = json_object('id', json_extract(payload_json, '$.id'))
		WHERE event LIKE 'work_item.%' OR event LIKE 'timer.%' OR event LIKE 'signoff.%' OR event LIKE 'comment.%';
	UPDATE outbox SET payload_json = json_remove(payload_json, '$.reason') WHERE event = 'transition.fired';

	CREATE TABLE common_types (
		key TEXT PRIMARY KEY,
		type_id TEXT NOT NULL UNIQUE REFERENCES types(id)
	);
	CREATE TABLE app_type_bindings (
		app TEXT NOT NULL,
		alias TEXT NOT NULL,
		type_id TEXT NOT NULL REFERENCES types(id),
		common_key TEXT NOT NULL DEFAULT '',
		PRIMARY KEY(app, alias)
	);
	INSERT INTO app_type_bindings(app, alias, type_id)
		SELECT a.app, t.name, t.id FROM app_types a JOIN types t ON t.id = a.type_id;
	CREATE TABLE type_epochs (
		type_id TEXT PRIMARY KEY REFERENCES types(id),
		mapping INTEGER NOT NULL,
		residency INTEGER NOT NULL
	);
	CREATE TABLE canonical_entities (
		id TEXT PRIMARY KEY,
		type_id TEXT NOT NULL REFERENCES types(id)
	);
	CREATE TABLE external_keys (
		binding TEXT NOT NULL,
		object TEXT NOT NULL,
		key TEXT NOT NULL,
		generation INTEGER NOT NULL,
		entity_id TEXT NOT NULL REFERENCES canonical_entities(id),
		PRIMARY KEY(binding, object, key, generation)
	);
	CREATE INDEX external_keys_entity ON external_keys(entity_id);
	CREATE TABLE source_observations (
		binding TEXT NOT NULL,
		object TEXT NOT NULL,
		key TEXT NOT NULL,
		generation INTEGER NOT NULL,
		source_revision TEXT NOT NULL,
		acl_ref TEXT NOT NULL,
		acl_epoch INTEGER NOT NULL,
		observed_at TEXT NOT NULL,
		PRIMARY KEY(binding, object, key, generation)
	);
	CREATE TABLE source_claims (
		binding TEXT NOT NULL,
		object TEXT NOT NULL,
		key TEXT NOT NULL,
		generation INTEGER NOT NULL,
		field_id TEXT NOT NULL REFERENCES fields(id),
		claim_json TEXT NOT NULL,
		PRIMARY KEY(binding, object, key, generation, field_id)
	);
	CREATE TABLE source_events (
		binding TEXT NOT NULL,
		object TEXT NOT NULL,
		key TEXT NOT NULL,
		generation INTEGER NOT NULL,
		event TEXT NOT NULL,
		PRIMARY KEY(binding, object, key, generation, event)
	);
	CREATE TABLE app_publications (
		app TEXT PRIMARY KEY,
		version INTEGER NOT NULL,
		archived INTEGER NOT NULL,
		deleted INTEGER NOT NULL,
		metadata TEXT NOT NULL
	);
`

// CommonTypeKey is the portable name of a common type. Its space-local type
// and field IDs keep their meaning when a source or an app is replaced.
type CommonTypeKey string

const (
	CommonPerson       CommonTypeKey = "person"
	CommonOrganization CommonTypeKey = "organization"
	CommonWorkItem     CommonTypeKey = "work_item"
	CommonEvent        CommonTypeKey = "event"
	CommonDocument     CommonTypeKey = "document"
	CommonFile         CommonTypeKey = "file"
	CommonMessage      CommonTypeKey = "message"
	CommonTransaction  CommonTypeKey = "transaction"
	CommonProduct      CommonTypeKey = "product"
	CommonLocation     CommonTypeKey = "location"
)

// BindCommonType adopts an existing store type without changing its IDs.
func (s *Store) BindCommonType(key CommonTypeKey, typ string) error {
	if !identShape.MatchString(string(key)) {
		return errorf(CodeInvalid, "a common type needs a stable key")
	}
	return s.write(func(w *writeTx) error {
		t := w.schema.typeNamed(typ)
		if t == nil {
			return errorf(CodeNotFound, "there is no record type named %q", typ)
		}
		if t.Common == key {
			return nil
		}
		if err := w.bindCommonType(key, t.ID); err != nil {
			return err
		}
		next, _, err := loadSchema(w.tx, s.now)
		if err != nil {
			return err
		}
		w.next = next
		return w.invalidate(EventMappingChanged, t.ID, "")
	})
}

// bindCommonType is shared by explicit adoption and publication so the
// common key, record IDs and app grants commit in the same writer.
func (w *writeTx) bindCommonType(key CommonTypeKey, typeID string) error {
	if t := w.schema.common[key]; t != nil {
		if t.ID != typeID {
			return errorf(CodeInvalid, "the common key %q already names %s records", key, t.Name)
		}
		return nil
	}
	if t := w.schema.byID[typeID]; t != nil && t.Common != "" {
		return errorf(CodeInvalid, "%s records already use the common key %q", t.Name, t.Common)
	}
	_, err := w.tx.Exec(`INSERT INTO common_types(key, type_id) VALUES(?, ?)`, key, typeID)
	return err
}

// TypeBinding is an app's local alias resolved to the space's stable type.
type TypeBinding struct {
	Alias  string
	TypeID string
	Common CommonTypeKey
}

// TypeBindings resolves an app's grant-backed bindings in this store only.
func (s *Store) TypeBindings(app string) []TypeBinding {
	sch := s.schema.Load()
	bindings := sch.bindings[app]
	out := make([]TypeBinding, 0, len(bindings))
	for _, name := range sortedKeys(bindings) {
		b := bindings[name]
		if t := sch.byID[b.TypeID]; t != nil && sch.caps[app][t.Name] != "" {
			out = append(out, b)
		}
	}
	return out
}

// TypeEpochs fences mapping and residency changes independently of record revisions.
type TypeEpochs struct {
	Mapping   int64
	Residency int64
}

func readEpochs(q querier, typeID string) (TypeEpochs, error) {
	var epochs TypeEpochs
	err := q.QueryRow(`SELECT mapping, residency FROM type_epochs WHERE type_id = ?`, typeID).Scan(&epochs.Mapping, &epochs.Residency)
	if errors.Is(err, sql.ErrNoRows) {
		return epochs, nil
	}
	return epochs, err
}

// Epochs returns the epochs for a stable type ID through the caller's grant.
func (s *Store) Epochs(sc Scope, typ string) (TypeEpochs, error) {
	t, err := s.schema.Load().allow(sc, typ, false)
	if err != nil {
		return TypeEpochs{}, err
	}
	return readEpochs(s.rdb, t.ID)
}

// AdvanceEpochs commits a mapping or residency generation with its reset.
func (s *Store) AdvanceEpochs(typeID string, expected, next TypeEpochs) error {
	return s.write(func(w *writeTx) error { return w.advanceEpochs(typeID, expected, next) })
}

func (w *writeTx) advanceEpochs(typeID string, expected, next TypeEpochs) error {
	if next == expected || next.Mapping < expected.Mapping || next.Residency < expected.Residency {
		return errorf(CodeInvalid, "mapping and residency epochs must advance")
	}
	if w.schema.byID[typeID] == nil {
		return errorf(CodeNotFound, "there is no such record type")
	}
	current, err := readEpochs(w.tx, typeID)
	if err != nil {
		return err
	}
	if current != expected {
		return errorf(CodeStale, "the type's epochs have changed")
	}
	if _, err := w.tx.Exec(`INSERT INTO type_epochs VALUES(?, ?, ?)
			ON CONFLICT(type_id) DO UPDATE SET mapping = excluded.mapping, residency = excluded.residency`,
		typeID, next.Mapping, next.Residency); err != nil {
		return err
	}
	return w.invalidate(EventMappingChanged, typeID, "")
}

// ExternalKey carries an opaque, typed source key in its connection, object
// and key-generation namespace. Its contents are private to engine callers.
type ExternalKey struct {
	Binding    string
	Object     string
	Key        string
	Generation int64
}

func (k ExternalKey) args() []any { return []any{k.Binding, k.Object, k.Key, k.Generation} }

func (k ExternalKey) valid() bool {
	return strings.TrimSpace(k.Binding) != "" && len(k.Binding) <= 200 &&
		strings.TrimSpace(k.Object) != "" && len(k.Object) <= 200 &&
		k.Key != "" && len(k.Key) <= 4096 && k.Generation > 0
}

// LinkIdentity reserves a canonical ULID or attaches a reviewed source key to
// an existing entity. A retry with the same key cannot replace its identity.
func (s *Store) LinkIdentity(common CommonTypeKey, key ExternalKey, entityID string) (string, error) {
	if !key.valid() || entityID != "" && !validID(entityID) {
		return "", errorf(CodeInvalid, "an identity needs a complete source key and a canonical ULID")
	}
	var id string
	err := s.write(func(w *writeTx) error {
		var err error
		id, err = w.linkIdentity(common, key, entityID)
		return err
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

func (w *writeTx) linkIdentity(common CommonTypeKey, key ExternalKey, entityID string) (string, error) {
	if w.s.draft != "" {
		return "", errorf(CodeNotAllowed, "source identities belong to the published store")
	}
	var typeID string
	err := w.tx.QueryRow(`SELECT type_id FROM common_types WHERE key = ?`, common).Scan(&typeID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errorf(CodeNotFound, "the common type has no binding")
	}
	if err != nil {
		return "", err
	}
	existing, existingType, err := findIdentity(w.tx, key)
	if err != nil {
		return "", err
	}
	if entityID != "" {
		entityID, err = identityRoot(w.tx, entityID)
		if err != nil {
			return "", err
		}
	}
	if existing != "" {
		if existingType != typeID || entityID != "" && existing != entityID {
			return "", errorf(CodeInvalid, "the source key is already linked")
		}
		return existing, nil
	}
	id := entityID
	if id == "" {
		id = w.s.ids.next(w.now)
	} else {
		var have string
		err := w.tx.QueryRow(`SELECT type_id FROM canonical_entities WHERE id = ?
				UNION ALL SELECT type_id FROM records WHERE id = ? LIMIT 1`, id, id).Scan(&have)
		if errors.Is(err, sql.ErrNoRows) || err == nil && have != typeID {
			return "", errorf(CodeInvalid, "the canonical entity does not belong to the common type")
		}
		if err != nil {
			return "", err
		}
	}
	if _, err := w.tx.Exec(`INSERT OR IGNORE INTO canonical_entities VALUES(?, ?)`, id, typeID); err != nil {
		return "", err
	}
	_, err = w.tx.Exec(`INSERT INTO external_keys VALUES(?, ?, ?, ?, ?)`, append(key.args(), id)...)
	return id, err
}

func findIdentityOrigin(q querier, key ExternalKey) (id, typeID string, err error) {
	err = q.QueryRow(`SELECT e.id, e.type_id FROM external_keys k
		JOIN canonical_entities e ON e.id = k.entity_id
		WHERE k.binding = ? AND k.object = ? AND k.key = ? AND k.generation = ?`, key.args()...).Scan(&id, &typeID)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return
}

func findIdentity(q querier, key ExternalKey) (id, typeID string, err error) {
	id, typeID, err = findIdentityOrigin(q, key)
	if err == nil && id != "" {
		id, err = identityRoot(q, id)
	}
	return
}

// LookupIdentity reads a source key's canonical ULID without reserving one.
func (s *Store) LookupIdentity(key ExternalKey) (string, bool, error) {
	if !key.valid() {
		return "", false, errorf(CodeInvalid, "a complete source key is required")
	}
	id, _, err := findIdentity(s.rdb, key)
	return id, id != "", err
}

// LookupBoundIdentity resolves a source key only for its checked type binding.
func (s *Store) LookupBoundIdentity(binding TypeBinding, key ExternalKey) (string, bool, error) {
	if !key.valid() {
		return "", false, errorf(CodeInvalid, "a complete source key is required")
	}
	id, typeID, err := findIdentity(s.rdb, key)
	if err == nil && id != "" && typeID != binding.TypeID {
		return "", false, errorf(CodeInvalid, "the source identity belongs to a different type")
	}
	return id, id != "", err
}

// ExternalKeys returns the crosswalk's reverse lookup for trusted source adapters.
func (s *Store) ExternalKeys(entityID string) ([]ExternalKey, error) {
	return s.groupExternalKeys(entityID)
}

// resolveBindings checks all aliases before registration touches any declarations.
func (w *writeTx) resolveBindings(def *AppDef, bindings map[string]CommonTypeKey) (map[string]TypeBinding, error) {
	out := map[string]TypeBinding{}
	previous := w.schema.bindings[def.ID]
	aliases := map[string]bool{}
	for alias := range def.Records {
		aliases[alias] = true
	}
	for alias := range def.Capabilities.Records {
		aliases[alias] = true
	}
	for alias := range bindings {
		if !aliases[alias] {
			return nil, errorf(CodeInvalid, "the app does not declare or use its %q binding", alias)
		}
	}
	claimed := map[CommonTypeKey]string{}
	for _, alias := range sortedKeys(aliases) {
		b := previous[alias]
		b.Alias = alias
		record, declares := def.Records[alias]
		key := bindings[alias]
		if key == "" {
			key = record.Common
		} else if record.Common != "" && record.Common != key {
			return nil, errorf(CodeInvalid, "%s declares a different common key", alias)
		}
		if record.PackagePrivate {
			if key != "" {
				return nil, errorf(CodeInvalid, "%s is a private package type", alias)
			}
			t := w.schema.typeNamed(appschema.PrivateRecordName(def.ID, alias))
			if b.TypeID != "" && (t == nil || t.ID != b.TypeID) {
				return nil, errorf(CodeInvalid, "%s already names another record type", alias)
			}
			if t != nil {
				if t.DefinedBy != def.ID {
					return nil, errorf(CodeInvalid, "%s is already used by another app", alias)
				}
				b.TypeID = t.ID
			}
		} else if key != "" {
			if other := claimed[key]; other != "" {
				return nil, errorf(CodeInvalid, "%s and %s both name common type %q", other, alias, key)
			}
			claimed[key] = alias
			if t := w.schema.common[key]; t != nil {
				if b.TypeID != "" && b.TypeID != t.ID {
					return nil, errorf(CodeInvalid, "%s already names a different local type", alias)
				}
				b.TypeID = t.ID
			} else {
				if bindings[alias] != "" || record.Uses {
					return nil, errorf(CodeInvalid, "there is no local type for common key %q", key)
				}
				if t := w.schema.appType(def.ID, alias); t != nil {
					b.TypeID = t.ID
				}
			}
			b.Common = key
		} else if t := w.schema.appType(def.ID, alias); t != nil {
			b.TypeID = t.ID
		}
		if b.TypeID == "" && !declares {
			return nil, errorf(CodeInvalid, "%s's capabilities name %s records, and there is no %s type", def.ID, alias, alias)
		}
		out[alias] = b
	}
	return out, nil
}

func boundField(f FieldDef, names map[string]string) FieldDef {
	if name := names[f.To]; name != "" {
		f.To = name
	}
	return f
}
