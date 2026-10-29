package session

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/sqliteuri"
)

func TestKnowledgeTickersShareOneConsentRead(t *testing.T) {
	profile := t.TempDir()
	path := filepath.Join(profile, "knowledge", "knowledge.sqlite")
	var opens atomic.Int64
	t.Cleanup(knowledge.SetDatabaseHookForTest(func(opened string) {
		if opened == path {
			opens.Add(1)
		}
	}))

	started := time.Now()
	agents := make([]*Agent, 20)
	for i := range agents {
		agents[i], _ = newTestAgent(t, &scriptedCompleter{steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("The queue keeps work durable."), nil
			},
		}}, func(c *Config) {
			c.ProfileDir = profile
			c.SessionFile = filepath.Join(t.TempDir(), "chat.jsonl")
			c.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")
		})
	}

	// Real tickers establish the idle cost through the running session entry point.
	time.Sleep(5 * time.Second)
	elapsed := time.Since(started)
	count := opens.Load()
	limit := int64(1 + elapsed/(5*time.Second))
	t.Logf("20 idle chat tickers: %d database opens in %s (limit %d)", count, elapsed, limit)
	if count > limit {
		t.Fatalf("idle chats opened knowledge.sqlite %d times; want <= %d", count, limit)
	}
	if count == 0 {
		t.Fatal("knowledge tickers never read consent")
	}

	// Raw SQL models another process: it cannot invalidate this process's view.
	uri := sqliteuri.File(path)
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	since := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.Exec("INSERT INTO state(key,payload) VALUES('config',?)", fmt.Sprintf(`{"enabled":true,"schema":1,"created_at":%q}`, since))
	closeErr := db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	deadline := time.Now().Add(8 * time.Second)
	collect(t, mustSubmit(t, agents[0], "Explain how the queue keeps work durable"))
	for {
		snapshot, err := agents[0].Knowledge().Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Pending == 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("external opt-in did not capture both turn sources within one recheck: %+v", snapshot)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
