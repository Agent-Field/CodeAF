package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

const v1FixtureDDL = `
CREATE TABLE collections (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL
);
CREATE TABLE memberships (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 collection_id TEXT NOT NULL REFERENCES collections(id),
 kind TEXT NOT NULL CHECK(kind IN ('collection','conversation','task','standing','artifact')),
 ref_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 target_collection TEXT REFERENCES collections(id),
 CHECK ((kind='collection' AND target_collection IS NOT NULL AND target_collection=ref_id)
     OR (kind!='collection' AND target_collection IS NULL)),
 UNIQUE(collection_id,kind,ref_id,session_id)
);
CREATE INDEX memberships_reference ON memberships(kind,ref_id,session_id);
INSERT INTO collections(id,name) VALUES ('billing-v1','Billing');
INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection)
 VALUES ('billing-v1','conversation','old-chat','',NULL);
`

func writeV1Fixture(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(v1FixtureDDL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmtPragmas(1)); err != nil {
		t.Fatal(err)
	}
}

func fmtPragmas(version int) string {
	return "PRAGMA application_id=1095123788; PRAGMA user_version=" + strconv.Itoa(version)
}

func fileUserVersion(t *testing.T, path string) (app, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow("PRAGMA application_id").Scan(&app); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return app, version
}

func TestV1ListDoesNotMigrate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	writeV1Fixture(t, path)
	s := openTestStore(t, path)
	if s.SchemaVersion() != 1 {
		t.Fatalf("open reported version %d", s.SchemaVersion())
	}
	got, err := s.Collections(ctx)
	if err != nil || len(got) != 1 || got[0].ID != "billing-v1" || got[0].Name != "Billing" {
		t.Fatalf("v1 list: %v, %v", got, err)
	}
	if got[0].Purpose != "" || got[0].Lifecycle != "" || got[0].Revision != 0 || got[0].CreatedAt != "" {
		t.Fatalf("v1 list filled v2 fields before a write: %+v", got[0])
	}
	members, err := s.Members(ctx, "billing-v1")
	if err != nil || len(members) != 1 || members[0].ID != "old-chat" {
		t.Fatalf("v1 members: %v, %v", members, err)
	}
	parents, err := s.CollectionsFor(ctx, Ref{Kind: ConversationKind, ID: "old-chat"})
	if err != nil || len(parents) != 1 || parents[0].ID != "billing-v1" {
		t.Fatalf("v1 find: %v, %v", parents, err)
	}
	if _, _, _, err := s.RootState(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("v1 root state: %v", err)
	}
	if _, err := s.WhyHere(ctx, "billing-v1", Ref{Kind: ConversationKind, ID: "old-chat"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("v1 why-here: %v", err)
	}
	events, err := s.Events(ctx, "billing-v1", Ref{Kind: ConversationKind, ID: "old-chat"})
	if err != nil || len(events) != 0 {
		t.Fatalf("v1 events: %v, %v", events, err)
	}
	guidance, err := s.ListGuidance(ctx, "")
	if err != nil || len(guidance) != 0 {
		t.Fatalf("v1 list guidance: %v, %v", guidance, err)
	}
	people, err := s.ListParticipants(ctx, "mgmt")
	if err != nil || len(people) != 0 {
		t.Fatalf("v1 list participants: %v, %v", people, err)
	}
	queued, err := s.ListPendingDeliveries(ctx, "old-chat")
	if err != nil || len(queued) != 0 {
		t.Fatalf("v1 list deliveries: %v, %v", queued, err)
	}
	grants, err := s.ListGrants(ctx, "mgmt")
	if err != nil || len(grants) != 0 {
		t.Fatalf("v1 list grants: %v, %v", grants, err)
	}
	suppressed, err := s.IsSuppressed(ctx, "billing-v1", Ref{Kind: ConversationKind, ID: "old-chat"}, "hash")
	if err != nil || suppressed {
		t.Fatalf("v1 is-suppressed: %v, %v", suppressed, err)
	}
	app, version := fileUserVersion(t, path)
	if app != applicationID || version != 1 {
		t.Fatalf("list migrated the file: application %d version %d", app, version)
	}
	if s.SchemaVersion() != 1 {
		t.Fatalf("handle version moved to %d without a write", s.SchemaVersion())
	}
}

