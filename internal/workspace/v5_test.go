package workspace

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func writeV4Fixture(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(v2CollectionsDDL + v2HistoryDDL + v3DDL + v4DDL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO collections(id,name,purpose,lifecycle,revision,created_at,updated_at)
 VALUES ('billing-v4','Billing','','active',1,'','')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection)
 VALUES ('billing-v4','conversation','old-chat','',NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO root_state(id,purpose,revision,updated_at) VALUES (1,'',1,'')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmtPragmas(4)); err != nil {
		t.Fatal(err)
	}
}

func TestV4ListDoesNotMigrate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v4.db")
	writeV4Fixture(t, path)
	s := openTestStore(t, path)
	if s.SchemaVersion() != 4 {
		t.Fatalf("open reported version %d", s.SchemaVersion())
	}
	got, err := s.Collections(ctx)
	if err != nil || len(got) != 1 || got[0].ID != "billing-v4" {
		t.Fatalf("v4 list: %v, %v", got, err)
	}
	grants, err := s.ListGrants(ctx, "mgmt")
	if err != nil || len(grants) != 0 {
		t.Fatalf("v4 list grants: %v, %v", grants, err)
	}
	_, err = s.BindingByRequestKey(ctx, "req-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("v4 binding: %v", err)
	}
	listed, err := s.ListBindingsForChat(ctx, "mgmt")
	if err != nil || len(listed) != 0 {
		t.Fatalf("v4 list bindings: %v, %v", listed, err)
	}
	unbound, err := s.ListUnboundBindings(ctx)
	if err != nil || len(unbound) != 0 {
		t.Fatalf("v4 unbound: %v, %v", unbound, err)
	}
	if tablesNamed(t, path, v5TableNames...) != nil {
		t.Fatalf("v4 list created v5 tables: %v", tablesNamed(t, path, v5TableNames...))
	}
	app, version := fileUserVersion(t, path)
	if app != applicationID || version != 4 {
		t.Fatalf("list migrated the file: application %d version %d", app, version)
	}
}

func TestFirstWriteMigratesV4ToV5(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v4.db")
	writeV4Fixture(t, path)
	s := openTestStore(t, path)
	if err := s.Add(ctx, "billing-v4", Ref{Kind: ConversationKind, ID: "new-chat"}); err != nil {
		t.Fatal(err)
	}
	app, version := fileUserVersion(t, path)
	if app != applicationID || version != 5 {
		t.Fatalf("first write left application %d version %d", app, version)
	}
	requireTables(t, path, v3TableNames...)
	requireTables(t, path, v4TableNames...)
	requireTables(t, path, v5TableNames...)
	if extra := tablesNamed(t, path, laterPhaseTableNames...); len(extra) != 0 {
		t.Fatalf("wave 4 created later-phase tables: %v", extra)
	}
	people, err := s.PutParticipant(ctx, Participant{DiscussionID: "mgmt", SourceChatID: "mgmt", Origin: OriginPerson})
	if err != nil || people.ActorID == "" {
		t.Fatalf("wave 3 participant after v5: %+v, %v", people, err)
	}
}

