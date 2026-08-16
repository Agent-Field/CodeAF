package stalereaper

import (
	"bufio"
	"encoding/json"
	"fmt"
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

func fixtureLines(t *testing.T) []fixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	out := []fixture{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var fx fixture
		if err := json.Unmarshal(scanner.Bytes(), &fx); err != nil {
			t.Fatal(err)
		}
		out = append(out, fx)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func optionalNumber(value float64, ok bool) *jscompat.JSNumber {
	if !ok {
		return nil
	}
	number := jscompat.JSNumber(value)
	return &number
}

func replayFixture(fx fixture) (any, error) {
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
		return nil, err
	}
	switch fx.Fn {
	case "planStampToMs":
		var value any
		if err := json.Unmarshal(args[0], &value); err != nil {
			return nil, err
		}
		return optionalNumber(PlanStampToMS(value)), nil
	case "planStampToMsSpecial":
		var sentinel struct {
			Num string `json:"$num"`
		}
		if err := json.Unmarshal(args[0], &sentinel); err != nil {
			return nil, err
		}
		value := math.NaN()
		if sentinel.Num == "positive-infinity" {
			value = math.Inf(1)
		} else if sentinel.Num == "negative-infinity" {
			value = math.Inf(-1)
		}
		return optionalNumber(PlanStampToMS(value)), nil
	case "lastActivityMs":
		var row ReapTaskRow
		if err := json.Unmarshal(args[0], &row); err != nil {
			return nil, err
		}
		return optionalNumber(LastActivityMS(&row)), nil
	case "selectStaleActiveTasks":
		var input struct {
			Tasks       []*ReapTaskRow `json:"tasks"`
			NowMS       float64        `json:"nowMs"`
			ThresholdMS *float64       `json:"thresholdMs"`
			LiveTaskIDs []string       `json:"liveTaskIDs"`
		}
		if err := json.Unmarshal(args[0], &input); err != nil {
			return nil, err
		}
		live := map[string]struct{}(nil)
		if input.LiveTaskIDs != nil {
			live = map[string]struct{}{}
			for _, id := range input.LiveTaskIDs {
				live[id] = struct{}{}
			}
		}
		return SelectStaleActiveTasks(StaleReapInput{
			Tasks: input.Tasks, NowMS: input.NowMS,
			ThresholdMS: input.ThresholdMS, LiveTaskIDs: live,
		}), nil
	case "legacyReapSelection":
		if len(args) != 3 {
			return nil, fmt.Errorf("legacy fixture has %d args", len(args))
		}
		var tasks []*LegacyReapTaskRow
		var nowMS, threshold float64
		if err := json.Unmarshal(args[0], &tasks); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(args[1], &nowMS); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(args[2], &threshold); err != nil {
			return nil, err
		}
		return LegacyReapSelection(tasks, nowMS, threshold), nil
	default:
		return nil, fmt.Errorf("unknown fixture function %q", fx.Fn)
	}
}

func TestTypeScriptFixtures(t *testing.T) {
	seen := map[string]int{}
	for _, fx := range fixtureLines(t) {
		t.Run(fx.Name, func(t *testing.T) {
			got, err := replayFixture(fx)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != fx.OutJSON {
				t.Fatalf("got  %s\nwant %s", encoded, fx.OutJSON)
			}
			seen[fx.Fn]++
		})
	}
	for _, name := range []string{
		"planStampToMs", "planStampToMsSpecial", "lastActivityMs",
		"selectStaleActiveTasks", "legacyReapSelection",
	} {
		if seen[name] == 0 {
			t.Errorf("no fixtures for %s", name)
		}
	}
}
