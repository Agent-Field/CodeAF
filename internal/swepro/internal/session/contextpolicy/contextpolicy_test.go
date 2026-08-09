package contextpolicy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/session/ledgers"
	"github.com/Agent-Field/swe-pro-go/internal/session/sizeband"
)

// Translation of src/session/context-policy.test.ts. describe/test names are
// verbatim. `expect(x).toContain(y)` becomes strings.Contains, `toMatch(/re/i)`
// becomes a case-insensitive substring check (both TS patterns are literal
// prose, not real regexes), and the fs-backed test drives a t.TempDir().

// BANDS mirrors `const BANDS: readonly SizeBand[]`.
var testBands = []sizeband.SizeBand{"xs", "s", "m", "l", "xl"}

// clean mirrors `const CLEAN = { turns: 1, toolErrors: 0, repairRounds: 0 }`.
func clean(band sizeband.SizeBand) AssessContextInput {
	return AssessContextInput{Band: band, Turns: 1, ToolErrors: 0, RepairRounds: 0}
}

func TestTurnBudgetFor(t *testing.T) {
	t.Run("turnBudgetFor", func(t *testing.T) {
		t.Run("returns the per-band constants", func(t *testing.T) {
			for _, tc := range []struct {
				band sizeband.SizeBand
				want float64
			}{{"xs", 8}, {"s", 12}, {"m", 20}, {"l", 32}, {"xl", 48}} {
				if got := TurnBudgetFor(tc.band); got != tc.want {
					t.Fatalf("turnBudgetFor(%q) = %v, want %v", tc.band, got, tc.want)
				}
			}
		})

		t.Run("budgets are strictly monotone increasing in band", func(t *testing.T) {
			for i := 1; i < len(testBands); i++ {
				if !(TurnBudgetFor(testBands[i]) > TurnBudgetFor(testBands[i-1])) {
					t.Fatalf("turnBudgetFor(%q)=%v is not > turnBudgetFor(%q)=%v",
						testBands[i], TurnBudgetFor(testBands[i]),
						testBands[i-1], TurnBudgetFor(testBands[i-1]))
				}
			}
		})

		t.Run("TURN_BUDGET table and turnBudgetFor agree", func(t *testing.T) {
			for _, band := range testBands {
				want, ok := TurnBudget.Get(band)
				if !ok {
					t.Fatalf("TURN_BUDGET has no entry for %q", band)
				}
				if got := TurnBudgetFor(band); got != want {
					t.Fatalf("turnBudgetFor(%q) = %v, want %v", band, got, want)
				}
			}
		})
	})
}

func TestAssessContextHealthyBaseline(t *testing.T) {
	t.Run("assessContext: healthy baseline", func(t *testing.T) {
		t.Run("fresh trajectory is healthy at every band", func(t *testing.T) {
			for _, band := range testBands {
				if got := AssessContext(clean(band)).Verdict; got != "healthy" {
					t.Fatalf("band %q: verdict = %q, want healthy", band, got)
				}
			}
		})
	})
}

