package ledgers

// Hand-written I/O-shell and lifecycle tests translated from
// src/session/decision-ledger.test.ts, src/session/cycle-ledger.test.ts, and
// the run-failure-ledger consumers.

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/leafoutcome"
)

func jsNumPtr(n float64) *jscompat.JSNumber {
	value := jscompat.JSNumber(n)
	return &value
}

func float64Ptr(n float64) *float64 {
	return &n
}

func textPtr(s string) *string {
	return &s
}

func testDecision(kind DecisionKind, taskID string) DecisionRecord {
	return BuildDecision(BuildDecisionInput{
		Kind:         kind,
		TaskID:       taskID,
		Model:        "qwen/qwen3.6-plus",
		Band:         "m",
		ReliableBand: "l",
		Chosen:       "dispatch-as-leaf",
		Reason:       "test",
		KnobsHash:    "abc",
		Now:          jsNumPtr(1_700_000_000_000),
	})
}

func TestDecisionLedgerCompletion(t *testing.T) {
	t.Run("buildDecision assembles a full record with a fixed clock", func(t *testing.T) {
		got := BuildDecision(BuildDecisionInput{
			Kind:         DecisionCut,
			TaskID:       "t-1",
			Model:        "gemma-4-26b",
			Band:         "l",
			ReliableBand: "s",
			Chosen:       "split-first",
			Reason:       "task=l > model-reliable=s",
			KnobsHash:    "deadbeef",
			Now:          jsNumPtr(42),
		})
		if got.Ts != 42 || got.Kind != DecisionCut || got.TaskID != "t-1" ||
			got.Model != "gemma-4-26b" || got.Band != "l" || got.ReliableBand != "s" ||
			got.Chosen != "split-first" || got.Reason != "task=l > model-reliable=s" ||
			got.KnobsHash != "deadbeef" {
			t.Fatalf("unexpected record: %+v", got)
		}
	})

	t.Run("append then load round-trips records and exact JSONL", func(t *testing.T) {
		ws := t.TempDir()
		a := testDecision(DecisionCut, "t-a")
		b := testDecision(DecisionProbe, "t-b")
		b.Ts = 99
		b.Chosen = "wave-0"
		AppendDecision(AppendDecisionArgs{Workspace: ws, Decision: a})
		AppendDecision(AppendDecisionArgs{Workspace: ws, Decision: b})

		file := filepath.Join(ws, decisionLedgerFile)
		loaded := LoadDecisions(file)
		if len(loaded) != 2 || loaded[0].TaskID != "t-a" || loaded[1].TaskID != "t-b" {
			t.Fatalf("unexpected loaded decisions: %+v", loaded)
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read JSONL: %v", err)
		}
		want := "{\"ts\":1700000000000,\"kind\":\"cut\",\"taskID\":\"t-a\",\"model\":\"qwen/qwen3.6-plus\",\"band\":\"m\",\"reliableBand\":\"l\",\"chosen\":\"dispatch-as-leaf\",\"reason\":\"test\",\"knobsHash\":\"abc\"}\n" +
			"{\"ts\":99,\"kind\":\"probe\",\"taskID\":\"t-b\",\"model\":\"qwen/qwen3.6-plus\",\"band\":\"m\",\"reliableBand\":\"l\",\"chosen\":\"wave-0\",\"reason\":\"test\",\"knobsHash\":\"abc\"}\n"
		if string(raw) != want {
			t.Fatalf("JSONL mismatch\n got: %q\nwant: %q", raw, want)
		}
	})

	t.Run("load skips malformed rows and missing file yields empty", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "decisions.jsonl")
		good, _ := testDecision(DecisionCoalesce, "ok").MarshalJSON()
		body := string(good) + "\n{not json\n\n{\"kind\":\"cut\"}\n" + string(good) + "\n"
		if err := os.WriteFile(file, []byte(body), 0o666); err != nil {
			t.Fatalf("seed JSONL: %v", err)
		}
		if got := LoadDecisions(file); len(got) != 2 {
			t.Fatalf("got %d valid rows, want 2", len(got))
		}
		if got := LoadDecisions(filepath.Join(t.TempDir(), "missing")); len(got) != 0 {
			t.Fatalf("missing file returned %+v", got)
		}
	})

	t.Run("append never fails caller on a file workspace", func(t *testing.T) {
		workspace := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(workspace, []byte("x"), 0o666); err != nil {
			t.Fatal(err)
		}
		AppendDecision(AppendDecisionArgs{Workspace: workspace, Decision: testDecision(DecisionAuditMode, "x")})
	})

	t.Run("score joins by taskID and aggregates by ordered kind", func(t *testing.T) {
		decisions := []DecisionRecord{
			testDecision(DecisionCut, "a"),
			testDecision(DecisionCut, "b"),
			testDecision(DecisionProbe, "c"),
		}
		outcomes := []*leafoutcome.LeafOutcome{
			{TaskID: "a", Verdict: leafoutcome.VerdictPass, CostUsd: 0.2, WallMs: 1000, RepairRounds: 0},
			{TaskID: "b", Verdict: leafoutcome.VerdictPassAfterRepair, CostUsd: 0.4, WallMs: 3000, RepairRounds: 2},
			{TaskID: "c", Verdict: leafoutcome.VerdictFail, CostUsd: 0.1, WallMs: 500, RepairRounds: 1},
		}
		scores := ScoreDecisions(decisions, outcomes)
		cut, ok := scores.Get(DecisionCut)
		if !ok || cut.N != 2 || cut.SuccessRate != 1 || math.Abs(float64(cut.MeanCostUsd)-0.3) > 1e-12 ||
			cut.MeanWallMs != 2000 || cut.MeanRepairRounds != 1 {
			t.Fatalf("unexpected cut score: %+v, found=%v", cut, ok)
		}
		if entries := scores.Entries(); len(entries) != 2 || entries[0].Key != DecisionCut || entries[1].Key != DecisionProbe {
			t.Fatalf("unexpected score order: %+v", entries)
		}
	})
}

