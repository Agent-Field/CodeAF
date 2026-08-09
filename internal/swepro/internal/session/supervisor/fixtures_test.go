package supervisor

import (
	"bufio"
	"encoding/json"
	"fmt"
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

func replayFixture(fx fixture) (any, error) {
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
		return nil, err
	}
	switch fx.Fn {
	case "ratchetMetric":
		var snapshot RatchetSnapshot
		if err := json.Unmarshal(args[0], &snapshot); err != nil {
			return nil, err
		}
		return jscompat.JSNumber(RatchetMetric(snapshot)), nil
	case "snapshotIndicatesPass":
		var snapshot RatchetSnapshot
		if err := json.Unmarshal(args[0], &snapshot); err != nil {
			return nil, err
		}
		return SnapshotIndicatesPass(snapshot), nil
	case "autoResumeEnabled":
		var env map[string]string
		if err := json.Unmarshal(args[0], &env); err != nil {
			return nil, err
		}
		return AutoResumeEnabled(env), nil
	case "runSupervisor":
		var input struct {
			Snapshots []RatchetSnapshot `json:"snapshots"`
			Budgets   []struct {
				Yes    bool    `json:"yes"`
				Reason *string `json:"reason"`
			} `json:"budgets"`
			Opts *SupervisorOptions `json:"opts"`
		}
		if err := json.Unmarshal(args[0], &input); err != nil {
			return nil, err
		}
		phase, budgetCall := 0, 0
		resumes := []int{}
		logs := []string{}
		result, err := RunSupervisor(SupervisorDeps{
			ResumeOnce: func(attempt int) error {
				resumes = append(resumes, attempt)
				phase++
				return nil
			},
			Snapshot: func() (RatchetSnapshot, error) {
				index := phase
				if index >= len(input.Snapshots) {
					index = len(input.Snapshots) - 1
				}
				return input.Snapshots[index], nil
			},
			BudgetExhausted: func() BudgetExhaustion {
				if len(input.Budgets) == 0 {
					return BudgetExhaustion{}
				}
				index := budgetCall
				if index >= len(input.Budgets) {
					index = len(input.Budgets) - 1
				}
				budgetCall++
				return BudgetExhaustion{
					Yes: input.Budgets[index].Yes, Reason: input.Budgets[index].Reason,
				}
			},
			Log: func(message string) { logs = append(logs, message) },
		}, input.Opts)
		if err != nil {
			return nil, err
		}
		return struct {
			Result  SupervisorResult `json:"result"`
			Resumes []int            `json:"resumes"`
			Logs    []string         `json:"logs"`
		}{result, resumes, logs}, nil
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
	for _, name := range []string{
		"ratchetMetric", "snapshotIndicatesPass", "autoResumeEnabled", "runSupervisor",
	} {
		if seen[name] == 0 {
			t.Errorf("no fixtures for %s", name)
		}
	}
}