func TestGrantIDsAreMintedAndCannotSelfExpand(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "grants.db"))
	billing := createTestCollection(t, s, "Billing")
	before, _, _ := mustRoot(t, s)
	first, err := s.PutGrant(ctx, Grant{
		ID:            "model-invented-grant",
		Goal:          "keep receipts current",
		CoordinatorID: "mgmt",
		ScopeKind:     ScopeSelected,
		FolderID:      billing.ID,
		SnapshotJSON:  `["chat-a"]`,
		ActionJSON:    `["read","organize"]`,
		Issuer:        "person",
		Origin:        OriginPerson,
		Actor:         "should-not-become-id",
	})
	if err != nil || first.ID == "" || first.ID == "model-invented-grant" || first.Status != GrantActive {
		t.Fatalf("minted grant: %+v, %v", first, err)
	}
	if first.ActionJSON != `["organize","read"]` {
		t.Fatalf("canonical actions %q", first.ActionJSON)
	}
	after, _, _ := mustRoot(t, s)
	if after != before+1 {
		t.Fatalf("root grant revision %d, want %d", after, before+1)
	}
	_, err = s.PutGrant(ctx, Grant{
		ID:            first.ID,
		Goal:          "keep receipts current",
		CoordinatorID: "mgmt",
		ScopeKind:     ScopeSelected,
		FolderID:      billing.ID,
		SnapshotJSON:  `["chat-a"]`,
		ActionJSON:    `["read","organize","execute"]`,
		Origin:        OriginPerson,
	})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "expand") {
		t.Fatalf("added class: %v", err)
	}
	_, err = s.PutGrant(ctx, Grant{
		ID:            first.ID,
		Goal:          "keep receipts current",
		CoordinatorID: "mgmt",
		ScopeKind:     ScopeFolderDynamic,
		FolderID:      billing.ID,
		ActionJSON:    `["read","organize"]`,
		Origin:        OriginPerson,
	})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "expand") {
		t.Fatalf("enlarged scope: %v", err)
	}
	narrower, err := s.PutGrant(ctx, Grant{
		ID:            first.ID,
		Goal:          "keep receipts current",
		CoordinatorID: "mgmt",
		ScopeKind:     ScopeSelected,
		FolderID:      billing.ID,
		SnapshotJSON:  `["chat-a"]`,
		ActionJSON:    `["read"]`,
		Origin:        OriginPerson,
	})
	if err != nil || narrower.Revision != first.Revision+1 || narrower.ActionJSON != `["read"]` {
		t.Fatalf("narrower update: %+v, %v", narrower, err)
	}
	child, err := s.PutGrant(ctx, Grant{
		Goal:          "delegate execute",
		CoordinatorID: "child",
		ScopeKind:     ScopeSelected,
		FolderID:      billing.ID,
		SnapshotJSON:  `["chat-a"]`,
		ActionJSON:    `["execute"]`,
		Issuer:        first.ID,
		Origin:        OriginPerson,
	})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "expand") {
		t.Fatalf("issuer expand: %+v, %v", child, err)
	}
	listed, err := s.ListGrants(ctx, "mgmt")
	if err != nil || len(listed) != 1 || listed[0].ID != first.ID {
		t.Fatalf("list: %+v, %v", listed, err)
	}
	got, err := s.GetGrant(ctx, first.ID)
	if err != nil || got.ID != first.ID {
		t.Fatalf("get: %+v, %v", got, err)
	}
}

