package mergecoordinator

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type fixtureLine struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtures(t *testing.T) []fixtureLine {
	t.Helper()
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()

	out := []fixtureLine{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var fixture fixtureLine
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		out = append(out, fixture)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

func callFixture(t *testing.T, fixture fixtureLine) any {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
		t.Fatalf("decode args_json: %v", err)
	}
	switch fixture.Fn {
	case "computeMergeRisk":
		if len(args) != 2 {
			t.Fatalf("computeMergeRisk wants 2 replay args, got %d", len(args))
		}
		var stdout string
		var exitCode int
		if err := json.Unmarshal(args[0], &stdout); err != nil {
			t.Fatalf("decode stdout: %v", err)
		}
		if err := json.Unmarshal(args[1], &exitCode); err != nil {
			t.Fatalf("decode exit code: %v", err)
		}
		return jscompat.JSNumber(ComputeMergeRiskFromNumstat(stdout, exitCode))
	case "orderByMergeRisk":
		if len(args) != 1 {
			t.Fatalf("orderByMergeRisk wants 1 arg, got %d", len(args))
		}
		var items []RiskOrderItem
		if err := json.Unmarshal(args[0], &items); err != nil {
			t.Fatalf("decode order items: %v", err)
		}
		ordered := OrderByMergeRisk(items)
		ids := make([]string, 0, len(ordered))
		for _, item := range ordered {
			ids = append(ids, item.TaskID)
		}
		return ids
	default:
		t.Fatalf("unknown fixture fn %q", fixture.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 35 {
		t.Fatalf("expected broad fixture corpus, got %d", len(fixtures))
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Fn+"/"+fixture.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callFixture(t, fixture))
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(got) != fixture.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fixture.ArgsJSON, got, fixture.OutJSON)
			}
		})
	}
}

func TestFixtureCoverage(t *testing.T) {
	seen := map[string]int{}
	for _, fixture := range loadFixtures(t) {
		seen[fixture.Fn]++
	}
	for _, fn := range []string{"computeMergeRisk", "orderByMergeRisk"} {
		if seen[fn] == 0 {
			t.Errorf("no fixtures for %s", fn)
		}
	}
}
