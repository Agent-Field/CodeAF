package heftassign

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"testing"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Replays testdata/fixtures.json, produced by tools/fixtures/gen-heftassign.ts
// from the real src/session/heft-assign.ts. The gate is BYTE equality between
// jscompat.Stringify(goResult) and the JSON.stringify the TS run recorded.

type fixtureLine struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

// ── the JSON-transportable specs mirrored from tools/fixtures/gen-heftassign.ts ──

type durationEntry struct {
	TaskID     string          `json:"taskId"`
	DurationMs json.RawMessage `json:"durationMs"`
}

type durationSpec struct {
	Kind     string          `json:"kind"`
	Value    json.RawMessage `json:"value"`
	Entries  []durationEntry `json:"entries"`
	Fallback json.RawMessage `json:"fallback"`
	A        json.RawMessage `json:"a"`
	B        json.RawMessage `json:"b"`
	Start    json.RawMessage `json:"start"`
	Step     json.RawMessage `json:"step"`
}

type lowCanTakeSpec struct {
	Kind string   `json:"kind"`
	IDs  []string `json:"ids"`
}

type assignCase struct {
	Remaining      []RemainingTask `json:"remaining"`
	Edges          []Edge          `json:"edges"`
	Duration       durationSpec    `json:"duration"`
	HighModelID    string          `json:"highModelId"`
	LowModelID     string          `json:"lowModelId"`
	ParallelWindow json.RawMessage `json:"parallelWindow"`
	CriticalIDs    []string        `json:"criticalIds"`
	LowCanTake     lowCanTakeSpec  `json:"lowCanTake"`
	HighCostPerSec json.RawMessage `json:"highCostPerSec"`
	LowCostPerSec  json.RawMessage `json:"lowCostPerSec"`
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

// optionalNumber models a TS optional field: absent and explicit null both mean
// "not supplied", which is the `?? default` path.
func optionalNumber(t *testing.T, raw json.RawMessage) *float64 {
	t.Helper()
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	value := specNumber(t, raw)
	return &value
}

// utf16Len is JS `s.length` — UTF-16 code units, not runes or bytes.
func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

func buildDuration(t *testing.T, spec durationSpec) func(string) float64 {
	t.Helper()
	switch spec.Kind {
	case "constant":
		value := specNumber(t, spec.Value)
		return func(string) float64 { return value }
	case "byLength":
		a := specNumber(t, spec.A)
		b := specNumber(t, spec.B)
		return func(taskID string) float64 { return a*float64(utf16Len(taskID)) + b }
	case "counter":
		// Order-sensitive on purpose: proves the Go port drives heft's
		// prepare() through the same estimate call sequence as the TS.
		calls := 0
		start := specNumber(t, spec.Start)
		step := specNumber(t, spec.Step)
		return func(string) float64 {
			index := calls
			calls++
			return start + float64(index)*step
		}
	case "table":
		table := map[string]float64{}
		for _, entry := range spec.Entries {
			table[entry.TaskID] = specNumber(t, entry.DurationMs)
		}
		fallback := specNumber(t, spec.Fallback)
		return func(taskID string) float64 {
			if hit, ok := table[taskID]; ok {
				return hit
			}
			return fallback
		}
	}
	t.Fatalf("unknown duration kind %q", spec.Kind)
	return nil
}

func buildLowCanTake(t *testing.T, spec lowCanTakeSpec) func(string) bool {
	t.Helper()
	ids := map[string]bool{}
	for _, id := range spec.IDs {
		ids[id] = true
	}
	switch spec.Kind {
	case "all":
		return func(string) bool { return true }
	case "none":
		return func(string) bool { return false }
	case "except":
		return func(taskID string) bool { return !ids[taskID] }
	case "only":
		return func(taskID string) bool { return ids[taskID] }
	}
	t.Fatalf("unknown lowCanTake kind %q", spec.Kind)
	return nil
}

// ── result envelopes (field order == the gen script's object literals) ─────

// assignOut mirrors `{ tier: [...map], startMs: [...map], makespanMs }`. The
// maps are recorded as [key, value] pair arrays because JSON.stringify(Map) is
// "{}" and would pin nothing.
type assignOut struct {
	Tier       []any             `json:"tier"`
	StartMs    []any             `json:"startMs"`
	MakespanMs jscompat.JSNumber `json:"makespanMs"`
}

type meanOut struct {
	Value jscompat.JSNumber `json:"value"`
	Repr  string            `json:"repr"`
}

// orderCandidate is the `T extends { id: string }` element the fixtures use.
// The id lives in a differently-named field so the type can also carry the
// ID() method that the mirrored OrderByHeftStart constraint needs.
type orderCandidate struct {
	IDValue string `json:"id"`
	Tag     string `json:"tag"`
}

func (c orderCandidate) ID() string { return c.IDValue }

// knownDivergences are fixture cases the Go port deliberately does NOT match,
// with the reason. Nothing else is allowed to differ.
//
// orderByHeftStart's comparator
//
//	(a, b) => (a.start === b.start ? a.index - b.index : a.start - b.start)
//
// induces a total order for every finite/±Infinity start — the index tie-break
// strictly orders every pair — so any stable sort reproduces the TS exactly.
// A NaN start breaks that: `NaN === NaN` is false and `NaN - x` is NaN, which
// SortCompare coerces to +0 ("equal"), so the NaN element compares equal to
// everything while its neighbours stay strictly ordered. The relation stops
// being transitive and the OUTPUT becomes a readout of the host's sort
// algorithm, not of heft-assign.ts.
//
// swe-pro runs on bun 1.2.23 = JavaScriptCore, whose Array.prototype.sort is
// neither Go's sort.SliceStable (insertion sort + symmerge) nor V8's TimSort:
// a 2,000-case differential over random NaN-laden inputs mismatches
// sort.SliceStable on 814 and a faithful V8 TimSort on 250, and JSC's own
// algorithm shows engine-internal behaviour (a sortedness pre-pass, a
// size-dependent small-array path) that no portable Go sort reproduces.
// Matching it is out of scope, so the port keeps sort.SliceStable — correct for
// the entire reachable domain, since computeHeftAssignments can never emit a
// NaN start (heft rejects non-finite durations) — and pins the boundary here.
//
// The four other NaN fixtures are NOT on this list: they agree byte-for-byte
// and pin the "NaN compares equal to everything" semantics itself.
var knownDivergences = map[string]string{
	"order/NaN starts interleaved with descending numbers": "non-transitive comparator: JSC sort internals, see comment above",
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
			case "computeHeftAssignments":
				result = replayAssign(t, fixture.ArgsJSON)
			case "recencyWeightedMean":
				result = replayMean(t, fixture.ArgsJSON)
			case "orderByHeftStart":
				result = replayOrder(t, fixture.ArgsJSON)
			default:
				t.Fatalf("unknown fn %q", fixture.Fn)
			}

			encoded, err := jscompat.Stringify(result)
			if err != nil {
				t.Fatalf("stringify go result: %v", err)
			}
			matched := string(encoded) == fixture.OutJSON
			if reason, known := knownDivergences[fixture.Name]; known {
				if matched {
					t.Errorf("case is on knownDivergences (%s) but now matches — delete the entry", reason)
				} else {
					t.Logf("documented divergence (%s)\n  args: %s\n  ts:   %s\n  go:   %s",
						reason, fixture.ArgsJSON, fixture.OutJSON, encoded)
				}
				return
			}
			if !matched {
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

func replayAssign(t *testing.T, argsJSON string) any {
	t.Helper()
	var args []assignCase
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		t.Fatalf("decode args_json: %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}
	spec := args[0]
	result := ComputeHeftAssignments(HeftAssignInput{
		Remaining:      spec.Remaining,
		Edges:          spec.Edges,
		DurationMsFor:  buildDuration(t, spec.Duration),
		HighModelID:    spec.HighModelID,
		LowModelID:     spec.LowModelID,
		ParallelWindow: specNumber(t, spec.ParallelWindow),
		CriticalIDs:    spec.CriticalIDs,
		LowCanTake:     buildLowCanTake(t, spec.LowCanTake),
		HighCostPerSec: optionalNumber(t, spec.HighCostPerSec),
		LowCostPerSec:  optionalNumber(t, spec.LowCostPerSec),
	})
	if result == nil {
		// TS `null`.
		return nil
	}
	out := assignOut{
		Tier:       make([]any, 0, result.Tier.Len()),
		StartMs:    make([]any, 0, result.StartMs.Len()),
		MakespanMs: result.MakespanMs,
	}
	for _, entry := range result.Tier.Entries() {
		out.Tier = append(out.Tier, []any{entry.Key, entry.Val})
	}
	for _, entry := range result.StartMs.Entries() {
		out.StartMs = append(out.StartMs, []any{entry.Key, entry.Val})
	}
	return out
}

func replayMean(t *testing.T, argsJSON string) any {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		t.Fatalf("decode args_json: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}
	var rawValues []json.RawMessage
	if err := json.Unmarshal(args[0], &rawValues); err != nil {
		t.Fatalf("decode values: %v", err)
	}
	values := make([]float64, 0, len(rawValues))
	for _, raw := range rawValues {
		values = append(values, specNumber(t, raw))
	}
	var got float64
	if string(args[1]) == "null" {
		got = RecencyWeightedMean(values)
	} else {
		got = RecencyWeightedMean(values, specNumber(t, args[1]))
	}
	return meanOut{Value: jscompat.JSNumber(got), Repr: jscompat.FormatNumber(got)}
}

func replayOrder(t *testing.T, argsJSON string) any {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		t.Fatalf("decode args_json: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}
	var candidates []orderCandidate
	if err := json.Unmarshal(args[0], &candidates); err != nil {
		t.Fatalf("decode candidates: %v", err)
	}
	var rawEntries [][]json.RawMessage
	if err := json.Unmarshal(args[1], &rawEntries); err != nil {
		t.Fatalf("decode start entries: %v", err)
	}
	startMs := jscompat.NewOrderedMap[string, jscompat.JSNumber]()
	for _, entry := range rawEntries {
		if len(entry) != 2 {
			t.Fatalf("expected [id, value] pair, got %d elements", len(entry))
		}
		var id string
		if err := json.Unmarshal(entry[0], &id); err != nil {
			t.Fatalf("decode start key: %v", err)
		}
		startMs.Set(id, jscompat.JSNumber(specNumber(t, entry[1])))
	}
	return OrderByHeftStart(candidates, startMs)
}
