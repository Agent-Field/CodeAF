package retrysched

// Replays testdata/fixtures.json, produced by tools/fixtures/gen-retrysched.ts
// from the real src/session/retry.ts AND the real src/router/adaptive.ts. The
// gate is BYTE equality between jscompat.Stringify(goResult) and the
// JSON.stringify the TS run recorded.
//
// Two conventions the generator documents and this file mirrors:
//   - out_json is the literal string "undefined" when the TS call returned
//     undefined (JSON has no spelling for it), and
//     {"__error":{"name":…,"message":…}} when it threw. A Go panic is
//     recovered and matched against the latter.
//   - args_json carries NaN / ±Infinity / -0 as "@@num:…" sentinels, because
//     plain JSON turns them into null / 0 and would silently weaken the very
//     cases meant to pin them.
//
// TZ: the generator runs under TZ=UTC because the asctime HTTP-date form has
// no zone and both JavaScriptCore and time.ParseInLocation read it in the
// runtime's local zone. TestMain pins time.Local to match.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/router/adaptive"
)

// fixedNow mirrors FIXED_NOW in tools/fixtures/gen-retrysched.ts
// (2026-07-27T00:00:00.000Z).
const fixedNow float64 = 1785110400000

func TestMain(m *testing.M) {
	// The generator ran under TZ=UTC; jsDateParse reads zone-less HTTP dates
	// in time.Local, so the replay has to agree.
	time.Local = time.UTC
	os.Exit(m.Run())
}

type fixtureLine struct {
	Name     string            `json:"name"`
	Fn       string            `json:"fn"`
	Env      map[string]string `json:"env"`
	ArgsJSON string            `json:"args_json"`
	OutJSON  string            `json:"out_json"`
}

func loadFixtures(t *testing.T) []fixtureLine {
	t.Helper()
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()
	var out []fixtureLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var fx fixtureLine
		if err := json.Unmarshal([]byte(line), &fx); err != nil {
			t.Fatalf("decode fixture line %q: %v", line, err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("no fixtures loaded")
	}
	return out
}

// ── argument decoding ────────────────────────────────────────────────────

// specNumber decodes a JSON number that may be a "@@num:…" sentinel.
func specNumber(t *testing.T, raw json.RawMessage) float64 {
	t.Helper()
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("decode number sentinel %s: %v", raw, err)
		}
		switch s {
		case "@@num:NaN":
			return math.NaN()
		case "@@num:Infinity":
			return math.Inf(1)
		case "@@num:-Infinity":
			return math.Inf(-1)
		case "@@num:-0":
			return math.Copysign(0, -1)
		}
		t.Fatalf("unknown number sentinel %q", s)
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode number %s: %v", raw, err)
	}
	return f
}

func decodeArgs(t *testing.T, fx fixtureLine) []json.RawMessage {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
		t.Fatalf("%s: decode args %q: %v", fx.Name, fx.ArgsJSON, err)
	}
	return args
}

func decodeErr(t *testing.T, raw json.RawMessage) Err {
	t.Helper()
	var e Err
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("decode Err %s: %v", raw, err)
	}
	return e
}

func decodeString(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("decode string %s: %v", raw, err)
	}
	return s
}

// ── router-value spec (mirrors buildVal in gen-retrysched.ts) ────────────

type valSpec struct {
	K       string   `json:"k"`
	Message string   `json:"message"`
	Name    *string  `json:"name"`
	Status  *float64 `json:"status"`
	Cause   *valSpec `json:"cause"`
	Text    string   `json:"text"`
}

// buildRouterValue rebuilds the JS value the generator handed adaptive.ts,
// going through the PUBLIC adapter surface so the fixture exercises what
// internal/llm/orclient will call.
func buildRouterValue(t *testing.T, spec valSpec) *adaptive.JSValue {
	t.Helper()
	switch spec.K {
	case "null":
		return ToRouterValue(nil)
	case "json":
		return JSONToRouterValue([]byte(spec.Text))
	case "error":
		return ToRouterValue(buildStatusError(t, spec))
	}
	t.Fatalf("unknown val spec kind %q", spec.K)
	return nil
}

func buildStatusError(t *testing.T, spec valSpec) error {
	t.Helper()
	e := &StatusError{Message: spec.Message, Status: spec.Status}
	if spec.Name != nil {
		e.Name = *spec.Name
	}
	if spec.Cause != nil {
		e.Cause = buildStatusError(t, *spec.Cause)
	}
	return e
}

// ── output encoding ──────────────────────────────────────────────────────

// tsJSON renders a Go result the way JSON.stringify would, including the two
// out-of-band spellings the generator uses.
func tsJSON(t *testing.T, v any) string {
	t.Helper()
	if v == nil {
		return "undefined"
	}
	b, err := jscompat.Stringify(v)
	if err != nil {
		t.Fatalf("stringify %#v: %v", v, err)
	}
	return string(b)
}

// thrown is the {"__error":{name,message}} shape the generator records for a
// TS throw. The Go twin panics; recoverThrow turns that back into the same
// JSON so the comparison stays a byte comparison.
type thrown struct {
	Error struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	} `json:"__error"`
}

