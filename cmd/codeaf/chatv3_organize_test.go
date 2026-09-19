package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/wsapi"
)

func TestEnqueueOrganizeCoalescesTheSameSourceRevision(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	path := collectionsPath()
	jobs, err := workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = jobs.Close() })
	if _, err := jobs.Create(context.Background(), "Inbox"); err != nil {
		t.Fatal(err)
	}

	enqueue := v3EnqueueOrganize(jobs)
	if enqueue == nil {
		t.Fatal("a working jobs store handed back no enqueue callback")
	}
	enqueue("aaaaaaaaaaaaaaaa", "3:deadbeef")
	enqueue("aaaaaaaaaaaaaaaa", "3:deadbeef")

	first, err := jobs.LeaseJob(context.Background(), []string{workspace.JobOrganize}, session.OrganizeOwner, "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if first.CoalesceKey != session.OrganizeCoalesceKey("aaaaaaaaaaaaaaaa", "3:deadbeef") {
		t.Fatalf("coalesce key %q", first.CoalesceKey)
	}
	_, err = jobs.LeaseJob(context.Background(), []string{workspace.JobOrganize}, session.OrganizeOwner, "2099-01-01T00:00:00Z")
	if err == nil {
		t.Fatal("the same source revision leased twice")
	}
}

func TestOrganizePassLeavesPendingWhenTheOrganizerIsUnbound(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	jobs, err := workspace.Open(collectionsPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.Create(context.Background(), "Inbox"); err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.EnqueueJob(context.Background(), workspace.Job{
		Type:        workspace.JobOrganize,
		ChatID:      "aaaaaaaaaaaaaaaa",
		SourceRev:   "1:cafe",
		CoalesceKey: session.OrganizeCoalesceKey("aaaaaaaaaaaaaaaa", "1:cafe"),
	}); err != nil {
		t.Fatal(err)
	}
	_ = jobs.Close()

	if err := v3OrganizePass(t.TempDir())(context.Background()); err != nil {
		t.Fatal(err)
	}

	again, err := workspace.Open(collectionsPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	job, err := again.LeaseJob(context.Background(), []string{workspace.JobOrganize}, "probe", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("unbound organizer consumed the pending row: %v", err)
	}
	if job.State != workspace.JobLeased {
		t.Fatalf("job state %q", job.State)
	}
}

func TestStandingTickerWiresOrganizeOntoThePass(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	store, err := standing.Open(home.Join("v3", "standing"))
	if err != nil {
		t.Fatal(err)
	}
	ticker, err := v3StandingTicker(store)
	if err != nil {
		t.Fatal(err)
	}
	if ticker.Organize == nil {
		t.Fatal("the standing pass would skip observe_and_organize")
	}
}

func TestPersonRemoveOfAnOrganizerPlacementSuppressesThatEvidence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "collections.db")
	svc, err := wsapi.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	jobs, err := workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = jobs.Close() })

	adapter := newFoldersAdapter(svc, jobs)
	billing, err := adapter.CreateFolder(ctx, "Billing")
	if err != nil {
		t.Fatal(err)
	}
	chat := workspace.Ref{Kind: workspace.ConversationKind, ID: "aaaaaaaaaaaaaaaa"}
	evidence := "customers must authenticate"
	if err := svc.AddPlacement(ctx, billing.ID, chat, workspace.Provenance{
		Origin:   workspace.OriginOrganizer,
		Actor:    "organizer",
		Reason:   "related",
		Evidence: evidence,
	}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RemovePlacement(ctx, billing.ID, chat.ID); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(evidence))
	blocked, err := jobs.IsSuppressed(ctx, billing.ID, chat, hex.EncodeToString(sum[:]))
	if err != nil || !blocked {
		t.Fatalf("organizer evidence was not suppressed: blocked=%v err=%v", blocked, err)
	}
}

func TestPersonRemoveOfAPersonPlacementDoesNotSuppress(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "collections.db")
	svc, err := wsapi.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	jobs, err := workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = jobs.Close() })

	adapter := newFoldersAdapter(svc, jobs)
	billing, err := adapter.CreateFolder(ctx, "Billing")
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.AddPlacement(ctx, billing.ID, "aaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RemovePlacement(ctx, billing.ID, "aaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	chat := workspace.Ref{Kind: workspace.ConversationKind, ID: "aaaaaaaaaaaaaaaa"}
	sum := sha256.Sum256(nil)
	blocked, err := jobs.IsSuppressed(ctx, billing.ID, chat, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	if blocked {
		t.Fatal("a person-filed chat suppressed evidence on remove")
	}
}

func TestHistoryOpensWhenMemoryIsOff(t *testing.T) {
	fresh := t.TempDir()
	t.Setenv("HOME", fresh)
	t.Setenv(home.EnvVar, filepath.Join(fresh, ".codeaf"))
	profile := t.TempDir()
	registry := config.NewSettings(config.SettingsOptions{
		ProfileDir: profile,
		ModelValue: func(slot string) string { return slot + "/model" },
		SetModel:   func(string, string) error { return nil },
		SplitPct:   func() int { return 0 },
	})
	row, found := registry.Row(config.KeyMemoryEnabled)
	if !found {
		t.Fatal("no memory.enabled row")
	}
	if err := row.Apply(config.MemoryOff); err != nil {
		t.Fatal(err)
	}
	if brain := v3Memory(profile); brain != nil {
		_ = brain.Close()
		t.Fatal("memory off still opened the remember store")
	}
	history := v3History()
	if history == nil {
		t.Fatal("memory off opened no history reader")
	}
	t.Cleanup(func() { _ = history.Close() })
	if v3SearchSeam(history) == nil {
		t.Fatal("the search place was absent with history open")
	}
	if v3SearchSeam(nil) != nil {
		t.Fatal("a nil history still built a search place")
	}
}

func TestAMemoryOffProcessStillCarriesHistoryAndEnqueue(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	registry := config.NewSettings(config.SettingsOptions{
		ProfileDir: profile,
		ModelValue: func(slot string) string { return slot + "/model" },
		SetModel:   func(string, string) error { return nil },
		SplitPct:   func() int { return 0 },
	})
	row, found := registry.Row(config.KeyMemoryEnabled)
	if !found {
		t.Fatal("no memory.enabled row")
	}
	if err := row.Apply(config.MemoryOff); err != nil {
		t.Fatal(err)
	}
	proc, err := openV3Process("chat")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proc.closeAll)
	if proc.Memory != nil {
		t.Fatal("memory off still handed the session a remember store")
	}
	if proc.History == nil {
		t.Fatal("memory off opened no conversation history")
	}
	launch, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if launch.Config.Memory != nil {
		t.Fatal("a memory-off launch still set Config.Memory")
	}
	if launch.Config.ConversationHistory != proc.History {
		t.Fatal("a memory-off launch did not set ConversationHistory")
	}
	if launch.Config.EnqueueOrganize == nil {
		t.Fatal("a memory-off launch dropped observe_and_organize enqueue")
	}
}
