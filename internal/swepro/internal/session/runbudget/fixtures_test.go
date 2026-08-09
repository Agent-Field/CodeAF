package runbudget

import (
	"bufio"
	"encoding/json"
	"fmt"
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

func parseBudget(raw json.RawMessage) (RunBudget, error) {
	var budget RunBudget
	if err := json.Unmarshal(raw, &budget); err != nil {
		return RunBudget{}, err
	}
	return budget, nil
}

func replayFixture(fx fixture) (any, error) {
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
		return nil, err
	}
	switch fx.Fn {
	case "resolveRunBudget":
		var flags RunBudgetFlags
		var env map[string]string
		if err := json.Unmarshal(args[0], &flags); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(args[1], &env); err != nil {
			return nil, err
		}
		return ResolveRunBudget(&flags, env), nil
	case "isBounded":
		budget, err := parseBudget(args[0])
		if err != nil {
			return nil, err
		}
		return IsBounded(budget), nil
	case "trackerScenario":
		var input struct {
			Budget       json.RawMessage `json:"budget"`
			StartTS      float64         `json:"startTs"`
			PriorCostUSD *float64        `json:"priorCostUsd"`
			Ops          []struct {
				Op    string  `json:"op"`
				USD   float64 `json:"usd"`
				NowMS float64 `json:"nowMs"`
			} `json:"ops"`
		}
		if err := json.Unmarshal(args[0], &input); err != nil {
			return nil, err
		}
		budget, err := parseBudget(input.Budget)
		if err != nil {
			return nil, err
		}
		var tracker *BudgetTracker
		if input.PriorCostUSD == nil {
			tracker = MakeBudgetTracker(budget, input.StartTS)
		} else {
			tracker = MakeBudgetTracker(budget, input.StartTS, *input.PriorCostUSD)
		}
		results := []any{}
		for _, op := range input.Ops {
			switch op.Op {
			case "add":
				tracker.AddCost(op.USD)
				results = append(results, nil)
			case "cost":
				results = append(results, jscompat.JSNumber(tracker.CostUSD()))
			case "exhausted":
				results = append(results, tracker.Exhausted(op.NowMS))
			default:
				return nil, fmt.Errorf("unknown tracker op %q", op.Op)
			}
		}
		return results, nil
	default:
		return nil, fmt.Errorf("unknown function %q", fx.Fn)
	}
}

func TestTypeScriptFixtures(t *testing.T) {
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	seen := map[string]int{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var fx fixture
		if err := json.Unmarshal(scanner.Bytes(), &fx); err != nil {
			t.Fatal(err)
		}
		got, err := replayFixture(fx)
		if err != nil {
			t.Fatalf("%s: %v", fx.Name, err)
		}
		encoded, err := jscompat.Stringify(got)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != fx.OutJSON {
			t.Errorf("%s:\ngot  %s\nwant %s", fx.Name, encoded, fx.OutJSON)
		}
		seen[fx.Fn]++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"resolveRunBudget", "isBounded", "trackerScenario"} {
		if seen[name] == 0 {
			t.Errorf("no fixtures for %s", name)
		}
	}
}

func TestTrackerIgnoresNonFiniteDeltas(t *testing.T) {
	tracker := MakeBudgetTracker(RunBudget{}, 0)
	tracker.AddCost(math.NaN())
	tracker.AddCost(math.Inf(1))
	tracker.AddCost(math.Inf(-1))
	if got := tracker.CostUSD(); got != 0 {
		t.Fatalf("cost = %v, want 0", got)
	}
}
