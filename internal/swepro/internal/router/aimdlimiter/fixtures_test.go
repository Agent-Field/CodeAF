package aimdlimiter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// Replays testdata/fixtures.json, produced by tools/fixtures/gen-aimdlimiter.ts
// from the real src/router/aimd-limiter.ts. The gate is BYTE equality between
// jscompat.Stringify(goResult) and the JSON.stringify the TS run recorded.
//
// The TS harness observes wake order through .then callbacks flushed at the end
// of every step; the Go harness observes it by scanning the outstanding probes
// in creation order for newly-closed channels. Those agree because
// drainWaiters always resolves a PREFIX of the FIFO queue and an acquire can
// only take the fast path when the queue is empty (drainWaiters runs to the
// fixpoint inflight >= cap on every release and every cap raise).

type fixtureLine struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

// ── the JSON-transportable specs mirrored from the gen script ─────────────

type optionsSpec struct {
	InitialCap json.RawMessage `json:"initialCap"`
	MinCap     json.RawMessage `json:"minCap"`
	MaxCap     json.RawMessage `json:"maxCap"`
	OnChange   string          `json:"onChange"`
}

type opSpec struct {
	Op string `json:"op"`
	ID string `json:"id"`
}

type scriptSpec struct {
	Options optionsSpec `json:"options"`
	Ops     []opSpec    `json:"ops"`
}

type singletonOpSpec struct {
	Op      string      `json:"op"`
	Options optionsSpec `json:"options"`
}

type singletonSpec struct {
	Ops []singletonOpSpec `json:"ops"`
}

