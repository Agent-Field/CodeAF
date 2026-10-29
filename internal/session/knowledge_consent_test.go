package session

import (
	"context"
	"database/sql"
	"fmt"
	"os"
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
	collect(t, mustSubmit(t, agents[0], "Explain how the queue keeps work durable"))
	deadline := time.Now().Add(8 * time.Second)
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

// consentChat is one persisted chat on its own profile, as the knowledge tests
// above build them, answering every turn with the same words.
func consentChat(t *testing.T, profile string) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("The queue keeps work durable."), nil
		},
	}}, func(c *Config) {
		c.ProfileDir = profile
		c.SessionFile = filepath.Join(t.TempDir(), "chat.jsonl")
		c.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")
	})
	return agent
}

// writeConsent is another process writing this profile's consent row.
func writeConsent(t *testing.T, path string, at time.Time) {
	t.Helper()
	uri := sqliteuri.File(path)
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	payload := fmt.Sprintf(`{"enabled":true,"schema":1,"created_at":%q}`, at.UTC().Format(time.RFC3339Nano))
	if _, err = db.Exec("INSERT INTO state(key,payload) VALUES('config',?) ON CONFLICT(key) DO UPDATE SET payload=excluded.payload", payload); err != nil {
		t.Fatal(err)
	}
}

// A vault reset elsewhere moves the opt-in time. A turn written before the new
// opt-in is not captured, although this process still remembers the old one.
func TestKnowledgeAdmitsSourcesOnTheVaultsCurrentOptIn(t *testing.T) {
	profile := t.TempDir()
	agent := consentChat(t, profile)
	vault := agent.Knowledge()
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	if enabled, since := vault.CaptureConsent(); !enabled || since.IsZero() {
		t.Fatalf("own opt-in not seen: (%v, %v)", enabled, since)
	}
	writeConsent(t, filepath.Join(vault.Directory(), "knowledge.sqlite"), time.Now().Add(time.Hour))
	collect(t, mustSubmit(t, agent, "Explain how the queue keeps work durable"))
	time.Sleep(2500 * time.Millisecond)
	snapshot, err := vault.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Pending != 0 {
		t.Fatalf("a turn older than the vault's opt-in was captured: %+v", snapshot)
	}
}

// A vault deleted elsewhere withdraws consent at once for what is admitted:
// the chat's sources wait in its journal, and are captured when consent that
// covers them comes back.
func TestKnowledgeKeepsSourcesWhileTheVaultIsGone(t *testing.T) {
	profile := t.TempDir()
	agent := consentChat(t, profile)
	vault := agent.Knowledge()
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	optIn := vault.EnabledSince()
	if enabled, _ := vault.CaptureConsent(); !enabled {
		t.Fatal("own opt-in not seen")
	}
	path := filepath.Join(vault.Directory(), "knowledge.sqlite")
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	collect(t, mustSubmit(t, agent, "Explain how the queue keeps work durable"))
	time.Sleep(2500 * time.Millisecond)
	snapshot, err := vault.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Pending != 0 || snapshot.Enabled {
		t.Fatalf("a deleted vault took sources: %+v", snapshot)
	}
	writeConsent(t, path, optIn)
	deadline := time.Now().Add(consentWait)
	for {
		if snapshot, err = vault.Snapshot(); err != nil {
			t.Fatal(err)
		}
		if snapshot.Pending == 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("sources written while the vault was gone were dropped: %+v", snapshot)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// consentWait is one consent recheck plus a capture tick and room for a loaded
// machine.
const consentWait = 8 * time.Second
