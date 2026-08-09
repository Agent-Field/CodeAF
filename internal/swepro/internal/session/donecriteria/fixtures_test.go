package donecriteria

import (
	"bufio"
	"encoding/json"
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

func callFixture(t *testing.T, fx fixture) any {
	t.Helper()
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &raw); err != nil {
		t.Fatal(err)
	}
	switch fx.Fn {
	case "parseDoneCriteria":
		var input any
		if err := json.Unmarshal(raw[0], &input); err != nil {
			t.Fatal(err)
		}
		return ParseDoneCriteria(input)
	case "checkDone":
		var criteria []DoneCriterion
		if err := json.Unmarshal(raw[0], &criteria); err != nil {
			t.Fatal(err)
		}
		var evidence *DoneEvidence
		if err := json.Unmarshal(raw[1], &evidence); err != nil {
			t.Fatal(err)
		}
		return CheckDone(criteria, evidence)
	case "buildDoneCriteriaPrompt":
		var criteria []DoneCriterion
		if err := json.Unmarshal(raw[0], &criteria); err != nil {
			t.Fatal(err)
		}
		return BuildDoneCriteriaPrompt(criteria)
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
	for _, fn := range []string{"parseDoneCriteria", "checkDone", "buildDoneCriteriaPrompt"} {
		if seen[fn] == 0 {
			t.Errorf("no fixtures for %s", fn)
		}
	}
}
