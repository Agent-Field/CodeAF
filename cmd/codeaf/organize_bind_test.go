package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/embed"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/wsapi"
	_ "modernc.org/sqlite"
)

const (
	organizeBillingID  = "bbbbbbbbbbbbbbbb"
	organizeSecurityID = "ssssssssssssssss"
)

func TestV3OrganizerIsBound(t *testing.T) {
	if v3Organizer() == nil {
		t.Fatal("v3Organizer must return the production type, not nil")
	}
	if _, ok := v3Organizer().(*doorOrganizer); !ok {
		t.Fatalf("v3Organizer returned %T, want *doorOrganizer", v3Organizer())
	}
}

func TestBoundOrganizerAppliesAnAddFromCitedEvidence(t *testing.T) {
	ctx := context.Background()
	homeDir := isolateOrganizeHome(t)
	writeOrganizeChat(t, organizeSecurityID, "customers must authenticate receipt links")
	writeOrganizeChat(t, organizeBillingID, "emailed download links for the receipt")

	live := liveOrganizeEmbedder()
	svc, _ := openV3FolderServiceWith(live)
	if svc == nil {
		t.Fatal("folders did not open")
	}
	security, err := svc.CreateFolder(ctx, "Security")
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.Close()

	job := enqueueOrganizeJob(t, organizeBillingID, "1:cafe")
	work := newDoorOrganizer(live, addSecurityPlan(security.ID, organizeBillingID))
	runOrganizeJob(t, work)

	state, detail := readJobFinish(t, homeDir, job.ID)
	if state != workspace.JobCompleted {
		t.Fatalf("bound organizer left the job %s (%s), want completed", state, detail)
	}
	if detail != wsapi.PlanAdd && detail != session.PlanAdd {
		t.Fatalf("finish detail %q, want add", detail)
	}
	assertChatInFolder(t, security.ID, organizeBillingID)
}

