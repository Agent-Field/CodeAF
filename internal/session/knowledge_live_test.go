package session

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/sqliteuri"
)

func TestKnowledgeLiveNativeCompiler(t *testing.T) {
	if os.Getenv("CODEAF_KNOWLEDGE_LIVE") != "1" {
		t.Skip("set CODEAF_KNOWLEDGE_LIVE=1 for explicit live verification")
	}
	key := config.APIKeyAt(config.ProfileDir())
	if key == "" {
		key = os.Getenv("OPENROUTER_API_KEY")
	}
	if key == "" {
		t.Fatal("live verification requires the existing OpenRouter credential")
	}
	root := t.TempDir()
	profile := t.TempDir()
	v := knowledge.Open(profile)
	writeKnowledgeChat(t, root, "routing", "routing-chat", knowledgeMessage("Cedar is our background job system. Cedar stores queued jobs in SQLite. A worker claims one queued job in a transaction, runs it outside the transaction, and marks it completed afterwards. We chose this design so chat remains responsive while background jobs run."))
	writeKnowledgeChat(t, root, "recovery", "recovery-chat", knowledgeMessage("Cedar retries failed jobs at most twice. Each job has a stable request ID, so retrying its submission does not create duplicate jobs. If the application closes, unfinished Cedar jobs remain in SQLite and resume on the next launch. These are design requirements, not a claim that Cedar is deployed."))
	t.Setenv("PATH", t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- v.Run(ctx, KnowledgeHistory(root), func() string { return key }) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("live worker did not stop")
		}
	}()
	var state knowledge.Snapshot
	for {
		var err error
		state, err = v.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if state.Build.Phase == knowledge.BuildFailed {
			t.Fatalf("live build failed: %+v", state.Build)
		}
		if state.Build.Phase == knowledge.BuildReady && state.Pending == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("live build deadline")
		case <-time.After(200 * time.Millisecond):
		}
	}
	hits, err := v.Search("Cedar", 10)
	if err != nil || len(hits) == 0 {
		t.Fatalf("live knowledge not searchable: %v", err)
	}
	cited := false
	for _, hit := range hits {
		if len(hit.Page.Evidence) > 0 {
			cited = true
		}
	}
	if !cited {
		t.Fatal("live pages lost original source citations")
	}
	uri := sqliteuri.File(filepath.Join(v.Directory(), "knowledge.sqlite"))
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT model,role,count(*) FROM receipts GROUP BY model,role ORDER BY role")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var model, role string
		var count int
		if err = rows.Scan(&model, &role, &count); err != nil {
			t.Fatal(err)
		}
		if model != "deepseek/deepseek-v4.1-flash" {
			t.Fatalf("live receipt substituted %s", model)
		}
		t.Logf("verified %s: %s (%d calls)", model, role, count)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("native live build: %d sources, %d pages, $%.6f recorded cost; Python unavailable", state.Build.Sources, len(state.Pages), state.CostUSD)
}

func TestKnowledgeLiveSupportContracts(t *testing.T) {
	if os.Getenv("CODEAF_KNOWLEDGE_LIVE") != "1" {
		t.Skip("set CODEAF_KNOWLEDGE_LIVE=1 for explicit live verification")
	}
	key := config.APIKeyAt(config.ProfileDir())
	if key == "" {
		key = os.Getenv("OPENROUTER_API_KEY")
	}
	if key == "" {
		t.Fatal("live verification requires the existing OpenRouter credential")
	}
	v := knowledge.Open(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	t.Setenv("PATH", t.TempDir())
	report, err := v.RunSupportContracts(ctx, key)
	if len(report) > 0 {
		t.Logf("frozen support contracts: %s", report)
	}
	if err != nil {
		t.Fatal(err)
	}
}
