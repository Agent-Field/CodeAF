package specidentifiers

import (
	"bufio"
	"encoding/json"
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

func fixtureArgs(t *testing.T, fx fixture) []json.RawMessage {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
		t.Fatalf("decode args_json: %v", err)
	}
	return args
}

func callFixture(t *testing.T, fx fixture) any {
	t.Helper()
	args := fixtureArgs(t, fx)
	switch fx.Fn {
	case "classifyLineContexts":
		var text string
		if err := json.Unmarshal(args[0], &text); err != nil {
			t.Fatal(err)
		}
		return ClassifyLineContexts(text)
	case "extractSpecIdentifiers":
		var text string
		if err := json.Unmarshal(args[0], &text); err != nil {
			t.Fatal(err)
		}
		return ExtractSpecIdentifiers(text)
	case "enforceableIdentifiers":
		var identifiers []SpecIdentifier
		if err := json.Unmarshal(args[0], &identifiers); err != nil {
			t.Fatal(err)
		}
		return EnforceableIdentifiers(identifiers)
	case "checkIdentifiersInTree":
		var input CheckIdentifiersInput
		if err := json.Unmarshal(args[0], &input); err != nil {
			t.Fatal(err)
		}
		return CheckIdentifiersInTree(input)
	case "identifierBlockerDetails":
		var missing []SpecIdentifier
		if err := json.Unmarshal(args[0], &missing); err != nil {
			t.Fatal(err)
		}
		return IdentifierBlockerDetails(missing)
	default:
		t.Fatalf("unknown function %q", fx.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 60 {
		t.Fatalf("expected at least 60 fixtures, got %d", len(fixtures))
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
		"classifyLineContexts",
		"extractSpecIdentifiers",
		"enforceableIdentifiers",
		"checkIdentifiersInTree",
		"identifierBlockerDetails",
	} {
		if seen[name] == 0 {
			t.Errorf("no fixtures for %s", name)
		}
	}
}