// specNumber decodes a NumSpec: a JSON number, or one of the string sentinels
// that JSON cannot carry natively.
func specNumber(t *testing.T, raw json.RawMessage) float64 {
	t.Helper()
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

// specPtr collapses "key absent" and "key is null" to nil, exactly like TS `??`.
func specPtr(t *testing.T, raw json.RawMessage) *float64 {
	t.Helper()
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	value := specNumber(t, raw)
	return &value
}

// ── result envelopes (field order == the gen script's object literals) ────

type rawView struct {
	Cap      string `json:"cap"`
	Inflight string `json:"inflight"`
	Min      string `json:"min"`
	Max      string `json:"max"`
}

type changeRaw struct {
	OldCap string `json:"oldCap"`
	NewCap string `json:"newCap"`
}

type recordedChange struct {
	Change ConcurrencyChange `json:"change"`
	Raw    changeRaw         `json:"raw"`
}

type stepRecord struct {
	Op       string            `json:"op"`
	Settled  []string          `json:"settled"`
	Cap      jscompat.JSNumber `json:"cap"`
	Inflight jscompat.JSNumber `json:"inflight"`
	Raw      rawView           `json:"raw"`
	Inspect  Inspection        `json:"inspect"`
	Changes  []recordedChange  `json:"changes"`
}

type scriptResult struct {
	Steps    []stepRecord `json:"steps"`
	Final    Inspection   `json:"final"`
	FinalRaw rawView      `json:"finalRaw"`
	Pending  []string     `json:"pending"`
}

type singletonStep struct {
	Op      string     `json:"op"`
	Same    bool       `json:"same"`
	Inspect Inspection `json:"inspect"`
	Raw     rawView    `json:"raw"`
}

type singletonResult struct {
	Steps []singletonStep `json:"steps"`
}

// rawOf is the gen script's String(n) view, which is what actually
// discriminates a wedged NaN cap from a wedged Infinity cap — JSON.stringify
// flattens both to null.
func rawOf(limiter *AIMDConcurrencyLimiter) rawView {
	snapshot := limiter.Inspect()
	return rawView{
		Cap:      jscompat.FormatNumber(float64(snapshot.Cap)),
		Inflight: jscompat.FormatNumber(float64(snapshot.Inflight)),
		Min:      jscompat.FormatNumber(float64(snapshot.Min)),
		Max:      jscompat.FormatNumber(float64(snapshot.Max)),
	}
}

func mkRecorded(change ConcurrencyChange) recordedChange {
	return recordedChange{
		Change: change,
		Raw: changeRaw{
			OldCap: jscompat.FormatNumber(float64(change.OldCap)),
			NewCap: jscompat.FormatNumber(float64(change.NewCap)),
		},
	}
}

type probe struct {
	id   string
	ch   <-chan struct{}
	done bool
}

func buildOptions(t *testing.T, spec optionsSpec, onChange func(ConcurrencyChange)) AIMDOptions {
	t.Helper()
	return AIMDOptions{
		InitialCap: specPtr(t, spec.InitialCap),
		MinCap:     specPtr(t, spec.MinCap),
		MaxCap:     specPtr(t, spec.MaxCap),
		OnChange:   onChange,
	}
}

func runScript(t *testing.T, spec scriptSpec) scriptResult {
	t.Helper()

	mode := spec.Options.OnChange
	if mode == "" {
		mode = "none"
	}
	stepChanges := []recordedChange{}
	var onChange func(ConcurrencyChange)
	switch mode {
	case "none":
		// nil callback: exercises resize()'s log.info fallback branch.
	case "collect":
		onChange = func(change ConcurrencyChange) {
			stepChanges = append(stepChanges, mkRecorded(change))
		}
	case "throw":
		onChange = func(ConcurrencyChange) { panic(errors.New("telemetry exploded")) }
	case "collect-then-throw":
		onChange = func(change ConcurrencyChange) {
			stepChanges = append(stepChanges, mkRecorded(change))
			panic(errors.New("telemetry exploded"))
		}
	default:
		t.Fatalf("unknown onChange mode %q", mode)
	}

	limiter := NewAIMDConcurrencyLimiter(buildOptions(t, spec.Options, onChange))

	outstanding := []*probe{}
	steps := []stepRecord{}
	for _, op := range spec.Ops {
		stepChanges = []recordedChange{}
		switch op.Op {
		case "acquire":
			outstanding = append(outstanding, &probe{id: op.ID, ch: limiter.Acquire()})
		case "release":
			limiter.Release()
		case "onSuccess":
			limiter.OnSuccess()
		case "onThrottle":
			limiter.OnThrottle()
		case "onLowRemaining":
			limiter.OnLowRemaining()
		default:
			t.Fatalf("unknown op %q", op.Op)
		}

		settled := []string{}
		for _, p := range outstanding {
			if p.done {
				continue
			}
			select {
			case <-p.ch:
				p.done = true
				settled = append(settled, p.id)
			default:
			}
		}

		steps = append(steps, stepRecord{
			Op:       op.Op,
			Settled:  settled,
			Cap:      limiter.CurrentCap(),
			Inflight: limiter.CurrentInflight(),
			Raw:      rawOf(limiter),
			Inspect:  limiter.Inspect(),
			Changes:  stepChanges,
		})
	}

	pending := []string{}
	for _, p := range outstanding {
		if !p.done {
			pending = append(pending, p.id)
		}
	}

	return scriptResult{
		Steps:    steps,
		Final:    limiter.Inspect(),
		FinalRaw: rawOf(limiter),
		Pending:  pending,
	}
}

func runSingleton(t *testing.T, spec singletonSpec) singletonResult {
	t.Helper()

	// The singleton is process-global; every case starts from a clean slate.
	SetOpenRouterLimiterForTest(nil)
	var prev *AIMDConcurrencyLimiter
	steps := []singletonStep{}
	for _, op := range spec.Ops {
		switch op.Op {
		case "get":
		case "onSuccess":
			GetOpenRouterLimiter().OnSuccess()
		case "onThrottle":
			GetOpenRouterLimiter().OnThrottle()
		case "onLowRemaining":
			GetOpenRouterLimiter().OnLowRemaining()
		case "acquire":
			GetOpenRouterLimiter().Acquire()
		case "release":
			GetOpenRouterLimiter().Release()
		case "reset":
			SetOpenRouterLimiterForTest(nil)
		case "setCustom":
			SetOpenRouterLimiterForTest(
				NewAIMDConcurrencyLimiter(buildOptions(t, op.Options, nil)),
			)
		default:
			t.Fatalf("unknown singleton op %q", op.Op)
		}
		limiter := GetOpenRouterLimiter()
		steps = append(steps, singletonStep{
			Op:      op.Op,
			Same:    limiter == prev,
			Inspect: limiter.Inspect(),
			Raw:     rawOf(limiter),
		})
		prev = limiter
	}
	SetOpenRouterLimiterForTest(nil)
	return singletonResult{Steps: steps}
}

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
			var result any
			switch fixture.Fn {
			case "script":
				var args []scriptSpec
				if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
					t.Fatalf("decode args_json: %v", err)
				}
				if len(args) != 1 {
					t.Fatalf("expected 1 arg, got %d", len(args))
				}
				result = runScript(t, args[0])
			case "singleton":
				var args []singletonSpec
				if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
					t.Fatalf("decode args_json: %v", err)
				}
				if len(args) != 1 {
					t.Fatalf("expected 1 arg, got %d", len(args))
				}
				result = runSingleton(t, args[0])
			default:
				t.Fatalf("unknown fn %q", fixture.Fn)
			}

			encoded, err := jscompat.Stringify(result)
			if err != nil {
				t.Fatalf("stringify go result: %v", err)
			}
			if string(encoded) != fixture.OutJSON {
				t.Errorf("byte parity failure\n  fn:   %s\n  args: %s\n  want: %s\n  got:  %s",
					fixture.Fn, fixture.ArgsJSON, fixture.OutJSON, encoded)
			}
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	if cases < 30 {
		t.Fatalf("expected at least 30 fixture cases, got %d", cases)
	}
	t.Logf("replayed %d fixture cases", cases)
}
