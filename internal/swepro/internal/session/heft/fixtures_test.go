package heft

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"testing"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// Replays testdata/fixtures.json, produced by tools/fixtures/gen-heft.ts from
// the real src/session/heft.ts. The gate is BYTE equality between
// jscompat.Stringify(goResult) and the JSON.stringify the TS run recorded.

type fixtureLine struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

// ── the JSON-transportable specs mirrored from tools/fixtures/gen-heft.ts ──

type modelSpec struct {
	ID    string          `json:"id"`
	Slots json.RawMessage `json:"slots"`
}

type tableEntry struct {
	TaskID     string          `json:"taskId"`
	ModelID    string          `json:"modelId"`
	DurationMs json.RawMessage `json:"durationMs"`
	CostUsd    json.RawMessage `json:"costUsd"`
}

type estimateSpec struct {
	Kind               string          `json:"kind"`
	Entries            []tableEntry    `json:"entries"`
	FallbackDurationMs json.RawMessage `json:"fallbackDurationMs"`
	FallbackCostUsd    json.RawMessage `json:"fallbackCostUsd"`
	DurationStart      json.RawMessage `json:"durationStart"`
	DurationStep       json.RawMessage `json:"durationStep"`
	CostStart          json.RawMessage `json:"costStart"`
	CostStep           json.RawMessage `json:"costStep"`
	A                  json.RawMessage `json:"a"`
	B                  json.RawMessage `json:"b"`
	C                  json.RawMessage `json:"c"`
}

type caseSpec struct {
	Tasks          []HeftTask   `json:"tasks"`
	Models         []modelSpec  `json:"models"`
	Estimate       estimateSpec `json:"estimate"`
	UniformModelID string       `json:"uniformModelId"`
}

// specNumber decodes a NumSpec: a JSON number, or one of the string sentinels
// that JSON cannot carry natively.
func specNumber(t *testing.T, raw json.RawMessage) float64 {
	t.Helper()
	if len(raw) == 0 {
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

func buildModels(t *testing.T, specs []modelSpec) []HeftModel {
	t.Helper()
	models := make([]HeftModel, 0, len(specs))
	for _, spec := range specs {
		model := HeftModel{ID: spec.ID}
		// Absent and explicit null both mean "no slots given".
		if len(spec.Slots) > 0 && string(spec.Slots) != "null" {
			value := specNumber(t, spec.Slots)
			model.Slots = &value
		}
		models = append(models, model)
	}
	return models
}

// utf16Len is JS `s.length` — UTF-16 code units, not runes or bytes.
func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

func buildEstimate(t *testing.T, spec estimateSpec) func(string, string) HeftEstimate {
	t.Helper()
	switch spec.Kind {
	case "counter":
		// Order-sensitive on purpose: proves prepare() calls estimate in the
		// same task-major/model-minor sequence as the TS.
		calls := 0
		durationStart := specNumber(t, spec.DurationStart)
		durationStep := specNumber(t, spec.DurationStep)
		costStart := specNumber(t, spec.CostStart)
		costStep := specNumber(t, spec.CostStep)
		return func(string, string) HeftEstimate {
			index := calls
			calls++
			return HeftEstimate{
				DurationMs: jscompat.JSNumber(durationStart + float64(index)*durationStep),
				CostUsd:    jscompat.JSNumber(costStart + float64(index)*costStep),
			}
		}
	case "lengths":
		a := specNumber(t, spec.A)
		b := specNumber(t, spec.B)
		c := specNumber(t, spec.C)
		return func(taskID string, modelID string) HeftEstimate {
			taskLen := float64(utf16Len(taskID))
			modelLen := float64(utf16Len(modelID))
			return HeftEstimate{
				DurationMs: jscompat.JSNumber(a*taskLen + b*modelLen),
				CostUsd:    jscompat.JSNumber(c * taskLen * modelLen),
			}
		}
	case "table":
		table := map[string]map[string]HeftEstimate{}
		for _, entry := range spec.Entries {
			if table[entry.TaskID] == nil {
				table[entry.TaskID] = map[string]HeftEstimate{}
			}
			table[entry.TaskID][entry.ModelID] = HeftEstimate{
				DurationMs: jscompat.JSNumber(specNumber(t, entry.DurationMs)),
				CostUsd:    jscompat.JSNumber(specNumber(t, entry.CostUsd)),
			}
		}
		fallbackDuration := specNumber(t, spec.FallbackDurationMs)
		fallbackCost := specNumber(t, spec.FallbackCostUsd)
		return func(taskID string, modelID string) HeftEstimate {
			if byModel, ok := table[taskID]; ok {
				if hit, ok := byModel[modelID]; ok {
					return HeftEstimate{DurationMs: hit.DurationMs, CostUsd: hit.CostUsd}
				}
			}
			return HeftEstimate{
				DurationMs: jscompat.JSNumber(fallbackDuration),
				CostUsd:    jscompat.JSNumber(fallbackCost),
			}
		}
	}
	t.Fatalf("unknown estimate kind %q", spec.Kind)
	return nil
}

// ── result envelopes (field order == the gen script's object literals) ─────

type okEnvelope struct {
	Ok    bool `json:"ok"`
	Value any  `json:"value"`
}

type errEnvelope struct {
	Ok      bool     `json:"ok"`
	Name    string   `json:"name"`
	Message string   `json:"message"`
	TaskIDs []string `json:"taskIds"`
}

func envelope(value any, err error) any {
	if err == nil {
		return okEnvelope{Ok: true, Value: value}
	}
	wrapped := errEnvelope{Ok: false, Message: err.Error()}
	switch typed := err.(type) {
	case *HeftCycleError:
		wrapped.Name = typed.Name()
		wrapped.TaskIDs = typed.TaskIDs
	case *HeftInputError:
		wrapped.Name = typed.Name()
	default:
		wrapped.Name = "Error"
	}
	return wrapped
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
			var args []caseSpec
			if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
				t.Fatalf("decode args_json: %v", err)
			}
			if len(args) != 1 {
				t.Fatalf("expected 1 arg, got %d", len(args))
			}
			spec := args[0]
			opts := HeftOptions{
				Tasks:    spec.Tasks,
				Models:   buildModels(t, spec.Models),
				Estimate: buildEstimate(t, spec.Estimate),
			}

			var result any
			switch fixture.Fn {
			case "heftSchedule":
				value, err := HeftSchedule(opts)
				result = envelope(value, err)
			case "compareToUniform":
				value, err := CompareToUniform(CompareToUniformOptions{
					HeftOptions:    opts,
					UniformModelID: spec.UniformModelID,
				})
				result = envelope(value, err)
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
	if cases < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", cases)
	}
	t.Logf("replayed %d fixture cases", cases)
}
