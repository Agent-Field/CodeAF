package sprt_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/sprt"
)

// ── fixture line ───────────────────────────────────────────────────────────

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

// ── capture envelope (mirrors tools/fixtures/gen-sprt.ts `capture`) ────────

type errInfo struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

type capture struct {
	OK    bool     `json:"ok"`
	Value any      `json:"value"`
	Error *errInfo `json:"error"`
}

func ok(v any) capture { return capture{OK: true, Value: v, Error: nil} }

func thrown(err error) capture {
	name := "Error"
	var re *sprt.RangeError
	if errors.As(err, &re) {
		name = "RangeError"
	}
	return capture{OK: false, Value: nil, Error: &errInfo{Name: name, Message: err.Error()}}
}

// ── argument decoding ──────────────────────────────────────────────────────

// jsNum decodes the generator's non-finite sentinels. JSON has no NaN or
// Infinity literal, and mapping them to null would be WRONG here: null takes
// the `??` default branch while NaN reaches the finiteness guard and throws.
type jsNum float64

func (n *jsNum) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case `"@NaN"`:
		*n = jsNum(math.NaN())
		return nil
	case `"@Infinity"`:
		*n = jsNum(math.Inf(1))
		return nil
	case `"@-Infinity"`:
		*n = jsNum(math.Inf(-1))
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	*n = jsNum(f)
	return nil
}

func optFloat(n *jsNum) *float64 {
	if n == nil {
		return nil
	}
	f := float64(*n)
	return &f
}

type argOptions struct {
	P0        jsNum  `json:"p0"`
	P1        jsNum  `json:"p1"`
	Alpha     *jsNum `json:"alpha"`
	Beta      *jsNum `json:"beta"`
	MaxTrials *jsNum `json:"maxTrials"`
}

func (a argOptions) toOptions() sprt.SprtOptions {
	return sprt.SprtOptions{
		P0:        float64(a.P0),
		P1:        float64(a.P1),
		Alpha:     optFloat(a.Alpha),
		Beta:      optFloat(a.Beta),
		MaxTrials: optFloat(a.MaxTrials),
	}
}

type argSnapshot struct {
	Successes jsNum  `json:"successes"`
	Trials    jsNum  `json:"trials"`
	P0        jsNum  `json:"p0"`
	P1        jsNum  `json:"p1"`
	Alpha     *jsNum `json:"alpha"`
	Beta      *jsNum `json:"beta"`
}

func (a argSnapshot) toSnapshot() sprt.SprtSnapshot {
	return sprt.SprtSnapshot{
		Successes: float64(a.Successes),
		Trials:    float64(a.Trials),
		P0:        float64(a.P0),
		P1:        float64(a.P1),
		Alpha:     optFloat(a.Alpha),
		Beta:      optFloat(a.Beta),
	}
}

// ── createSprt trace (mirrors gen-sprt.ts `trace`) ─────────────────────────

type traceStep struct {
	Observation   sprt.SprtObservation `json:"observation"`
	Llr           jscompat.JSNumber    `json:"llr"`
	LlrStr        string               `json:"llrStr"`
	Trials        jscompat.JSNumber    `json:"trials"`
	ForceDecision string               `json:"forceDecision"`
}

type sprtTrace struct {
	InitialLlr           jscompat.JSNumber `json:"initialLlr"`
	InitialLlrStr        string            `json:"initialLlrStr"`
	InitialTrials        jscompat.JSNumber `json:"initialTrials"`
	InitialForceDecision string            `json:"initialForceDecision"`
	Steps                []traceStep       `json:"steps"`
}

func runTrace(options sprt.SprtOptions, seq []bool) (sprtTrace, error) {
	s, err := sprt.CreateSprt(options)
	if err != nil {
		return sprtTrace{}, err
	}
	out := sprtTrace{
		InitialLlr:           jscompat.JSNumber(s.Llr()),
		InitialLlrStr:        jscompat.FormatNumber(s.Llr()),
		InitialTrials:        jscompat.JSNumber(s.Trials()),
		InitialForceDecision: s.ForceDecision(),
		Steps:                []traceStep{},
	}
	for _, passed := range seq {
		observation := s.Observe(passed)
		out.Steps = append(out.Steps, traceStep{
			Observation:   observation,
			Llr:           jscompat.JSNumber(s.Llr()),
			LlrStr:        jscompat.FormatNumber(s.Llr()),
			Trials:        jscompat.JSNumber(s.Trials()),
			ForceDecision: s.ForceDecision(),
		})
	}
	return out, nil
}

// ── replay ─────────────────────────────────────────────────────────────────

func replay(t *testing.T, fx fixture) capture {
	t.Helper()
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &raw); err != nil {
		t.Fatalf("%s: decoding args_json %q: %v", fx.Name, fx.ArgsJSON, err)
	}
	switch fx.Fn {
	case "createSprt":
		if len(raw) != 2 {
			t.Fatalf("%s: createSprt wants 2 args, got %d", fx.Name, len(raw))
		}
		var ao argOptions
		if err := json.Unmarshal(raw[0], &ao); err != nil {
			t.Fatalf("%s: decoding options: %v", fx.Name, err)
		}
		var seq []bool
		if err := json.Unmarshal(raw[1], &seq); err != nil {
			t.Fatalf("%s: decoding sequence: %v", fx.Name, err)
		}
		tr, err := runTrace(ao.toOptions(), seq)
		if err != nil {
			return thrown(err)
		}
		return ok(tr)

	case "sprtDecision":
		var as argSnapshot
		if err := json.Unmarshal(raw[0], &as); err != nil {
			t.Fatalf("%s: decoding snapshot: %v", fx.Name, err)
		}
		v, err := sprt.SprtDecision(as.toSnapshot())
		if err != nil {
			return thrown(err)
		}
		return ok(v)

	case "nextSampleNeeded":
		var as argSnapshot
		if err := json.Unmarshal(raw[0], &as); err != nil {
			t.Fatalf("%s: decoding snapshot: %v", fx.Name, err)
		}
		v, err := sprt.NextSampleNeeded(as.toSnapshot())
		if err != nil {
			return thrown(err)
		}
		return ok(v)

	case "sprtPlanFor":
		var kind string
		if err := json.Unmarshal(raw[0], &kind); err != nil {
			t.Fatalf("%s: decoding kind: %v", fx.Name, err)
		}
		return ok(sprt.SprtPlanFor(sprt.SprtPlanKind(kind)))

	default:
		t.Fatalf("%s: unknown fn %q", fx.Name, fx.Fn)
		return capture{}
	}
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("opening fixtures: %v", err)
	}
	defer f.Close()
	var out []fixture
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var fx fixture
		if err := json.Unmarshal([]byte(line), &fx); err != nil {
			t.Fatalf("decoding fixture line: %v", err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanning fixtures: %v", err)
	}
	return out
}

// TestFixtureParity is the parity gate: for every fixture case the Go result,
// serialised with jscompat.Stringify, must be byte-for-byte identical to what
// the real TypeScript module produced under JSON.stringify.
func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(fixtures))
	}
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			got := replay(t, fx)
			b, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(b) != fx.OutJSON {
				t.Errorf("fn=%s args=%s\n  want %s\n  got  %s", fx.Fn, fx.ArgsJSON, fx.OutJSON, string(b))
			}
		})
	}
}