func TestCycleLedgerCompletion(t *testing.T) {
	restore := SetClockForTesting(func() int64 { return 1_700_000_000_123 })
	defer restore()

	t.Run("appends rows in order with exact row shape", func(t *testing.T) {
		ws := t.TempDir()
		AppendCycleRecord(ws, CycleInput{
			Kind:           CycleFixCycle,
			Cycle:          1,
			BlockersBefore: 0,
			BlockersAfter:  float64Ptr(3),
			Verdict:        "fail",
			WallMs:         float64Ptr(1200),
		})
		AppendCycleRecord(ws, CycleInput{
			Kind:           CycleConfirmation,
			Cycle:          2,
			BlockersBefore: 3,
			BlockersAfter:  float64Ptr(0),
			Verdict:        "pass",
			CostUsdApprox:  float64Ptr(0.42),
		})
		rows := ReadCycleRecords(ws)
		if len(rows) != 2 || rows[0].Kind != CycleFixCycle || rows[1].Kind != CycleConfirmation ||
			rows[0].BlockersAfter == nil || *rows[0].BlockersAfter != 3 ||
			rows[1].CostUsdApprox == nil || *rows[1].CostUsdApprox != 0.42 {
			t.Fatalf("unexpected rows: %+v", rows)
		}
		raw, err := os.ReadFile(filepath.Join(ws, cycleLedgerFile))
		if err != nil {
			t.Fatal(err)
		}
		wantFirst := "{\"ts\":1700000000123,\"kind\":\"fix-cycle\",\"cycle\":1,\"blockersBefore\":0,\"verdict\":\"fail\",\"blockersAfter\":3,\"wallMs\":1200}\n"
		if !strings.HasPrefix(string(raw), wantFirst) {
			t.Fatalf("first row mismatch: %q", raw)
		}
	})

	t.Run("clamps invalid values drops bad kind and clips verdict", func(t *testing.T) {
		ws := t.TempDir()
		negative := -100.0
		after := 2.9
		AppendCycleRecord(ws, CycleInput{
			Kind:           CycleFixCycle,
			Cycle:          -5,
			BlockersBefore: -3,
			BlockersAfter:  &after,
			Verdict:        strings.Repeat("x", 500),
			WallMs:         &negative,
		})
		AppendCycleRecord(ws, CycleInput{Kind: "bogus", Cycle: 1, Verdict: "fail"})
		rows := ReadCycleRecords(ws)
		if len(rows) != 1 || rows[0].Cycle != 0 || rows[0].BlockersBefore != 0 ||
			rows[0].BlockersAfter == nil || *rows[0].BlockersAfter != 2 ||
			utf16Length(rows[0].Verdict) != 201 || rows[0].WallMs != nil {
			t.Fatalf("unexpected clamped row: %+v", rows)
		}
	})

	t.Run("reader tolerates malformed lines and missing files", func(t *testing.T) {
		ws := t.TempDir()
		AppendCycleRecord(ws, CycleInput{Kind: CycleFixCycle, Cycle: 1, Verdict: "pass"})
		file := filepath.Join(ws, cycleLedgerFile)
		f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND, 0o666)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString("{not json\n\n")
		_ = f.Close()
		if rows := ReadCycleRecords(ws); len(rows) != 1 {
			t.Fatalf("got %d rows, want 1", len(rows))
		}
		if rows := ReadCycleRecords(filepath.Join(t.TempDir(), "missing")); len(rows) != 0 {
			t.Fatalf("missing workspace returned %+v", rows)
		}
	})

	t.Run("append never fails caller on a file workspace", func(t *testing.T) {
		workspace := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(workspace, []byte("x"), 0o666); err != nil {
			t.Fatal(err)
		}
		AppendCycleRecord(workspace, CycleInput{Kind: CycleFixCycle, Cycle: 1, Verdict: "x"})
	})
}

