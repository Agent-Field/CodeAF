package overflow

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

type compactionSpec struct {
	Auto     *bool            `json:"auto"`
	Reserved *json.RawMessage `json:"reserved"`
}

type configSpec struct {
	Compaction *compactionSpec `json:"compaction"`
}

type limitSpec struct {
	Context json.RawMessage  `json:"context"`
	Input   *json.RawMessage `json:"input"`
	Output  json.RawMessage  `json:"output"`
}

type modelSpec struct {
	Limit limitSpec `json:"limit"`
}

type cacheSpec struct {
	Read  json.RawMessage `json:"read"`
	Write json.RawMessage `json:"write"`
}

type tokensSpec struct {
	Total     *json.RawMessage `json:"total"`
	Input     json.RawMessage  `json:"input"`
	Output    json.RawMessage  `json:"output"`
	Reasoning json.RawMessage  `json:"reasoning"`
	Cache     cacheSpec        `json:"cache"`
}

type inputSpec struct {
	Cfg    configSpec       `json:"cfg"`
	Model  modelSpec        `json:"model"`
	Tokens *tokensSpec      `json:"tokens"`
	Drift  *json.RawMessage `json:"drift"`
	Agent  *string          `json:"agent"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := []fixture{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for sc.Scan() {
		var fx fixture
		if err := json.Unmarshal(sc.Bytes(), &fx); err != nil {
			t.Fatal(err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func number(t *testing.T, raw json.RawMessage) float64 {
	t.Helper()
	var marker string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &marker); err != nil {
			t.Fatal(err)
		}
		switch marker {
		case "NaN":
			return math.NaN()
		case "Infinity":
			return math.Inf(1)
		case "-Infinity":
			return math.Inf(-1)
		case "-0":
			return math.Copysign(0, -1)
		default:
			t.Fatalf("unknown number marker %q", marker)
		}
	}
	var out float64
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func decodeInput(t *testing.T, raw json.RawMessage) inputSpec {
	t.Helper()
	var spec inputSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	return spec
}

func buildConfig(t *testing.T, spec configSpec) Config {
	t.Helper()
	cfg := Config{}
	if spec.Compaction != nil {
		cfg.Compaction = &CompactionConfig{Auto: spec.Compaction.Auto}
		if spec.Compaction.Reserved != nil {
			value := number(t, *spec.Compaction.Reserved)
			cfg.Compaction.Reserved = &value
		}
	}
	return cfg
}

func buildModel(t *testing.T, spec modelSpec) Model {
	t.Helper()
	model := Model{Limit: ModelLimit{
		Context: number(t, spec.Limit.Context),
		Output:  number(t, spec.Limit.Output),
	}}
	if spec.Limit.Input != nil {
		value := number(t, *spec.Limit.Input)
		model.Limit.Input = &value
	}
	return model
}

func buildTokens(t *testing.T, spec *tokensSpec) Tokens {
	t.Helper()
	if spec == nil {
		t.Fatal("tokens required")
	}
	tokens := Tokens{
		Input:     number(t, spec.Input),
		Output:    number(t, spec.Output),
		Reasoning: number(t, spec.Reasoning),
		Cache: TokenCache{
			Read: number(t, spec.Cache.Read), Write: number(t, spec.Cache.Write),
		},
	}
	if spec.Total != nil {
		value := number(t, *spec.Total)
		tokens.Total = &value
	}
	return tokens
}

func callFixture(t *testing.T, fx fixture) any {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
		t.Fatal(err)
	}
	spec := decodeInput(t, args[0])
	var env map[string]string
	if err := json.Unmarshal(args[1], &env); err != nil {
		t.Fatal(err)
	}
	restore := SetEnvForTesting(env)
	defer restore()
	cfg, model := buildConfig(t, spec.Cfg), buildModel(t, spec.Model)
	switch fx.Fn {
	case "usable":
		return jscompat.JSNumber(Usable(UsableInput{Cfg: cfg, Model: model}))
	case "shouldScanDrift":
		return ShouldScanDrift(ScanDriftInput{Cfg: cfg, Model: model, Tokens: buildTokens(t, spec.Tokens)})
	case "isOverflow":
		input := OverflowInput{Cfg: cfg, Model: model, Tokens: buildTokens(t, spec.Tokens), Agent: spec.Agent}
		if spec.Drift != nil {
			value := number(t, *spec.Drift)
			input.Drift = &value
		}
		return IsOverflow(input)
	default:
		t.Fatalf("unknown fn %q", fx.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 80 {
		t.Fatalf("expected at least 80 fixtures, got %d", len(fixtures))
	}
	seen := map[string]int{}
	for _, fx := range fixtures {
		seen[fx.Fn]++
		t.Run(fx.Fn+"/"+fx.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callFixture(t, fx))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, []byte(fx.OutJSON)) {
				t.Errorf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}
	for _, fn := range []string{"usable", "shouldScanDrift", "isOverflow"} {
		if seen[fn] == 0 {
			t.Errorf("no fixture cases for %s", fn)
		}
	}
}
