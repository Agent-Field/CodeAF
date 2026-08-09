package adaptive

// Gate for the correctly-rounded Math.log / Math.cos / Math.pow kernels.
//
// testdata/v8-math.json (produced by tools/fixtures/gen-adaptive-math.ts) holds
// (argument, bun result) pairs as raw IEEE-754 bit patterns, so nothing is lost
// to decimal formatting. Two assertions:
//
//  1. Agreement with bun must be overwhelming (>= 99.5% per function). That is
//     the actual parity claim; see mathcr.go for why 100% is out of reach.
//  2. Every disagreement must be EXACTLY 1 ULP. A larger gap means the port is
//     wrong, not that glibc rounded differently.

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
)

type v8MathRows struct {
	Log [][2]string `json:"log"`
	Cos [][2]string `json:"cos"`
	Pow [][3]string `json:"pow"`
}

func parseBits(t *testing.T, s string) float64 {
	t.Helper()
	var bits uint64
	if _, err := fmt.Sscanf(s, "%x", &bits); err != nil {
		t.Fatalf("parse bits %q: %v", s, err)
	}
	return math.Float64frombits(bits)
}

// sameDouble compares bit patterns but treats every NaN as equal, since JS and
// Go pick different NaN payloads and JSON.stringify erases them anyway.
func sameDouble(a, b float64) bool {
	if math.IsNaN(a) && math.IsNaN(b) {
		return true
	}
	return math.Float64bits(a) == math.Float64bits(b)
}

// ulpGap is the number of representable doubles between a and b, or -1 when
// they are not comparable (different signs of infinity, NaN, etc.).
func ulpGap(a, b float64) int64 {
	if math.IsNaN(a) && math.IsNaN(b) {
		return 0
	}
	if math.IsNaN(a) != math.IsNaN(b) {
		return -1
	}
	ordinal := func(f float64) int64 {
		bits := int64(math.Float64bits(f))
		if bits < 0 {
			// Map the sign-magnitude negatives onto a monotone ordinal line.
			return math.MinInt64 - bits + 1
		}
		return bits
	}
	gap := ordinal(a) - ordinal(b)
	if gap < 0 {
		return -gap
	}
	return gap
}

func loadV8Math(t *testing.T) v8MathRows {
	t.Helper()
	raw, err := os.ReadFile("testdata/v8-math.json")
	if err != nil {
		t.Fatalf("read v8-math.json: %v", err)
	}
	var rows v8MathRows
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode v8-math.json: %v", err)
	}
	if len(rows.Log) < 200 || len(rows.Cos) < 200 || len(rows.Pow) < 200 {
		t.Fatalf("v8-math.json too small: log=%d cos=%d pow=%d", len(rows.Log), len(rows.Cos), len(rows.Pow))
	}
	return rows
}

func TestMathKernelsMatchBun(t *testing.T) {
	rows := loadV8Math(t)

	check := func(t *testing.T, name string, total int, mismatches int, worstULP int64, worst string) {
		t.Helper()
		rate := float64(mismatches) / float64(total)
		if worstULP > 1 {
			t.Errorf("%s: a disagreement was %d ULP wide (expected at most 1) — %s", name, worstULP, worst)
		}
		if rate > 0.005 {
			t.Errorf("%s: %d/%d (%.3f%%) disagreements exceeds the 0.5%% budget", name, mismatches, total, rate*100)
		}
		t.Logf("%s: %d/%d disagreements (%.3f%%), all within 1 ULP", name, mismatches, total, rate*100)
	}

	t.Run("log", func(t *testing.T) {
		mismatches, worstULP := 0, int64(0)
		worst := ""
		for _, row := range rows.Log {
			x := parseBits(t, row[0])
			want := parseBits(t, row[1])
			got := jsLog(x)
			if sameDouble(got, want) {
				continue
			}
			mismatches++
			if gap := ulpGap(got, want); gap > worstULP {
				worstULP, worst = gap, fmt.Sprintf("log(%v): go=%v bun=%v", x, got, want)
			}
		}
		check(t, "log", len(rows.Log), mismatches, worstULP, worst)
	})

	t.Run("cos", func(t *testing.T) {
		mismatches, worstULP := 0, int64(0)
		worst := ""
		for _, row := range rows.Cos {
			x := parseBits(t, row[0])
			want := parseBits(t, row[1])
			got := jsCos(x)
			if sameDouble(got, want) {
				continue
			}
			mismatches++
			if gap := ulpGap(got, want); gap > worstULP {
				worstULP, worst = gap, fmt.Sprintf("cos(%v): go=%v bun=%v", x, got, want)
			}
		}
		check(t, "cos", len(rows.Cos), mismatches, worstULP, worst)
	})

	t.Run("pow", func(t *testing.T) {
		mismatches, worstULP := 0, int64(0)
		worst := ""
		for _, row := range rows.Pow {
			base := parseBits(t, row[0])
			exp := parseBits(t, row[1])
			want := parseBits(t, row[2])
			got := jsPow(base, exp)
			if sameDouble(got, want) {
				continue
			}
			mismatches++
			if gap := ulpGap(got, want); gap > worstULP {
				worstULP, worst = gap, fmt.Sprintf("pow(%v, %v): go=%v bun=%v", base, exp, got, want)
			}
		}
		check(t, "pow", len(rows.Pow), mismatches, worstULP, worst)
	})
}

