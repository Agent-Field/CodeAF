package main

// The restart-time judge sweep: the runs a live process would have judged but a
// process death left unjudged, and the headless doors' pending rows. Every test
// runs the real sweep against a temp profile with the same fake ask and catalog
// the hook tests use, and reads back the own sheet, the judged markers and the
// pending file.

import (
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/record"
)

// TestPoolJudgeSweepJudgesAPendingRowOnceThenNeverAgain writes one headless
// pending row, sweeps, and reads back a scored own sheet, a judged marker and a
// consumed pending file — then sweeps again and asserts the judge is not re-asked.
func TestPoolJudgeSweepJudgesAPendingRowOnceThenNeverAgain(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")
	restoreOwnCells(t)

	profileDir := t.TempDir()
	poolDir := config.ProfilePath(profileDir, "pool")
	settings := config.Config{APIKey: "k"}

	landing := poolTestLanding()
	landing.ID = 42
	if err := writePendingLanding(profileDir, "do", landing); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	var asked []string
	poolJudgeSweep(settings, profileDir, "", poolTestCatalog, poolTestAsk(settings, &asked), time.Now)

	sheet, err := record.LoadSheet(record.OwnSheetPath(poolDir))
	if err != nil {
		t.Fatalf("own sheet: %v", err)
	}
	if len(record.Cells(sheet)) == 0 {
		t.Fatal("the sweep judged no seat of the pending row")
	}
	if !alreadyJudged(poolDir, landing.ID, landing.Attempt) {
		t.Fatal("the sweep left no judged marker for the pending row")
	}
	if _, err := os.Stat(pendingPath(poolDir)); !os.IsNotExist(err) {
		t.Fatalf("the pending file was not consumed: %v", err)
	}
	firstAsks := len(asked)
	if firstAsks == 0 {
		t.Fatal("the judge was never asked")
	}

	poolJudgeSweep(settings, profileDir, "", poolTestCatalog, poolTestAsk(settings, &asked), time.Now)
	if len(asked) != firstAsks {
		t.Fatalf("the second sweep re-asked the judge: %d asks then %d", firstAsks, len(asked))
	}
}

// TestPoolJudgeSweepWithNoKeyLeavesRowsWaiting: with no judge-capable key the
// sweep judges nothing and leaves the pending rows in place for a later start.
func TestPoolJudgeSweepWithNoKeyLeavesRowsWaiting(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")
	restoreOwnCells(t)

	profileDir := t.TempDir()
	poolDir := config.ProfilePath(profileDir, "pool")
	settings := config.Config{} // no APIKey

	landing := poolTestLanding()
	landing.ID = 43
	if err := writePendingLanding(profileDir, "do", landing); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	var asked []string
	poolJudgeSweep(settings, profileDir, "", poolTestCatalog, poolTestAsk(settings, &asked), time.Now)
	if len(asked) != 0 {
		t.Fatalf("the sweep asked a judge with no key: %v", asked)
	}
	if _, err := os.Stat(pendingPath(poolDir)); err != nil {
		t.Fatalf("the sweep consumed the pending file with no key; rows must wait: %v", err)
	}
}

// TestPoolJudgeSweepToleratesATornPendingLine: a row half-written when the sweep
// claimed the file is skipped, not fatal, and the whole valid row before it is
// still judged.
func TestPoolJudgeSweepToleratesATornPendingLine(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")
	restoreOwnCells(t)

	profileDir := t.TempDir()
	poolDir := config.ProfilePath(profileDir, "pool")
	settings := config.Config{APIKey: "k"}

	landing := poolTestLanding()
	landing.ID = 42
	if err := writePendingLanding(profileDir, "do", landing); err != nil {
		t.Fatalf("write pending: %v", err)
	}
	f, err := os.OpenFile(pendingPath(poolDir), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open pending: %v", err)
	}
	if _, err := f.WriteString(`{"door":"do","landing":{"id":99,`); err != nil {
		t.Fatalf("append torn line: %v", err)
	}
	f.Close()

	var asked []string
	poolJudgeSweep(settings, profileDir, "", poolTestCatalog, poolTestAsk(settings, &asked), time.Now)
	if !alreadyJudged(poolDir, 42, 0) {
		t.Fatal("the valid row before the torn line was not judged")
	}
	if alreadyJudged(poolDir, 99, 0) {
		t.Fatal("the torn row was judged; it should have been skipped")
	}
}