func TestFirstWriteMigratesV1ToV5(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	writeV1Fixture(t, path)
	s := openTestStore(t, path)
	listed, err := s.Collections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ref := Ref{Kind: ConversationKind, ID: "new-chat"}
	if err := s.Add(ctx, "billing-v1", ref); err != nil {
		t.Fatal(err)
	}
	app, version := fileUserVersion(t, path)
	if app != applicationID || version != schemaVersion {
		t.Fatalf("first write left application %d version %d", app, version)
	}
	if s.SchemaVersion() != schemaVersion {
		t.Fatalf("handle version %d after write", s.SchemaVersion())
	}
	after, err := s.Collections(ctx)
	if err != nil || len(after) != 1 || after[0].ID != listed[0].ID || after[0].Name != "Billing" {
		t.Fatalf("migration dropped the v1 collection: %v, %v", after, err)
	}
	members, err := s.Members(ctx, "billing-v1")
	if err != nil || len(members) != 2 {
		t.Fatalf("migration dropped memberships: %v, %v", members, err)
	}
	why, err := s.WhyHere(ctx, "billing-v1", ref)
	if err != nil || why.Origin != OriginPerson || why.Action != ActionAdd {
		t.Fatalf("migrated add wrote no event: %+v, %v", why, err)
	}
	requireTables(t, path, v3TableNames...)
	requireTables(t, path, v4TableNames...)
	requireTables(t, path, v5TableNames...)
	if extra := tablesNamed(t, path, laterPhaseTableNames...); len(extra) != 0 {
		t.Fatalf("wave 4 created later-phase tables: %v", extra)
	}
}

