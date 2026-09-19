package workspace

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeV2Fixture(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(v2CollectionsDDL + v2HistoryDDL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO collections(id,name,purpose,lifecycle,revision,created_at,updated_at)
 VALUES ('billing-v2','Billing','','active',1,'','')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection)
 VALUES ('billing-v2','conversation','old-chat','',NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO root_state(id,purpose,revision,updated_at) VALUES (1,'',1,'')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmtPragmas(2)); err != nil {
		t.Fatal(err)
	}
}

func TestV2ListDoesNotMigrate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v2.db")
	writeV2Fixture(t, path)
	s := openTestStore(t, path)
	if s.SchemaVersion() != 2 {
		t.Fatalf("open reported version %d", s.SchemaVersion())
	}
	got, err := s.Collections(ctx)
	if err != nil || len(got) != 1 || got[0].ID != "billing-v2" {
		t.Fatalf("v2 list: %v, %v", got, err)
	}
	guidance, err := s.ListGuidance(ctx, "billing-v2")
	if err != nil || len(guidance) != 0 {
		t.Fatalf("v2 list guidance: %v, %v", guidance, err)
	}
	if tablesNamed(t, path, v3TableNames...) != nil {
		t.Fatalf("v2 list created v3 tables: %v", tablesNamed(t, path, v3TableNames...))
	}
	if tablesNamed(t, path, v4TableNames...) != nil {
		t.Fatalf("v2 list created v4 tables: %v", tablesNamed(t, path, v4TableNames...))
	}
	if tablesNamed(t, path, v5TableNames...) != nil {
		t.Fatalf("v2 list created v5 tables: %v", tablesNamed(t, path, v5TableNames...))
	}
	app, version := fileUserVersion(t, path)
	if app != applicationID || version != 2 {
		t.Fatalf("list migrated the file: application %d version %d", app, version)
	}
}

func TestFirstWriteMigratesV2ToV5(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v2.db")
	writeV2Fixture(t, path)
	s := openTestStore(t, path)
	if err := s.Add(ctx, "billing-v2", Ref{Kind: ConversationKind, ID: "new-chat"}); err != nil {
		t.Fatal(err)
	}
	app, version := fileUserVersion(t, path)
	if app != applicationID || version != schemaVersion {
		t.Fatalf("first write left application %d version %d", app, version)
	}
	requireTables(t, path, v3TableNames...)
	requireTables(t, path, v4TableNames...)
	requireTables(t, path, v5TableNames...)
	if extra := tablesNamed(t, path, laterPhaseTableNames...); len(extra) != 0 {
		t.Fatalf("wave 4 created later-phase tables: %v", extra)
	}
	members, err := s.Members(ctx, "billing-v2")
	if err != nil || len(members) != 2 {
		t.Fatalf("v2 memberships dropped: %v, %v", members, err)
	}
}

func TestPutGuidanceBumpsRootAndListsByScope(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "guidance.db"))
	security := createTestCollection(t, s, "Security")
	before, _, _ := mustRoot(t, s)
	root, err := s.PutGuidance(ctx, Guidance{Text: "authenticate receipt links", Origin: OriginPerson, Actor: "person"})
	if err != nil || root.ScopeID != "" || root.Status != GuidanceActive || root.ID == "" {
		t.Fatalf("root guidance: %+v, %v", root, err)
	}
	afterRoot, _, _ := mustRoot(t, s)
	if afterRoot != before+1 {
		t.Fatalf("root guidance revision %d, want %d", afterRoot, before+1)
	}
	folder, err := s.PutGuidance(ctx, Guidance{ScopeID: security.ID, Text: "standing for this folder", Origin: OriginPerson})
	if err != nil || folder.ScopeID != security.ID {
		t.Fatalf("folder guidance: %+v, %v", folder, err)
	}
	replaced, err := s.PutGuidance(ctx, Guidance{
		ScopeID: security.ID, Text: "updated standing", Origin: OriginPerson, Supersedes: folder.ID,
	})
	if err != nil || replaced.Supersedes != folder.ID {
		t.Fatalf("supersede: %+v, %v", replaced, err)
	}
	listed, err := s.ListGuidance(ctx, security.ID)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list folder: %+v, %v", listed, err)
	}
	if listed[0].Status != GuidanceSuperseded || listed[1].Status != GuidanceActive {
		t.Fatalf("supersession: %+v", listed)
	}
	roots, err := s.ListGuidance(ctx, "")
	if err != nil || len(roots) != 1 || roots[0].ID != root.ID {
		t.Fatalf("list root: %+v, %v", roots, err)
	}
}

