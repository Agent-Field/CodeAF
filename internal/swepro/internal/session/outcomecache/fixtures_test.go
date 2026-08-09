package outcomecache

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafoutcome"
)

// Fixture replay: every line of testdata/fixtures.json was produced by the real
// src/session/outcome-cache.ts under bun (see tools/fixtures/gen-outcomecache.ts).
// The gate is byte-for-byte equality between jscompat.Stringify(goResult) and
// the JSON.stringify output V8 recorded — with NO normalization of any kind,
// including U+2028/U+2029 and unpaired surrogates, which the package's own
// serializer emits V8-identically.
//
// Three of the seven exports are impure (fs), so a "result" is the scenario
// transcript: the value the export returned plus the sidecar's final bytes. The
// runners below mirror the generator's runners step for step.

type fixtureCase struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

// ── fixture-side scalar types ────────────────────────────────────────────

// jsStr survives the round trip through Go's JSON decoder without losing the
// unpaired surrogates the generator can emit: it decodes with the package's own
// JSON.parse-equivalent (WTF-8 preserving) and re-encodes with its
// JSON.stringify-equivalent quoter.
type jsStr string

func (s jsStr) MarshalJSON() ([]byte, error) { return appendJSQuoted(nil, string(s)), nil }

func (s *jsStr) UnmarshalJSON(b []byte) error {
	v, ok := parseJSON(string(b))
	if !ok || v.kind != jsString {
		return fmt.Errorf("jsStr: not a JSON string: %s", b)
	}
	*s = jsStr(v.str)
	return nil
}

// jsNum decodes the generator's number encoding: plain JSON numbers, plus the
// "@@num:" tokens standing in for the four values JSON cannot carry.
type jsNum float64