func TestMoveCycleIsRefusedInOneTransaction(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "cycle.db"))
	billing := createTestCollection(t, s, "Billing")
	receipts := createTestCollection(t, s, "Receipts")
	if err := s.Add(ctx, billing.ID, Ref{Kind: CollectionKind, ID: receipts.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.Move(ctx, billing.ID, receipts.ID, Ref{Kind: CollectionKind, ID: billing.ID}, Provenance{Origin: OriginPerson, Reason: "loop"}); !errors.Is(err, ErrCycle) {
		t.Fatalf("move closed a cycle: %v", err)
	}
	members, err := s.Members(ctx, billing.ID)
	if err != nil || len(members) != 1 || members[0].ID != receipts.ID {
		t.Fatalf("refused move changed the source: %v, %v", members, err)
	}
	nested, err := s.Members(ctx, receipts.ID)
	if err != nil || len(nested) != 0 {
		t.Fatalf("refused move wrote the destination: %v, %v", nested, err)
	}
}

func TestMembershipEventsRecordAddRemoveMoveAndWhyHere(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "events.db"))
	fixed := time.Date(2026, 9, 18, 20, 0, 0, 0, time.UTC)
	s.clock = func() time.Time { return fixed }
	billing := createTestCollection(t, s, "Billing")
	receipts := createTestCollection(t, s, "Receipts")
	security := createTestCollection(t, s, "Security")
	chat := Ref{Kind: ConversationKind, ID: "shared"}
	add := Provenance{Origin: OriginPerson, Reason: "current billing thread", Actor: "person"}
	if err := s.AddWith(ctx, billing.ID, chat, add); err != nil {
		t.Fatal(err)
	}
	if err := s.AddWith(ctx, security.ID, chat, Provenance{Origin: OriginPerson, Reason: "also security"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Move(ctx, billing.ID, receipts.ID, chat, Provenance{Origin: OriginPerson, Reason: "belongs with receipts"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveWith(ctx, security.ID, chat, Provenance{Origin: OriginPerson, Reason: "done there"}); err != nil {
		t.Fatal(err)
	}

	whyReceipts, err := s.WhyHere(ctx, receipts.ID, chat)
	if err != nil || whyReceipts.Action != ActionAdd || whyReceipts.Reason != "belongs with receipts" || whyReceipts.At != "2026-09-18T20:00:00Z" {
		t.Fatalf("why receipts: %+v, %v", whyReceipts, err)
	}
	whyBilling, err := s.WhyHere(ctx, billing.ID, chat)
	if err != nil || whyBilling.Action != ActionRemove || whyBilling.Reason != "belongs with receipts" {
		t.Fatalf("why billing after move: %+v, %v", whyBilling, err)
	}
	whySecurity, err := s.WhyHere(ctx, security.ID, chat)
	if err != nil || whySecurity.Action != ActionRemove || whySecurity.Reason != "done there" {
		t.Fatalf("why security: %+v, %v", whySecurity, err)
	}

	history, err := s.Events(ctx, billing.ID, chat)
	if err != nil || len(history) != 2 || history[0].Action != ActionAdd || history[1].Action != ActionRemove {
		t.Fatalf("billing history: %+v, %v", history, err)
	}
	members, err := s.Members(ctx, receipts.ID)
	if err != nil || len(members) != 1 || members[0] != chat {
		t.Fatalf("move dest: %v, %v", members, err)
	}
	if got, err := s.Members(ctx, billing.ID); err != nil || len(got) != 0 {
		t.Fatalf("move left the source: %v, %v", got, err)
	}
	if got, err := s.Members(ctx, security.ID); err != nil || len(got) != 0 {
		t.Fatalf("remove left security: %v, %v", got, err)
	}
}

func TestRootIsNotStoredAsACollectionOrMembership(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "root.db")
	s := openTestStore(t, path)
	billing := createTestCollection(t, s, "Billing")
	if err := s.Add(ctx, billing.ID, Ref{Kind: ConversationKind, ID: "filed"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Collections(ctx)
	if err != nil || len(got) != 1 || got[0].ID != billing.ID {
		t.Fatalf("created a Root collection: %v, %v", got, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var collections, memberships, roots int
	if err := db.QueryRow("SELECT count(*) FROM collections").Scan(&collections); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM memberships").Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM root_state WHERE id=1").Scan(&roots); err != nil {
		t.Fatal(err)
	}
	if collections != 1 || memberships != 1 || roots != 1 {
		t.Fatalf("root leaked into storage: collections=%d memberships=%d root_state=%d", collections, memberships, roots)
	}
	var named int
	if err := db.QueryRow("SELECT count(*) FROM collections WHERE id='root' OR name='Root'").Scan(&named); err != nil {
		t.Fatal(err)
	}
	if named != 0 {
		t.Fatal("stored virtual Root as a collection row")
	}
}

func TestOneConversationCanSitInTwoCollections(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "dual.db"))
	billing := createTestCollection(t, s, "Billing")
	security := createTestCollection(t, s, "Security")
	chat := Ref{Kind: ConversationKind, ID: "aabbccddeeff0011"}
	if err := s.Add(ctx, billing.ID, chat); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, security.ID, chat); err != nil {
		t.Fatal(err)
	}
	parents, err := s.CollectionsFor(ctx, chat)
	if err != nil || len(parents) != 2 {
		t.Fatalf("dual placement: %v, %v", parents, err)
	}
	if err := s.Move(ctx, billing.ID, createTestCollection(t, s, "Receipts").ID, chat, Provenance{Origin: OriginPerson}); err != nil {
		t.Fatal(err)
	}
	parents, err = s.CollectionsFor(ctx, chat)
	if err != nil || len(parents) != 2 {
		t.Fatalf("move dropped the other placement: %v, %v", parents, err)
	}
	names := map[string]bool{parents[0].Name: true, parents[1].Name: true}
	if !names["Receipts"] || !names["Security"] || names["Billing"] {
		t.Fatalf("move rewrote unrelated placements: %v", parents)
	}
}

func TestIdempotentAddRemoveAndIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "idemp.db"))
	inbox := createTestCollection(t, s, "Inbox")
	chat := Ref{Kind: ConversationKind, ID: "chat"}
	p := Provenance{Origin: OriginPerson, Reason: "file it", IdempotencyKey: "add-1"}
	if err := s.AddWith(ctx, inbox.ID, chat, p); err != nil {
		t.Fatal(err)
	}
	if err := s.AddWith(ctx, inbox.ID, chat, p); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, inbox.ID, chat); err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(ctx, inbox.ID, chat)
	if err != nil || len(events) != 1 || events[0].IdempotencyKey != "add-1" {
		t.Fatalf("repeated add wrote extra history: %+v, %v", events, err)
	}
	other := createTestCollection(t, s, "Other")
	err = s.AddWith(ctx, other.ID, chat, Provenance{Origin: OriginPerson, IdempotencyKey: "add-1"})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "different operation") {
		t.Fatalf("same key on a different add: %v", err)
	}
	err = s.RemoveWith(ctx, inbox.ID, chat, Provenance{Origin: OriginPerson, IdempotencyKey: "add-1"})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "different operation") {
		t.Fatalf("same key on a remove: %v", err)
	}
	if got, err := s.Members(ctx, inbox.ID); err != nil || len(got) != 1 {
		t.Fatalf("refused key reuse mutated membership: %v, %v", got, err)
	}
	if err := s.RemoveWith(ctx, inbox.ID, chat, Provenance{Origin: OriginPerson, IdempotencyKey: "rm-1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveWith(ctx, inbox.ID, chat, Provenance{Origin: OriginPerson, IdempotencyKey: "rm-1"}); err != nil {
		t.Fatal(err)
	}
	events, err = s.Events(ctx, inbox.ID, chat)
	if err != nil || len(events) != 2 || events[1].Action != ActionRemove {
		t.Fatalf("repeated remove wrote extra history: %+v, %v", events, err)
	}
	members, err := s.Members(ctx, inbox.ID)
	if err != nil || len(members) != 0 {
		t.Fatalf("idempotent remove left membership: %v, %v", members, err)
	}
}

