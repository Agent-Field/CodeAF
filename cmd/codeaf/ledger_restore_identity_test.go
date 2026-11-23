package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestLedgerInteriorRestoreReplaysExistingIdentityOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), session.UsageLedgerName)
	defer session.StopUsageWriter(path)
	for i := range 200 {
		id := fmt.Sprintf("historical-%03d", i)
		if i == 100 {
			id = "a"
		}
		session.RecordUsage(path, session.UsageLine{ID: id, Input: 1, USD: .001})
	}
	if !session.FlushUsage() {
		t.Fatal("initial flush")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := bytes.Replace(raw, []byte(`"id":"a"`), []byte(`"id":"b"`), 1)
	if bytes.Equal(raw, rewritten) {
		t.Fatal("fixture did not rewrite")
	}
	rewritten = append(rewritten, []byte("{\"at\":\"2026-11-03T12:00:00Z\",\"id\":\"external\",\"input\":1,\"usd\":0.001}\n")...)
	if err = os.WriteFile(path, rewritten, 0600); err != nil {
		t.Fatal(err)
	}
	// The replayed b is already in the restored ledger, outside sampled ends.
	session.RecordUsage(path, session.UsageLine{ID: "b", Input: 1, USD: .001})
	if !session.FlushUsage() {
		t.Fatal("replay flush")
	}
	rows, err := session.ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var total float64
	for _, row := range rows {
		total += row.USD
	}
	n := len(rows)
	t.Logf("interior rewrite plus growth, replay restored identity expected=$0.201000 recorded=$%.6f rows=%d want_rows=201", total, n)
	if math.Abs(total-.201) > 1e-9 || n != 201 {
		t.Error("index misses restored identity and records the same paid call twice")
	}
}
