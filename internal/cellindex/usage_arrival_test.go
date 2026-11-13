package cellindex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestArrivingParentNeverRebillsWorkerCallsAndNewSpendLandsOnce(t *testing.T) {
	for _, format := range []string{"transcript", "receipts"} {
		t.Run(format, func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			c := chatCell(t)
			worker := session.UsageLine{At: time.Date(2026, 11, 10, 12, 0, 0, 123, time.UTC), Session: "worker-session", Model: "m", Calls: 1, Input: 40, Output: 8, USD: .056}
			parent := session.UsageLine{At: worker.At.Add(-time.Second), Session: c.ID, Model: "m", Calls: 1, Input: 10, Output: 2, USD: .001, CallID: "parent-paid-call"}
			if format == "receipts" {
				seedArrivalUsage(t, worker, parent)
				receiptDir := filepath.Join(c.Root, cellstore.ReceiptsDir)
				if err := os.MkdirAll(receiptDir, 0700); err != nil {
					t.Fatal(err)
				}
				receipt, _ := json.Marshal(cellstore.Receipt{SealedAtMs: worker.At.UnixMilli(), ModelCalls: []cellstore.ModelCall{{CallID: parent.CallID, Model: "m", TokensIn: 10, TokensOut: 2, CostMicroUSD: 1000}}})
				if err := os.WriteFile(filepath.Join(receiptDir, "paid.json"), receipt, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				seedArrivalUsage(t, worker)
			}
			before := totalOf(t)
			// The origin carries the worker's call identity even though its own session differs.
			// This is the same canonical worker row the task spend transport carries.
			path := session.Place{Dir: c.Root}.Transcript()
			writeTranscript(t, c)
			appendUsage := func(id string, cost float64) {
				t.Helper()
				f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				line, _ := json.Marshal(map[string]any{"type": "usage", "timestamp": "2026-11-10T12:00:01Z", "usage": map[string]any{"call_id": id, "remote_event": "attempt:" + id, "model": "m", "calls": 1, "input": 40, "output": 8, "costUsd": cost, "aux": true}})
				if _, err = f.Write(append(line, '\n')); err != nil {
					t.Fatal(err)
				}
				if err = f.Close(); err != nil {
					t.Fatal(err)
				}
			}
			appendUsage(session.UsageCallID(worker), .056)
			for range 2 {
				if _, err := Refresh(c, ""); err != nil {
					t.Fatal(err)
				}
				if got := totalOf(t); got != before {
					t.Fatalf("continue rebilled worker: before=%+v after=%+v", before, got)
				}
			}
			appendUsage("new-paid-call", .012)
			for range 2 {
				if _, err := Refresh(c, ""); err != nil {
					t.Fatal(err)
				}
			}
			got := totalOf(t)
			if got.micro != before.micro+12000 || got.in != before.in+40 || got.out != before.out+8 {
				t.Fatalf("new call did not land once: %+v -> %+v", before, got)
			}
			// A move changes the cell folder, never its carried call identity.
			moved := filepath.Join(t.TempDir(), c.ID)
			if err := os.Rename(c.Root, moved); err != nil {
				t.Fatal(err)
			}
			c.Root = moved
			if _, err := Refresh(c, ""); err != nil {
				t.Fatal(err)
			}
			if again := totalOf(t); again != got {
				t.Fatalf("moving rebilled calls: %+v -> %+v", got, again)
			}
		})
	}
}

func TestArrivingOlderRemoteEventMatchesItsOriginalWorkerLedgerRow(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	c := chatCell(t)
	worker := session.UsageLine{At: time.Date(2026, 11, 10, 12, 0, 0, 123, time.UTC), Session: "worker-session", Model: "m", Calls: 1, Input: 40, Output: 8, USD: .056}
	seedArrivalUsage(t, worker)
	info, err := os.Stat(session.UsageLedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	attempt := "att_0123456789abcdef"
	events := filepath.Join(filepath.Dir(session.UsageLedgerPath()), "taskdoor", attempt, "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(events), 0700); err != nil {
		t.Fatal(err)
	}
	event, _ := json.Marshal(map[string]any{"t": "spend", "attempt": attempt, "seq": 1, "source": "usage", "offset": info.Size()})
	if err := os.WriteFile(events, append(event, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, c)
	path := session.Place{Dir: c.Root}.Transcript()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"type":"usage","timestamp":"2026-11-10T12:00:01Z","usage":{"remote_event":"att_0123456789abcdef:1","model":"m","calls":1,"input":40,"output":8,"costUsd":0.056,"aux":true}}` + "\n")
	if _, err = f.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	before := totalOf(t)
	for range 2 {
		if _, err := Refresh(c, ""); err != nil {
			t.Fatal(err)
		}
	}
	if got := totalOf(t); got != before {
		t.Fatalf("older continue rebilled worker: %+v -> %+v", before, got)
	}
}

// The fixture starts with bills already on disk; an asynchronous write deadline
// must not turn that precondition into a different reconstruction scenario.
func seedArrivalUsage(t *testing.T, lines ...session.UsageLine) {
	t.Helper()
	path := session.UsageLedgerPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	for _, line := range lines {
		line.Day = line.At.Local().Format("2006-01-02")
		row, _ := json.Marshal(line)
		raw = append(raw, append(row, '\n')...)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.StopUsageWriter(path) })
}
