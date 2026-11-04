package session

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func review2332Total(t *testing.T, path string) (float64, int) {
	t.Helper()
	rows, err := ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var total float64
	for _, r := range rows {
		total += r.USD
	}
	return total, len(rows)
}
func TestReview2332Rotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	defer StopUsageWriter(path)
	RecordUsage(path, UsageLine{ID: "a", Input: 1, USD: .001})
	if !FlushUsage() {
		t.Fatal("flush")
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	RecordUsage(path, UsageLine{ID: "b", Input: 1, USD: .002})
	RecordUsage(path, UsageLine{ID: "c", Input: 1, USD: .003})
	if !FlushUsage() {
		t.Fatal("flush")
	}
	total, n := review2332Total(t, path)
	old, oldN := review2332Total(t, path+".old")
	t.Logf("active expected=$0.005000 recorded=$%.6f rows=%d; archive expected=$0.001000 recorded=$%.6f rows=%d drops=%d", total, n, old, oldN, UsageDrops())
	if math.Abs(total-.005) > 1e-9 || n != 2 || math.Abs(old-.001) > 1e-9 || oldN != 1 {
		t.Fatal("new charges written to rotated inode, active ledger loses money")
	}
}
func TestReview2332ReplacementAfterExternalGrowth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	defer StopUsageWriter(path)
	RecordUsage(path, UsageLine{ID: "a", Input: 1, USD: .001})
	if !FlushUsage() {
		t.Fatal("flush")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("{\"at\":\"2026-11-03T12:00:00Z\",\"id\":\"external\",\"input\":1,\"usd\":0.004}\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	before := UsageDrops()
	RecordUsage(path, UsageLine{ID: "b", Input: 1, USD: .002})
	RecordUsage(path, UsageLine{ID: "c", Input: 1, USD: .003})
	if !FlushUsage() {
		t.Fatal("flush")
	}
	total, n := review2332Total(t, path)
	t.Logf("active expected=$0.005000 recorded=$%.6f rows=%d dropped=%d", total, n, UsageDrops()-before)
	if math.Abs(total-.005) > 1e-9 || n != 2 || UsageDrops() != before {
		t.Fatal("replacement detection discards first paid charge")
	}
}
func TestReview2332RewrittenLedgerDropsAbsentCharge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	defer StopUsageWriter(path)
	paid := UsageLine{ID: "a", At: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC), Input: 1, USD: .001}
	RecordUsage(path, paid)
	if !FlushUsage() {
		t.Fatal("flush")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := bytes.Replace(raw, []byte(`"id":"a"`), []byte(`"id":"b"`), 1)
	if bytes.Equal(raw, rewritten) {
		t.Fatal("fixture did not change ID")
	}
	if err = os.WriteFile(path, rewritten, 0600); err != nil {
		t.Fatal(err)
	}
	before := UsageDrops()
	RecordUsage(path, paid)
	if !FlushUsage() {
		t.Fatal("flush replay")
	}
	total, n := review2332Total(t, path)
	t.Logf("same-inode/same-size rewritten ledger expected=$0.002000 recorded=$%.6f rows=%d drops=%d", total, n, UsageDrops()-before)
	if n != 2 || math.Abs(total-.002) > 1e-9 || UsageDrops() != before {
		t.Fatal("stale identity map suppresses a paid call absent from current ledger")
	}
}