func TestAssessContextTurnBoundaries(t *testing.T) {
	t.Run("assessContext: turn boundaries", func(t *testing.T) {
		// m budget = 20, degrade line = 0.7 * 20 = 14.
		t.Run("turns exactly at DEGRADE_FRACTION * budget stays healthy (strict >)", func(t *testing.T) {
			degradeLine := DegradeFraction * TurnBudgetFor("m") // 14
			in := clean("m")
			in.Turns = degradeLine
			if got := AssessContext(in).Verdict; got != "healthy" {
				t.Fatalf("verdict = %q, want healthy", got)
			}
		})

		t.Run("one turn past the degrade line -> degrading", func(t *testing.T) {
			in := clean("m")
			in.Turns = 15
			a := AssessContext(in)
			if a.Verdict != "degrading" {
				t.Fatalf("verdict = %q, want degrading", a.Verdict)
			}
			if !strings.Contains(a.Reason, "turns") {
				t.Fatalf("reason %q does not contain %q", a.Reason, "turns")
			}
		})

		t.Run("turns exactly at budget is degrading, not yet polluted (strict >)", func(t *testing.T) {
			in := clean("m")
			in.Turns = 20
			if got := AssessContext(in).Verdict; got != "degrading" {
				t.Fatalf("verdict = %q, want degrading", got)
			}
		})

		t.Run("one turn past budget -> polluted", func(t *testing.T) {
			in := clean("m")
			in.Turns = 21
			a := AssessContext(in)
			if a.Verdict != "polluted" {
				t.Fatalf("verdict = %q, want polluted", a.Verdict)
			}
			if !strings.Contains(a.Reason, "budget") {
				t.Fatalf("reason %q does not contain %q", a.Reason, "budget")
			}
		})

		t.Run("boundaries scale with band (xs pollutes at 9 turns, xl not until 49)", func(t *testing.T) {
			xs := clean("xs")
			xs.Turns = 9
			if got := AssessContext(xs).Verdict; got != "polluted" {
				t.Fatalf("xs@9: verdict = %q, want polluted", got)
			}
			xl48 := clean("xl")
			xl48.Turns = 48
			if got := AssessContext(xl48).Verdict; got != "degrading" {
				t.Fatalf("xl@48: verdict = %q, want degrading", got)
			}
			xl49 := clean("xl")
			xl49.Turns = 49
			if got := AssessContext(xl49).Verdict; got != "polluted" {
				t.Fatalf("xl@49: verdict = %q, want polluted", got)
			}
		})
	})
}

func TestAssessContextToolErrorBoundaries(t *testing.T) {
	t.Run("assessContext: tool-error boundaries", func(t *testing.T) {
		t.Run("2 tool errors healthy, 3 degrading (>=)", func(t *testing.T) {
			two := clean("m")
			two.ToolErrors = 2
			if got := AssessContext(two).Verdict; got != "healthy" {
				t.Fatalf("verdict = %q, want healthy", got)
			}
			three := clean("m")
			three.ToolErrors = 3
			a := AssessContext(three)
			if a.Verdict != "degrading" {
				t.Fatalf("verdict = %q, want degrading", a.Verdict)
			}
			if !strings.Contains(a.Reason, "toolErrors") {
				t.Fatalf("reason %q does not contain %q", a.Reason, "toolErrors")
			}
		})

		t.Run("5 tool errors degrading, 6 polluted (>=)", func(t *testing.T) {
			five := clean("m")
			five.ToolErrors = 5
			if got := AssessContext(five).Verdict; got != "degrading" {
				t.Fatalf("verdict = %q, want degrading", got)
			}
			six := clean("m")
			six.ToolErrors = 6
			if got := AssessContext(six).Verdict; got != "polluted" {
				t.Fatalf("verdict = %q, want polluted", got)
			}
		})
	})
}

func TestAssessContextRepairRoundBoundary(t *testing.T) {
	t.Run("assessContext: repair-round boundary", func(t *testing.T) {
		t.Run("1 repair round healthy, 2 polluted (>=, no degrading tier)", func(t *testing.T) {
			one := clean("m")
			one.RepairRounds = 1
			if got := AssessContext(one).Verdict; got != "healthy" {
				t.Fatalf("verdict = %q, want healthy", got)
			}
			two := clean("m")
			two.RepairRounds = 2
			a := AssessContext(two)
			if a.Verdict != "polluted" {
				t.Fatalf("verdict = %q, want polluted", a.Verdict)
			}
			if !strings.Contains(a.Reason, "repairRounds") {
				t.Fatalf("reason %q does not contain %q", a.Reason, "repairRounds")
			}
		})
	})
}

func TestAssessContextPollutionDominates(t *testing.T) {
	t.Run("assessContext: pollution dominates degradation", func(t *testing.T) {
		t.Run("simultaneously degrading turns and polluted repair rounds reads polluted", func(t *testing.T) {
			a := AssessContext(AssessContextInput{Band: "m", Turns: 15, ToolErrors: 3, RepairRounds: 2})
			if a.Verdict != "polluted" {
				t.Fatalf("verdict = %q, want polluted", a.Verdict)
			}
		})
	})
}