func TestEnqueueJobCoalescesPendingAndLeased(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "coalesce.db"))
	createTestCollection(t, s, "Inbox")
	first, err := s.EnqueueJob(ctx, Job{Type: JobOrganize, CoalesceKey: "chat-1:rev-a", ChatID: "chat-1", SourceRev: "rev-a"})
	if err != nil || first.State != JobPending || first.Type != JobOrganize {
		t.Fatalf("enqueue: %+v, %v", first, err)
	}
	second, err := s.EnqueueJob(ctx, Job{Type: JobOrganize, CoalesceKey: "chat-1:rev-a", ChatID: "chat-1", SourceRev: "rev-a"})
	if err != nil || second.ID != first.ID {
		t.Fatalf("pending coalesce: %+v vs %+v, %v", second, first, err)
	}
	leased, err := s.LeaseJob(ctx, []string{JobOrganize}, "tick", "2099-01-01T00:00:00Z")
	if err != nil || leased.ID != first.ID || leased.State != JobLeased || leased.Fence == "" || leased.Attempt != 1 {
		t.Fatalf("lease: %+v, %v", leased, err)
	}
	third, err := s.EnqueueJob(ctx, Job{CoalesceKey: "chat-1:rev-a"})
	if err != nil || third.ID != first.ID || third.State != JobLeased {
		t.Fatalf("leased coalesce: %+v, %v", third, err)
	}
	if err := s.FinishJob(ctx, leased.ID, leased.Fence, JobCompleted, ""); err != nil {
		t.Fatal(err)
	}
	fourth, err := s.EnqueueJob(ctx, Job{CoalesceKey: "chat-1:rev-a"})
	if err != nil || fourth.ID == first.ID || fourth.State != JobPending {
		t.Fatalf("completed key reused: %+v vs %s, %v", fourth, first.ID, err)
	}
}

func TestLeaseFenceMismatchRefusesAndExpiredReturnsToPending(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fence.db")
	s := openTestStore(t, path)
	createTestCollection(t, s, "Inbox")
	fixed := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	s.clock = func() time.Time { return fixed }
	job, err := s.EnqueueJob(ctx, Job{CoalesceKey: "chat:1"})
	if err != nil {
		t.Fatal(err)
	}
	leased, err := s.LeaseJob(ctx, nil, "tick", "2026-09-19T11:00:00Z")
	if err != nil || leased.ID != job.ID {
		t.Fatalf("lease: %+v, %v", leased, err)
	}
	err = s.FinishJob(ctx, leased.ID, leased.Fence, JobCompleted, "")
	requireFenceConflict(t, err)
	err = s.HeartbeatJob(ctx, leased.ID, leased.Fence, "2026-09-19T13:00:00Z")
	requireFenceConflict(t, err)
	err = s.FinishJob(ctx, leased.ID, "not-the-fence", JobFailed, "nope")
	requireFenceConflict(t, err)

	again, err := s.LeaseJob(ctx, []string{JobOrganize}, "other", "2026-09-19T13:00:00Z")
	if err != nil || again.ID != job.ID || again.Fence == leased.Fence || again.Owner != "other" || again.Attempt != 2 {
		t.Fatalf("re-lease after expiry: %+v, %v", again, err)
	}
	if err := s.FinishJob(ctx, again.ID, again.Fence, JobDeferred, "budget"); err != nil {
		t.Fatal(err)
	}
	if got := jobState(t, path, job.ID); got != JobDeferred {
		t.Fatalf("finished state %q", got)
	}
}

func TestSuppressKeyBlocksIdenticalEvidenceOnly(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "suppress.db"))
	security := createTestCollection(t, s, "Security")
	chat := Ref{Kind: ConversationKind, ID: "new-chat"}
	before, _, _ := mustRoot(t, s)
	if err := s.Suppress(ctx, security.ID, chat, "evidence-a", Provenance{Origin: OriginPerson, Actor: "person"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Suppress(ctx, security.ID, chat, "evidence-a", Provenance{Origin: OriginPerson}); err != nil {
		t.Fatal(err)
	}
	after, _, _ := mustRoot(t, s)
	if after != before+1 {
		t.Fatalf("idempotent suppress bumped root from %d to %d", before, after)
	}
	same, err := s.IsSuppressed(ctx, security.ID, chat, "evidence-a")
	if err != nil || !same {
		t.Fatalf("same evidence: %v, %v", same, err)
	}
	other, err := s.IsSuppressed(ctx, security.ID, chat, "evidence-b")
	if err != nil || other {
		t.Fatalf("new evidence: %v, %v", other, err)
	}
}

func TestObservationAndProposalPersist(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "records.db"))
	createTestCollection(t, s, "Inbox")
	obs, err := s.PutObservation(ctx, Observation{ChatID: "chat", SourceRev: "1", Body: "purpose guess", Model: "embed"})
	if err != nil || obs.ID == "" || obs.CreatedAt == "" {
		t.Fatalf("observation: %+v, %v", obs, err)
	}
	first, err := s.PutProposal(ctx, Proposal{ChatID: "chat", PlanJSON: `{"result":"no-action"}`, IdempotencyKey: "plan-1"})
	if err != nil || first.ID == "" {
		t.Fatalf("proposal: %+v, %v", first, err)
	}
	again, err := s.PutProposal(ctx, Proposal{ChatID: "chat", PlanJSON: `{"result":"add"}`, IdempotencyKey: "plan-1"})
	if err != nil || again.ID != first.ID || again.PlanJSON != first.PlanJSON {
		t.Fatalf("proposal idempotency: %+v vs %+v, %v", again, first, err)
	}
}

