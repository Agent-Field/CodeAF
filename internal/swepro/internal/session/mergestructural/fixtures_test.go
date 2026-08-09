package mergestructural

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
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
	case "WEAVE_SUPPORTED_LANGUAGES":
		return WEAVE_SUPPORTED_LANGUAGES
	case "weaveLanguageForPath":
		var path string
		if len(args) != 1 {
			t.Fatalf("weaveLanguageForPath wants 1 arg, got %d", len(args))
		}
		if err := json.Unmarshal(args[0], &path); err != nil {
			t.Fatalf("decode path: %v", err)
		}
		return WeaveLanguageForPath(path)
	case "weaveSupportsPath":
		var path string
		if len(args) != 1 {
			t.Fatalf("weaveSupportsPath wants 1 arg, got %d", len(args))
		}
		if err := json.Unmarshal(args[0], &path); err != nil {
			t.Fatalf("decode path: %v", err)
		}
		return WeaveSupportsPath(path)
	default:
		t.Fatalf("unknown fixture fn %q", fixture.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 100 {
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
	for _, fn := range []string{
		"WEAVE_SUPPORTED_LANGUAGES",
		"weaveLanguageForPath",
		"weaveSupportsPath",
	} {
		if seen[fn] == 0 {
			t.Errorf("no fixtures for %s", fn)
		}
	}
}
