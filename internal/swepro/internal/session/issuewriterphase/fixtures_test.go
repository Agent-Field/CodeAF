package issuewriterphase

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
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var fixtures []fixture
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var value fixture
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			t.Fatalf("bad fixture: %v", err)
		}
		fixtures = append(fixtures, value)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func TestFixtureParity(t *testing.T) {
	for _, value := range loadFixtures(t) {
		value := value
		t.Run(value.Name, func(t *testing.T) {
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(value.ArgsJSON), &args); err != nil {
				t.Fatal(err)
			}
			var input SingleTaskInput
			if err := json.Unmarshal(args[0], &input); err != nil {
				t.Fatal(err)
			}
			var got any
			switch value.Fn {
			case "taskPrompt":
				got = BuildTaskPrompt(input)
			case "reminder":
				got = BuildReminder(input.OutputPath).Text
			default:
				t.Fatalf("unknown function %q", value.Fn)
			}
			encoded, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != value.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", value.ArgsJSON, encoded, value.OutJSON)
			}
		})
	}
}
