package data

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// AppPublication ties a committed definition version to its data grants.
// Definition is the app declaration on an initial publish or a new
// version. Metadata is the app store's receipt for the prepared snapshot.
type AppPublication struct {
	App        string
	Version    int
	Archived   bool
	Deleted    bool
	Metadata   json.RawMessage
	Definition []byte
	Bindings   map[string]CommonTypeKey
	Starter    map[string][]map[string]any
}

// Publication reads the data writer's authoritative publication pointer.
func (s *Store) Publication(app string) (AppPublication, bool, error) {
	p, err := readPublication(s.rdb, app)
	return p, err == nil, ignoreMissing(err)
}

func ignoreMissing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func readPublication(q querier, app string) (AppPublication, error) {
	p := AppPublication{App: app}
	var raw string
	err := q.QueryRow(`SELECT version, archived, deleted, metadata FROM app_publications WHERE app = ?`, app).
		Scan(&p.Version, &p.Archived, &p.Deleted, &raw)
	p.Metadata = json.RawMessage(raw)
	return p, err
}

// PublishApp commits a prepared snapshot pointer, checked type bindings and
// grants together. A missing pointer may adopt a legacy filesystem version.
func (s *Store) PublishApp(p AppPublication, previousVersion int) error {
	if s.draft != "" || p.App == "" || p.Version < 1 || previousVersion < 0 ||
		p.Version < previousVersion || previousVersion != 0 && p.Version-previousVersion > 1 ||
		!json.Valid(p.Metadata) || p.Deleted && !p.Archived || len(p.Definition) == 0 && len(p.Bindings) != 0 {
		return errorf(CodeInvalid, "invalid app publication")
	}
	var def *AppDef
	if len(p.Definition) != 0 {
		var err error
		def, err = ParseApp(p.Definition)
		if err != nil {
			return err
		}
		if def.ID != p.App || p.Deleted {
			return errorf(CodeInvalid, "the declaration does not belong to the publication")
		}
	}
	if len(p.Starter) != 0 && (previousVersion != 0 || p.Version != 1 || p.Archived || def == nil) {
		return errorf(CodeInvalid, "starter records require a first publication")
	}
	return s.write(func(w *writeTx) error {
		before, err := readPublication(w.tx, p.App)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if before.Version != previousVersion || before.Deleted {
				return errorf(CodeStale, "the app publication has changed")
			}
			if (def != nil) != (p.Version > before.Version) {
				return errorf(CodeInvalid, "a changed definition needs a new app version")
			}
		} else if def == nil && !p.Deleted {
			return errorf(CodeInvalid, "an initial publication needs its declaration")
		}
		switch {
		case p.Deleted:
			w.next, err = forgetApp(w, p.App)
		case def != nil:
			w.next, err = registerApp(w, def, p.Bindings)
		}
		if err != nil {
			return err
		}
		if len(p.Starter) != 0 {
			w.schema = w.next
			if err := w.seedStarter(p.App, p.Starter); err != nil {
				return err
			}
		}
		if _, err := w.tx.Exec(`INSERT INTO app_publications VALUES(?, ?, ?, ?, ?)
			ON CONFLICT(app) DO UPDATE SET version = excluded.version, archived = excluded.archived,
			deleted = excluded.deleted, metadata = excluded.metadata`, p.App, p.Version, p.Archived, p.Deleted, string(p.Metadata)); err != nil {
			return err
		}
		if p.Archived {
			if _, err := w.tx.Exec("DELETE FROM source_indexes WHERE app = ?", p.App); err != nil {
				return err
			}
		}
		return w.invalidate(EventPolicyChanged, "", "")
	})
}

// Starter rows share the publication transaction so a retry cannot re-create
// an example the person already changed or deleted.
func (w *writeTx) seedStarter(app string, rows map[string][]map[string]any) error {
	scope := Scope{App: app, Actor: "codeaf"}
	for _, alias := range sortedKeys(rows) {
		t, err := w.schema.allow(scope, alias, true)
		if err != nil {
			return err
		}
		if t.DefinedBy != app {
			return errorf(CodeInvalid, "starter records must belong to the new app")
		}
		if err := w.seedRows(t, scope, rows[alias]); err != nil {
			return err
		}
	}
	return nil
}
