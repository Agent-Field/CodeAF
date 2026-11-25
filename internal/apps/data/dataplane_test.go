package data

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestDataPlaneReopensBStoreWithoutReplacingIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	const personID = "01K00000000000000000000001"
	const typeID = "01K00000000000000000000002"
	const fieldID = "01K00000000000000000000003"
	const recordID = "01K00000000000000000000004"
	if _, err := db.Exec(migrations[0] + `
		PRAGMA user_version=1;
		INSERT INTO space VALUES('person', '` + personID + `');
		INSERT INTO apps VALUES('crm', '{"contact":"rw"}', '');
		INSERT INTO types VALUES('` + typeID + `', 'contact', 'crm', 'crm', '{}', '');
		INSERT INTO app_types VALUES('crm', '` + typeID + `', 0);
		INSERT INTO fields VALUES('` + fieldID + `', '` + typeID + `', 'name', '');
		INSERT INTO type_fields VALUES('` + fieldID + `', 'crm', '{"type":"text"}', 0, 0);
		INSERT INTO records(id, type_id, data_json, created_at, updated_at)
			VALUES('` + recordID + `', '` + typeID + `', '{"` + fieldID + `":"Ari"}', '', '');
		INSERT INTO outbox(event, type_id, record, by_kind, payload_json, at)
			VALUES('record.created', '` + typeID + `', '` + recordID + `', 'person', '{}', '');
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path, Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sc := Scope{App: "crm"}
	typ, err := s.Type(sc, "contact")
	if err != nil || typ.ID != typeID || typ.Fields["name"].ID != fieldID || s.Person() != personID {
		t.Fatalf("original schema and person: %+v, %v", typ, err)
	}
	if err := s.BindCommonType(CommonPerson, "contact"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []ExternalKey{
		{Binding: "previous", Object: "contacts", Key: "103", Generation: 1},
		{Binding: "replacement", Object: "contacts", Key: "new key", Generation: 1},
	} {
		id, err := s.LinkIdentity(CommonPerson, key, recordID)
		if err != nil || id != recordID {
			t.Fatalf("replacement resolved %q: %v", id, err)
		}
	}
	row, err := s.Get(sc, "contact", recordID, false)
	if err != nil || row.ID() != recordID || row["name"] != "Ari" {
		t.Fatalf("original row: %v, %v", row, err)
	}
	events, err := s.Outbox(0, 1)
	if err != nil || len(events) != 1 || events[0].TypeID != typeID || events[0].Version != 1 {
		t.Fatalf("original event: %+v, %v", events, err)
	}
}

func TestDataPlaneSyncProvenance(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*testing.T, *Store, string)
		held    bool
	}{
		{name: "AI can replace a sync value"},
		{name: "person clear survives sync", held: true, prepare: func(t *testing.T, s *Store, id string) {
			if _, err := s.Update(person, "deal", []string{id}, map[string]any{"likely": nil}, UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := openCRM(t)
			r := mustCreate(t, s, person, "deal", map[string]any{"name": "Ari"})[0]
			if err := s.BindCommonType("deal", "deal"); err != nil {
				t.Fatal(err)
			}
			k := ExternalKey{Binding: "trusted", Object: "contacts", Key: "ari", Generation: 1}
			if _, err := s.LinkIdentity("deal", k, r.ID()); err != nil {
				t.Fatal(err)
			}
			if tc.prepare != nil {
				tc.prepare(t, s, r.ID())
			}
			observation := SourceObservation{Key: k, Event: "event-1", Revision: "v7", CauseChain: []string{"refresh"}, NativeValues: true,
				Claims: map[string]SourceFieldClaim{"likely": {Path: "description", Value: "Maybe"}}}
			if _, err := s.ObserveSource(observation); err != nil {
				t.Fatal(err)
			}
			cursor, _ := s.Cursor()
			if _, err := s.ObserveSource(observation); err != nil {
				t.Fatal(err)
			}
			if after, _ := s.Cursor(); after != cursor {
				t.Fatal("a duplicate observation advanced the cursor")
			}
			row, err := s.Get(person, "deal", r.ID(), false)
			if err != nil {
				t.Fatal(err)
			}
			if tc.held && row["likely"] != nil {
				t.Fatal("sync overwrote a person's clear")
			}
			if !tc.held {
				if row["likely"] != "Maybe" || row["_meta"].(RowMeta).By["likely"] != BySync {
					t.Fatalf("expected a sync value: %v", row)
				}
			}
			out, err := s.Update(Scope{AI: true}, "deal", []string{r.ID()}, map[string]any{"likely": "Likely"}, UpdateOptions{})
			if err != nil || (len(out.Held) != 0) != tc.held {
				t.Fatalf("AI write: %+v, %v", out, err)
			}
			if !tc.held && out.Rows[0]["likely"] != "Likely" {
				t.Fatalf("sync pinned the field: %v", out.Rows[0])
			}
		})
	}
}

func TestDataPlaneSubscriptionsOwnSnapshotAndReset(t *testing.T) {
	s, _ := openCRM(t)
	row := mustCreate(t, s, person, "deal", map[string]any{"name": "Ari"})[0]
	a, b := new(SubscriptionOwner), new(SubscriptionOwner)
	first, second := newRecorder(), newRecorder()
	spec := SubSpec{ID: "same", Type: "deal"}
	sc := Scope{App: "crm"}
	snap, err := s.Watch(a, sc, spec, first.take)
	if err != nil || snap.Cursor == 0 || len(snap.Rows) != 1 || snap.Rows[0].ID() != row.ID() {
		t.Fatalf("snapshot: %+v, %v", snap, err)
	}
	spec.InvalidateOnly = true
	if _, err := s.Watch(b, person, spec, second.take); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(person, "deal", []string{row.ID()}, map[string]any{"name": "Paz"}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if d := first.next(t); d.Reset || len(d.Upserts) != 1 || d.Cursor <= snap.Cursor {
		t.Fatalf("native diff: %+v", d)
	}
	if d := second.next(t); !d.Reset || len(d.Upserts) != 0 || len(d.Removes) != 0 || d.Total != 0 {
		t.Fatalf("federated reset: %+v", d)
	}
	if err := s.ForgetApp(sc.App); err != nil {
		t.Fatal(err)
	}
	if d := first.next(t); !d.Reset || len(d.Upserts) != 0 || len(d.Removes) != 0 {
		t.Fatalf("revocation: %+v", d)
	}
}

func TestDataPlanePublicationBindsAnAppAliasWithoutBindingTheDraft(t *testing.T) {
	place := NewPlace(t.TempDir(), Options{NoRefresh: true})
	defer place.Close()
	s, err := place.Main()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterApp(fixture(t, "crm-app.json")); err != nil {
		t.Fatal(err)
	}
	if err := s.BindCommonType(CommonPerson, "contact"); err != nil {
		t.Fatal(err)
	}
	p := AppPublication{App: "phonebook", Version: 1, Metadata: []byte("{}"),
		Definition: []byte(`{"id":"phonebook","records":{},"capabilities":{"records":{"person":"r"}}}`),
		Bindings:   map[string]CommonTypeKey{"person": CommonPerson}}
	if err := s.PublishApp(p, 0); err != nil {
		t.Fatal(err)
	}
	want, err := s.Type(person, "contact")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Type(Scope{App: p.App}, "person")
	if err != nil || got.ID != want.ID {
		t.Fatalf("local alias: %v, %v", got, err)
	}
	if err := place.SeedSample("draft", fixture(t, "crm-app.json"), nil); err != nil {
		t.Fatal(err)
	}
	sample, err := place.Store("draft")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sample.Type(Scope{App: p.App}, "person")
	wantCode(t, err, CodeNotAllowed)
	p.Version++
	p.Bindings["person"] = CommonOrganization
	wantCode(t, s.PublishApp(p, 1), CodeInvalid)
	if current, ok, err := s.Publication(p.App); err != nil || !ok || current.Version != 1 {
		t.Fatalf("failed publication changed the pointer: %+v %v %v", current, ok, err)
	}
}

func TestDataPlaneKeepsACLClaimsPrivateAndFencesOldEvidence(t *testing.T) {
	s, _ := openCRM(t)
	row := mustCreate(t, s, person, "contact", map[string]any{"name": "Ari"})[0]
	if err := s.BindCommonType(CommonPerson, "contact"); err != nil {
		t.Fatal(err)
	}
	key := ExternalKey{Binding: "private", Object: "people", Key: "opaque", Generation: 1}
	if _, err := s.LinkIdentity(CommonPerson, key, row.ID()); err != nil {
		t.Fatal(err)
	}
	tp, err := s.Type(person, "contact")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AdvanceEpochs(tp.ID, TypeEpochs{}, TypeEpochs{Mapping: 1, Residency: 1}); err != nil {
		t.Fatal(err)
	}
	o := SourceObservation{Key: key, Event: "observed", Revision: "v1", ACLRef: "read-grant", ACLEpoch: 1,
		Claims: map[string]SourceFieldClaim{"name": {Path: "legal_name", Value: "Private name", Class: "pii"}}}
	_, err = s.ObserveSource(o)
	wantCode(t, err, CodeStale)
	o.Epochs = TypeEpochs{Mapping: 1, Residency: 1}
	if _, err := s.ObserveSource(o); err != nil {
		t.Fatal(err)
	}
	o.Event, o.Revision, o.ACLRef, o.ACLEpoch, o.Claims = "acl", "v2", "", 2, nil
	if _, err := s.ObserveSource(o); err != nil {
		t.Fatal(err)
	}
	state, err := s.SourceState(key)
	if err != nil || state.ACLRef != "" || state.ACLEpoch != 2 ||
		state.Claims[tp.Fields["name"].ID].Revision != "v1" || state.Revision != "v2" {
		t.Fatalf("latest proof and old contribution: %+v, %v", state, err)
	}
	o.Event, o.ACLEpoch = "old", 1
	_, err = s.ObserveSource(o)
	wantCode(t, err, CodeStale)
	visible, err := s.Get(person, "contact", row.ID(), false)
	if err != nil || visible["name"] != "Ari" || visible["_rev"] != row["_rev"] {
		t.Fatalf("unprojected claim changed a native record: %v, %v", visible, err)
	}
}