// ---------------------------------------------------------------------------

var (
	testHealthy   = ContextAssessment{Verdict: "healthy", Reason: "test"}
	testDegrading = ContextAssessment{Verdict: "degrading", Reason: "test"}
	testPolluted  = ContextAssessment{Verdict: "polluted", Reason: "test"}
)

func boolPtr(b bool) *bool       { return &b }
func numPtr(f float64) *float64  { return &f }
func mode(d RetryDecision) Mode  { return d.Mode }
func brief(d RetryDecision) bool { return d.DistillBrief }

func TestRetryMode(t *testing.T) {
	t.Run("retryMode", func(t *testing.T) {
		t.Run("healthy + attempts remaining -> continue, no brief", func(t *testing.T) {
			d := RetryMode(testHealthy, 0)
			if mode(d) != "continue" {
				t.Fatalf("mode = %q, want continue", d.Mode)
			}
			if brief(d) {
				t.Fatalf("distillBrief = true, want false")
			}
		})

		t.Run("degrading -> fresh-context with distilled brief", func(t *testing.T) {
			d := RetryMode(testDegrading, 0)
			if mode(d) != "fresh-context" {
				t.Fatalf("mode = %q, want fresh-context", d.Mode)
			}
			if !brief(d) {
				t.Fatalf("distillBrief = false, want true")
			}
		})

		t.Run("polluted -> fresh-context with distilled brief", func(t *testing.T) {
			d := RetryMode(testPolluted, 1)
			if mode(d) != "fresh-context" {
				t.Fatalf("mode = %q, want fresh-context", d.Mode)
			}
			if !brief(d) {
				t.Fatalf("distillBrief = false, want true")
			}
		})

		t.Run("attempt >= MAX_FRESH_RETRIES -> give-up regardless of verdict", func(t *testing.T) {
			for _, a := range []ContextAssessment{testHealthy, testDegrading, testPolluted} {
				d := RetryMode(a, MaxFreshRetries)
				if mode(d) != "give-up" {
					t.Fatalf("%q: mode = %q, want give-up", a.Verdict, d.Mode)
				}
				if brief(d) {
					t.Fatalf("%q: distillBrief = true, want false", a.Verdict)
				}
			}
			if got := RetryMode(testPolluted, MaxFreshRetries+5).Mode; got != "give-up" {
				t.Fatalf("mode = %q, want give-up", got)
			}
		})

		t.Run("last allowed attempt (MAX_FRESH_RETRIES - 1) still retries fresh", func(t *testing.T) {
			if got := RetryMode(testPolluted, MaxFreshRetries-1).Mode; got != "fresh-context" {
				t.Fatalf("mode = %q, want fresh-context", got)
			}
		})

		t.Run("progression: continue -> fresh-context -> give-up", func(t *testing.T) {
			if got := RetryMode(testHealthy, 0).Mode; got != "continue" {
				t.Fatalf("mode = %q, want continue", got)
			}
			if got := RetryMode(testPolluted, 1).Mode; got != "fresh-context" {
				t.Fatalf("mode = %q, want fresh-context", got)
			}
			if got := RetryMode(testPolluted, 2).Mode; got != "give-up" {
				t.Fatalf("mode = %q, want give-up", got)
			}
		})

		t.Run("every decision carries a reason", func(t *testing.T) {
			for _, d := range []RetryDecision{
				RetryMode(testHealthy, 0),
				RetryMode(testPolluted, 0),
				RetryMode(testPolluted, 9),
			} {
				if len(d.Reason) == 0 {
					t.Fatalf("empty reason on %+v", d)
				}
			}
		})
	})
}

