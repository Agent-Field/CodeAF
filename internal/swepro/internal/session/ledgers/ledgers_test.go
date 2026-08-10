package ledgers

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Translation of src/session/attempt-ledger.test.ts and
// src/session/blocker-ledger.test.ts. describe/test names are verbatim; the
// TS `expect(...).resolves.toBeUndefined()` / `.not.toThrow()` assertions
// become plain calls, since the Go port returns nothing and — like the TS —
// swallows every failure.

// tmpWorkspace mirrors the tests' fs.mkdtemp helper; t.TempDir cleans up the
// way their afterEach does.
func tmpWorkspace(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// texts mirrors `const texts = (ws) => loadOpenBlockers(ws).map((b) => b.text).sort()`.
func texts(ws string) []string {
	out := []string{}
	for _, b := range LoadOpenBlockers(ws) {
		out = append(out, b.Text)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	return strings.Join(a, "\x00") == strings.Join(b, "\x00")
}

// ── attempt-ledger.test.ts ───────────────────────────────────────────────

func TestAttemptLedger(t *testing.T) {
	t.Run("attempt ledger", func(t *testing.T) {
		t.Run("appends across calls with monotonically numbered attempts, per task", func(t *testing.T) {
			ws := tmpWorkspace(t)
			AppendAttempt(ws, "t1", AttemptInput{Approach: "regex rewrite", Outcome: "audit fail: clause 3"})
			evidence := "bun test exit=0"
			AppendAttempt(ws, "t1", AttemptInput{Approach: "AST transform", Outcome: "tests green, merged", Evidence: &evidence})
			AppendAttempt(ws, "t2", AttemptInput{Approach: "other task", Outcome: "n/a"})

			t1 := ReadAttempts(ws, "t1")
			var attempts []float64
			for _, r := range t1 {
				attempts = append(attempts, r.Attempt)
			}
			if len(attempts) != 2 || attempts[0] != 0 || attempts[1] != 1 {
				t.Fatalf("attempt numbers: got %v want [0 1]", attempts)
			}
			if t1[1].Evidence == nil || *t1[1].Evidence != "bun test exit=0" {
				t.Fatalf("evidence: got %v want %q", t1[1].Evidence, "bun test exit=0")
			}
			if got := ReadAttempts(ws, "t2"); len(got) != 1 {
				t.Fatalf("t2 length: got %d want 1", len(got))
			}
			if got := ReadAttempts(ws, "missing"); len(got) != 0 {
				t.Fatalf("missing: got %v want []", got)
			}
		})

		t.Run("clips oversized fields and never throws on unwritable workspace", func(t *testing.T) {
			ws := tmpWorkspace(t)
			AppendAttempt(ws, "t", AttemptInput{Approach: strings.Repeat("x", 2000), Outcome: "y"})
			rows := ReadAttempts(ws, "t")
			if len(rows) != 1 {
				t.Fatalf("rows: got %d want 1", len(rows))
			}
			if n := utf16Length(rows[0].Approach); n > 501 {
				t.Fatalf("clipped approach length: got %d want <= 501", n)
			}
			// Resolves without throwing.
			AppendAttempt("/nonexistent/nope", "t", AttemptInput{Approach: "a", Outcome: "b"})
		})

		t.Run("atomic write leaves no dangling tmp files and round-trips", func(t *testing.T) {
			ws := tmpWorkspace(t)
			AppendAttempt(ws, "k", AttemptInput{Approach: "a", Outcome: "b"})
			AppendAttempt(ws, "k", AttemptInput{Approach: "c", Outcome: "d"})
			entries, err := os.ReadDir(filepath.Join(ws, ".codeaf"))
			if err != nil {
				t.Fatalf("readdir: %v", err)
			}
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			for _, n := range names {
				// Only the final ledger file — the tmp-*-rename target must not survive.
				if strings.Contains(n, ".tmp-") {
					t.Fatalf("dangling tmp file %q in %v", n, names)
				}
			}
			found := false
			for _, n := range names {
				if n == "attempt-ledger.json" {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected attempt-ledger.json in %v", names)
			}
			if got := ReadAttempts(ws, "k"); len(got) != 2 {
				t.Fatalf("rows: got %d want 2", len(got))
			}
		})
	})
}

func TestAuditFixKey(t *testing.T) {
	t.Run("auditFixKey", func(t *testing.T) {
		t.Run("is deterministic per goal and survives across processes (goal-derived)", func(t *testing.T) {
			if AuditFixKey("Fix bug X") != AuditFixKey("Fix bug X") {
				t.Fatalf("not deterministic")
			}
			if !regexp.MustCompile(`^audit-fix:[0-9a-f]{12}$`).MatchString(AuditFixKey("Fix bug X")) {
				t.Fatalf("shape: got %q", AuditFixKey("Fix bug X"))
			}
		})
		t.Run("distinct goals get distinct keys", func(t *testing.T) {
			if AuditFixKey("goal A") == AuditFixKey("goal B") {
				t.Fatalf("distinct goals collided")
			}
		})
	})
}

// ── blocker-ledger.test.ts ───────────────────────────────────────────────

func TestBlockerID(t *testing.T) {
	t.Run("blockerId", func(t *testing.T) {
		t.Run("is stable and derived only from canonicalized content", func(t *testing.T) {
			if BlockerID("missing null check in parse()") != BlockerID("missing null check in parse()") {
				t.Fatalf("not stable")
			}
			// Whitespace differences do NOT change identity (same clause).
			if BlockerID("missing null check in parse()") != BlockerID("  missing   null check\nin parse() ") {
				t.Fatalf("whitespace changed identity")
			}
			// Different clauses → different ids.
			if BlockerID("a") == BlockerID("b") {
				t.Fatalf("distinct clauses collided")
			}
			// 12 hex chars.
			if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(BlockerID("anything")) {
				t.Fatalf("shape: got %q", BlockerID("anything"))
			}
		})
	})
}

func TestBlockerAppendRead(t *testing.T) {
	t.Run("append + read", func(t *testing.T) {
		t.Run("round-trips a lifecycle row and writes to .codeaf/blocker-ledger.jsonl", func(t *testing.T) {
			ws := tmpWorkspace(t)
			AppendBlockerRecord(ws, BlockerInput{BlockerID: BlockerID("x"), Status: "open", CycleOpened: 1, Text: "x"})
			rows := ReadBlockerRecords(ws)
			if len(rows) != 1 {
				t.Fatalf("rows: got %d want 1", len(rows))
			}
			if rows[0].Status != "open" {
				t.Fatalf("status: got %q want open", rows[0].Status)
			}
			if rows[0].CycleOpened != 1 {
				t.Fatalf("cycleOpened: got %v want 1", rows[0].CycleOpened)
			}
			// `typeof rows[0].ts === "number"` holds statically in Go; assert it
			// was actually populated from the row instead.
			if rows[0].Ts == 0 {
				t.Fatalf("ts was not populated")
			}
			raw, err := os.ReadFile(filepath.Join(ws, ".codeaf", "blocker-ledger.jsonl"))
			if err != nil {
				t.Fatalf("read raw: %v", err)
			}
			if got := strings.Split(strings.TrimSpace(string(raw)), "\n"); len(got) != 1 {
				t.Fatalf("raw lines: got %d want 1", len(got))
			}
		})

		t.Run("drops unknown status and tolerates malformed lines", func(t *testing.T) {
			ws := tmpWorkspace(t)
			AppendBlockerRecord(ws, BlockerInput{BlockerID: "id", Status: "bogus", CycleOpened: 1, Text: "x"})
			if got := ReadBlockerRecords(ws); len(got) != 0 {
				t.Fatalf("rows: got %d want 0", len(got))
			}
			AppendBlockerRecord(ws, BlockerInput{BlockerID: "id", Status: "open", CycleOpened: 1, Text: "x"})
			f, err := os.OpenFile(filepath.Join(ws, ".codeaf", "blocker-ledger.jsonl"), os.O_WRONLY|os.O_APPEND, 0o666)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if _, err := f.WriteString("{not json\n\n"); err != nil {
				t.Fatalf("append: %v", err)
			}
			f.Close()
			if got := ReadBlockerRecords(ws); len(got) != 1 {
				t.Fatalf("rows: got %d want 1", len(got))
			}
		})
	})
}

func TestBlockerOpenSetLifecycle(t *testing.T) {
	t.Run("open-set lifecycle open → fixed → verified", func(t *testing.T) {
		t.Run("first audit cycle opens every blocker; the OPEN set is exactly them", func(t *testing.T) {
			ws := tmpWorkspace(t)
			ReconcileVerdictBlockers(ws, 1, []string{"blocker A", "blocker B"})
			if got := texts(ws); !equalStrings(got, []string{"blocker A", "blocker B"}) {
				t.Fatalf("open texts: got %v", got)
			}
		})

		t.Run("a subsequent audit that no longer reports a blocker verifies it (drops from OPEN)", func(t *testing.T) {
			ws := tmpWorkspace(t)
			ReconcileVerdictBlockers(ws, 1, []string{"blocker A", "blocker B"})
			// Cycle 2: A is gone (fixed + verified), B persists.
			ReconcileVerdictBlockers(ws, 2, []string{"blocker B"})
			if got := texts(ws); !equalStrings(got, []string{"blocker B"}) {
				t.Fatalf("open texts: got %v", got)
			}
			// `.filter((r) => r.blockerId === blockerId("blocker A")).at(-1)`
			var latestA *BlockerRecord
			for _, r := range ReadBlockerRecords(ws) {
				if r.BlockerID == BlockerID("blocker A") {
					rec := r
					latestA = &rec
				}
			}
			if latestA == nil {
				t.Fatalf("no row for blocker A")
			}
			if latestA.Status != "verified" {
				t.Fatalf("status: got %q want verified", latestA.Status)
			}
			if latestA.CycleClosed == nil || *latestA.CycleClosed != 2 {
				t.Fatalf("cycleClosed: got %v want 2", latestA.CycleClosed)
			}
		})

		t.Run("all blockers cleared → empty OPEN set", func(t *testing.T) {
			ws := tmpWorkspace(t)
			ReconcileVerdictBlockers(ws, 1, []string{"blocker A"})
			ReconcileVerdictBlockers(ws, 2, []string{})
			if got := LoadOpenBlockers(ws); len(got) != 0 {
				t.Fatalf("open: got %v want []", got)
			}
		})

		t.Run("markBlockersFixed moves open → fixed, removing it from the OPEN set", func(t *testing.T) {
			ws := tmpWorkspace(t)
			ReconcileVerdictBlockers(ws, 1, []string{"blocker A"})
			MarkBlockersFixed(ws, 1, []string{"blocker A"})
			if got := LoadOpenBlockers(ws); len(got) != 0 {
				t.Fatalf("open: got %v want []", got)
			}
			rows := ReadBlockerRecords(ws)
			if rows[len(rows)-1].Status != "fixed" {
				t.Fatalf("last status: got %q want fixed", rows[len(rows)-1].Status)
			}
		})

		t.Run("a fixed blocker that reappears in a later audit is re-opened", func(t *testing.T) {
			ws := tmpWorkspace(t)
			ReconcileVerdictBlockers(ws, 1, []string{"blocker A"})
			MarkBlockersFixed(ws, 1, []string{"blocker A"})
			if got := LoadOpenBlockers(ws); len(got) != 0 { // fixed, off the worklist
				t.Fatalf("open: got %v want []", got)
			}
			ReconcileVerdictBlockers(ws, 2, []string{"blocker A"}) // still there → re-open
			if got := texts(ws); !equalStrings(got, []string{"blocker A"}) {
				t.Fatalf("open texts: got %v", got)
			}
		})

		t.Run("re-running the same cycle's verdict appends no duplicate open rows (idempotent)", func(t *testing.T) {
			ws := tmpWorkspace(t)
			ReconcileVerdictBlockers(ws, 1, []string{"blocker A"})
			ReconcileVerdictBlockers(ws, 1, []string{"blocker A"})
			if got := ReadBlockerRecords(ws); len(got) != 1 {
				t.Fatalf("rows: got %d want 1", len(got))
			}
			if got := texts(ws); !equalStrings(got, []string{"blocker A"}) {
				t.Fatalf("open texts: got %v", got)
			}
		})

		t.Run("OPEN set carries the stable blocker_id and survives a fresh read (durability)", func(t *testing.T) {
			ws := tmpWorkspace(t)
			ReconcileVerdictBlockers(ws, 1, []string{"missing null check in parse()"})
			open := LoadOpenBlockers(ws)
			if len(open) != 1 {
				t.Fatalf("open: got %d want 1", len(open))
			}
			if open[0].BlockerID != BlockerID("missing null check in parse()") {
				t.Fatalf("blockerId: got %q", open[0].BlockerID)
			}
		})
	})
}

func TestBlockerBestEffort(t *testing.T) {
	t.Run("best-effort — never throws", func(t *testing.T) {
		t.Run("append/reconcile/load on an unwritable path resolve without throwing", func(t *testing.T) {
			AppendBlockerRecord("/nonexistent/nope", BlockerInput{BlockerID: "i", Status: "open", CycleOpened: 1, Text: "x"})
			ReconcileVerdictBlockers("/nonexistent/nope", 1, []string{"a"})
			if got := LoadOpenBlockers("/nonexistent/nope"); len(got) != 0 {
				t.Fatalf("open: got %v want []", got)
			}
			if got := ReadBlockerRecords("/nonexistent/nope"); len(got) != 0 {
				t.Fatalf("records: got %v want []", got)
			}
		})
	})
}