func TestBlankCreateWritesV5WithGrantTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	s := openTestStore(t, path)
	createTestCollection(t, s, "Inbox")
	app, version := fileUserVersion(t, path)
	if app != applicationID || version != schemaVersion {
		t.Fatalf("fresh write left application %d version %d", app, version)
	}
	requireTables(t, path, v3TableNames...)
	requireTables(t, path, v4TableNames...)
	requireTables(t, path, v5TableNames...)
	if extra := tablesNamed(t, path, laterPhaseTableNames...); len(extra) != 0 {
		t.Fatalf("fresh v5 created later-phase tables: %v", extra)
	}
}

func TestCancelAndLookupFollowTheExplicitKey(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "existing.db"))
	cols, err := s.Collections(ctx)
	if err != nil || len(cols) != 0 {
		t.Fatalf("blank store already had folders: %+v, %v", cols, err)
	}
	first, err := s.EnqueueJob(ctx, Job{Type: JobOrganize, CoalesceKey: OrganizeExistingKey})
	if err != nil || first.State != JobPending {
		t.Fatalf("enqueue: %+v, %v", first, err)
	}
	cols, err = s.Collections(ctx)
	if err != nil || len(cols) != 0 {
		t.Fatalf("enqueue invented folders: %+v, %v", cols, err)
	}
	again, err := s.EnqueueJob(ctx, Job{Type: JobOrganize, CoalesceKey: OrganizeExistingKey})
	if err != nil || again.ID != first.ID {
		t.Fatalf("coalesce: %+v vs %s, %v", again, first.ID, err)
	}
	got, err := s.LookupJob(ctx, JobOrganize, OrganizeExistingKey)
	if err != nil || got.ID != first.ID || got.State != JobPending {
		t.Fatalf("lookup pending: %+v, %v", got, err)
	}
	cancelled, err := s.CancelJob(ctx, JobOrganize, OrganizeExistingKey)
	if err != nil || cancelled.ID != first.ID || cancelled.State != JobCancelled {
		t.Fatalf("cancel: %+v, %v", cancelled, err)
	}
	status, err := s.LookupJob(ctx, JobOrganize, OrganizeExistingKey)
	if err != nil || status.State != JobCancelled {
		t.Fatalf("lookup cancelled: %+v, %v", status, err)
	}
	next, err := s.EnqueueJob(ctx, Job{Type: JobOrganize, CoalesceKey: OrganizeExistingKey})
	if err != nil || next.ID == first.ID || next.State != JobPending {
		t.Fatalf("re-enqueue after cancel: %+v vs %s, %v", next, first.ID, err)
	}
}

func TestFinishAfterCancelIsNoOpAndRestartKeepsPending(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "resume.db")
	s := openTestStore(t, path)
	job, err := s.EnqueueJob(ctx, Job{Type: JobOrganize, CoalesceKey: OrganizeExistingKey})
	if err != nil {
		t.Fatal(err)
	}
	leased, err := s.LeaseJob(ctx, []string{JobOrganize}, "tick", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelJob(ctx, JobOrganize, OrganizeExistingKey); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishJob(ctx, leased.ID, leased.Fence, JobCompleted, "add"); err != nil {
		t.Fatalf("finish after cancel: %v", err)
	}
	if got := jobState(t, path, job.ID); got != JobCancelled {
		t.Fatalf("cancelled job became %q", got)
	}
	s.Close()

	again := openTestStore(t, path)
	pending, err := again.EnqueueJob(ctx, Job{Type: JobOrganize, CoalesceKey: OrganizeExistingKey, ChatID: "chat-a"})
	if err != nil {
		t.Fatal(err)
	}
	found, err := again.LookupJob(ctx, JobOrganize, OrganizeExistingKey)
	if err != nil || found.ID != pending.ID || found.State != JobPending {
		t.Fatalf("restart lost the queued survey: %+v, %v", found, err)
	}
}

func requireFenceConflict(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
	if !strings.Contains(err.Error(), "fence") {
		t.Fatalf("conflict %q does not name the fence", err)
	}
}

func jobState(t *testing.T, path, id string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var state string
	if err := db.QueryRow("SELECT state FROM jobs WHERE id=?", id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}
