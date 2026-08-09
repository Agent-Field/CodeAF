package adaptive

// Replays testdata/fixtures.json, produced by tools/fixtures/gen-adaptive.ts
// from the real src/router/adaptive.ts. The gate is BYTE equality between
// jscompat.Stringify(goResult) and the JSON.stringify the TS run recorded.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type fixtureLine struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

// ── number-spec decoding (JSON cannot carry NaN / ±Infinity) ──────────────

func specNumber(t *testing.T, raw json.RawMessage) float64 {
	t.Helper()
	if len(raw) == 0 || string(raw) == "null" {
		t.Fatalf("missing number in fixture spec")
	}
	if raw[0] == '"' {
		var sentinel string
		if err := json.Unmarshal(raw, &sentinel); err != nil {
			t.Fatalf("decode sentinel %s: %v", raw, err)
		}
		switch sentinel {
		case "NaN":
			return math.NaN()
		case "Infinity":
			return math.Inf(1)
		case "-Infinity":
			return math.Inf(-1)
		}
		t.Fatalf("unknown number sentinel %q", sentinel)
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode number %s: %v", raw, err)
	}
	return value
}

func specNumberOr(t *testing.T, raw json.RawMessage, fallback float64) float64 {
	t.Helper()
	if len(raw) == 0 || string(raw) == "null" {
		return fallback
	}
	return specNumber(t, raw)
}

// ── error-value spec (mirrors buildErr in gen-adaptive.ts) ────────────────

type errSpec struct {
	K       string          `json:"k"`
	V       json.RawMessage `json:"v"`
	Message string          `json:"message"`
	Name    *string         `json:"name"`
	Props   [][2]json.RawMessage
	Cause   json.RawMessage `json:"cause"`
	Items   []json.RawMessage
}

