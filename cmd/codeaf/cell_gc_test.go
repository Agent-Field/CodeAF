package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestCellGCOnAHomeWithNoCellsReportsAnEmptyBudget(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_HOME", t.TempDir())
	var out bytes.Buffer
	if err := runCellIn([]string{"gc", "--dry-run"}, &out, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "cells hold 0.0 MiB") {
		t.Fatalf("report: %q", out.String())
	}
}

func TestCellNamesItsVerbsWhenAskedForANonsense(t *testing.T) {
	celltest.Available(t)
	if err := runCell([]string{"nope"}); err == nil || !strings.Contains(err.Error(), "gc") {
		t.Fatalf("want a usage line naming gc, got %v", err)
	}
}

// C25 exercises explicit gc and ordinary reopen. A withheld file, a user
// exclusion and a dependency folder survive, while a captured file is restored.
func TestCellGCKeepsExcludedFilesAndRestoresCapturedFiles(t *testing.T) {
	// It seals and restores for real, so it needs the engine, not just the flag.
	celltest.RequireEngine(t)
	t.Setenv("CODEAF_HOME", t.TempDir())

	t.Setenv("CODEAF_SYNC_URL", "off")
	t.Setenv("CODEAF_CELL_BUDGET_GB", "0.000000001")
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(c.Root, "work")
	if err := session.SaveMeta(c.Root, session.Meta{ID: c.ID, Owned: true, Workspace: work}); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{".env": "LOCAL_TOKEN=kept-here\n", ".furrowpolicy": "exclude excluded.txt\nexclude nested/private.txt\n", "excluded.txt": "excluded", "package-lock.json": "{}", "node_modules/a/index.js": "dependency", "nested/private.txt": "private", "nested/public.txt": "public", "captured.txt": "captured bytes\x00\xff"}
	for name, body := range files {
		path := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	engine := cellstore.EngineFor(work)
	if _, err := engine.Seal(t.Context(), c, cellstore.TurnInfo{Trigger: cellstore.AgentRun}); err != nil {
		t.Fatal(err)
	}
	m := cellBudget()
	if err := m.Open(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	// The gc door has a real recent-open grace; its persisted timestamp lets
	// this fixture be old without making the shared laptop wait ten minutes.
	ledger := filepath.Join(engine.LocalDir(c), "budget.json")
	raw, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	entry["opened_ms"] = float64(1)
	raw, _ = json.Marshal(entry)
	if err := os.WriteFile(ledger, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var dry bytes.Buffer
	if err := runCellIn([]string{"gc", "--dry-run"}, &dry, work); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dry.String(), "    captured.txt\n") {
		t.Fatalf("dry run: %s", &dry)
	}
	for _, name := range []string{".env", "excluded.txt", "node_modules", "nested", ".furrowpolicy", "package-lock.json"} {
		if strings.Contains(dry.String(), "    "+name+"\n") {
			t.Fatalf("dry run would remove excluded %s: %s", name, &dry)
		}
	}
	var out bytes.Buffer
	if err := runCellIn([]string{"gc"}, &out, work); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "captured.txt")); !os.IsNotExist(err) {
		t.Fatalf("gc did not remove captured file: %v, %s", err, &out)
	}
	for _, name := range []string{".env", "excluded.txt", "node_modules/a/index.js", "nested/private.txt", "nested/public.txt"} {
		raw, err := os.ReadFile(filepath.Join(work, name))
		if err != nil || string(raw) != files[name] {
			t.Fatalf("gc changed %s: %q %v", name, raw, err)
		}
	}
	budgetOnOpen(c.Root)
	for name, body := range files {
		raw, err := os.ReadFile(filepath.Join(work, name))
		if err != nil || string(raw) != body {
			t.Fatalf("reopen changed %s: %q %v", name, raw, err)
		}
	}
	// Make the restored cell old and eligible again before opening a different
	// chat. The old implementation would start collection from that open.
	if err := os.WriteFile(ledger, rawLedgerWithOldOpen(t, ledger), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	budgetOnOpen(fresh.Root)
	if _, err := os.Stat(filepath.Join(work, "captured.txt")); err != nil {
		t.Fatal("opening another chat removed working files", err)
	}
	// A new open previously launched an automatic sweep of this old cell.
	// No sweep stamp is now written and no workspace file is removed.
	if _, err := os.Stat(filepath.Join(engine.Root(), "sweep.stamp")); !os.IsNotExist(err) {
		t.Fatalf("open started a sweep: %v", err)
	}
}

// rawLedgerWithOldOpen makes the fixture eligible without a real-time wait.
func rawLedgerWithOldOpen(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	entry["opened_ms"] = float64(1)
	raw, err = json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
