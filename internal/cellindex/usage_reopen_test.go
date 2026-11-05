package cellindex

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestUsageReopenRecoversMissingChargeExactlyOnce(t *testing.T) {
	usageReopen(t, false, false)
}

func TestUsageReopenAfterCleanFlushBooksNothing(t *testing.T) {
	usageReopen(t, true, false)
}

func TestUsageReopenPreservesChargeDayAmountAndOwner(t *testing.T) {
	usageReopen(t, false, true)
}

func usageReopen(t *testing.T, clean, attribution bool) {
	t.Helper()
	bin := celltest.Engine(t)
	t.Setenv(home.EnvVar, t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	transcript := (session.Place{Dir: c.Root}).Transcript()
	if err := session.SeedConversation(transcript, "", "Usage recovery", "Keep each call."); err != nil {
		t.Fatal(err)
	}
	d, err := session.ReadDigest(transcript)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := cellstore.NewRecorder(nil, cellstore.Engine{Binary: bin, DataRoot: t.TempDir()}, c, filepath.Join(t.TempDir(), "wal"), cellstore.Options{Report: func(err error) {
		if err != nil {
			t.Errorf("save fixture receipt: %v", err)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cellstore.StopSpawned)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := rec.Drain(ctx); err != nil {
			t.Errorf("drain fixture recorder: %v", err)
		}
	})
	ledger := session.UsageLedgerPath()
	t.Cleanup(func() { session.StopUsageWriter(ledger) })
	for i, micro := range []int64{1000, 9000} {
		id := []string{"first-charge", "last-charge"}[i]
		rec.NoteModelCall(executor.ModelCall{ID: id, Model: "fixture/model", Role: "turn", TokensIn: 10, TokensOut: 1, CostMicroUSD: micro})
		rec.Settle(context.Background())
		// This is the disk state after exit loses the queued last row. The
		// receipt survived; an earlier row already names the same chat.
		if i == 0 || clean {
			session.RecordUsage(ledger, session.UsageLine{ID: id, At: time.Now(), Session: d.ID, Model: "fixture/model", Role: "turn", Calls: 1, Input: 10, Output: 1, USD: float64(micro) / 1e6})
		}
	}
	saved, err := cellstore.ModelCalls(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 || saved[0].ID != "first-charge" || saved[1].ID != "last-charge" {
		t.Fatalf("fixture lacks both identified saved calls: %+v", saved)
	}
	if !session.StopUsageWriter(ledger) {
		t.Fatal("initial writer did not stop")
	}
	last := session.UsageLine{ID: "last-charge", Session: d.ID, Model: "fixture/model", Role: "turn", Calls: 1, Input: 10, Output: 1, USD: .009}
	if attribution {
		last.At = time.Now().AddDate(0, 0, -1)
		last.Day = last.At.Format("2006-01-02")
		last.USD = .0090004
		last.FactoryJob = "fixture-job"
		last.Root = "fixture-root"
		last.Task = "17"
		last.Billing = "included"
		last.BillingBasis = "door:fixture/plan"
		last.Price = "provider"
		last.Seat = session.SeatTalk
		last.Workspace = "fixture-workspace"
		last.Run = "fixture-run"
	}
	if !clean {
		queueChargeThroughExit(t, ledger, last)
	}
	before, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := RebuildAt(c.Root, d.Workspace); err != nil {
			t.Fatal(err)
		}
		if !session.StopUsageWriter(ledger) {
			t.Fatal("rebuild writer did not stop")
		}
		rows, err := session.ReadUsage(ledger, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]int{}
		var usd float64
		for _, row := range rows {
			if attribution && row.ID == last.ID && (row.Day != last.Day || row.USD != last.USD || row.FactoryJob != last.FactoryJob || row.Root != last.Root || row.Task != last.Task || row.Billing != last.Billing || row.BillingBasis != last.BillingBasis || !row.At.Equal(last.At) || row.Workspace != last.Workspace || row.Run != last.Run || row.Price != last.Price || row.Seat != last.Seat) {
				t.Fatalf("charge changed: day %q -> %q; USD %.7f -> %.7f; job %q -> %q; task %q -> %q; billing %q -> %q", last.Day, row.Day, last.USD, row.USD, last.FactoryJob, row.FactoryJob, last.Task, row.Task, last.Billing, row.Billing)
			}
			seen[row.ID]++
			usd += row.USD
			if row.Session != d.ID || row.Model != "fixture/model" || row.Role != "turn" || row.Input != 10 || row.Output != 1 || row.Calls != 1 {
				t.Fatalf("charge moved: %+v", row)
			}
		}
		t.Logf("reopen: rows=%d first=%d last=%d USD=%.6f", len(rows), seen["first-charge"], seen["last-charge"], usd)
		if len(rows) != 2 || seen["first-charge"] != 1 || seen["last-charge"] != 1 || math.Abs(usd-(.001+last.USD)) > 1e-12 {
			t.Fatalf("want each charge once, total $0.010000; rows=%+v", rows)
		}
	}
	after, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, before) {
		t.Fatal("reopen changed existing charges")
	}
	if clean && !bytes.Equal(after, before) {
		t.Fatal("clean flush replay added rows")
	}
}

// Exit after the public durable handoff without running clean-close compaction.
func TestUsageReopenDeadlineProcess(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "usage-flush-gap" && len(os.Args) == i+2 {
			var line session.UsageLine
			if err := json.NewDecoder(os.Stdin).Decode(&line); err != nil {
				t.Fatal(err)
			}
			session.RecordUsage(os.Args[i+1], line)

			return
		}
	}
	t.Skip("subprocess helper")
}

func queueChargeThroughExit(t *testing.T, ledger string, line session.UsageLine) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUsageReopenDeadlineProcess$", "-test.v", "--", "usage-flush-gap", ledger)
	payload, err := json.Marshal(line)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exit helper: %v: %s", err, output)
	}
	raw, err := os.ReadFile(ledger + ".pending-v1.jsonl")
	if err != nil || len(raw) == 0 {
		t.Fatalf("exit lost durable journal: %v", err)
	}
}
