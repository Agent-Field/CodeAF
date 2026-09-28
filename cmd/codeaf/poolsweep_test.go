package main

// The restart-time judge sweep: the runs a live process would have judged but a
// process death left unjudged, and the headless doors' pending rows. Every test
// runs the real sweep against a temp profile with the same fake ask and catalog
// the hook tests use, and reads back the own sheet, the judged markers and the
// pending file.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
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

// pendingIDs reads the landing ids a pending file or a claim holds, the way the
// sweep reads them: one row a line, a torn line counted as nothing.
func pendingIDs(t *testing.T, path string) map[uint64]bool {
	t.Helper()
	ids := map[uint64]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		return ids
	}
	for _, line := range strings.Split(string(data), "\n") {
		var row pendingLanding
		if json.Unmarshal([]byte(strings.TrimSpace(line)), &row) == nil {
			ids[row.Landing.ID] = true
		}
	}
	return ids
}

// TestPoolJudgeSweepCutShortKeepsALeftoverClaimsRows: a sweep that stops
// before it has reached every row of a leftover claim leaves that claim for the
// next start. The fresh pending file must not then be renamed over it, which
// replaced the leftover's unjudged rows with the new ones and lost them for good.
func TestPoolJudgeSweepCutShortKeepsALeftoverClaimsRows(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")

	profileDir := t.TempDir()
	poolDir := config.ProfilePath(profileDir, "pool")
	settings := config.Config{APIKey: "k"}

	// A LEFTOVER CLAIM, from a sweep a process death cut short.
	leftover := poolTestLanding()
	leftover.ID = 101
	if err := writePendingLanding(profileDir, "do", leftover); err != nil {
		t.Fatalf("write leftover row: %v", err)
	}
	claim := pendingPath(poolDir) + ".sweeping"
	if err := os.Rename(pendingPath(poolDir), claim); err != nil {
		t.Fatalf("leave the claim behind: %v", err)
	}
	// AND A ROW A DOOR WROTE SINCE.
	fresh := poolTestLanding()
	fresh.ID = 202
	if err := writePendingLanding(profileDir, "do", fresh); err != nil {
		t.Fatalf("write fresh row: %v", err)
	}

	// THE CLOSE HAS ALREADY CANCELLED THE SWEEP.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var asked []string
	sweepPendingContext(ctx, settings, profileDir, poolDir, poolTestCatalog, poolTestAsk(settings, &asked), time.Now, time.Now().Add(time.Minute))

	inClaim, inPending := pendingIDs(t, claim), pendingIDs(t, pendingPath(poolDir))
	if !inClaim[101] {
		t.Fatalf("the leftover claim lost its unjudged row 101: the claim holds %v, pending holds %v", inClaim, inPending)
	}
	if !inClaim[202] && !inPending[202] {
		t.Fatalf("the fresh row 202 is in neither file: the claim holds %v, pending holds %v", inClaim, inPending)
	}
}