func (n *jsNum) UnmarshalJSON(b []byte) error {
	s := string(b)
	if len(s) > 0 && s[0] == '"' {
		var tok string
		if err := json.Unmarshal(b, &tok); err != nil {
			return err
		}
		switch tok {
		case "@@num:NaN":
			*n = jsNum(math.NaN())
		case "@@num:Infinity":
			*n = jsNum(math.Inf(1))
		case "@@num:-Infinity":
			*n = jsNum(math.Inf(-1))
		case "@@num:-0":
			*n = jsNum(math.Copysign(0, -1))
		default:
			return fmt.Errorf("unknown number token %q", tok)
		}
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*n = jsNum(f)
	return nil
}

// pair is one [key, value] entry of a Map serialized as [...map.entries()].
type pair [2]jsStr

// ── fixture-side LeafOutcome mirror ──────────────────────────────────────

type fxModel struct {
	ProviderID jsStr `json:"providerID"`
	ModelID    jsStr `json:"modelID"`
}

type fxEvidence struct {
	AuditCommandsRun jsNum `json:"auditCommandsRun"`
	AuditBlockers    jsNum `json:"auditBlockers"`
	InRunTestsPassed bool  `json:"inRunTestsPassed"`
}

type fxOutcome struct {
	TaskID        jsStr       `json:"taskID"`
	Model         fxModel     `json:"model"`
	SizeBand      jsStr       `json:"sizeBand"`
	Verdict       jsStr       `json:"verdict"`
	RepairRounds  jsNum       `json:"repairRounds"`
	Turns         jsNum       `json:"turns"`
	ToolErrors    jsNum       `json:"toolErrors"`
	CostUsd       jsNum       `json:"costUsd"`
	WallMs        jsNum       `json:"wallMs"`
	MergeConflict bool        `json:"mergeConflict"`
	Timestamp     jsNum       `json:"timestamp"`
	Evidence      *fxEvidence `json:"evidence"`
}

func (o fxOutcome) to() LeafOutcome {
	out := LeafOutcome{
		TaskID:        string(o.TaskID),
		Model:         LeafModel{ProviderID: string(o.Model.ProviderID), ModelID: string(o.Model.ModelID)},
		SizeBand:      leafoutcome.SizeBand(o.SizeBand),
		Verdict:       leafoutcome.LeafVerdict(o.Verdict),
		RepairRounds:  jscompat.JSNumber(o.RepairRounds),
		Turns:         jscompat.JSNumber(o.Turns),
		ToolErrors:    jscompat.JSNumber(o.ToolErrors),
		CostUsd:       jscompat.JSNumber(o.CostUsd),
		WallMs:        jscompat.JSNumber(o.WallMs),
		MergeConflict: o.MergeConflict,
		Timestamp:     jscompat.JSNumber(o.Timestamp),
	}
	if o.Evidence != nil {
		out.Evidence = &LeafEvidence{
			AuditCommandsRun: jscompat.JSNumber(o.Evidence.AuditCommandsRun),
			AuditBlockers:    jscompat.JSNumber(o.Evidence.AuditBlockers),
			InRunTestsPassed: o.Evidence.InRunTestsPassed,
		}
	}
	return out
}

// ── workspace plants ─────────────────────────────────────────────────────

type fxPlant struct {
	Kind    string `json:"kind"`
	Content jsStr  `json:"content"`
	B64     string `json:"b64"`
}

func (p fxPlant) apply(t *testing.T, ws string) {
	t.Helper()
	file := filepath.Join(ws, cacheFile)
	switch p.Kind {
	case "none":
	case "file":
		mustMkdirAll(t, filepath.Dir(file))
		mustWrite(t, file, []byte(toUTF8(string(p.Content))))
	case "bytes":
		raw, err := base64.StdEncoding.DecodeString(p.B64)
		if err != nil {
			t.Fatalf("bad b64 plant: %v", err)
		}
		mustMkdirAll(t, filepath.Dir(file))
		mustWrite(t, file, raw)
	case "dirAtFile":
		mustMkdirAll(t, file)
	case "fileAtDir":
		mustWrite(t, filepath.Join(ws, ".codeaf"), []byte("not a directory"))
	default:
		t.Fatalf("unknown plant kind %q", p.Kind)
	}
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func mustWrite(t *testing.T, file string, data []byte) {
	t.Helper()
	if err := os.WriteFile(file, data, 0o666); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
}

// readSidecar mirrors the generator's readSidecar: the file's bytes decoded as
// utf8, or null when it cannot be read.
func readSidecar(ws string) *jsStr {
	data, err := os.ReadFile(filepath.Join(ws, cacheFile))
	if err != nil {
		return nil
	}
	s := jsStr(decodeUTF8Lossy(data))
	return &s
}

func entriesOf(m *jscompat.OrderedMap[string, string]) []pair {
	out := make([]pair, 0, m.Len())
	for _, e := range m.Entries() {
		out = append(out, pair{jsStr(e.Key), jsStr(e.Val)})
	}
	return out
}

// ── per-fn scenario shapes ───────────────────────────────────────────────

type fxKeyOpts struct {
	BriefText jsStr `json:"briefText"`
	TreeHash  jsStr `json:"treeHash"`
	ModelID   jsStr `json:"modelID"`
}

type fxExecStep struct {
	Stdout   jsStr   `json:"stdout"`
	ExitCode jsNum   `json:"exitCode"`
	Throw    *string `json:"throw"`
}

type fxTreeOut struct {
	Result   *TreeState `json:"result"`
	Commands [][]jsStr  `json:"commands"`
}

type fxCacheOp struct {
	Op      string    `json:"op"`
	Key     jsStr     `json:"key"`
	Outcome fxOutcome `json:"outcome"`
	Dirty   bool      `json:"dirty"`
}

type fxCacheSpec struct {
	WithStore bool        `json:"withStore"`
	Seed      []pair      `json:"seed"`
	Ops       []fxCacheOp `json:"ops"`
}

type fxCacheOut struct {
	Results []any   `json:"results"`
	Store   *[]pair `json:"store"`
}

type fxPersistOut struct {
	File *jsStr `json:"file"`
}

type fxTreeState struct {
	TreeHash jsStr `json:"treeHash"`
	Dirty    bool  `json:"dirty"`
}

func (s fxTreeState) to() TreeState { return TreeState{TreeHash: string(s.TreeHash), Dirty: s.Dirty} }

type fxLookup struct {
	BriefText jsStr       `json:"briefText"`
	TreeState fxTreeState `json:"treeState"`
	ModelID   jsStr       `json:"modelID"`
}

type fxRecordEntry struct {
	BriefText jsStr       `json:"briefText"`
	TreeState fxTreeState `json:"treeState"`
	ModelID   jsStr       `json:"modelID"`
	Outcome   fxOutcome   `json:"outcome"`
}

type fxRecordOut struct {
	Result PutResult `json:"result"`
	File   *jsStr    `json:"file"`
}

// ── the replay ───────────────────────────────────────────────────────────

func decodeArgs(t *testing.T, fc fixtureCase, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(fc.ArgsJSON), into); err != nil {
		t.Fatalf("decode args for %s: %v\n%s", fc.Name, err, fc.ArgsJSON)
	}
}

func TestFixtures(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "fixtures.json"))
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(lines))
	}

	for _, line := range lines {
		var fc fixtureCase
		if err := json.Unmarshal([]byte(line), &fc); err != nil {
			t.Fatalf("decode fixture line: %v\n%s", err, line)
		}
		t.Run(fc.Name, func(t *testing.T) {
			got := runFixture(t, fc)
			encoded, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatalf("stringify result: %v", err)
			}
			if string(encoded) != fc.OutJSON {
				t.Errorf("fn=%s\n args: %s\n  got: %s\n want: %s", fc.Fn, fc.ArgsJSON, encoded, fc.OutJSON)
			}
		})
	}
}