func TestRetryModeConvergenceAwareRelayBudget(t *testing.T) {
	t.Run("retryMode: convergence-aware relay budget", func(t *testing.T) {
		t.Run("(a) blockers strictly decreased grants a fresh retry past the flat cap", func(t *testing.T) {
			if got := RetryMode(testPolluted, MaxFreshRetries).Mode; got != "give-up" {
				t.Fatalf("mode = %q, want give-up", got)
			}
			d := RetryMode(testPolluted, MaxFreshRetries, RetrySignal{
				PrevBlockers: numPtr(4), CurrBlockers: numPtr(1),
			})
			if mode(d) != "fresh-context" {
				t.Fatalf("mode = %q, want fresh-context", d.Mode)
			}
			if !brief(d) {
				t.Fatalf("distillBrief = false, want true")
			}
		})

		t.Run("(b) still gives up at the absolute ceiling even while converging", func(t *testing.T) {
			d := RetryMode(testPolluted, MaxFreshRetriesHardCeiling, RetrySignal{
				PrevBlockers: numPtr(4), CurrBlockers: numPtr(1),
			})
			if mode(d) != "give-up" {
				t.Fatalf("mode = %q, want give-up", d.Mode)
			}
			// One below the ceiling, still converging -> keep going.
			got := RetryMode(testPolluted, MaxFreshRetriesHardCeiling-1, RetrySignal{
				PrevBlockers: numPtr(4), CurrBlockers: numPtr(1),
			}).Mode
			if got != "fresh-context" {
				t.Fatalf("mode = %q, want fresh-context", got)
			}
		})

		t.Run("(c) gives up at the normal cap when blockers did NOT decrease", func(t *testing.T) {
			flat := RetryMode(testPolluted, MaxFreshRetries, RetrySignal{
				PrevBlockers: numPtr(3), CurrBlockers: numPtr(3),
			}).Mode
			if flat != "give-up" {
				t.Fatalf("flat blockers: mode = %q, want give-up", flat)
			}
			rising := RetryMode(testPolluted, MaxFreshRetries, RetrySignal{
				PrevBlockers: numPtr(2), CurrBlockers: numPtr(5),
			}).Mode
			if rising != "give-up" {
				t.Fatalf("rising blockers: mode = %q, want give-up", rising)
			}
			partial := RetryMode(testPolluted, MaxFreshRetries, RetrySignal{
				PrevBlockers: numPtr(5),
			}).Mode
			if partial != "give-up" {
				t.Fatalf("partial signal: mode = %q, want give-up", partial)
			}
		})

		t.Run("(d) hard mode raises the relay ceiling alongside the cycle cap", func(t *testing.T) {
			if got := RetryMode(testPolluted, MaxFreshRetries).Mode; got != "give-up" {
				t.Fatalf("mode = %q, want give-up", got)
			}
			hard := RetryMode(testPolluted, MaxFreshRetries, RetrySignal{HardMode: boolPtr(true)}).Mode
			if hard != "fresh-context" {
				t.Fatalf("hardMode: mode = %q, want fresh-context", hard)
			}
			ceil := RetryMode(testPolluted, MaxFreshRetriesHardCeiling, RetrySignal{HardMode: boolPtr(true)}).Mode
			if ceil != "give-up" {
				t.Fatalf("hardMode at ceiling: mode = %q, want give-up", ceil)
			}
		})

		t.Run("absolute ceiling sits above the flat cap (budgets are ordered)", func(t *testing.T) {
			if !(MaxFreshRetriesHardCeiling > MaxFreshRetries) {
				t.Fatalf("%v is not > %v", MaxFreshRetriesHardCeiling, MaxFreshRetries)
			}
		})
	})
}

