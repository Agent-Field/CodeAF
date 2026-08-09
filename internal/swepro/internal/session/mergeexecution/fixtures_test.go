package mergeexecution

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
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()

	out := []fixtureLine{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
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

func fixtureArgs(t *testing.T, fixture fixtureLine) []json.RawMessage {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
		t.Fatalf("decode args_json: %v", err)
	}
	return args
}

func callFixture(t *testing.T, fixture fixtureLine) any {
	t.Helper()
	args := fixtureArgs(t, fixture)
	switch fixture.Fn {
	case "decideReconcile":
		var input DecideOptions
		if len(args) != 1 {
			t.Fatalf("decideReconcile wants 1 arg, got %d", len(args))
		}
		if err := json.Unmarshal(args[0], &input); err != nil {
			t.Fatal(err)
		}
		return DecideReconcile(input)
	case "buildReconciliationMessage":
		if len(args) != 2 {
			t.Fatalf("buildReconciliationMessage wants 2 args, got %d", len(args))
		}
		var best AttemptResult
		var others []AttemptResult
		if err := json.Unmarshal(args[0], &best); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &others); err != nil {
			t.Fatal(err)
		}
		return BuildReconciliationMessage(best, others)
	case "auditorDraftPath":
		var workspace string
		if err := json.Unmarshal(args[0], &workspace); err != nil {
			t.Fatal(err)
		}
		return AuditorDraftPath(workspace)
	case "draftPresenceSuspect":
		var input DraftPresenceInput
		if err := json.Unmarshal(args[0], &input); err != nil {
			t.Fatal(err)
		}
		return DraftPresenceSuspect(input)
	case "staggerMs":
		var index, random float64
		if err := json.Unmarshal(args[0], &index); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &random); err != nil {
			t.Fatal(err)
		}
		return jscompat.JSNumber(StaggerMS(index, func() float64 { return random }))
	default:
		t.Fatalf("unknown fixture fn %q", fixture.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 45 {
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
		"decideReconcile",
		"buildReconciliationMessage",
		"auditorDraftPath",
		"draftPresenceSuspect",
		"staggerMs",
	} {
		if seen[fn] == 0 {
			t.Errorf("no fixtures for %s", fn)
		}
	}
}
