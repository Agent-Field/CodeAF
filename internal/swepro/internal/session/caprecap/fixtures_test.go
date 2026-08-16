package caprecap

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// Golden-fixture replay. testdata/fixtures.json is generated straight out of
// the real TS module by tools/fixtures/gen-caprecap.ts; the gate is that
// jscompat.Stringify of the Go result is BYTE-FOR-BYTE the TS
// JSON.stringify(result).

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()

	var out []fixture
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var fx fixture
		if err := json.Unmarshal(line, &fx); err != nil {
			t.Fatalf("decode fixture line %q: %v", line, err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(fixtures))
	}
	seen := map[string]int{}

	for _, fx := range fixtures {
		seen[fx.Fn]++
		t.Run(fx.Name, func(t *testing.T) {
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
				t.Fatalf("decode args_json %q: %v", fx.ArgsJSON, err)
			}

			var got any
			switch fx.Fn {
			case "estimateResidualDefects":
				if len(args) != 1 {
					t.Fatalf("estimateResidualDefects wants 1 arg, got %d", len(args))
				}
				var in ResidualInput
				if err := json.Unmarshal(args[0], &in); err != nil {
					t.Fatalf("decode input: %v", err)
				}
				got = EstimateResidualDefects(in)

			case "matchFindings":
				if len(args) != 2 && len(args) != 3 {
					t.Fatalf("matchFindings wants 2 or 3 args, got %d", len(args))
				}
				var a, b []string
				if err := json.Unmarshal(args[0], &a); err != nil {
					t.Fatalf("decode a: %v", err)
				}
				if err := json.Unmarshal(args[1], &b); err != nil {
					t.Fatalf("decode b: %v", err)
				}
				if len(args) == 3 {
					// JSON.stringify flattens NaN/±Infinity to null, and
					// jscompat.JSNumber decodes null back to NaN. Both
					// thresholds make every comparison false, so the
					// observable result is unchanged.
					var th jscompat.JSNumber
					if err := json.Unmarshal(args[2], &th); err != nil {
						t.Fatalf("decode threshold: %v", err)
					}
					got = MatchFindings(a, b, float64(th))
				} else {
					got = MatchFindings(a, b)
				}

			default:
				t.Fatalf("unknown fn %q", fx.Fn)
			}

			enc, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(enc) != fx.OutJSON {
				t.Errorf("parity mismatch\n  args: %s\n    go: %s\n    ts: %s",
					fx.ArgsJSON, enc, fx.OutJSON)
			}
		})
	}

	for _, fn := range []string{"estimateResidualDefects", "matchFindings"} {
		if seen[fn] == 0 {
			t.Errorf("no fixture cases for %s", fn)
		}
	}
}

// The fixture file cannot round-trip a non-finite INPUT (JSON.stringify writes
// null), so the ±Infinity arms of clampCount are pinned here instead. All
// three non-finite values collapse to 0, exactly like NaN.
func TestNonFiniteInputsClampLikeNaN(t *testing.T) {
	inf := math.Inf(1)
	cases := []struct {
		name string
		in   ResidualInput
	}{
		{"+Infinity sample1", ResidualInput{jscompat.JSNumber(inf), 3, 1}},
		{"-Infinity sample1", ResidualInput{jscompat.JSNumber(-inf), 3, 1}},
		{"NaN sample1", ResidualInput{jscompat.JSNumber(math.NaN()), 3, 1}},
	}
	// n1 → 0, n2 → 3, m → min(1, 0, 3) = 0.
	// distinct = 3; chapman = (1*4)/1 - 1 = 3; total = 3; residual = 0.
	want := `{"estimatedTotal":3,"estimatedResidual":0}`
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := jscompat.Stringify(EstimateResidualDefects(tc.in))
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(enc) != want {
				t.Errorf("got %s, want %s", enc, want)
			}
		})
	}

	t.Run("+Infinity overlap clamps below both samples", func(t *testing.T) {
		got := EstimateResidualDefects(ResidualInput{4, 4, jscompat.JSNumber(inf)})
		// overlap → 0, so this is the zero-overlap case, not full overlap.
		enc, _ := jscompat.Stringify(got)
		if string(enc) != `{"estimatedTotal":24,"estimatedResidual":16}` {
			t.Errorf("got %s", enc)
		}
	})
}

// jsRound is the one numeric primitive where the usual math.Floor(x+0.5)
// shorthand is NOT Math.round. Expectations captured from V8 (bun 1.2.23).
func TestJSRoundMatchesV8(t *testing.T) {
	negZero := math.Copysign(0, -1)
	cases := []struct {
		in   float64
		want float64
	}{
		{0.49999999999999994, 0}, // floor(x+0.5) would say 1
		{0.5, 1},
		{1.5, 2},
		{2.5, 3},
		{8.5, 9},
		{15.5, 16},
		{-0.5, negZero},
		{-1.5, -1}, // ties go toward +Inf, so NOT -2
		{-2.5, -2},
		{-0.3, negZero},
		{-0.6, -1},
		{0.3, 0},
		{0, 0},
		{negZero, negZero},
		{4503599627370497, 4503599627370497}, // floor(x+0.5) would say ...498
		{-4503599627370497, -4503599627370497},
		{1e308, 1e308},
	}
	for _, tc := range cases {
		got := jsRound(tc.in)
		if got != tc.want || math.Signbit(got) != math.Signbit(tc.want) {
			t.Errorf("jsRound(%v) = %v (signbit %v), want %v (signbit %v)",
				tc.in, got, math.Signbit(got), tc.want, math.Signbit(tc.want))
		}
	}
	for _, x := range []float64{math.Inf(1), math.Inf(-1)} {
		if got := jsRound(x); got != x {
			t.Errorf("jsRound(%v) = %v, want %v", x, got, x)
		}
	}
	if got := jsRound(math.NaN()); !math.IsNaN(got) {
		t.Errorf("jsRound(NaN) = %v, want NaN", got)
	}
}

// The tokenizer hand-rolls JS toLowerCase for the ASCII-alnum subset; U+0130
// is the code point where Go's strings.ToLower would silently disagree.
func TestTokenizerUnicodeParity(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"U+0130 breaks the run", "İİ", nil},
		// The 'i' joins the run BEFORE the combining dot ends it, so this is
		// "ai"+"b", not "aib" (strings.ToLower) and not "a"+"i"+"b".
		{"U+0130 mid-word breaks the word", "aİb", []string{"ai"}},
		{"U+0130 leads a token", "İnfo", []string{"nfo"}},
		{"U+212A kelvin lowercases to k", "Kelvin", []string{"kelvin"}},
		{"dotless i is not ASCII", "ıı", nil},
		{"fullwidth latin is not ASCII", "ＡＢＣ", nil},
		{"final sigma is a separator", "ΑΣ ok fine", []string{"ok", "fine"}},
		{"digits count", "err 42 x7", []string{"err", "42", "x7"}},
		{"one-char tokens dropped", "a bb c dd", []string{"bb", "dd"}},
		{"dedupe keeps first-seen order", "bb aa bb aa", []string{"bb", "aa"}},
		{"astral chars separate", "aa🎉bb", []string{"aa", "bb"}},
		{"empty string", "", nil},
		{"separators only", "-_./ :;", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := newTokenSet(tc.in).order
			if len(got) != len(tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
			}
		})
	}
}
