//go:build unix

package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// THE TURN PATH NEVER WAITS ON THE LEDGER'S DISK. `~/.aforge` on a stalled home
// mount used to mean a turn could not finish: [RecordUsage] held a process-wide
// mutex across a mkdir, an open, a write and a close, so one hung disk stopped
// every conversation in the process. A row is worth less than a turn, and this
// test is what says so.
//
// A FIFO is the honest stand-in for that mount: opening one for writing BLOCKS
// until somebody opens the other end, which is a stall this test can start and
// end on purpose.
func TestRecordingUsageNeverWaitsOnTheDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("this filesystem has no fifos to stall on: %v", err)
	}
	at := usageAt(t, "2026-08-25 08:00")

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		RecordUsage(path, UsageLine{At: at, Model: "opus-4.1", Role: "title", Calls: 2,
			Input: 900, Output: 120, USD: 0.31, Session: "aaaa1111aaaa1111", Workspace: "/repo"})
		// A QUEUE THAT FILLS DROPS ROWS, it does not wait: far more rows than
		// [usageQueueDepth] are pushed at a writer that cannot write one of
		// them, and every one of these calls still has to come straight back.
		for i := 0; i < 4*usageQueueDepth; i++ {
			RecordUsage(path, UsageLine{At: at.Add(time.Duration(i) * time.Second),
				Model: "opus-4.1", Calls: 1, Input: 10, USD: 0.01})
		}
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("RecordUsage was still inside the disk two seconds later: the turn path is waiting on the ledger")
	}

	// The reading end is opened only NOW, which is what made the wait above a
	// real one — until this line there was nobody for the writer's own open to
	// rendezvous with.
	reader, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("open the reading end: %v", err)
	}
	defer reader.Close()
	first := make(chan string, 1)
	drained := make(chan struct{})
	go func() {
		// The pipe is drained to the end so the writer can never be caught
		// mid-write when this test finishes: a writer parked on its queue is
		// one a later [FlushUsage] can answer, and a writer blocked inside a
		// pipe nobody is reading is one that would hang the next test.
		defer close(drained)
		buffered := bufio.NewReader(reader)
		for {
			line, err := buffered.ReadString('\n')
			if line != "" {
				select {
				case first <- line:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	FlushUsage()

	select {
	case line := <-first:
		var row UsageLine
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("the row the background writer wrote does not parse: %v (%q)", err, line)
		}
		// THE SCHEMA IS THE SAME SCHEMA. Moving the write off the turn's
		// goroutine must not change one byte of what lands in the file.
		if !row.At.Equal(at) || row.Day != at.Format(usageDayLayout) {
			t.Fatalf("the row's day is %q at %s", row.Day, row.At)
		}
		if row.Model != "opus-4.1" || row.Role != "title" || row.Calls != 2 {
			t.Fatalf("the row is %+v", row)
		}
		if row.Input != 900 || row.Output != 120 || row.USD != 0.31 {
			t.Fatalf("the figures are %+v", row)
		}
		if row.Session != "aaaa1111aaaa1111" || row.Workspace != "/repo" {
			t.Fatalf("the ids are %+v", row)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing reached the ledger once the disk answered again")
	}

	reader.Close()
	<-drained
}