// runMaybePanic executes body, converting a panic whose value is a string of
// the form "TypeError: <message>" into the recorded throw shape.
func runMaybePanic(t *testing.T, body func() string) (out string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		text, ok := r.(string)
		if !ok {
			panic(r)
		}
		name, message, found := strings.Cut(text, ": ")
		if !found {
			panic(r)
		}
		var th thrown
		th.Error.Name = name
		th.Error.Message = message
		b, err := jscompat.Stringify(th)
		if err != nil {
			t.Fatalf("stringify throw: %v", err)
		}
		out = string(b)
	}()
	return body()
}

// ── the replay ───────────────────────────────────────────────────────────

func TestFixtures(t *testing.T) {
	restore := SetNowMSForTesting(func() float64 { return fixedNow })
	defer restore()

	fixtures := loadFixtures(t)
	counts := map[string]int{}

	for _, fx := range fixtures {
		fx := fx
		counts[fx.Fn]++
		t.Run(fx.Name, func(t *testing.T) {
			// The generator deletes CODEAF_TIMEOUT_RETRY_DELAY_MS for every
			// case and then applies the record's env, so do the same.
			t.Setenv(TimeoutRetryDelayEnv, "")
			os.Unsetenv(TimeoutRetryDelayEnv)
			for k, v := range fx.Env {
				t.Setenv(k, v)
			}

			args := decodeArgs(t, fx)
			var got string

			switch fx.Fn {
			case "isTimeoutError":
				got = tsJSON(t, IsTimeoutError(decodeErr(t, args[0])))

			case "delay":
				attempt := specNumber(t, args[0])
				var errArg *Err
				if len(args[1]) > 0 && string(args[1]) != "null" {
					e := decodeErr(t, args[1])
					errArg = &e
				}
				var isTimeout bool
				if err := json.Unmarshal(args[2], &isTimeout); err != nil {
					t.Fatalf("decode isTimeout: %v", err)
				}
				got = tsJSON(t, jscompat.JSNumber(Delay(attempt, errArg, isTimeout)))

			case "dateParse":
				ms := jsDateParse(decodeString(t, args[0]))
				if math.IsNaN(ms) {
					got = "null"
				} else {
					got = tsJSON(t, jscompat.JSNumber(ms))
				}

			case "retryable":
				e := decodeErr(t, args[0])
				provider := decodeString(t, args[1])
				got = runMaybePanic(t, func() string {
					r := Retryable(e, provider)
					if r == nil {
						return "undefined"
					}
					return tsJSON(t, r)
				})

			case "classify":
				e := decodeErr(t, args[0])
				// The authoritative router-side path: the exact JSON text the
				// TS handed to adaptive.ts, decoded key-order-preserving.
				fromJSON := JSONToRouterValue([]byte(args[0]))
				// The Err-shaped path, which must build the identical value.
				fromErr := ErrToRouterValue(e)
				if !reflect.DeepEqual(fromJSON, fromErr) {
					t.Fatalf("JSONToRouterValue and ErrToRouterValue disagree:\n json = %#v\n err  = %#v", fromJSON, fromErr)
				}
				c := ClassifyForRouter(fromJSON)
				got = tsJSON(t, struct {
					RetryIsTimeout         bool `json:"retryIsTimeout"`
					RateLimit              bool `json:"rateLimit"`
					ProviderIncompatible   bool `json:"providerIncompatible"`
					Timeout                bool `json:"timeout"`
					StructuredFailure      bool `json:"structuredFailure"`
					TransientProviderError bool `json:"transientProviderError"`
					RetryableRouteError    bool `json:"retryableRouteError"`
				}{
					RetryIsTimeout:         IsTimeoutError(e),
					RateLimit:              c.RateLimit,
					ProviderIncompatible:   c.ProviderIncompatible,
					Timeout:                c.Timeout,
					StructuredFailure:      c.StructuredFailure,
					TransientProviderError: c.TransientProviderErr,
					RetryableRouteError:    c.RetryableRouteError,
				})

			case "routerval":
				var spec valSpec
				if err := json.Unmarshal(args[0], &spec); err != nil {
					t.Fatalf("decode val spec: %v", err)
				}
				c := ClassifyForRouter(buildRouterValue(t, spec))
				got = tsJSON(t, c)

			case "step":
				attempt := specNumber(t, args[0])
				e := decodeErr(t, args[1])
				provider := decodeString(t, args[2])
				d := Step(attempt, e, provider)
				if d == nil {
					got = "undefined"
				} else {
					got = tsJSON(t, d)
				}

			default:
				t.Fatalf("unknown fn %q", fx.Fn)
			}

			if got != fx.OutJSON {
				t.Errorf("mismatch\n  args %s\n  want %s\n  got  %s", fx.ArgsJSON, fx.OutJSON, got)
			}
		})
	}

	t.Logf("replayed %d fixtures: %s", len(fixtures), formatCounts(counts))
}

func formatCounts(counts map[string]int) string {
	parts := make([]string, 0, len(counts))
	for _, fn := range []string{"isTimeoutError", "delay", "dateParse", "retryable", "classify", "routerval", "step"} {
		if n, ok := counts[fn]; ok {
			parts = append(parts, fmt.Sprintf("%s=%d", fn, n))
		}
	}
	return strings.Join(parts, " ")
}
