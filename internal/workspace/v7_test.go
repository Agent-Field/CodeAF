package workspace

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func writeV6Fixture(t *testing.T, path string) {
	t.Helper()
	writeV5Fixture(t, path)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`ALTER TABLE execution_bindings ADD COLUMN joiner_json TEXT NOT NULL DEFAULT '[]'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmtPragmas(6)); err != nil {
		t.Fatal(err)
	}
}

func TestFirstWriteMigratesV6ToJobTimings(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v6.db")
	writeV6Fixture(t, path)
	s := openTestStore(t, path)
	job, err := s.EnqueueJob(ctx, Job{Type: JobOrganize, CoalesceKey: "chat-1:rev-a", ChatID: "chat-1", SourceRev: "rev-a"})
	if err != nil || job.EnqueuedAt == "" {
		t.Fatalf("enqueue after v6: %+v, %v", job, err)
	}
	app, version := fileUserVersion(t, path)
	if app != applicationID || version != schemaVersion {
		t.Fatalf("first write left application %d version %d", app, version)
	}
	leased, err := s.LeaseJob(ctx, []string{JobOrganize}, "tick", "2099-01-01T00:00:00Z")
	if err != nil || leased.StartedAt == "" {
		t.Fatalf("lease timings: %+v, %v", leased, err)
	}
	if err := s.SetJobCursor(ctx, leased.ID, leased.Fence, "chat-next"); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishJob(ctx, leased.ID, leased.Fence, JobCompleted, "add"); err != nil {
		t.Fatal(err)
	}
	got, err := s.LookupJob(ctx, JobOrganize, "chat-1:rev-a")
	if err != nil || got.Cursor != "chat-next" || got.CommittedAt == "" || got.EnqueuedAt == "" || got.StartedAt == "" {
		t.Fatalf("committed timings: %+v, %v", got, err)
	}
}

func TestCreateInNestsInOneTransaction(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "create-in.db"))
	billing := createTestCollection(t, s, "Billing")
	child, err := s.CreateIn(ctx, "Receipts", billing.ID)
	if err != nil || child.ID == "" || child.Name != "Receipts" {
		t.Fatalf("CreateIn: %+v, %v", child, err)
	}
	members, err := s.Members(ctx, billing.ID)
	if err != nil || len(members) != 1 || members[0].Kind != CollectionKind || members[0].ID != child.ID {
		t.Fatalf("nested members %+v, %v", members, err)
	}
	_, err = s.CreateIn(ctx, "Orphan", "missing-parent")
	if err == nil {
		t.Fatal("CreateIn with a missing parent wrote a Root orphan")
	}
	root, err := s.CreateIn(ctx, "Inbox", "")
	if err != nil || root.Name != "Inbox" {
		t.Fatalf("empty parent should Create at Root: %+v, %v", root, err)
	}
	parents, err := s.CollectionsFor(ctx, Ref{Kind: CollectionKind, ID: root.ID})
	if err != nil || len(parents) != 0 {
		t.Fatalf("Root create grew a parent: %+v, %v", parents, err)
	}
}

func TestSupersedePendingChatJobsLeavesTheKeptKey(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "supersede.db"))
	createTestCollection(t, s, "Inbox")
	old, err := s.EnqueueJob(ctx, Job{Type: JobOrganize, ChatID: "chat-a", SourceRev: "1:old", CoalesceKey: OrganizeChatKey("chat-a", "1:old")})
	if err != nil {
		t.Fatal(err)
	}
	keep, err := s.EnqueueJob(ctx, Job{Type: JobOrganize, ChatID: "chat-a", SourceRev: "2:new", CoalesceKey: OrganizeChatKey("chat-a", "2:new")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SupersedePendingChatJobs(ctx, "chat-a", keep.CoalesceKey); err != nil {
		t.Fatal(err)
	}
	stale, err := s.LookupJob(ctx, JobOrganize, old.CoalesceKey)
	if err != nil || stale.State != JobCancelled {
		t.Fatalf("stale %+v, %v", stale, err)
	}
	live, err := s.LookupJob(ctx, JobOrganize, keep.CoalesceKey)
	if err != nil || live.ID != keep.ID || live.State != JobPending {
		t.Fatalf("kept %+v, %v", live, err)
	}
}
