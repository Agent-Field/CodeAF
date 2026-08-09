package question

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type questionFixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadQuestionFixtures(t *testing.T) []questionFixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var fixtures []questionFixture
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var fixture questionFixture
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, fixture)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func TestQuestionFixtureParity(t *testing.T) {
	fixtures := loadQuestionFixtures(t)
	if len(fixtures) < 100 {
		t.Fatalf("fixture count = %d", len(fixtures))
	}
	seen := map[string]int{}
	for _, fixture := range fixtures {
		seen[fixture.Fn]++
		t.Run(fixture.Fn+"/"+fixture.Name, func(t *testing.T) {
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
				t.Fatal(err)
			}
			var out any
			switch fixture.Fn {
			case "schema":
				var kind, mode string
				if err := json.Unmarshal(args[0], &kind); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(args[1], &mode); err != nil {
					t.Fatal(err)
				}
				out = SchemaAccepts(kind, mode, args[2])
			case "ascending":
				var given string
				if err := json.Unmarshal(args[0], &given); err != nil {
					t.Fatal(err)
				}
				value, err := AscendingQuestionID(given)
				out = struct {
					OK    bool       `json:"ok"`
					Value QuestionID `json:"value,omitempty"`
				}{OK: err == nil, Value: value}
			default:
				t.Fatalf("unknown fixture function %q", fixture.Fn)
			}
			encoded, err := jscompat.Stringify(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != fixture.OutJSON {
				t.Fatalf("args=%s\n got %s\nwant %s", fixture.ArgsJSON, encoded, fixture.OutJSON)
			}
		})
	}
	for _, function := range []string{"schema", "ascending"} {
		if seen[function] == 0 {
			t.Errorf("no fixtures for %s", function)
		}
	}
}
