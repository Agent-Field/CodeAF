package main

import (
	"bytes"
	"runtime"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// TestProcessCloseJoinsTheUsageWriter proves the product process door owns the
// ledger writer it starts. FlushUsage is the ordering seam: it makes the queued
// row finish without sleeping or loading the machine, leaving the run loop
// parked and visible immediately before closeAll. Once closeAll returns, no
// usage writer started by the process may remain alive.
func TestProcessCloseJoinsTheUsageWriter(t *testing.T) {
	baseline := liveUsageWriters()
	proc := v3TestProcess(t)
	session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{
		At:    time.Now(),
		Model: "test/model",
		Calls: 1,
		Input: 1,
		USD:   0.01,
	})
	session.FlushUsage()

	if before := liveUsageWriters(); before != baseline+1 {
		t.Fatalf("immediately before product Close, live usage writers = %d, want baseline %d + owned writer", before, baseline)
	}
	proc.closeAll()
	if after := liveUsageWriters(); after != 0 {
		t.Fatalf("after product Close returned, live usage writers = %d, want 0", after)
	}
}

func liveUsageWriters() int {
	buffer := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buffer, true)
		if n < len(buffer) {
			return bytes.Count(buffer[:n], []byte("internal/session.(*usageWriter).run"))
		}
		buffer = make([]byte, len(buffer)*2)
	}
}