func TestRunFailureLedgerCompletion(t *testing.T) {
	t.Run("records caps isolates roots and renders the exact digest", func(t *testing.T) {
		root := "hand-failure-basic"
		ClearLedger(root)
		RecordLeafFailure(root, &LeafFailure{
			TaskID: "task-a",
			Title:  "Implement parser",
			Reason: "review failed",
			Bugs: []FailureBug{{
				Severity: textPtr("blocker"),
				File:     textPtr("src/a.ts"),
				Line:     jsNumPtr(12),
				Detail:   "Missing null check",
			}},
			RepairHints: []string{"Add the guard"},
			Timestamp:   99,
		})
		if FailureCount(root) != 1 || len(GetRecentFailures(root)) != 1 {
			t.Fatalf("unexpected count/recent")
		}
		digest := RenderFailureDigest(root)
		want := "Run failure ledger (1 of 1 total leaf failures this run):\n" +
			"- task-a \"Implement parser\": review failed\n" +
			"    bug: [blocker] src/a.ts:12 — Missing null check\n" +
			"    hint: Add the guard\n" +
			"Use this history when replanning. Recurring failure patterns above suggest a structural fix (split tasks differently, add prerequisite work, change approach) rather than another retry of the same shape."
		if digest == nil || *digest != want {
			t.Fatalf("digest mismatch\n got: %v\nwant: %s", digest, want)
		}
	})

	t.Run("recent is a shallow copy and cap drops oldest", func(t *testing.T) {
		root := "hand-failure-cap"
		ClearLedger(root)
		for i := 0; i < 52; i++ {
			RecordLeafFailure(root, &LeafFailure{TaskID: jscompat.FormatNumber(float64(i)), Bugs: []FailureBug{}, RepairHints: []string{}})
		}
		all := GetRecentFailures(root, 100)
		if FailureCount(root) != 50 || len(all) != 50 || all[0].TaskID != "2" || all[49].TaskID != "51" {
			t.Fatalf("unexpected capped failures: count=%d len=%d first=%q last=%q",
				FailureCount(root), len(all), all[0].TaskID, all[len(all)-1].TaskID)
		}
		all[0].Title = "mutated"
		if GetRecentFailures(root, 100)[0].Title != "mutated" {
			t.Fatal("returned objects are not shared with the ledger")
		}
		all = append(all, &LeafFailure{TaskID: "outside"})
		if FailureCount(root) != 50 {
			t.Fatal("returned array was not a shallow copy")
		}
	})

	t.Run("nudge state is idempotent and survives clear like TS", func(t *testing.T) {
		root := "hand-failure-nudge"
		RecordLeafFailure(root, &LeafFailure{TaskID: "a", Bugs: []FailureBug{}, RepairHints: []string{}})
		MarkFailureNudged(root, "a")
		MarkFailureNudged(root, "a")
		ClearLedger(root)
		if FailureCount(root) != 0 || !WasFailureNudged(root, "a") {
			t.Fatalf("clear/nudge mismatch: count=%d nudged=%v", FailureCount(root), WasFailureNudged(root, "a"))
		}
	})

	t.Run("empty digest is undefined", func(t *testing.T) {
		root := "hand-failure-empty"
		ClearLedger(root)
		if got := RenderFailureDigest(root); got != nil {
			t.Fatalf("got %q, want nil", *got)
		}
	})
}
