package repomap

import (
	"bufio"
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

type graphSpec struct {
	Files      []string            `json:"files"`
	DefsByFile [][]json.RawMessage `json:"defsByFile"`
	Edges      [][]json.RawMessage `json:"edges"`
}

type optionsSpec struct {
	Personalization  [][]json.RawMessage `json:"personalization"`
	BudgetChars      *json.RawMessage    `json:"budgetChars"`
	FocusPaths       []string            `json:"focusPaths"`
	FocusIdentifiers []string            `json:"focusIdentifiers"`
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
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
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
	if len(raw) > 0 && raw[0] == '"' {
		var marker string
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
			t.Fatalf("unknown marker %q", marker)
		}
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func graphFromSpec(t *testing.T, spec graphSpec) SymbolGraph {
	t.Helper()
	graph := SymbolGraph{
		Files:      spec.Files,
		DefsByFile: jscompat.NewOrderedMap[string, []SymbolTag](),
		Edges:      jscompat.NewOrderedMap[string, *jscompat.OrderedMap[string, float64]](),
	}
	for _, pair := range spec.DefsByFile {
		var path string
		var defs []SymbolTag
		if err := json.Unmarshal(pair[0], &path); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(pair[1], &defs); err != nil {
			t.Fatal(err)
		}
		graph.DefsByFile.Set(path, defs)
	}
	for _, pair := range spec.Edges {
		var path string
		var entries [][]json.RawMessage
		if err := json.Unmarshal(pair[0], &path); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(pair[1], &entries); err != nil {
			t.Fatal(err)
		}
		record := jscompat.NewOrderedMap[string, float64]()
		for _, edge := range entries {
			var target string
			if err := json.Unmarshal(edge[0], &target); err != nil {
				t.Fatal(err)
			}
			record.Set(target, number(t, edge[1]))
		}
		graph.Edges.Set(path, record)
	}
	return graph
}

func rankOptions(t *testing.T, spec *optionsSpec) *RankFilesOptions {
	t.Helper()
	if spec == nil {
		return nil
	}
	weights := jscompat.NewOrderedMap[string, float64]()
	for _, pair := range spec.Personalization {
		var path string
		if err := json.Unmarshal(pair[0], &path); err != nil {
			t.Fatal(err)
		}
		weights.Set(path, number(t, pair[1]))
	}
	return &RankFilesOptions{Personalization: weights}
}

func buildOptions(t *testing.T, spec *optionsSpec) *BuildRepoMapOptions {
	t.Helper()
	if spec == nil {
		return nil
	}
	opts := &BuildRepoMapOptions{FocusPaths: spec.FocusPaths, FocusIdentifiers: spec.FocusIdentifiers}
	if spec.BudgetChars != nil {
		value := number(t, *spec.BudgetChars)
		opts.BudgetChars = &value
	}
	return opts
}

func callFixture(t *testing.T, fx fixture) any {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
		t.Fatal(err)
	}
	switch fx.Fn {
	case "consts":
		return struct {
			Damping         jscompat.JSNumber `json:"PAGERANK_DAMPING"`
			Tolerance       jscompat.JSNumber `json:"PAGERANK_TOLERANCE"`
			MaxIterations   jscompat.JSNumber `json:"PAGERANK_MAX_ITERATIONS"`
			DefaultBudget   jscompat.JSNumber `json:"DEFAULT_BUDGET_CHARS"`
			FocusPath       jscompat.JSNumber `json:"FOCUS_PATH_WEIGHT"`
			FocusIdentifier jscompat.JSNumber `json:"FOCUS_IDENTIFIER_WEIGHT"`
		}{
			jscompat.JSNumber(PAGERANK_DAMPING),
			jscompat.JSNumber(PAGERANK_TOLERANCE),
			jscompat.JSNumber(PAGERANK_MAX_ITERATIONS),
			jscompat.JSNumber(DEFAULT_BUDGET_CHARS),
			jscompat.JSNumber(FOCUS_PATH_WEIGHT),
			jscompat.JSNumber(FOCUS_IDENTIFIER_WEIGHT),
		}
	case "rankFiles":
		var graph graphSpec
		var opts *optionsSpec
		if err := json.Unmarshal(args[0], &graph); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &opts); err != nil {
			t.Fatal(err)
		}
		return RankFiles(graphFromSpec(t, graph), rankOptions(t, opts))
	case "renderRepoMap":
		var graph graphSpec
		var ranked []RankedFile
		var budget json.RawMessage
		if err := json.Unmarshal(args[0], &graph); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &ranked); err != nil {
			t.Fatal(err)
		}
		budget = args[2]
		return RenderRepoMap(graphFromSpec(t, graph), ranked, number(t, budget))
	case "buildRepoMap":
		var files []SourceFile
		var opts *optionsSpec
		var graph graphSpec
		if err := json.Unmarshal(args[0], &files); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &opts); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[2], &graph); err != nil {
			t.Fatal(err)
		}
		captured := graphFromSpec(t, graph)
		builder := SymbolGraphBuilderFunc(func(got []SourceFile) SymbolGraph {
			if len(got) != len(files) {
				t.Fatalf("builder got %d files, want %d", len(got), len(files))
			}
			return captured
		})
		return BuildRepoMap(files, buildOptions(t, opts), builder)
	default:
		t.Fatalf("unknown fn %q", fx.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 35 {
		t.Fatalf("expected at least 35 fixtures, got %d", len(fixtures))
	}
	seen := map[string]int{}
	for _, fx := range fixtures {
		seen[fx.Fn]++
		t.Run(fx.Fn+"/"+fx.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callFixture(t, fx))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != fx.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}
	for _, fn := range []string{"consts", "rankFiles", "renderRepoMap", "buildRepoMap"} {
		if seen[fn] == 0 {
			t.Errorf("no fixture cases for %s", fn)
		}
	}
}