func TestRevokeGrantStoresRevisionAndLeavesBindings(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "revoke.db"))
	createTestCollection(t, s, "Inbox")
	g, err := s.PutGrant(ctx, Grant{
		Goal:          "run the change",
		CoordinatorID: "mgmt",
		ActionJSON:    `["execute"]`,
		Origin:        OriginPerson,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RevokeGrant(ctx, g.ID, g.Revision+9)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revoke: %v", err)
	}
	revoked, err := s.RevokeGrant(ctx, g.ID, g.Revision)
	if err != nil || revoked.Status != GrantRevoked || revoked.RevocationRevision != g.Revision || revoked.Revision != g.Revision+1 {
		t.Fatalf("revoke: %+v, %v", revoked, err)
	}
	again, err := s.RevokeGrant(ctx, g.ID, 0)
	if err != nil || again.Status != GrantRevoked || again.Revision != revoked.Revision {
		t.Fatalf("idempotent revoke: %+v, %v", again, err)
	}
	_, err = s.PutGrant(ctx, Grant{
		ID:            g.ID,
		Goal:          "run the change",
		CoordinatorID: "mgmt",
		ActionJSON:    `["execute"]`,
		Origin:        OriginPerson,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("put on revoked: %v", err)
	}
}

func TestRequestKeyIsUniqueAndBindRuntimeRefusesASecondInstance(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "bind.db"))
	createTestCollection(t, s, "Inbox")
	first, err := s.PutBinding(ctx, ExecutionBinding{
		ID:             "model-binding-id",
		RequestKey:     "launch-1",
		EquivalenceKey: "issue-42",
		GrantID:        "grant-1",
		CoordinatorID:  "mgmt",
		OwnerChatID:    "mgmt",
		Road:           RoadSessionTask,
	})
	if err != nil || first.ID == "model-binding-id" || first.State != BindReserved || first.WorkID == "" {
		t.Fatalf("reserved: %+v, %v", first, err)
	}
	owned, err := s.ListBindingsForChat(ctx, "mgmt")
	if err != nil || len(owned) != 1 || owned[0].RequestKey != "launch-1" {
		t.Fatalf("list for chat: %+v, %v", owned, err)
	}
	unbound, err := s.ListUnboundBindings(ctx)
	if err != nil || len(unbound) != 1 || unbound[0].RunInstanceID != "" {
		t.Fatalf("unbound reserved: %+v, %v", unbound, err)
	}
	again, err := s.PutBinding(ctx, ExecutionBinding{
		RequestKey:     "launch-1",
		EquivalenceKey: "other",
		OwnerChatID:    "other",
	})
	if err != nil || again.ID != first.ID || again.EquivalenceKey != "issue-42" {
		t.Fatalf("request key reuse: %+v, %v", again, err)
	}
	admitted, err := s.BindRuntime(ctx, "launch-1", "run-aaa", "session:1")
	if err != nil || admitted.State != BindBound || admitted.RunInstanceID != "run-aaa" || admitted.BoundAt == "" || admitted.RuntimeRef != "session:1" {
		t.Fatalf("bind: %+v, %v", admitted, err)
	}
	still, err := s.ListUnboundBindings(ctx)
	if err != nil || len(still) != 0 {
		t.Fatalf("bound row must leave the recover scan: %+v, %v", still, err)
	}
	same, err := s.BindRuntime(ctx, "launch-1", "run-aaa", "session:1")
	if err != nil || same.RunInstanceID != "run-aaa" {
		t.Fatalf("idempotent bind: %+v, %v", same, err)
	}
	_, err = s.BindRuntime(ctx, "launch-1", "run-bbb", "session:2")
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "binding") {
		t.Fatalf("second instance: %v", err)
	}
	got, err := s.BindingByRequestKey(ctx, "launch-1")
	if err != nil || got.RunInstanceID != "run-aaa" {
		t.Fatalf("by key: %+v, %v", got, err)
	}
	joined, err := s.BindingByEquivalence(ctx, "issue-42")
	if err != nil || joined.ID != first.ID {
		t.Fatalf("by equivalence: %+v, %v", joined, err)
	}
	named, err := s.GetBinding(ctx, first.ID)
	if err != nil || named.RequestKey != "launch-1" {
		t.Fatalf("get: %+v, %v", named, err)
	}
}

func TestBlankPutGrantInitializesV5Only(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "blank.db")
	s := openTestStore(t, path)
	got, err := s.PutGrant(ctx, Grant{Goal: "do the work", CoordinatorID: "mgmt", ActionJSON: `["execute"]`, Origin: OriginPerson})
	if err != nil || got.ID == "" {
		t.Fatalf("blank put: %+v, %v", got, err)
	}
	if s.SchemaVersion() != 5 {
		t.Fatalf("schema %d, want 5", s.SchemaVersion())
	}
	app, version := fileUserVersion(t, path)
	if app != applicationID || version != 5 {
		t.Fatalf("blank write left application %d version %d", app, version)
	}
	requireTables(t, path, v5TableNames...)
	if extra := tablesNamed(t, path, laterPhaseTableNames...); len(extra) != 0 {
		t.Fatalf("wave 4 created later-phase tables: %v", extra)
	}
	bound, err := s.PutBinding(ctx, ExecutionBinding{RequestKey: "k", EquivalenceKey: "eq", Road: RoadBashRun})
	if err != nil || bound.Road != RoadBashRun || bound.State != BindReserved {
		t.Fatalf("bash-run reserved: %+v, %v", bound, err)
	}
}
