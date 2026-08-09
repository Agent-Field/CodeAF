package knobs

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Replay of testdata/fixtures.json, generated from the real TS module by
// tools/fixtures/gen-knobs.ts. The gate is byte-for-byte equality between
// jscompat.Stringify(goResult) and the recorded JSON.stringify output.
//
// Wire conventions (mirrored in the generator):
//   - out_json is JSON.stringify(result ?? null), so knobDef's `undefined`
//     miss arrives as JSON null;
//   - JSON has no NaN/Infinity/-0 literal, so any number inside an argument
//     object may be the sentinel {"$num":"NaN"|"Infinity"|"-Infinity"|"-0"};
//     sentinels are recognised only at the top level of resolveKnobs'
//     fileOverrides and of knobsSnapshotHash's values map;
//   - an omitted optional argument is encoded by a shorter args array, which
//     is how the TS default-parameter paths (resolveKnobs(), resolveKnobs(f),
//     knobsSnapshotHash()) are exercised distinctly from explicit {};
//   - a filesystem case carries {"mode":…, "knobsJson":…} and the identical
//     tree is rebuilt here under t.TempDir().

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

type dirSpec struct {
	Mode      string  `json:"mode"`
	KnobsJSON *string `json:"knobsJson"`
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
	sc.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var fx fixture
		if err := json.Unmarshal([]byte(line), &fx); err != nil {
			t.Fatalf("bad fixture line: %v", err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

func splitArgs(t *testing.T, argsJSON string) []json.RawMessage {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		t.Fatalf("bad args_json %q: %v", argsJSON, err)
	}
	return args
}

var numSentinel = map[string]float64{
	"NaN":       math.NaN(),
	"Infinity":  math.Inf(1),
	"-Infinity": math.Inf(-1),
	"-0":        math.Copysign(0, -1),
}

// reviveSentinel maps {"$num":"NaN"} to the real float, one level deep.
func reviveSentinel(v any) (float64, bool) {
	rec, ok := v.(*Record[any])
	if !ok || rec.Len() != 1 {
		return 0, false
	}
	tag, ok := rec.Get("$num")
	if !ok {
		return 0, false
	}
	s, ok := tag.(string)
	if !ok {
		return 0, false
	}
	f, ok := numSentinel[s]
	return f, ok
}

func decodeObject(t *testing.T, raw json.RawMessage) *Record[any] {
	t.Helper()
	parsed, ok := parseJSON(string(raw))
	if !ok {
		t.Fatalf("bad object arg %q", string(raw))
	}
	obj, ok := parsed.(*Record[any])
	if !ok {
		t.Fatalf("arg is not an object: %q", string(raw))
	}
	out := NewRecord[any]()
	for _, k := range obj.Keys() {
		v, _ := obj.Get(k)
		if f, ok := reviveSentinel(v); ok {
			out.Set(k, f)
		} else {
			out.Set(k, v)
		}
	}
	return out
}

func decodeValues(t *testing.T, raw json.RawMessage) *Record[float64] {
	t.Helper()
	obj := decodeObject(t, raw)
	out := NewRecord[float64]()
	for _, k := range obj.Keys() {
		v, _ := obj.Get(k)
		f, ok := v.(float64)
		if !ok {
			t.Fatalf("values map entry %q is not a number: %#v", k, v)
		}
		out.Set(k, f)
	}
	return out
}

func decodeEnv(t *testing.T, raw json.RawMessage) map[string]string {
	t.Helper()
	var env map[string]string
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("bad env arg %q: %v", string(raw), err)
	}
	return env
}

// makeProject rebuilds the temp tree the generator built for a filesystem case.
func makeProject(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var spec dirSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("bad dir spec %q: %v", string(raw), err)
	}
	dir := t.TempDir()
	if spec.Mode == "absent" {
		return dir
	}
	if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	switch spec.Mode {
	case "emptyDir":
		return dir
	case "dirAsFile":
		if err := os.MkdirAll(filepath.Join(dir, ".codeaf", "knobs.json"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		return dir
	}
	body := ""
	if spec.KnobsJSON != nil {
		body = *spec.KnobsJSON
	}
	if err := os.WriteFile(filepath.Join(dir, ".codeaf", "knobs.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write knobs.json: %v", err)
	}
	return dir
}

// clearAmbientKnobEnv neutralises any CODEAF_KNOB_* variable inherited from the
// developer's shell. TS treats an unset variable and an empty string
// identically (both fail parseNumeric), so setting to "" is a faithful unset.
func clearAmbientKnobEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 && strings.HasPrefix(kv, "CODEAF_KNOB_") {
			t.Setenv(kv[:i], "")
		}
	}
}

func runFixture(t *testing.T, fx fixture) any {
	t.Helper()
	args := splitArgs(t, fx.ArgsJSON)
	switch fx.Fn {
	case "KNOB_REGISTRY":
		return KNOB_REGISTRY
	case "knobDef":
		var name string
		if err := json.Unmarshal(args[0], &name); err != nil {
			t.Fatalf("bad name arg: %v", err)
		}
		return KnobDefFor(name)
	case "defaultKnobValues":
		return DefaultKnobValues()
	case "resolveKnobs":
		switch len(args) {
		case 0:
			return ResolveKnobs(nil, nil)
		case 1:
			return ResolveKnobs(decodeObject(t, args[0]), nil)
		default:
			return ResolveKnobs(decodeObject(t, args[0]), decodeEnv(t, args[1]))
		}
	case "knobsSnapshotHash":
		if len(args) == 0 {
			return KnobsSnapshotHash(nil)
		}
		return KnobsSnapshotHash(decodeValues(t, args[0]))
	case "loadKnobFile":
		return LoadKnobFile(makeProject(t, args[0]))
	case "resolveProjectKnobs":
		return ResolveProjectKnobs(makeProject(t, args[0]), decodeEnv(t, args[1]))
	case "resolveProjectKnobs_defaultEnv":
		root := makeProject(t, args[0])
		clearAmbientKnobEnv(t)
		for k, v := range decodeEnv(t, args[1]) {
			t.Setenv(k, v)
		}
		return ResolveProjectKnobs(root, nil)
	}
	t.Fatalf("unknown fn %q", fx.Fn)
	return nil
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(fixtures))
	}
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			got := runFixture(t, fx)
			enc, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(enc) != fx.OutJSON {
				t.Fatalf("fn=%s args=%s\n got: %s\nwant: %s", fx.Fn, fx.ArgsJSON, enc, fx.OutJSON)
			}
		})
	}
}