func runFixture(t *testing.T, fc fixtureCase) any {
	t.Helper()
	switch fc.Fn {

	case "outcomeCacheKey":
		var args []fxKeyOpts
		decodeArgs(t, fc, &args)
		return jsStr(OutcomeCacheKey(OutcomeCacheKeyOpts{
			BriefText: string(args[0].BriefText),
			TreeHash:  string(args[0].TreeHash),
			ModelID:   string(args[0].ModelID),
		}))

	case "computeTreeState":
		var args [][]fxExecStep
		decodeArgs(t, fc, &args)
		steps := args[0]
		var commands [][]jsStr
		i := 0
		result := ComputeTreeState(func(cmd []string) (ExecResult, error) {
			row := make([]jsStr, 0, len(cmd))
			for _, c := range cmd {
				row = append(row, jsStr(c))
			}
			commands = append(commands, row)
			if i >= len(steps) {
				i++
				return ExecResult{}, fmt.Errorf("unscripted exec call")
			}
			step := steps[i]
			i++
			if step.Throw != nil {
				return ExecResult{}, fmt.Errorf("%s", *step.Throw)
			}
			return ExecResult{Stdout: string(step.Stdout), ExitCode: float64(step.ExitCode)}, nil
		})
		return fxTreeOut{Result: result, Commands: commands}

	case "createOutcomeCache":
		var args []fxCacheSpec
		decodeArgs(t, fc, &args)
		spec := args[0]
		var store *jscompat.OrderedMap[string, string]
		if spec.WithStore {
			store = jscompat.NewOrderedMap[string, string]()
			for _, e := range spec.Seed {
				store.Set(string(e[0]), string(e[1]))
			}
		}
		cache := CreateOutcomeCache(store)
		results := make([]any, 0, len(spec.Ops))
		for _, op := range spec.Ops {
			if op.Op == "get" {
				hit := cache.Get(string(op.Key))
				if hit == nil {
					results = append(results, nil)
				} else {
					results = append(results, hit)
				}
				continue
			}
			results = append(results, cache.Put(PutEntry{
				Key:     string(op.Key),
				Outcome: op.Outcome.to(),
				Dirty:   op.Dirty,
			}))
		}
		out := fxCacheOut{Results: results}
		if store != nil {
			entries := entriesOf(store)
			out.Store = &entries
		}
		return out

	case "loadOutcomeCacheStore":
		var args []struct {
			Plant fxPlant `json:"plant"`
		}
		decodeArgs(t, fc, &args)
		ws := t.TempDir()
		args[0].Plant.apply(t, ws)
		return entriesOf(LoadOutcomeCacheStore(ws))

	case "persistOutcomeCacheStore":
		var args []struct {
			Plant   fxPlant `json:"plant"`
			Entries []pair  `json:"entries"`
		}
		decodeArgs(t, fc, &args)
		ws := t.TempDir()
		args[0].Plant.apply(t, ws)
		store := jscompat.NewOrderedMap[string, string]()
		for _, e := range args[0].Entries {
			store.Set(string(e[0]), string(e[1]))
		}
		PersistOutcomeCacheStore(ws, store)
		return fxPersistOut{File: readSidecar(ws)}

	case "lookupVerifiedOutcome":
		var args []struct {
			Plant  fxPlant  `json:"plant"`
			Lookup fxLookup `json:"lookup"`
		}
		decodeArgs(t, fc, &args)
		ws := t.TempDir()
		args[0].Plant.apply(t, ws)
		hit := LookupVerifiedOutcome(ws, LookupOpts{
			BriefText: string(args[0].Lookup.BriefText),
			TreeState: args[0].Lookup.TreeState.to(),
			ModelID:   string(args[0].Lookup.ModelID),
		})
		if hit == nil {
			return nil
		}
		return hit

	case "recordVerifiedOutcome":
		var args []struct {
			Plant fxPlant       `json:"plant"`
			Entry fxRecordEntry `json:"entry"`
		}
		decodeArgs(t, fc, &args)
		ws := t.TempDir()
		args[0].Plant.apply(t, ws)
		result := RecordVerifiedOutcome(ws, RecordOpts{
			BriefText: string(args[0].Entry.BriefText),
			TreeState: args[0].Entry.TreeState.to(),
			ModelID:   string(args[0].Entry.ModelID),
			Outcome:   args[0].Entry.Outcome.to(),
		})
		return fxRecordOut{Result: result, File: readSidecar(ws)}
	}

	t.Fatalf("unknown fn %q", fc.Fn)
	return nil
}