func TestOrganizeIngestsJournalsIntoDiscovery(t *testing.T) {
	ctx := context.Background()
	isolateOrganizeHome(t)
	writeOrganizeChat(t, organizeBillingID, "emailed download links for the receipt")

	live := liveOrganizeEmbedder()
	svc, _ := openV3FolderServiceWith(live)
	if svc == nil {
		t.Fatal("folders did not open")
	}
	if _, err := svc.CreateFolder(ctx, "Billing"); err != nil {
		t.Fatal(err)
	}
	_ = svc.Close()

	enqueueOrganizeJob(t, organizeBillingID, "1:cafe")
	work := newDoorOrganizer(live, func(context.Context, session.OrganizeRequest) (session.OrganizePlan, error) {
		return session.OrganizePlan{Kind: session.PlanNoAction, Model: "organize-test"}, nil
	})
	runOrganizeJob(t, work)

	again, adapter := openV3FolderServiceWith(live)
	if again == nil || adapter == nil {
		t.Fatal("reopen after organize must bind discovery.db")
	}
	t.Cleanup(func() { _ = again.Close() })
	hits, err := again.SearchEvidence(ctx, wsapi.SearchQuery{Query: "receipt", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !searchHitHas(hits, "receipt") {
		t.Fatalf("hybrid search missed ingested journal passages: %+v", hits)
	}
	view, err := again.IndexProgress(ctx)
	if err != nil || view.Passages < 1 {
		t.Fatalf("discovery.db has no passages after Organize: %+v %v", view, err)
	}
}

func TestDownEmbedderIngestsWithoutDummyVectors(t *testing.T) {
	ctx := context.Background()
	homeDir := isolateOrganizeHome(t)
	writeOrganizeChat(t, organizeBillingID, "emailed download links for the receipt")

	svc, _ := openV3FolderServiceWith(nil)
	if svc == nil {
		t.Fatal("folders did not open")
	}
	if _, err := svc.CreateFolder(ctx, "Billing"); err != nil {
		t.Fatal(err)
	}
	_ = svc.Close()

	job := enqueueOrganizeJob(t, organizeBillingID, "1:cafe")
	called := false
	work := newDoorOrganizer(nil, func(context.Context, session.OrganizeRequest) (session.OrganizePlan, error) {
		called = true
		return session.OrganizePlan{Kind: session.PlanAdd, Model: "organize-test"}, nil
	})
	runOrganizeJob(t, work)
	if called {
		t.Fatal("a down embedder still called RoleOrganize")
	}

	state, detail := readJobFinish(t, homeDir, job.ID)
	if state != workspace.JobDeferred {
		t.Fatalf("down embedder finished as %s (%s), want deferred", state, detail)
	}
	if detail != embed.LabelDelayed {
		t.Fatalf("down embedder detail %q, want %q", detail, embed.LabelDelayed)
	}

	again, adapter := openV3FolderServiceWith(nil)
	if again == nil || adapter == nil {
		t.Fatal("reopen after delayed organize must still bind discovery.db")
	}
	t.Cleanup(func() { _ = again.Close() })
	hits, err := again.SearchEvidence(ctx, wsapi.SearchQuery{Query: "receipt", Limit: 10})
	if err != nil || !searchHitHas(hits, "receipt") {
		t.Fatalf("nil embedder must still store passages: %+v %v", hits, err)
	}
	assertNoDummyVectors(t)
}

func TestKeywordOnlyPlanRecordsNoAction(t *testing.T) {
	ctx := context.Background()
	homeDir := isolateOrganizeHome(t)
	writeOrganizeChat(t, organizeBillingID, "emailed download links for the receipt")

	live := liveOrganizeEmbedder()
	svc, _ := openV3FolderServiceWith(live)
	if svc == nil {
		t.Fatal("folders did not open")
	}
	security, err := svc.CreateFolder(ctx, "Security")
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.Close()

	job := enqueueOrganizeJob(t, organizeBillingID, "1:cafe")
	work := newDoorOrganizer(live, func(context.Context, session.OrganizeRequest) (session.OrganizePlan, error) {
		raw, err := json.Marshal(wsapi.Action{
			Kind: wsapi.PlanAdd, CollectionID: security.ID,
			Ref:    workspace.Ref{Kind: workspace.ConversationKind, ID: organizeBillingID},
			Reason: "keyword overlap",
		})
		if err != nil {
			return session.OrganizePlan{}, err
		}
		return session.OrganizePlan{Kind: session.PlanAdd, Actions: []json.RawMessage{raw}}, nil
	})
	runOrganizeJob(t, work)

	state, detail := readJobFinish(t, homeDir, job.ID)
	if state != workspace.JobCompleted || detail != wsapi.PlanNoAction {
		t.Fatalf("keyword-only finished %s %s, want completed no-action", state, detail)
	}
	assertChatNotInFolder(t, security.ID, organizeBillingID)
}

func TestMissingEvidenceRecordsNoAction(t *testing.T) {
	ctx := context.Background()
	homeDir := isolateOrganizeHome(t)
	// Another chat keeps the index caught up so this job is not the delayed
	// empty-index path. The billing id has no journal, so there is no evidence
	// to file from — no-action, not invented membership.
	writeOrganizeChat(t, organizeSecurityID, "customers must authenticate receipt links")

	live := liveOrganizeEmbedder()
	svc, _ := openV3FolderServiceWith(live)
	if svc == nil {
		t.Fatal("folders did not open")
	}
	security, err := svc.CreateFolder(ctx, "Security")
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.Close()

	job := enqueueOrganizeJob(t, organizeBillingID, "1:cafe")
	work := newDoorOrganizer(live, addSecurityPlan(security.ID, organizeBillingID))
	runOrganizeJob(t, work)

	state, detail := readJobFinish(t, homeDir, job.ID)
	if state != workspace.JobCompleted || detail != wsapi.PlanNoAction {
		t.Fatalf("missing evidence finished %s %s, want completed no-action", state, detail)
	}
	assertChatNotInFolder(t, security.ID, organizeBillingID)
}

func isolateOrganizeHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(home.EnvVar, dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	return dir
}

func liveOrganizeEmbedder() embed.Embedder {
	wire := &scriptedEmbedWire{vector: []float32{1, 0}, model: "openai/text-embedding-3-small"}
	return embed.New(wire, "openai/text-embedding-3-small", nil)
}

func writeOrganizeChat(t *testing.T, id, said string) {
	t.Helper()
	writeV3Session(t, filepath.Join(session.PlacesRoot(), "lab"), id, said, time.Now())
}

func enqueueOrganizeJob(t *testing.T, chatID, rev string) workspace.Job {
	t.Helper()
	jobs, err := workspace.Open(collectionsPath())
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close()
	if _, err := jobs.Create(context.Background(), "Inbox"); err != nil {
		t.Fatal(err)
	}
	job, err := jobs.EnqueueJob(context.Background(), workspace.Job{
		Type:        workspace.JobOrganize,
		ChatID:      chatID,
		SourceRev:   rev,
		CoalesceKey: session.OrganizeCoalesceKey(chatID, rev),
	})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func runOrganizeJob(t *testing.T, work session.Organizer) {
	t.Helper()
	jobs, err := workspace.Open(collectionsPath())
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close()
	if err := session.ProcessOrganizeJobs(context.Background(), jobs, work, true, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func addSecurityPlan(folderID, chatID string) organizeFn {
	return func(context.Context, session.OrganizeRequest) (session.OrganizePlan, error) {
		raw, err := json.Marshal(wsapi.Action{
			Kind: wsapi.PlanAdd, CollectionID: folderID,
			Ref:    workspace.Ref{Kind: workspace.ConversationKind, ID: chatID},
			Reason: "authenticated receipt links",
		})
		if err != nil {
			return session.OrganizePlan{}, err
		}
		return session.OrganizePlan{
			Kind: session.PlanAdd, Model: "organize-test",
			Actions: []json.RawMessage{raw},
		}, nil
	}
}

func readJobFinish(t *testing.T, homeDir, id string) (state, detail string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(homeDir, "v3", "collections.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow(`SELECT state, IFNULL(error,'') FROM jobs WHERE id=?`, id).Scan(&state, &detail); err != nil {
		t.Fatal(err)
	}
	return state, detail
}

func assertChatInFolder(t *testing.T, folderID, chatID string) {
	t.Helper()
	svc, err := wsapi.Open(collectionsPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	_, members, err := svc.FolderSnapshot(context.Background(), folderID)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		if member.Ref.ID == chatID {
			return
		}
	}
	t.Fatalf("chat %s was not filed in %s: %+v", chatID, folderID, members)
}

func assertChatNotInFolder(t *testing.T, folderID, chatID string) {
	t.Helper()
	svc, err := wsapi.Open(collectionsPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	_, members, err := svc.FolderSnapshot(context.Background(), folderID)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		if member.Ref.ID == chatID {
			t.Fatalf("invented membership of %s in %s", chatID, folderID)
		}
	}
}

func searchHitHas(hits []wsapi.SearchHit, needle string) bool {
	for _, hit := range hits {
		if strings.Contains(hit.Passage, needle) {
			return true
		}
	}
	return false
}

func assertNoDummyVectors(t *testing.T) {
	t.Helper()
	db, err := sql.Open("sqlite", discoveryPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM passages WHERE vector IS NOT NULL AND length(vector)>0`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("down embedder stored %d dummy vectors", n)
	}
	var passages int
	if err := db.QueryRow(`SELECT COUNT(*) FROM passages`).Scan(&passages); err != nil {
		t.Fatal(err)
	}
	if passages < 1 {
		t.Fatal("down embedder wrote no passages")
	}
}
