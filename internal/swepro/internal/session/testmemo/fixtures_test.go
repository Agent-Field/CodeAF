package testmemo

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()
	out := []fixture{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var fx fixture
		if err := json.Unmarshal(scanner.Bytes(), &fx); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		out = append(out, fx)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

type memoAction struct {
	Op    string            `json:"op"`
	Key   string            `json:"key"`
	Value *CachedTestResult `json:"value"`
}

type memoEvent struct {
	Op    string            `json:"op"`
	Value *CachedTestResult `json:"-"`
	Size  int               `json:"size"`
}

func (event memoEvent) MarshalJSON() ([]byte, error) {
	if event.Op == "set" {
		return jscompat.Stringify(struct {
			Op   string `json:"op"`
			Size int    `json:"size"`
		}{Op: event.Op, Size: event.Size})
	}
	return jscompat.Stringify(struct {
		Op    string            `json:"op"`
		Value *CachedTestResult `json:"value"`
		Size  int               `json:"size"`
	}{Op: event.Op, Value: event.Value, Size: event.Size})
}

type memoTranscript struct {
	Events []memoEvent `json:"events"`
	Size   int         `json:"size"`
}

func memoCapacity(mode string) []float64 {
	switch mode {
	case "default":
		return nil
	case "zero":
		return []float64{0}
	case "one":
		return []float64{1}
	case "fractional":
		return []float64{1.5}
	case "negative":
		return []float64{-1}
	case "nan":
		return []float64{math.NaN()}
	case "infinity":
		return []float64{math.Inf(1)}
	default:
		panic("unknown cap mode " + mode)
	}
}

func runMemo(mode string, actions []memoAction) memoTranscript {
	memo := NewTestCommandMemo(memoCapacity(mode)...)
	events := []memoEvent{}
	for _, action := range actions {
		if action.Op == "set" {
			memo.Set(action.Key, action.Value)
			events = append(events, memoEvent{Op: "set", Size: memo.Size()})
		} else {
			events = append(events, memoEvent{Op: "get", Value: memo.Get(action.Key), Size: memo.Size()})
		}
	}
	return memoTranscript{Events: events, Size: memo.Size()}
}

func callFixture(t *testing.T, fx fixture) any {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	switch fx.Fn {
	case "isTestCommand":
		var command string
		if err := json.Unmarshal(args[0], &command); err != nil {
			t.Fatal(err)
		}
		return IsTestCommand(command)
	case "latestTestCommandPassed":
		var messages []any
		if err := json.Unmarshal(args[0], &messages); err != nil {
			t.Fatal(err)
		}
		return LatestTestCommandPassed(messages)
	case "normalizeTestCommand":
		var command string
		if err := json.Unmarshal(args[0], &command); err != nil {
			t.Fatal(err)
		}
		return NormalizeTestCommand(command)
	case "buildTestMemoKey":
		var input BuildTestMemoKeyInput
		if err := json.Unmarshal(args[0], &input); err != nil {
			t.Fatal(err)
		}
		return BuildTestMemoKey(input)
	case "testCommandMemo":
		var mode string
		var actions []memoAction
		if err := json.Unmarshal(args[0], &mode); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &actions); err != nil {
			t.Fatal(err)
		}
		return runMemo(mode, actions)
	default:
		t.Fatalf("unknown function %q", fx.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 70 {
		t.Fatalf("expected at least 70 fixtures, got %d", len(fixtures))
	}
	for _, fx := range fixtures {
		t.Run(fx.Fn+"/"+fx.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callFixture(t, fx))
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(got) != fx.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}
}

func TestFixtureCoverage(t *testing.T) {
	seen := map[string]int{}
	for _, fx := range loadFixtures(t) {
		seen[fx.Fn]++
	}
	for _, name := range []string{
		"isTestCommand",
		"latestTestCommandPassed",
		"normalizeTestCommand",
		"buildTestMemoKey",
		"testCommandMemo",
	} {
		if seen[name] == 0 {
			t.Errorf("no fixtures for %s", name)
		}
	}
}