// UnmarshalJSON is hand-rolled because `props` is an array of [key, ErrSpec]
// tuples (a JS Map-literal shape) and `items` is a heterogeneous list.
func (s *errSpec) UnmarshalJSON(data []byte) error {
	var raw struct {
		K       string               `json:"k"`
		V       json.RawMessage      `json:"v"`
		Message string               `json:"message"`
		Name    *string              `json:"name"`
		Props   [][2]json.RawMessage `json:"props"`
		Cause   json.RawMessage      `json:"cause"`
		Items   []json.RawMessage    `json:"items"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.K = raw.K
	s.V = raw.V
	s.Message = raw.Message
	s.Name = raw.Name
	s.Props = raw.Props
	s.Cause = raw.Cause
	s.Items = raw.Items
	return nil
}

func buildErrValue(t *testing.T, raw json.RawMessage) *JSValue {
	t.Helper()
	// An absent spec is JS `undefined`, which `?? null` then turns into null;
	// both are nullish, so the distinction never reaches behaviour.
	if len(raw) == 0 || string(raw) == "null" {
		return Null()
	}
	var spec errSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("decode err spec %s: %v", raw, err)
	}
	switch spec.K {
	case "undefined":
		return Undefined()
	case "null":
		return Null()
	case "string":
		var s string
		if err := json.Unmarshal(spec.V, &s); err != nil {
			t.Fatalf("decode string err spec: %v", err)
		}
		return Str(s)
	case "number":
		return Num(specNumber(t, spec.V))
	case "bool":
		var b bool
		if err := json.Unmarshal(spec.V, &b); err != nil {
			t.Fatalf("decode bool err spec: %v", err)
		}
		return Bool(b)
	case "error":
		v := Err(spec.Message)
		if spec.Name != nil {
			v.Name = *spec.Name
		}
		if len(spec.Cause) > 0 && string(spec.Cause) != "null" {
			v.Cause = buildErrValue(t, spec.Cause)
		}
		if len(spec.Props) > 0 {
			v.Props = jscompat.NewOrderedMap[string, *JSValue]()
			for _, pair := range spec.Props {
				var key string
				if err := json.Unmarshal(pair[0], &key); err != nil {
					t.Fatalf("decode prop key: %v", err)
				}
				v.Props.Set(key, buildErrValue(t, pair[1]))
			}
		}
		return v
	case "object":
		v := &JSValue{Kind: JSObject, Props: jscompat.NewOrderedMap[string, *JSValue]()}
		for _, pair := range spec.Props {
			var key string
			if err := json.Unmarshal(pair[0], &key); err != nil {
				t.Fatalf("decode prop key: %v", err)
			}
			v.Props.Set(key, buildErrValue(t, pair[1]))
		}
		return v
	case "array":
		items := make([]*JSValue, 0, len(spec.Items))
		for _, item := range spec.Items {
			items = append(items, buildErrValue(t, item))
		}
		return Arr(items...)
	}
	t.Fatalf("unknown err spec kind %q", spec.K)
	return nil
}

// ── candidate spec (mirrors buildCand in gen-adaptive.ts) ─────────────────

type candSpec struct {
	ID         string          `json:"id"`
	Tier       *ModelTier      `json:"tier"`
	Family     *string         `json:"family"`
	Prompt     json.RawMessage `json:"prompt"`
	Completion json.RawMessage `json:"completion"`
	Priority   json.RawMessage `json:"priority"`
}

func buildCand(t *testing.T, spec candSpec, defaultTier ModelTier) ModelCandidate {
	t.Helper()
	tier := defaultTier
	if spec.Tier != nil {
		tier = *spec.Tier
	}
	family := DeriveFamily(spec.ID)
	if spec.Family != nil {
		family = *spec.Family
	}
	return ModelCandidate{
		ID:                   spec.ID,
		Tier:                 tier,
		Family:               family,
		PromptUSDPerMtok:     jscompat.JSNumber(specNumberOr(t, spec.Prompt, 1)),
		CompletionUSDPerMtok: jscompat.JSNumber(specNumberOr(t, spec.Completion, 1)),
		Priority:             jscompat.JSNumber(specNumberOr(t, spec.Priority, 0)),
	}
}

func buildCands(t *testing.T, specs []candSpec, defaultTier ModelTier) []ModelCandidate {
	t.Helper()
	out := make([]ModelCandidate, 0, len(specs))
	for _, spec := range specs {
		out = append(out, buildCand(t, spec, defaultTier))
	}
	return out
}

// ── scenario spec ─────────────────────────────────────────────────────────

type configSpec struct {
	// Pointers so an ABSENT pool (defaults kick in) is distinguishable from an
	// explicit empty one — `if (spec.config.high)` in the gen script is a
	// truthiness test, and `[]` is truthy in JS.
	High        *[]candSpec     `json:"high"`
	Low         *[]candSpec     `json:"low"`
	Frontier    *[]candSpec     `json:"frontier"`
	MaxAttempts json.RawMessage `json:"maxAttempts"`
	Seed        json.RawMessage `json:"seed"`
	Unseeded    bool            `json:"unseeded"`
}

type opSpec struct {
	Op               string          `json:"op"`
	Slot             string          `json:"slot"`
	Tier             ModelTier       `json:"tier"`
	NowMs            json.RawMessage `json:"nowMs"`
	TimeoutMs        json.RawMessage `json:"timeoutMs"`
	AbortBefore      bool            `json:"abortBefore"`
	AbortAtSleep     *int            `json:"abortAtSleep"`
	Ref              int             `json:"ref"`
	ElapsedSeconds   json.RawMessage `json:"elapsedSeconds"`
	CompletionTokens json.RawMessage `json:"completionTokens"`
	Error            json.RawMessage `json:"error"`
	ForSlot          string          `json:"forSlot"`
	Ms               json.RawMessage `json:"ms"`
}

type scenarioSpec struct {
	Config     configSpec      `json:"config"`
	RandomSeed json.RawMessage `json:"randomSeed"`
	StartNow   json.RawMessage `json:"startNow"`
	Ops        []opSpec        `json:"ops"`
}

type pickOK struct {
	Ok     bool        `json:"ok"`
	Choice RouteChoice `json:"choice"`
}

type pickErr struct {
	Ok    bool   `json:"ok"`
	Error string `json:"error"`
}

type scenarioOut struct {
	Results []any                `json:"results"`
	Events  []AdaptiveRouteEvent `json:"events"`
}

func runScenario(t *testing.T, spec scenarioSpec) scenarioOut {
	t.Helper()

	now := specNumberOr(t, spec.StartNow, 1_700_000_000_000)
	restoreClock := SetClockForTesting(func() float64 { return now })
	defer restoreClock()

	if spec.Config.Unseeded {
		restoreRandom := SetRandomForTesting(jscompat.Mulberry32(toUint32(specNumberOr(t, spec.RandomSeed, 12345))))
		defer restoreRandom()
	}

	events := []AdaptiveRouteEvent{}
	cfg := AdaptiveRouterConfig{OnEvent: func(e AdaptiveRouteEvent) { events = append(events, e) }}
	if spec.Config.High != nil {
		cfg.HighModels = buildCands(t, *spec.Config.High, ModelTierHigh)
	}
	if spec.Config.Low != nil {
		cfg.LowModels = buildCands(t, *spec.Config.Low, ModelTierLow)
	}
	if spec.Config.Frontier != nil {
		cfg.FrontierModels = buildCands(t, *spec.Config.Frontier, ModelTierFrontier)
	}
	if len(spec.Config.MaxAttempts) > 0 {
		v := specNumber(t, spec.Config.MaxAttempts)
		cfg.MaxAttempts = &v
	}
	if !spec.Config.Unseeded {
		v := specNumberOr(t, spec.Config.Seed, 42)
		cfg.RandomSeed = &v
	}

	router := NewAdaptiveModelRouter(cfg)
	var choices []RouteChoice
	results := []any{}

	for _, op := range spec.Ops {
		switch op.Op {
		case "tryPick":
			var result TryPickResult
			if len(op.NowMs) > 0 {
				result = router.TryPick(op.Slot, op.Tier, specNumber(t, op.NowMs))
			} else {
				result = router.TryPick(op.Slot, op.Tier)
			}
			if result.Ok {
				choices = append(choices, result.Choice)
			}
			results = append(results, result)
		case "pickSync":
			choice := router.PickSync(op.Slot, op.Tier)
			choices = append(choices, choice)
			results = append(results, choice)
		case "pick":
			signal := NewAbortSignal()
			if op.AbortBefore {
				signal.Abort()
			}
			sleeps := 0
			restoreSleep := SetSleeperForTesting(func(ms float64, _ *AbortSignal) {
				now += ms
				sleeps++
				if op.AbortAtSleep != nil && sleeps == *op.AbortAtSleep {
					signal.Abort()
				}
			})
			opts := PickOptions{Signal: signal}
			if len(op.TimeoutMs) > 0 {
				v := specNumber(t, op.TimeoutMs)
				opts.TimeoutMs = &v
			}
			choice, err := router.Pick(op.Slot, op.Tier, opts)
			restoreSleep()
			if err != nil {
				results = append(results, pickErr{Ok: false, Error: err.Error()})
			} else {
				choices = append(choices, choice)
				results = append(results, pickOK{Ok: true, Choice: choice})
			}
		case "register":
			idx := op.Ref
			if idx < 0 {
				idx = len(choices) + idx
			}
			if idx < 0 || idx >= len(choices) {
				t.Fatalf("register ref %d out of range (%d choices)", op.Ref, len(choices))
			}
			event := router.Register(choices[idx],
				specNumber(t, op.ElapsedSeconds),
				specNumber(t, op.CompletionTokens),
				buildErrValue(t, op.Error))
			results = append(results, event)
		case "candidatesForTier":
			results = append(results, router.CandidatesForTier(op.Tier))
		case "candidatesForTierFiltered":
			results = append(results, router.CandidatesForTierFiltered(op.Tier, op.ForSlot))
		case "effectiveTier":
			results = append(results, string(router.EffectiveTier(op.Tier)))
		case "maxAttempts":
			results = append(results, jscompat.JSNumber(router.MaxAttempts()))
		case "advanceClock":
			now += specNumber(t, op.Ms)
			results = append(results, jscompat.JSNumber(now))
		default:
			t.Fatalf("unknown op %q", op.Op)
		}
	}
	return scenarioOut{Results: results, Events: events}
}

// ── classifier envelope (field order == the gen script's object literal) ──

type classifyOut struct {
	RateLimit            bool `json:"rateLimit"`
	ProviderIncompatible bool `json:"providerIncompatible"`
	Timeout              bool `json:"timeout"`
	Structured           bool `json:"structured"`
	Transient            bool `json:"transient"`
	Retryable            bool `json:"retryable"`
}

// ── replay ────────────────────────────────────────────────────────────────

func TestFixtureParity(t *testing.T) {
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	cases := 0
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var fixture fixtureLine
		if err := json.Unmarshal(raw, &fixture); err != nil {
			t.Fatalf("decode fixture line: %v", err)
		}
		cases++
		t.Run(fixture.Name, func(t *testing.T) {
			result := replay(t, fixture)
			encoded, err := jscompat.Stringify(result)
			if err != nil {
				t.Fatalf("stringify go result: %v", err)
			}
			reason, excused := glibcRoundingExceptions[fixture.Name]
			if string(encoded) == fixture.OutJSON {
				if excused {
					t.Errorf("stale entry in glibcRoundingExceptions: %q now matches byte-for-byte (%s)",
						fixture.Name, reason)
				}
				return
			}
			if !excused {
				t.Errorf("byte parity failure\n  fn:   %s\n  args: %s\n  want: %s\n  got:  %s",
					fixture.Fn, fixture.ArgsJSON, fixture.OutJSON, encoded)
				return
			}
			// A registered libm divergence: the bytes may differ, but only in
			// numeric leaves and only by a single ULP. Anything else is a real
			// regression wearing the exception as a disguise.
			if err := jsonWithinOneULP(encoded, []byte(fixture.OutJSON)); err != nil {
				t.Errorf("registered libm divergence %q exceeded 1 ULP: %v\n  want: %s\n  got:  %s",
					fixture.Name, err, fixture.OutJSON, encoded)
				return
			}
			t.Logf("documented libm divergence (%s); every numeric leaf within 1 ULP", reason)
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	if cases < 80 {
		t.Fatalf("expected at least 80 fixture cases, got %d", cases)
	}
	t.Logf("replayed %d fixture cases", cases)
}

// glibcRoundingExceptions lists the fixture cases whose bytes CANNOT match.
//
// bun routes Math.log/cos/pow to the host libm, and glibc is not correctly
// rounded (~0.1% of results are 1 ULP off the true value); internal/router/
// adaptive computes the correctly-rounded value instead — see mathcr.go for why
// that is the closest reachable pure-Go target. Each entry names the exact
// operation that diverges so the list stays auditable, and the replay above
// still requires structural equality with every numeric leaf within 1 ULP.
//
// Keep this list SHORT. A new entry means either a newly sampled non-CR libm
// input or a real bug — investigate before adding one.
var glibcRoundingExceptions = map[string]string{
	"rng/gauss/seed-0": "draw #12 calls cos(0.5518523475053569); glibc returns " +
		"0.851554861641658, the true value is 0.8515548616416578",
}

// jsonWithinOneULP reports whether two JSON documents are structurally identical
// with every numeric leaf at most one representable double apart.
func jsonWithinOneULP(got, want []byte) error {
	decode := func(b []byte) (any, error) {
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		return v, nil
	}
	gotValue, err := decode(got)
	if err != nil {
		return fmt.Errorf("decode go result: %w", err)
	}
	wantValue, err := decode(want)
	if err != nil {
		return fmt.Errorf("decode fixture: %w", err)
	}
	return compareJSONValues(gotValue, wantValue, "$")
}

func compareJSONValues(got, want any, path string) error {
	switch gotTyped := got.(type) {
	case map[string]any:
		wantTyped, ok := want.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: object vs %T", path, want)
		}
		if len(gotTyped) != len(wantTyped) {
			return fmt.Errorf("%s: %d keys vs %d", path, len(gotTyped), len(wantTyped))
		}
		for key, value := range gotTyped {
			counterpart, ok := wantTyped[key]
			if !ok {
				return fmt.Errorf("%s.%s: absent in the fixture", path, key)
			}
			if err := compareJSONValues(value, counterpart, path+"."+key); err != nil {
				return err
			}
		}
		return nil
	case []any:
		wantTyped, ok := want.([]any)
		if !ok {
			return fmt.Errorf("%s: array vs %T", path, want)
		}
		if len(gotTyped) != len(wantTyped) {
			return fmt.Errorf("%s: %d elements vs %d", path, len(gotTyped), len(wantTyped))
		}
		for i := range gotTyped {
			if err := compareJSONValues(gotTyped[i], wantTyped[i], fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	case json.Number:
		wantTyped, ok := want.(json.Number)
		if !ok {
			return fmt.Errorf("%s: number vs %T", path, want)
		}
		if gotTyped.String() == wantTyped.String() {
			return nil
		}
		gotFloat, err1 := gotTyped.Float64()
		wantFloat, err2 := wantTyped.Float64()
		if err1 != nil || err2 != nil {
			return fmt.Errorf("%s: unparseable numbers %s / %s", path, gotTyped, wantTyped)
		}
		if gap := ulpGap(gotFloat, wantFloat); gap >= 0 && gap <= 1 {
			return nil
		}
		return fmt.Errorf("%s: %s vs %s differ by more than 1 ULP", path, gotTyped, wantTyped)
	default:
		if got != want {
			return fmt.Errorf("%s: %v vs %v", path, got, want)
		}
		return nil
	}
}

func replay(t *testing.T, fixture fixtureLine) any {
	t.Helper()
	switch fixture.Fn {
	case "deriveFamily":
		var args []string
		decodeArgs(t, fixture.ArgsJSON, &args)
		return DeriveFamily(args[0])

	case "normalizeCandidateModel":
		var args []string
		decodeArgs(t, fixture.ArgsJSON, &args)
		return NormalizeCandidateModel(args[0])

	case "parseModelList":
		var args []json.RawMessage
		decodeArgs(t, fixture.ArgsJSON, &args)
		var raw *string
		if string(args[0]) != "null" {
			var s string
			if err := json.Unmarshal(args[0], &s); err != nil {
				t.Fatalf("decode raw: %v", err)
			}
			raw = &s
		}
		var tier ModelTier
		if err := json.Unmarshal(args[1], &tier); err != nil {
			t.Fatalf("decode tier: %v", err)
		}
		return ParseModelList(raw, tier)

	case "defaultHighModels":
		return DefaultHighModels()

	case "defaultLowModels":
		return DefaultLowModels()

	case "roleAdversaries":
		return RoleAdversaries

	case "classify":
		var args []json.RawMessage
		decodeArgs(t, fixture.ArgsJSON, &args)
		err := buildErrValue(t, args[0])
		return classifyOut{
			RateLimit:            IsLikelyRateLimit(err),
			ProviderIncompatible: IsLikelyProviderIncompatible(err),
			Timeout:              IsLikelyTimeout(err),
			Structured:           IsLikelyStructuredFailure(err),
			Transient:            IsLikelyTransientProviderError(err),
			Retryable:            IsRetryableRouteError(err),
		}

	case "mulberry32":
		var args []float64
		decodeArgs(t, fixture.ArgsJSON, &args)
		rng := jscompat.Mulberry32(toUint32(args[0]))
		return drawN(rng, int(args[1]), func(r func() float64) float64 { return r() })

	case "gauss":
		var args []float64
		decodeArgs(t, fixture.ArgsJSON, &args)
		rng := jscompat.Mulberry32(toUint32(args[0]))
		return drawN(rng, int(args[1]), gauss)

	case "sampleGamma":
		var args []float64
		decodeArgs(t, fixture.ArgsJSON, &args)
		rng := jscompat.Mulberry32(toUint32(args[0]))
		shape := args[1]
		return drawN(rng, int(args[2]), func(r func() float64) float64 { return sampleGamma(r, shape) })

	case "sampleBeta":
		var args []float64
		decodeArgs(t, fixture.ArgsJSON, &args)
		rng := jscompat.Mulberry32(toUint32(args[0]))
		alpha, beta := args[1], args[2]
		return drawN(rng, int(args[3]), func(r func() float64) float64 { return sampleBeta(r, alpha, beta) })

	case "scenario":
		var args []scenarioSpec
		decodeArgs(t, fixture.ArgsJSON, &args)
		return runScenario(t, args[0])
	}
	t.Fatalf("unknown fn %q", fixture.Fn)
	return nil
}

func decodeArgs(t *testing.T, argsJSON string, out any) {
	t.Helper()
	if err := json.Unmarshal([]byte(argsJSON), out); err != nil {
		t.Fatalf("decode args_json %s: %v", argsJSON, err)
	}
}

func drawN(rng func() float64, n int, draw func(func() float64) float64) []jscompat.JSNumber {
	out := make([]jscompat.JSNumber, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, jscompat.JSNumber(draw(rng)))
	}
	return out
}