func TestCollectionJSONOmitsEmptyV2Fields(t *testing.T) {
	c := Collection{ID: "abc", Name: "Billing"}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"id":"abc","name":"Billing"}` {
		t.Fatalf("list JSON grew extra fields: %s", raw)
	}
}

func TestCreateInsertsV2CollectionColumns(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "create.db"))
	fixed := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	s.clock = func() time.Time { return fixed }
	got := createTestCollection(t, s, "Inbox")
	want := Collection{
		ID:        got.ID,
		Name:      "Inbox",
		Lifecycle: LifecycleActive,
		Revision:  1,
		CreatedAt: "2026-09-18T12:00:00Z",
		UpdatedAt: "2026-09-18T12:00:00Z",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Create returned %+v, want %+v", got, want)
	}
	listed, err := s.Collections(ctx)
	if err != nil || !reflect.DeepEqual(listed, []Collection{want}) {
		t.Fatalf("list after Create: %v, %v", listed, err)
	}
}

func TestUnknownOriginIsRefused(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "origin.db"))
	inbox := createTestCollection(t, s, "Inbox")
	err := s.AddWith(ctx, inbox.ID, Ref{Kind: ConversationKind, ID: "chat"}, Provenance{Origin: "magic"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown origin: %v", err)
	}
	members, err := s.Members(ctx, inbox.ID)
	if err != nil || len(members) != 0 {
		t.Fatalf("invalid origin leaked: %v, %v", members, err)
	}
}

var v3TableNames = []string{"guidance", "jobs", "observations", "placement_suppressions", "proposed_actions"}

var v4TableNames = []string{"participants", "deliveries"}

var v5TableNames = []string{"grants", "execution_bindings"}

var laterPhaseTableNames = []string{
	"grant", "execution", "launch_intents", "responsibilities",
}

func tablesNamed(t *testing.T, path string, names ...string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	quoted := make([]string, len(names))
	args := make([]any, len(names))
	for i, name := range names {
		quoted[i] = "?"
		args[i] = name
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name IN (`+strings.Join(quoted, ",")+`)`, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var found []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		found = append(found, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}

func requireTables(t *testing.T, path string, names ...string) {
	t.Helper()
	found := tablesNamed(t, path, names...)
	if len(found) != len(names) {
		t.Fatalf("tables %v, want %v", found, names)
	}
}
