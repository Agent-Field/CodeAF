package cellindex

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestRefreshDoesNotRebookALegacyTurnAggregate(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	path := (session.Place{Dir: c.Root}).Transcript()
	if err := session.SeedConversation(path, "", "Legacy chat", "hello"); err != nil {
		t.Fatal(err)
	}
	d, err := session.ReadDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// A pre-model-call-receipt transcript stores one same-model aggregate per turn.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = json.NewEncoder(f).Encode(map[string]any{"type": "usage", "timestamp": at.Add(time.Minute).Format(time.RFC3339Nano), "usage": map[string]any{"model": "m", "calls": 2, "input": 30, "output": 10, "costUsd": 0.75}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	// The historical live ledger already booked each of the two provider calls.
	for i, usd := range []float64{0.25, 0.50} {
		session.RecordImportedUsage(session.UsageLedgerPath(), session.UsageLine{At: at.Add(time.Duration(i) * time.Second), Session: d.ID, Model: "m", Calls: 1, Input: 10 * (i + 1), Output: 5, USD: usd})
	}
	session.FlushUsage()
	if _, err := Refresh(c, ""); err != nil {
		t.Fatal(err)
	}
	if got := totalOf(t).micro; got != 750000 {
		t.Fatalf("refresh charged %d microUSD; want 750000: both saved calls were already booked", got)
	}
}
