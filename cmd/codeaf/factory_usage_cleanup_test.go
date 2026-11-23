package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestLocalFactoryTestCleanupJoinsUsageWriter(t *testing.T) {
	before := liveUsageWriters()
	t.Run("factory", func(t *testing.T) {
		s := newLocalSetup(t)
		s.start()
		path := session.UsageLedgerPath()
		// This is also how factory meters without an agent record usage.
		session.RecordUsage(path, session.UsageLine{At: time.Now(), ID: "factory-cleanup", Input: 1, USD: .01})
		if !session.FlushUsage() {
			t.Fatal("factory usage did not reach its ledger")
		}
		if got := liveUsageWriters(); got != before+1 {
			t.Fatalf("factory started %d usage writers, want one", got-before)
		}
	})
	if got := liveUsageWriters(); got != before {
		t.Fatalf("factory cleanup left %d usage writers running", got-before)
	}
}
