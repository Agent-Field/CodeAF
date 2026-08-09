package tool

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type editFixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

type editFixtureOutcome struct {
	OK    bool   `json:"ok"`
	Value any    `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

func loadEditFixtures(t *testing.T) []editFixture {
	t.Helper()
	file, err := os.Open("testdata/edit-fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()
	out := []editFixture{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var fixture editFixture
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		out = append(out, fixture)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func callEditFixture(t *testing.T, fixture editFixture) editFixtureOutcome {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	stringArg := func(index int) string {
		t.Helper()
		var value string
		if err := json.Unmarshal(args[index], &value); err != nil {
			t.Fatalf("decode string arg %d: %v", index, err)
		}
		return value
	}

	var value any
	switch fixture.Fn {
	case "SimpleReplacer":
		value = SimpleReplacer(stringArg(0), stringArg(1))
	case "LineTrimmedReplacer":
		value = LineTrimmedReplacer(stringArg(0), stringArg(1))
	case "BlockAnchorReplacer":
		value = BlockAnchorReplacer(stringArg(0), stringArg(1))
	case "WhitespaceNormalizedReplacer":
		value = WhitespaceNormalizedReplacer(stringArg(0), stringArg(1))
	case "IndentationFlexibleReplacer":
		value = IndentationFlexibleReplacer(stringArg(0), stringArg(1))
	case "EscapeNormalizedReplacer":
		value = EscapeNormalizedReplacer(stringArg(0), stringArg(1))
	case "TrimmedBoundaryReplacer":
		value = TrimmedBoundaryReplacer(stringArg(0), stringArg(1))
	case "ContextAwareReplacer":
		value = ContextAwareReplacer(stringArg(0), stringArg(1))
	case "MultiOccurrenceReplacer":
		value = MultiOccurrenceReplacer(stringArg(0), stringArg(1))
	case "trimDiff":
		value = TrimDiff(stringArg(0))
	case "replace":
		replaceAll := false
		if len(args) > 3 {
			if err := json.Unmarshal(args[3], &replaceAll); err != nil {
				t.Fatalf("decode replaceAll: %v", err)
			}
		}
		replaced, err := Replace(stringArg(0), stringArg(1), stringArg(2), replaceAll)
		if err != nil {
			return editFixtureOutcome{OK: false, Error: err.Error()}
		}
		value = replaced
	default:
		t.Fatalf("unknown fn %q", fixture.Fn)
	}
	return editFixtureOutcome{OK: true, Value: value}
}

func TestEditFixtureParity(t *testing.T) {
	fixtures := loadEditFixtures(t)
	if len(fixtures) < 36 {
		t.Fatalf("expected at least 36 fixtures, got %d", len(fixtures))
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callEditFixture(t, fixture))
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(got) != fixture.OutJSON {
				t.Fatalf("fn=%s args=%s\n got %s\nwant %s", fixture.Fn, fixture.ArgsJSON, got, fixture.OutJSON)
			}
		})
	}
}

func TestEditFixtureCoverage(t *testing.T) {
	seen := map[string]bool{}
	for _, fixture := range loadEditFixtures(t) {
		seen[fixture.Fn] = true
	}
	for _, fn := range []string{
		"SimpleReplacer",
		"LineTrimmedReplacer",
		"BlockAnchorReplacer",
		"WhitespaceNormalizedReplacer",
		"IndentationFlexibleReplacer",
		"EscapeNormalizedReplacer",
		"TrimmedBoundaryReplacer",
		"ContextAwareReplacer",
		"MultiOccurrenceReplacer",
		"replace",
		"trimDiff",
	} {
		if !seen[fn] {
			t.Errorf("missing fixtures for %s", fn)
		}
	}
}

func TestEditJSWhitespaceLineSeparators(t *testing.T) {
	got := WhitespaceNormalizedReplacer("alpha\u2028beta", "alpha beta")
	if len(got) != 1 || got[0] != "alpha\u2028beta" {
		t.Fatalf("candidates = %#v", got)
	}
}
