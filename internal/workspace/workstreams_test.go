package workspace

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func seedWorkstream(t *testing.T, s *Store, id, name string) {
	t.Helper()
	if err := s.Write(context.Background(), func(tx *sql.Tx) error {
		if err := CreateIn(context.Background(), tx, id, name, 100); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO workstreams(id,agent_id,created_at,updated_at) VALUES (?,?,100,100)", id, "agent-"+id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// Part A (b): the collection store, including relink and raw SQL's unique
// constraint, enforces job ownership; plain collections make no job claim.
func TestPartAWorkstreamMembershipOwnershipIsEnforcedInSQLite(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "claims.db"))
	ctx := context.Background()
	seedWorkstream(t, s, "a", "First")
	seedWorkstream(t, s, "b", "Second")
	job := Ref{Kind: FactoryJobKind, ID: "7", Factory: "f"}
	fixedClock(s, 200)
	if err := s.Add(ctx, "a", job); err != nil {
		t.Fatal(err)
	}
	fixedClock(s, 300)
	_, _, err := s.AddRefs(ctx, "b", []Addition{{Ref: Ref{Kind: ConversationKind, ID: "extra"}}, {Ref: job}})
	var taken *WorkstreamTaken
	if !errors.As(err, &taken) || taken.WorkstreamID != "a" || taken.WorkstreamName != "First" || taken.Job != 7 || taken.Factory != "f" {
		t.Fatalf("taken: %+v %v", taken, err)
	}
	for id, want := range map[string]int64{"a": 200, "b": 100} {
		var at int64
		if err := s.db.QueryRow("SELECT updated_at FROM workstreams WHERE id=?", id).Scan(&at); err != nil || at != want {
			t.Fatalf("timestamp %s: %d %v", id, at, err)
		}
	}
	if members, err := s.Members(ctx, "b"); err != nil || len(members) != 0 {
		t.Fatalf("partial add: %+v %v", members, err)
	}
	if err := s.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO memberships(collection_id,kind,ref_id,session_id,factory) VALUES ('b','factory_job','7','','f')")
		return err
	}); err == nil {
		t.Fatal("SQL bypassed unique job owner")
	}
	plain := createTestCollection(t, s, "Plain")
	if err := s.Add(ctx, plain.ID, job); err != nil {
		t.Fatal(err)
	}
	differentFactory := Ref{Kind: FactoryJobKind, ID: "7", Factory: "other"}
	if err := s.Add(ctx, "b", differentFactory); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Relink(ctx, "b", differentFactory, job); !errors.As(err, &taken) {
		t.Fatalf("relink took another owner's job: %v", err)
	}
	if err := s.Remove(ctx, "a", job); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Relink(ctx, "b", differentFactory, job); err != nil {
		t.Fatal(err)
	}
	a, err := s.Describe(ctx, "a")
	if err != nil || !a.Workstream {
		t.Fatalf("workstream flag: %+v %v", a, err)
	}
	copy, err := s.Duplicate(ctx, "b", "Copy")
	if err != nil {
		t.Fatal(err)
	}
	copySummary, err := s.Describe(ctx, copy.ID)
	if err != nil || copySummary.Workstream {
		t.Fatalf("plain duplicate: %+v %v", copySummary, err)
	}
	if _, err := s.Delete(ctx, "a"); !errors.Is(err, ErrWorkstreamDelete) {
		t.Fatalf("delete: %v", err)
	}
}

// Part A (a): schema 1 upgrades to the current schema, preserving unknown times.
func TestPartASchemaOneMigratesStraightToCurrent(t *testing.T) {
	s := openTestStore(t, writeLegacyStore(t))
	ctx := context.Background()
	if err := s.Add(ctx, "aaaa", Ref{Kind: FactoryJobKind, ID: "11", Factory: "f"}); err != nil {
		t.Fatal(err)
	}
	if userVersion(t, s) != 4 {
		t.Fatal("did not migrate directly to schema 4")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name LIKE 'workstream%'").Scan(&count); err != nil || count != 9 {
		t.Fatalf("workstream tables: %d %v", count, err)
	}
	var created, added int64
	if err := s.db.QueryRow("SELECT created_at FROM collections WHERE id='aaaa'").Scan(&created); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT added_at FROM memberships WHERE kind='task'").Scan(&added); err != nil {
		t.Fatal(err)
	}
	if created != 0 || added != 0 {
		t.Fatal("invented legacy timestamps")
	}
	for _, q := range []string{
		"INSERT INTO memberships(collection_id,kind,ref_id,session_id) VALUES ('aaaa','factory_job','12','')",
		"INSERT INTO memberships(collection_id,kind,ref_id,session_id,factory) VALUES ('aaaa','conversation','other','','f')",
	} {
		if _, err := s.db.Exec(q); err == nil {
			t.Fatalf("accepted invalid factory: %s", q)
		}
	}
}

func TestPartAFactoryIsPartOfEveryCollectionReadIdentity(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "identity.db"))
	ctx := context.Background()
	c := createTestCollection(t, s, "Jobs")
	a, b := Ref{Kind: FactoryJobKind, ID: "1", Factory: "a"}, Ref{Kind: FactoryJobKind, ID: "1", Factory: "b"}
	if err := s.Add(ctx, c.ID, a); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, c.ID, b); err != nil {
		t.Fatal(err)
	}
	for _, r := range []Ref{a, b} {
		parents, err := s.CollectionsFor(ctx, r)
		if err != nil || len(parents) != 1 {
			t.Fatalf("find %+v: %+v %v", r, parents, err)
		}
	}
	expansion, err := s.Expand(ctx, c.ID, 1)
	if err != nil || len(expansion.Refs) != 2 {
		t.Fatalf("expansion: %+v %v", expansion, err)
	}
	if err := s.Remove(ctx, c.ID, a); err != nil {
		t.Fatal(err)
	}
	members, err := s.Members(ctx, c.ID)
	if err != nil || len(members) != 1 || members[0] != b {
		t.Fatalf("remove: %+v %v", members, err)
	}
	list, err := s.List(ctx, &a)
	if err != nil || len(list) != 1 || *list[0].Holds {
		t.Fatalf("holds: %+v %v", list, err)
	}
	parents, err := s.CollectionsFor(ctx, a)
	if err != nil || len(parents) != 0 {
		t.Fatalf("removed find: %+v %v", parents, err)
	}
}
