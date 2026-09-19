package workspace

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func writeV5Fixture(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(v2CollectionsDDL + v2HistoryDDL + v3DDL + v4DDL + v5DDL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO collections(id,name,purpose,lifecycle,revision,created_at,updated_at)
 VALUES ('billing-v5','Billing','','active',1,'','')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO root_state(id,purpose,revision,updated_at) VALUES (1,'',1,'')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmtPragmas(5)); err != nil {
		t.Fatal(err)
	}
}

func TestV5ListDoesNotMigrateJoiners(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v5.db")
	writeV5Fixture(t, path)
	s := openTestStore(t, path)
	listed, err := s.ListBindingsForChat(ctx, "mgmt")
	if err != nil || len(listed) != 0 {
		t.Fatalf("v5 list bindings: %v, %v", listed, err)
	}
	app, version := fileUserVersion(t, path)
	if app != applicationID || version != 5 {
		t.Fatalf("list migrated the file: application %d version %d", app, version)
	}
}

func TestRecordJoinerListsTheSecondChatOnTheSameRow(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "join.db"))
	createTestCollection(t, s, "Inbox")
	first, err := s.PutBinding(ctx, ExecutionBinding{
		RequestKey: "launch-join", EquivalenceKey: "issue-42",
		OwnerChatID: "chat-a", CoordinatorID: "chat-a", Road: RoadSessionTask,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined, err := s.RecordJoiner(ctx, first.RequestKey, "chat-b")
	if err != nil || joined.ID != first.ID || !joined.JoinedBy("chat-b") {
		t.Fatalf("record joiner: %+v, %v", joined, err)
	}
	if joined.JoinedBy("chat-a") {
		t.Fatal("owner must not paint as a joiner")
	}
	again, err := s.RecordJoiner(ctx, first.RequestKey, "chat-b")
	if err != nil || again.ID != first.ID {
		t.Fatalf("idempotent joiner: %+v, %v", again, err)
	}
	owned, err := s.ListBindingsForChat(ctx, "chat-a")
	if err != nil || len(owned) != 1 || owned[0].ID != first.ID {
		t.Fatalf("owner list: %+v, %v", owned, err)
	}
	seen, err := s.ListBindingsForChat(ctx, "chat-b")
	if err != nil || len(seen) != 1 || seen[0].ID != first.ID || !seen[0].JoinedBy("chat-b") {
		t.Fatalf("joiner list: %+v, %v", seen, err)
	}
	if !strings.Contains(seen[0].JoinerJSON, "chat-b") {
		t.Fatalf("joiner json %q", seen[0].JoinerJSON)
	}
}

func TestFirstWriteMigratesV5ToJoiners(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v5.db")
	writeV5Fixture(t, path)
	s := openTestStore(t, path)
	got, err := s.PutBinding(ctx, ExecutionBinding{RequestKey: "k", OwnerChatID: "mgmt"})
	if err != nil || got.RequestKey != "k" {
		t.Fatalf("v5 write: %+v, %v", got, err)
	}
	app, version := fileUserVersion(t, path)
	if app != applicationID || version != schemaVersion {
		t.Fatalf("first write left application %d version %d", app, version)
	}
	joined, err := s.RecordJoiner(ctx, "k", "peer")
	if err != nil || !joined.JoinedBy("peer") {
		t.Fatalf("joiner after migrate: %+v, %v", joined, err)
	}
}
