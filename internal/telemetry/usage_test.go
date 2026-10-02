package telemetry

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCountTokensQueuesOneUsageDeltaForTheActiveSession(t *testing.T) {
	testHome(t)
	VersionForTest(t, "v0.6.1-test")
	finish := BeginUsageSession(ModeTask, "usage-session")
	t.Cleanup(finish)

	CountTokens(100, 25)
	finish()

	rows := SpoolContents()
	if len(rows) != 1 {
		t.Fatalf("CountTokens queued %d rows, want one", len(rows))
	}
	var row jsonEvent
	if err := json.Unmarshal(rows[0], &row); err != nil {
		t.Fatal(err)
	}
	if row.EventName != "usage_delta" || row.SessionIDHash != hashHex("usage-session") {
		t.Fatalf("queued row = %+v", row)
	}
	if row.Props["mode"] != "task" || row.Props["input_tokens"] != float64(100) ||
		row.Props["output_tokens"] != float64(25) || row.Props["total_tokens"] != float64(125) {
		t.Fatalf("usage props = %v", row.Props)
	}
}

func TestCountTokensWithoutAnActiveSessionWritesNothing(t *testing.T) {
	testHome(t)
	CountTokens(100, 25)
	if rows := SpoolContents(); len(rows) != 0 {
		t.Fatalf("CountTokens without a session queued %d rows", len(rows))
	}
}

func TestPeriodicFlushSendsBeforeTheSessionEnds(t *testing.T) {
	testHome(t)
	VersionForTest(t, "v0.6.1-test")
	recorder := newRelay(t)
	// No notice gate stands before the first send since 2026-10-01: a queued
	// row leaves on the next flush, which is the whole claim here.
	if err := SpoolSync(UsageDelta(ModeChat, 100, 25, "open-session", time.Now())); err != nil {
		t.Fatal(err)
	}

	stop := StartPeriodicFlush(5 * time.Millisecond)
	t.Cleanup(stop)
	deadline := time.After(time.Second)
	for recorder.count() == 0 {
		select {
		case <-deadline:
			t.Fatal("periodic flush did not send the queued usage event")
		case <-time.After(time.Millisecond):
		}
	}
	stop()

	if rows := SpoolContents(); len(rows) != 0 {
		t.Fatalf("periodic flush left %d rows", len(rows))
	}
}
