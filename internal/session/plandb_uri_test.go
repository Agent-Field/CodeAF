package session

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

func TestPlanLedgerReadersUseLiteralDatabasePath(t *testing.T) {
	name := "plan%23 #&.db"
	if runtime.GOOS != "windows" {
		name = "plan%23 #&?.db"
	}
	path := filepath.Join(t.TempDir(), name)
	store, err := plandb.Open(path, "mock", "root", "Mock task", "", "mock-chat")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.AddSpend("root", "mock/model", "work", .25, 11, 7); err != nil {
		t.Fatal(err)
	}
	if got := planSpendByTask(path)["root"]; got != .25 {
		t.Fatalf("task spend=%v", got)
	}
	if got := planUsageByTask(path)["root"]; got.model != "mock/model" || got.tokens != 18 {
		t.Fatalf("task usage=%+v", got)
	}
	got := planSpendBySeat(path, "mock-chat", time.Time{})
	if len(got) != 1 {
		t.Fatalf("seat spend=%+v", got)
	}
}

func TestPlanLedgerReadersDoNotCreateMissingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing%23.db")
	if len(planSpendByTask(path)) != 0 || len(planUsageByTask(path)) != 0 || len(planSpendBySeat(path, "mock-chat", time.Time{})) != 0 {
		t.Fatal("missing database returned ledger rows")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("read created a missing database: %v", err)
	}
}