func TestRetryModeObjectiveProgress(t *testing.T) {
	t.Run("retryMode: objectiveProgress (whole-run convergence signal)", func(t *testing.T) {
		t.Run("objectiveProgress=true extends the relay cap exactly like a last-cycle decrease", func(t *testing.T) {
			if got := RetryMode(testPolluted, MaxFreshRetries).Mode; got != "give-up" {
				t.Fatalf("mode = %q, want give-up", got)
			}
			on := RetryMode(testPolluted, MaxFreshRetries, RetrySignal{ObjectiveProgress: boolPtr(true)}).Mode
			if on != "fresh-context" {
				t.Fatalf("mode = %q, want fresh-context", on)
			}
			ceil := RetryMode(testPolluted, MaxFreshRetriesHardCeiling, RetrySignal{
				ObjectiveProgress: boolPtr(true),
			}).Mode
			if ceil != "give-up" {
				t.Fatalf("mode = %q, want give-up", ceil)
			}
		})

		t.Run("objectiveProgress=false is inert (does not extend the cap)", func(t *testing.T) {
			got := RetryMode(testPolluted, MaxFreshRetries, RetrySignal{ObjectiveProgress: boolPtr(false)}).Mode
			if got != "give-up" {
				t.Fatalf("mode = %q, want give-up", got)
			}
		})

		t.Run("arg-absent BYTE-IDENTITY: adding no objectiveProgress reproduces the exact old decision + reason", func(t *testing.T) {
			cases := []struct {
				a ContextAssessment
				n float64
				s RetrySignal
			}{
				{testHealthy, 0, RetrySignal{}},
				{testDegrading, 1, RetrySignal{}},
				{testPolluted, MaxFreshRetries, RetrySignal{}},
				{testPolluted, MaxFreshRetries, RetrySignal{PrevBlockers: numPtr(4), CurrBlockers: numPtr(1)}},
				{testPolluted, MaxFreshRetries, RetrySignal{HardMode: boolPtr(true)}},
				{testPolluted, MaxFreshRetries, RetrySignal{PrevBlockers: numPtr(3), CurrBlockers: numPtr(3)}},
			}
			for _, c := range cases {
				withoutKey := RetryMode(c.a, c.n, c.s)
				// `{ ...c.s, objectiveProgress: undefined }` — an explicit
				// undefined is indistinguishable from an absent key, which is
				// a nil *bool here.
				withUndefined := RetryMode(c.a, c.n, RetrySignal{
					PrevBlockers:      c.s.PrevBlockers,
					CurrBlockers:      c.s.CurrBlockers,
					HardMode:          c.s.HardMode,
					ObjectiveProgress: nil,
				})
				if withUndefined != withoutKey {
					t.Fatalf("not byte-identical:\n with: %+v\nwithout: %+v", withUndefined, withoutKey)
				}
			}
		})
	})
}

// ---------------------------------------------------------------------------

func briefInput() BuildDistilledBriefInput {
	return BuildDistilledBriefInput{
		TaskDescription: "Implement the flux capacitor rate limiter in src/limits.ts",
		FailureSignals: []string{
			"test rate-limit.spec.ts failed: expected 429 got 500",
			"typecheck: TS2345 in limits.ts",
		},
		AttemptedApproaches: []string{"patched middleware ordering", "raised the token bucket size"},
	}
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("brief does not contain %q:\n%s", needle, haystack)
	}
}

func mustNotContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("brief unexpectedly contains %q:\n%s", needle, haystack)
	}
}

