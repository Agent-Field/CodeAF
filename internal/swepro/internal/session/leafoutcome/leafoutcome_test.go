package leafoutcome

import (
	"math"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Direct translation of src/session/leaf-outcome.test.ts (bun:test). The
// describe/test nesting becomes t.Run/t.Run and every subtest name is verbatim,
// so a failure here names the same case the TS suite would.
//
// Assertion mapping:
//   expect(x).toBe(y)            → exact ==
//   expect(x).toEqual(obj)       → jscompat.Stringify comparison (structural,
//                                  and also pins the JSON key order)
//   expect(x).toBeCloseTo(y, 10) → |x-y| < 0.5e-10, bun's numDigits semantics

func strPtr(s string) *string { return &s }

func jsNum(f float64) jscompat.JSNumber { return jscompat.JSNumber(f) }

func numPtr(f float64) *jscompat.JSNumber {
	n := jscompat.JSNumber(f)
	return &n
}

func mustStringify(t *testing.T, v any) string {
	t.Helper()
	b, err := jscompat.Stringify(v)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	return string(b)
}

func TestLeafOutcomeTS(t *testing.T) {
	t.Run("sizeBandFromTags", func(t *testing.T) {
		t.Run("maps scope tags to bands", func(t *testing.T) {
			if got := SizeBandFromTags([]string{"scope:tiny"}); got != "xs" {
				t.Errorf("scope:tiny → %q, want xs", got)
			}
			if got := SizeBandFromTags([]string{"scope:trivial"}); got != "xs" {
				t.Errorf("scope:trivial → %q, want xs", got)
			}
			if got := SizeBandFromTags([]string{"scope:small"}); got != "s" {
				t.Errorf("scope:small → %q, want s", got)
			}
			if got := SizeBandFromTags([]string{"scope:medium"}); got != "m" {
				t.Errorf("scope:medium → %q, want m", got)
			}
			if got := SizeBandFromTags([]string{"scope:large"}); got != "l" {
				t.Errorf("scope:large → %q, want l", got)
			}
		})

		t.Run("is case-insensitive and tolerates surrounding tags", func(t *testing.T) {
			if got := SizeBandFromTags([]string{"agent:fixer", "scope:LARGE", "risk:high"}); got != "l" {
				t.Errorf("got %q, want l", got)
			}
		})

		t.Run("defaults to m when absent, unknown, or malformed", func(t *testing.T) {
			if got := SizeBandFromTags(nil); got != "m" {
				t.Errorf("nil tags → %q, want m", got)
			}
			if got := SizeBandFromTags([]string{}); got != "m" {
				t.Errorf("empty tags → %q, want m", got)
			}
			if got := SizeBandFromTags([]string{"agent:fixer"}); got != "m" {
				t.Errorf("agent:fixer → %q, want m", got)
			}
			if got := SizeBandFromTags([]string{"scope:gigantic"}); got != "m" {
				t.Errorf("scope:gigantic → %q, want m", got)
			}
			if got := SizeBandFromTags([]string{"scope:"}); got != "m" {
				t.Errorf("scope: → %q, want m", got)
			}
		})

		t.Run("first recognized scope tag wins", func(t *testing.T) {
			if got := SizeBandFromTags([]string{"scope:small", "scope:large"}); got != "s" {
				t.Errorf("got %q, want s", got)
			}
		})
	})

	t.Run("classifyVerdict", func(t *testing.T) {
		check := func(t *testing.T, in ClassifyVerdictInput, want LeafVerdict) {
			t.Helper()
			if got := ClassifyVerdict(in); got != want {
				t.Errorf("classifyVerdict(%+v) = %q, want %q", in, got, want)
			}
		}

		t.Run("clean pass with no repairs", func(t *testing.T) {
			check(t, ClassifyVerdictInput{GateStatus: "pass", RepairRounds: 0, MergeConflict: false}, "pass")
			check(t, ClassifyVerdictInput{GateStatus: "skipped", RepairRounds: 0, MergeConflict: false}, "pass")
		})

		t.Run("pass after one or more repairs", func(t *testing.T) {
			check(t, ClassifyVerdictInput{GateStatus: "pass", RepairRounds: 1, MergeConflict: false}, "pass_after_repair")
			check(t, ClassifyVerdictInput{GateStatus: "pass", RepairRounds: 3, MergeConflict: false}, "pass_after_repair")
		})

		t.Run("merge conflict on an otherwise-passing leaf counts as fail", func(t *testing.T) {
			check(t, ClassifyVerdictInput{GateStatus: "pass", RepairRounds: 0, MergeConflict: true}, "fail")
			check(t, ClassifyVerdictInput{GateStatus: "pass", RepairRounds: 2, MergeConflict: true}, "fail")
		})

		t.Run("gate fail is fail regardless of repairs", func(t *testing.T) {
			check(t, ClassifyVerdictInput{GateStatus: "fail", RepairRounds: 0, MergeConflict: false}, "fail")
			check(t, ClassifyVerdictInput{GateStatus: "fail", RepairRounds: 3, MergeConflict: false}, "fail")
		})

		t.Run("advisor/replanner involvement is escalated", func(t *testing.T) {
			check(t, ClassifyVerdictInput{GateStatus: "escalated", RepairRounds: 3, MergeConflict: false}, "escalated")
			// done_partial = advisor accept_with_debt; escalation dominates even if it merged.
			check(t, ClassifyVerdictInput{GateStatus: "done_partial", RepairRounds: 1, MergeConflict: false}, "escalated")
			check(t, ClassifyVerdictInput{GateStatus: "done_partial", RepairRounds: 0, MergeConflict: true}, "escalated")
		})
	})

	t.Run("aggregateSessionStats", func(t *testing.T) {
		t.Run("sums assistant turns, cost, and tool errors", func(t *testing.T) {
			stats := AggregateSessionStats([]*SessionMessage{
				{Info: &SessionMessageInfo{Role: strPtr("user")}, Parts: []*SessionMessagePart{}},
				{
					Info: &SessionMessageInfo{Role: strPtr("assistant"), Cost: numPtr(0.01)},
					Parts: []*SessionMessagePart{
						{Type: strPtr("tool"), State: &SessionMessagePartState{Status: strPtr("completed")}},
						{Type: strPtr("tool"), State: &SessionMessagePartState{Status: strPtr("error")}},
						{Type: strPtr("text")},
					},
				},
				{
					Info: &SessionMessageInfo{Role: strPtr("assistant"), Cost: numPtr(0.02)},
					Parts: []*SessionMessagePart{
						{Type: strPtr("tool"), State: &SessionMessagePartState{Status: strPtr("error")}},
					},
				},
			})
			if stats.Turns != 2 {
				t.Errorf("turns = %v, want 2", float64(stats.Turns))
			}
			if stats.ToolErrors != 2 {
				t.Errorf("toolErrors = %v, want 2", float64(stats.ToolErrors))
			}
			if math.Abs(float64(stats.CostUsd)-0.03) >= 0.5e-10 {
				t.Errorf("costUsd = %v, want ~0.03", float64(stats.CostUsd))
			}
		})

		t.Run("empty session yields zeros", func(t *testing.T) {
			got := mustStringify(t, AggregateSessionStats(nil))
			want := `{"turns":0,"toolErrors":0,"costUsd":0}`
			if got != want {
				t.Errorf("got %s, want %s", got, want)
			}
		})

		t.Run("tolerates missing/malformed fields without throwing", func(t *testing.T) {
			nan := jscompat.JSNumber(math.NaN())
			stats := AggregateSessionStats([]*SessionMessage{
				{},
				{Info: nil, Parts: nil},
				{Info: &SessionMessageInfo{Role: strPtr("assistant")}, Parts: []*SessionMessagePart{nil, {Type: strPtr("tool")}}},
				{Info: &SessionMessageInfo{Role: strPtr("assistant"), Cost: &nan}, Parts: []*SessionMessagePart{}},
			})
			// Two assistant turns; NaN cost is skipped; no tool errors (missing state).
			if stats.Turns != 2 {
				t.Errorf("turns = %v, want 2", float64(stats.Turns))
			}
			if stats.ToolErrors != 0 {
				t.Errorf("toolErrors = %v, want 0", float64(stats.ToolErrors))
			}
			if stats.CostUsd != 0 {
				t.Errorf("costUsd = %v, want 0", float64(stats.CostUsd))
			}
		})
	})

	t.Run("buildLeafOutcome", func(t *testing.T) {
		t.Run("assembles a full record with derived band, verdict, and fixed clock", func(t *testing.T) {
			outcome := BuildLeafOutcome(BuildLeafOutcomeInput{
				TaskID:        "t-123",
				ProviderID:    "openrouter",
				ModelID:       "qwen/qwen3.6-plus",
				Tags:          []string{"agent:fixer", "scope:small"},
				GateStatus:    "pass",
				RepairRounds:  1,
				Turns:         7,
				ToolErrors:    2,
				CostUsd:       jsNum(0.42),
				WallMs:        163000,
				MergeConflict: false,
				Now:           numPtr(1_700_000_000_000),
			})
			got := mustStringify(t, outcome)
			want := `{"taskID":"t-123","model":{"providerID":"openrouter","modelID":"qwen/qwen3.6-plus"},` +
				`"sizeBand":"s","verdict":"pass_after_repair","repairRounds":1,"turns":7,"toolErrors":2,` +
				`"costUsd":0.42,"wallMs":163000,"mergeConflict":false,"timestamp":1700000000000}`
			if got != want {
				t.Errorf("got  %s\nwant %s", got, want)
			}
		})

		t.Run("merge conflict flips verdict to fail and is recorded", func(t *testing.T) {
			outcome := BuildLeafOutcome(BuildLeafOutcomeInput{
				TaskID:        "t-9",
				ProviderID:    "anthropic",
				ModelID:       "claude-opus-4-8",
				Tags:          nil,
				GateStatus:    "pass",
				RepairRounds:  0,
				Turns:         3,
				ToolErrors:    0,
				CostUsd:       jsNum(1.1),
				WallMs:        5000,
				MergeConflict: true,
				Now:           numPtr(42),
			})
			if outcome.SizeBand != "m" {
				t.Errorf("sizeBand = %q, want m", outcome.SizeBand)
			}
			if outcome.Verdict != "fail" {
				t.Errorf("verdict = %q, want fail", outcome.Verdict)
			}
			if !outcome.MergeConflict {
				t.Error("mergeConflict = false, want true")
			}
		})
	})
}

// ── port-only regression guards ──────────────────────────────────────────
//
// These have no TS counterpart; they lock down the two places where a naive Go
// translation silently diverges and where the fixtures alone would not explain
// the failure.

func TestClockFallbackIsInjectable(t *testing.T) {
	restore := SetClockForTesting(func() int64 { return 1234567 })
	defer restore()
	got := BuildLeafOutcome(BuildLeafOutcomeInput{GateStatus: "pass"}).Timestamp
	if got != 1234567 {
		t.Fatalf("timestamp = %v, want 1234567", float64(got))
	}
	// `input.now ?? Date.now()` — an explicit 0 must NOT fall back.
	zero := jscompat.JSNumber(0)
	if got := BuildLeafOutcome(BuildLeafOutcomeInput{GateStatus: "pass", Now: &zero}).Timestamp; got != 0 {
		t.Fatalf("timestamp with now=0 = %v, want 0", float64(got))
	}
}

func TestCommandRegexIsAsciiCaseInsensitiveOnly(t *testing.T) {
	// Go's (?i) folds U+017F LATIN SMALL LETTER LONG S onto "s"; JS's /i
	// without /u does not. Every one of the six tokens contains an "s", so a
	// (?i)-based port over-counts here.
	cases := []struct {
		evidence string
		want     float64
	}{
		{"jest", 1},
		{"JEST", 1},
		{"jeſt", 0},
		{"vıtest", 0},
		{"bun test", 1},
		{"bun  test", 0},
		{"cargo test and go test", 2},
		{"jesting", 0},
	}
	for _, c := range cases {
		got := AuditEvidenceFromVerdict(map[string]any{"evidence": c.evidence}, false)
		if float64(got.AuditCommandsRun) != c.want {
			t.Errorf("evidence %q → %v, want %v", c.evidence, float64(got.AuditCommandsRun), c.want)
		}
	}
}

func TestDecodeUTF8LossyUsesMaximalSubparts(t *testing.T) {
	// Node's Buffer.toString("utf8") emits ONE U+FFFD per maximal subpart.
	cases := []struct {
		in   []byte
		want string
	}{
		{[]byte{0x80}, "�"},
		{[]byte{0xE2, 0x82}, "�"},
		{[]byte{0xF0, 0x9F}, "�"},
		{[]byte{0xF0, 0x9F, 0x98}, "�"},
		{[]byte{0xF0, 0x9F, 0x98, 0x80}, "\U0001F600"},
		{[]byte{0xE2, 0x82, 0xAC}, "€"},
		{[]byte{0xC0, 0x80}, "��"},        // overlong: 0xC0 is never a lead
		{[]byte{0xED, 0xA0, 0x80}, "���"}, // surrogate half is rejected
		{[]byte{0x61, 0x80, 0x62}, "a�b"},
		{[]byte{}, ""},
	}
	for _, c := range cases {
		if got := decodeUTF8Lossy(c.in); got != c.want {
			t.Errorf("decodeUTF8Lossy(% x) = %q, want %q", c.in, got, c.want)
		}
	}
}
