//go:build e2e

package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestLiveMoneyReadsWaitForPositiveCostCutReceipts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	// Only the last 200 ms of the real receipt bound remain. Positive interim
	// cost does not prove that a cancelled call's final receipt has arrived.
	ended := time.Now().Add(-provider.ReceiptWait - time.Second + 200*time.Millisecond)
	raw, err := json.Marshal(calllog.Record{ID: "cut", Time: ended.Format(time.RFC3339Nano), Cost: 0.02, Error: "cancelled"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	awaitLiveMoneyReceipts(t, path)
	if time.Now().Before(ended.Add(provider.ReceiptWait + time.Second)) {
		t.Fatal("money read preceded the cut receipt bound despite positive interim cost")
	}
}

// awaitLiveMoneyReceipts waits outside measured stage tails. The call log does
// not join the sink, and positive interim cost cannot prove final settlement,
// so every terminal call gets the provider's full receipt bound before a read.
func awaitLiveMoneyReceipts(t *testing.T, path string) {
	t.Helper()
	terminalDeadline := time.Now().Add(provider.ReceiptWait + time.Second)
	for {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		pending := map[string]bool{}
		var receiptDeadline time.Time
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64<<10), 4<<20)
		for scanner.Scan() {
			var row calllog.Record
			if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
				file.Close()
				t.Fatal(err)
			}
			if row.Phase == "start" {
				pending[row.ID] = true
				continue
			}
			delete(pending, row.ID)
			ended, err := time.Parse(time.RFC3339Nano, row.Time)
			if err != nil {
				file.Close()
				t.Fatalf("call %s has no valid terminal timestamp: %v", row.ID, err)
			}
			if bound := ended.Add(provider.ReceiptWait + time.Second); bound.After(receiptDeadline) {
				receiptDeadline = bound
			}
		}
		scanErr := scanner.Err()
		file.Close()
		if scanErr != nil {
			t.Fatal(scanErr)
		}
		if len(pending) == 0 && !time.Now().Before(receiptDeadline) {
			return
		}
		if len(pending) > 0 && time.Now().After(terminalDeadline) {
			t.Fatalf("%d model calls still lack terminal records after job end", len(pending))
		}
		time.Sleep(50 * time.Millisecond)
	}
}