// TestJSCIntegerPowFastPath pins the engine behaviour the general path must NOT
// take over: JavaScriptCore evaluates an integral exponent in [0, 1000] by
// exponentiation-by-squaring, whose rounding differs from libm's. Values come
// from bun.
func TestJSCIntegerPowFastPath(t *testing.T) {
	// bun: Math.pow(0.5890091492328793, 1000) === 1.324466106542663e-230,
	// while the correctly-rounded value is 1.3244661065426534e-230.
	const base = 0.5890091492328793
	got := jsPow(base, 1000)
	if got != 1.324466106542663e-230 {
		t.Errorf("jsPow(%v, 1000) = %v, want bun's repeated-squaring value 1.324466106542663e-230", base, got)
	}
	// One past the fast path, bun falls back to libm.
	if squared := jsPow(base, 1001); squared == mathPowInteger(base, 1001) {
		t.Logf("note: pow(%v, 1001) happens to agree with repeated squaring", base)
	}
	// Spec corners that the fast path must still satisfy.
	for _, tc := range []struct {
		x, y, want float64
	}{
		{math.NaN(), 0, 1},
		{0, 0, 1},
		{math.Inf(1), 0, 1},
		{2, 10, 1024},
		{-2, 3, -8},
		{-2, 2, 4},
	} {
		if got := jsPow(tc.x, tc.y); got != tc.want {
			t.Errorf("jsPow(%v, %v) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

// TestMathSpecialValues pins the ECMAScript corners that the big.Float core
// must never reach.
func TestMathSpecialValues(t *testing.T) {
	if !math.IsInf(jsLog(0), -1) {
		t.Errorf("jsLog(0) = %v, want -Inf", jsLog(0))
	}
	if !math.IsNaN(jsLog(-1)) {
		t.Errorf("jsLog(-1) = %v, want NaN", jsLog(-1))
	}
	if jsLog(1) != 0 {
		t.Errorf("jsLog(1) = %v, want 0", jsLog(1))
	}
	if !math.IsInf(jsLog(math.Inf(1)), 1) {
		t.Errorf("jsLog(+Inf) = %v, want +Inf", jsLog(math.Inf(1)))
	}
	if jsCos(0) != 1 {
		t.Errorf("jsCos(0) = %v, want 1", jsCos(0))
	}
	if !math.IsNaN(jsCos(math.Inf(1))) {
		t.Errorf("jsCos(+Inf) = %v, want NaN", jsCos(math.Inf(1)))
	}
	if !math.IsNaN(jsPow(1, math.Inf(1))) {
		t.Errorf("jsPow(1, +Inf) = %v, want NaN (JS differs from C here)", jsPow(1, math.Inf(1)))
	}
	if !math.IsNaN(jsPow(-1, math.Inf(-1))) {
		t.Errorf("jsPow(-1, -Inf) = %v, want NaN", jsPow(-1, math.Inf(-1)))
	}
	if !math.IsNaN(jsPow(-2, 0.5)) {
		t.Errorf("jsPow(-2, 0.5) = %v, want NaN", jsPow(-2, 0.5))
	}
	if got := jsPow(math.Inf(-1), -3); math.Float64bits(got) != math.Float64bits(math.Copysign(0, -1)) {
		t.Errorf("jsPow(-Inf, -3) = %v, want -0", got)
	}
	if got := jsPow(math.Copysign(0, -1), -3); !math.IsInf(got, -1) {
		t.Errorf("jsPow(-0, -3) = %v, want -Inf", got)
	}
}

// TestGlibcNonCorrectlyRoundedCases makes the residual libm divergence
// concrete instead of leaving it as prose. These are the exact inputs where bun
// (glibc) returns a value one ULP away from the true one; the Go port returns
// the true one. If glibc ever gets fixed, or the port regresses, this test says
// so rather than a fixture failing mysteriously.
func TestGlibcNonCorrectlyRoundedCases(t *testing.T) {
	cases := []struct {
		name         string
		got          float64
		glibc        float64
		correctlyRnd float64
	}{
		{
			// Reached by gauss() on mulberry32 seed 0, draw #12 — the single
			// entry in glibcRoundingExceptions.
			name:         "cos(0.5518523475053569)",
			got:          jsCos(0.5518523475053569),
			glibc:        0.851554861641658,
			correctlyRnd: 0.8515548616416578,
		},
	}
	for _, tc := range cases {
		if tc.got != tc.correctlyRnd {
			t.Errorf("%s: port returned %v, want the correctly-rounded %v", tc.name, tc.got, tc.correctlyRnd)
		}
		if tc.glibc == tc.correctlyRnd {
			t.Errorf("%s: glibc no longer diverges — drop this case and the matching "+
				"glibcRoundingExceptions entry", tc.name)
		}
		if gap := ulpGap(tc.glibc, tc.correctlyRnd); gap != 1 {
			t.Errorf("%s: divergence is %d ULP, expected exactly 1", tc.name, gap)
		}
	}
}
