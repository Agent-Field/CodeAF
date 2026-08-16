package permission

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
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
		t.Fatal(err)
	}
	defer file.Close()
	out := []fixture{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var item fixture
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			t.Fatal(err)
		}
		out = append(out, item)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func callFixture(t *testing.T, fixture fixture) any {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
		t.Fatal(err)
	}
	switch fixture.Fn {
	case "fromConfig":
		config, err := ParseConfigJSON(args[0])
		if err != nil {
			t.Fatal(err)
		}
		return FromConfig(config)
	case "evaluate":
		var permission string
		var pattern string
		if err := json.Unmarshal(args[0], &permission); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &pattern); err != nil {
			t.Fatal(err)
		}
		rulesets := make([]Ruleset, 0, len(args)-2)
		for _, raw := range args[2:] {
			var rules Ruleset
			if err := json.Unmarshal(raw, &rules); err != nil {
				t.Fatal(err)
			}
			rulesets = append(rulesets, rules)
		}
		return Evaluate(permission, pattern, rulesets...)
	case "merge":
		var rulesets []Ruleset
		if err := json.Unmarshal(args[0], &rulesets); err != nil {
			t.Fatal(err)
		}
		return Merge(rulesets...)
	case "disabled":
		var tools []string
		var rules Ruleset
		if err := json.Unmarshal(args[0], &tools); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &rules); err != nil {
			t.Fatal(err)
		}
		return Disabled(tools, rules).Values()
	case "frontmatter":
		var name string
		if err := json.Unmarshal(args[0], &name); err != nil {
			t.Fatal(err)
		}
		markdown, ok := baked.GetBakedAgentMarkdown(name)
		if !ok {
			t.Fatalf("missing baked agent %q", name)
		}
		rules, err := RulesetFromFrontmatter(markdown)
		if err != nil {
			t.Fatal(err)
		}
		return rules
	default:
		t.Fatalf("unknown fn %q", fixture.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 18 {
		t.Fatalf("expected at least 18 fixtures, got %d", len(fixtures))
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callFixture(t, fixture))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != fixture.OutJSON {
				t.Fatalf("fn=%s args=%s\n got %s\nwant %s", fixture.Fn, fixture.ArgsJSON, got, fixture.OutJSON)
			}
		})
	}
}

func TestFixtureCoverage(t *testing.T) {
	seen := map[string]bool{}
	for _, fixture := range loadFixtures(t) {
		seen[fixture.Fn] = true
	}
	for _, fn := range []string{"fromConfig", "evaluate", "merge", "disabled", "frontmatter"} {
		if !seen[fn] {
			t.Errorf("missing fixture for %s", fn)
		}
	}
}