func TestBuildDistilledBrief(t *testing.T) {
	t.Run("buildDistilledBrief", func(t *testing.T) {
		input := briefInput()

		t.Run("contains the goal verbatim", func(t *testing.T) {
			mustContain(t, BuildDistilledBrief(input), input.TaskDescription)
		})

		t.Run("contains every failure signal", func(t *testing.T) {
			b := BuildDistilledBrief(input)
			for _, s := range input.FailureSignals {
				mustContain(t, b, s)
			}
		})

		t.Run("contains every attempted approach, honestly labeled as recent tool calls (not curated approaches)", func(t *testing.T) {
			b := BuildDistilledBrief(input)
			for _, a := range input.AttemptedApproaches {
				mustContain(t, b, a)
			}
			// TS: toMatch(/Recent tool calls/i) and /not curated approaches/i.
			lower := strings.ToLower(b)
			mustContain(t, lower, strings.ToLower("Recent tool calls"))
			mustContain(t, lower, strings.ToLower("not curated approaches"))
		})

		t.Run("has clearly separated sections in order: goal, failures, recent tool calls", func(t *testing.T) {
			b := BuildDistilledBrief(input)
			goalIdx := strings.Index(b, "## Goal")
			failIdx := strings.Index(b, "## What failed")
			recentIdx := strings.Index(b, "## Recent tool calls")
			if goalIdx < 0 {
				t.Fatalf("goalIdx = %d, want >= 0", goalIdx)
			}
			if !(failIdx > goalIdx) {
				t.Fatalf("failIdx %d is not > goalIdx %d", failIdx, goalIdx)
			}
			if !(recentIdx > failIdx) {
				t.Fatalf("recentIdx %d is not > failIdx %d", recentIdx, failIdx)
			}
		})

		t.Run("carries NOTHING beyond the three inputs (no transcript prose leaks)", func(t *testing.T) {
			b := BuildDistilledBrief(input)
			allowed := []string{input.TaskDescription}
			for _, s := range input.FailureSignals {
				allowed = append(allowed, "- "+s)
			}
			for _, a := range input.AttemptedApproaches {
				allowed = append(allowed, "- "+a)
			}
			for _, line := range strings.Split(b, "\n") {
				if line == "" || strings.HasPrefix(line, "## ") {
					continue
				}
				found := false
				for _, a := range allowed {
					if line == a {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("unexpected line %q", line)
				}
			}
			mustNotContain(t, b, "Assistant:")
			mustNotContain(t, b, "Tool call")
		})

		t.Run("empty signal/approach lists are marked explicitly, not omitted", func(t *testing.T) {
			b := BuildDistilledBrief(BuildDistilledBriefInput{
				TaskDescription:     "goal",
				FailureSignals:      []string{},
				AttemptedApproaches: []string{},
			})
			mustContain(t, b, "goal")
			mustContain(t, b, "no failure signals recorded")
			mustContain(t, b, "no recent tool calls recorded")
		})

		t.Run("drops internal assessment-counter prose from failure signals, keeps concrete evidence", func(t *testing.T) {
			in := briefInput()
			in.FailureSignals = []string{
				"turns 21 > budget 20 for band m",
				"toolErrors 6 >= 6",
				"repairRounds 2 >= 2",
				"test rate-limit.spec.ts failed: expected 429 got 500",
			}
			b := BuildDistilledBrief(in)
			mustNotContain(t, b, "turns 21 > budget 20")
			mustNotContain(t, b, "toolErrors 6 >= 6")
			mustNotContain(t, b, "repairRounds 2 >= 2")
			mustContain(t, b, "test rate-limit.spec.ts failed: expected 429 got 500")
		})

		t.Run("when every failure signal is counter prose, the section reads as empty (not counter noise)", func(t *testing.T) {
			in := briefInput()
			in.FailureSignals = []string{"turns 30 > budget 20 for band m", "toolErrors 8 >= 6"}
			mustContain(t, BuildDistilledBrief(in), "no failure signals recorded")
		})

		t.Run("renders ledger rows as approach → outcome under a 'do not repeat' heading", func(t *testing.T) {
			evidence := ".codeaf/patches/0.diff"
			in := briefInput()
			in.Ledger = []ledgers.AttemptRecord{
				{Attempt: 0, Approach: "patched middleware ordering", Outcome: "still 500s", Evidence: &evidence},
				{Attempt: 1, Approach: "raised the token bucket size", Outcome: "typecheck broke"},
			}
			b := BuildDistilledBrief(in)
			mustContain(t, b, "## Prior attempts (do not repeat)")
			mustContain(t, b, "[attempt 0] patched middleware ordering → still 500s")
			mustContain(t, b, "evidence: .codeaf/patches/0.diff")
			mustContain(t, b, "[attempt 1] raised the token bucket size → typecheck broke")
			mustNotContain(t, b, "## Recent tool calls")
		})

		t.Run("the raw tool-call tail is hard-trimmed to the last few calls when there is NO ledger", func(t *testing.T) {
			in := briefInput()
			in.AttemptedApproaches = []string{"c1", "c2", "c3", "c4", "c5"}
			b := BuildDistilledBrief(in)
			mustContain(t, b, "## Recent tool calls")
			mustContain(t, b, "- c5")
			mustContain(t, b, "- c3")
			mustNotContain(t, b, "- c1")
			mustNotContain(t, b, "- c2")
		})

		t.Run("omits the ledger section when no ledger is passed (additive, existing callers unaffected)", func(t *testing.T) {
			mustNotContain(t, BuildDistilledBrief(input), "## Prior attempts")
		})

		t.Run("renders a rejected-patch warning line when rejectedPatchPath is provided", func(t *testing.T) {
			in := briefInput()
			in.RejectedPatchPath = ".codeaf/patches/rejected-2.diff"
			b := BuildDistilledBrief(in)
			mustContain(t, b, "## Rejected patch")
			mustContain(t, b, ".codeaf/patches/rejected-2.diff")
			mustContain(t, strings.ToLower(b), strings.ToLower("do NOT resubmit it as-is"))
		})

		t.Run("omits the rejected-patch line when rejectedPatchPath is absent", func(t *testing.T) {
			mustNotContain(t, BuildDistilledBrief(input), "## Rejected patch")
		})

		t.Run("re-injects the durable OPEN-blocker set (pre-loaded) right below the goal", func(t *testing.T) {
			in := briefInput()
			in.OpenBlockers = []ledgers.OpenBlocker{
				{BlockerID: "abc123def456", Text: "missing null check in parse()", CycleOpened: 1},
				{BlockerID: "0011aabbccdd", Text: "[clause] rate limit not enforced", CycleOpened: 2},
			}
			b := BuildDistilledBrief(in)
			mustContain(t, b, "## Unresolved blockers (durable ledger")
			mustContain(t, b, "[abc123def456] missing null check in parse()")
			mustContain(t, b, "[0011aabbccdd] [clause] rate limit not enforced")
			if !(strings.Index(b, "## Unresolved blockers") > strings.Index(b, "## Goal")) {
				t.Fatalf("blocker section is not below the goal")
			}
			if !(strings.Index(b, "## Unresolved blockers") < strings.Index(b, "## What failed")) {
				t.Fatalf("blocker section is not above the failure section")
			}
		})

		t.Run("loads the durable OPEN-blocker set from the ledger when a workspace is given", func(t *testing.T) {
			ws := t.TempDir()
			ledgers.ReconcileVerdictBlockers(ws, 1, []string{"reentrancy guard missing"})
			in := briefInput()
			in.Workspace = ws
			b := BuildDistilledBrief(in)
			mustContain(t, b, "## Unresolved blockers (durable ledger")
			mustContain(t, b, "reentrancy guard missing")
		})

		t.Run("omits the durable-blocker section when neither workspace nor openBlockers is passed", func(t *testing.T) {
			mustNotContain(t, BuildDistilledBrief(input), "## Unresolved blockers")
		})
	})
}

// TestBuildDistilledBriefNullishOpenBlockers has no TS counterpart: the TS test
// never distinguishes an ABSENT openBlockers from an EMPTY one, but the module
// does (`??`, not `||`), so the Go nil-vs-empty modelling needs its own pin.
func TestBuildDistilledBriefNullishOpenBlockers(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	row := `{"ts":1,"blockerId":"aaa","status":"open","cycleOpened":1,"text":"would have shown"}` + "\n"
	if err := os.WriteFile(filepath.Join(ws, ".codeaf", "blocker-ledger.jsonl"), []byte(row), 0o644); err != nil {
		t.Fatal(err)
	}

	absent := briefInput()
	absent.Workspace = ws
	absent.OpenBlockers = nil
	mustContain(t, BuildDistilledBrief(absent), "would have shown")

	empty := briefInput()
	empty.Workspace = ws
	empty.OpenBlockers = []ledgers.OpenBlocker{}
	mustNotContain(t, BuildDistilledBrief(empty), "## Unresolved blockers")
}
