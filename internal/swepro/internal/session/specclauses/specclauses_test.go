package specclauses

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// Translation of src/session/spec-clauses.test.ts, restricted to the assertions
// that land inside the ported pure subset. Subtest names are verbatim from the
// TS `describe`/`test` titles.
//
// The TS file has 8 tests; 6 of them drive getSpecClauseJudgment / lowJudge /
// selectEvidence, which are CUT here (fs + zod + low-judge). What survives is
// the assertion each of those tests makes about a pure helper:
//
//	"model failure falls back to the heuristic count" → countSpecClauses
//	"clamps to 60 cells and clips over-long cells"    → clampMatrixCells
//
// The remaining tests ("llm path returns the clause list and memoizes per spec
// hash", "derives cells for a dense spec", "skips the matrix stage entirely
// when clause count < 4", "matrix absent (undefined) when the LOW model is
// unavailable", plus both lowJudge cases and all three selectEvidence cases)
// have NO pure surface at all and are intentionally not translated.

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

func TestGetSpecClauseJudgment(t *testing.T) {
	t.Run("model failure falls back to the heuristic count", func(t *testing.T) {
		// The TS asserts result.count === 6 for this spec on the fallback path;
		// spec-clauses.ts:199 sets count = countSpecClauses(input.spec).
		if got := CountSpecClauses("- a\n- b\n- c\n- d\n- e\n- f"); got != 6 {
			t.Fatalf("count = %v, want 6", got)
		}
	})
}

func TestGetSpecClauseJudgmentInteractionMatrix(t *testing.T) {
	t.Run("clamps to 60 cells and clips over-long cells", func(t *testing.T) {
		long := "clause a ⊗ " + strings.Repeat("x", 400)
		cells := []any{any(long)}
		for i := 0; i < 80; i++ {
			cells = append(cells, any("cell "+itoa(i)+" ⊗ context"))
		}
		matrix := ClampMatrixCells(cells)
		if len(matrix) != 60 {
			t.Fatalf("len(matrix) = %d, want 60", len(matrix))
		}
		if n := utf16Len(matrix[0]); n > MatrixCellMaxlen {
			t.Fatalf("matrix[0] length = %d, want <= %d", n, MatrixCellMaxlen)
		}
		if !strings.HasSuffix(matrix[0], "…") {
			t.Fatalf("matrix[0] = %q, want it to end with the ellipsis", matrix[0])
		}
	})
}

// itoa avoids pulling strconv in just for the loop above; the TS uses a
// template literal over 0..79.
func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// ---------------------------------------------------------------------------
// Behaviours the fixtures cannot express, pinned here instead.

// TestClampMatrixCellsSurrogateClipDivergence pins the ONE place this port is
// not byte-identical to V8, so the gap is visible in the test log rather than
// discovered in production. `s.slice(0, 159)` is a UTF-16 slice: when unit 159
// is the high half of a surrogate pair, V8 keeps an unpaired surrogate and
// JSON.stringify renders it "\ud83d". A Go string cannot hold one, so
// utf16.Decode substitutes U+FFFD.
//
// Reachability: clampMatrixCells is only ever fed LOW-model output, and the
// clause path (countSpecClauses) never calls it, so no fixture exercises this.
func TestClampMatrixCellsSurrogateClipDivergence(t *testing.T) {
	// 79 emoji = 158 units, then one more emoji straddles the 159-unit clip.
	in := strings.Repeat("\U0001f600", 79) + strings.Repeat("\U0001f600", 5)
	got := ClampMatrixCells([]any{any(in)})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	want := strings.Repeat("\U0001f600", 79) + "�…"
	if got[0] != want {
		t.Fatalf("got %q, want %q (V8 would emit a lone U+D83D instead of U+FFFD)", got[0], want)
	}
}

// TestSplitSentencesEmptyString pins the ECMA-262 step-13 branch that
// countSpecClauses can never reach (its `if (!spec) return 0` guard fires
// first): "".split(/(?<=[.!?])\s+|\n+/) is [""], not [].
func TestSplitSentencesEmptyString(t *testing.T) {
	got := splitSentences(nil)
	if len(got) != 1 || len(got[0]) != 0 {
		t.Fatalf("splitSentences(\"\") = %v, want one empty element", got)
	}
}

// TestSplitSentencesBoundaries pins the hand-rolled lookbehind scanner against
// the exact boundaries V8 produces (verified against bun before this port).
// The alternative ORDER is observable: after `.` the `\s+` branch eats the
// whole whitespace run including the newlines; with no `.!?` before it only the
// `\n+` branch fires and the following spaces survive into the next element.
func TestSplitSentencesBoundaries(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a.\n\n  b", []string{"a.", "b"}},
		{"a\n\nb", []string{"a", "b"}},
		{"a\n\n  b", []string{"a", "  b"}},
		{"a. b", []string{"a.", "b"}},
		{"a.b", []string{"a.b"}},
		{"  .  x", []string{"  .", "x"}},
		{"\nabc", []string{"", "abc"}},
		{"abc\n", []string{"abc", ""}},
		{"one.  two!   three?\tfour", []string{"one.", "two!", "three?", "four"}},
		{"end.", []string{"end."}},
		{".", []string{"."}},
		{"..  ..", []string{"..", ".."}},
		{"a?!  b", []string{"a?!", "b"}},
		{"\n\n\n", []string{"", ""}},
		// U+FEFF is JS whitespace (and Go source forbids a literal BOM here).
		{"\ufeff.\ufeffz", []string{"\ufeff.", "z"}},
		{"a.\rb", []string{"a.", "b"}},
		{"a.\r\nb", []string{"a.", "b"}},
		{"x.　 y", []string{"x.", "y"}},
		{
			"- [ ] do the thing.\n- [x] done thing\n",
			[]string{"- [ ] do the thing.", "- [x] done thing", ""},
		},
	}
	for _, tc := range cases {
		parts := splitSentences(utf16of(tc.in))
		got := make([]string, len(parts))
		for i, p := range parts {
			got[i] = decodeUTF16(p)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("%q: got %q, want %q", tc.in, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%q: got %q, want %q", tc.in, got, tc.want)
			}
		}
	}
}

// TestMatchAllBacktickTokens pins the greedy `{2,40}` scanner, including the
// UTF-16 code-unit counting that makes a single emoji a valid 2-unit token.
func TestMatchAllBacktickTokens(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"`ab``cd`", []string{"ab", "cd"}},
		{"`ab`cd`ef`", []string{"ab", "ef"}},
		{"`a`", nil},
		{"`ab`", []string{"ab"}},
		{"`" + strings.Repeat("x", 40) + "`", []string{strings.Repeat("x", 40)}},
		{"`" + strings.Repeat("x", 41) + "`", nil},
		{"``", nil},
		{"```", nil},
		{"````", nil},
		{"``ab``", []string{"ab"}},
		{"`\U0001f600`", []string{"\U0001f600"}},
		{"`a\rb`", []string{"a\rb"}},
		{"`ab", nil},
		{"no backticks", nil},
		{"`ab` `cd` `ab`", []string{"ab", "cd", "ab"}},
	}
	for _, tc := range cases {
		got := matchAllBacktickTokens(utf16of(tc.in))
		want := make([]string, len(tc.want))
		for i, w := range tc.want {
			want[i] = unitKey(utf16of(w))
		}
		if len(got) != len(want) {
			t.Fatalf("%q: got %d tokens, want %d", tc.in, len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%q: token %d = %q, want %q", tc.in, i, got[i], want[i])
			}
		}
	}
}
