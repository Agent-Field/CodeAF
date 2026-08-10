package productgate

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

type fixtureInput struct {
	Workspace         string  `json:"workspace"`
	ParentSessionID   string  `json:"parentSessionID"`
	UserPrompt        string  `json:"userPrompt"`
	AdditionalContext *string `json:"additionalContext"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var out []fixture
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var value fixture
		if json.Unmarshal(scanner.Bytes(), &value) != nil {
			t.Fatalf("bad fixture: %s", scanner.Bytes())
		}
		out = append(out, value)
	}
	return out
}

func TestFixtureParity(t *testing.T) {
	for _, value := range loadFixtures(t) {
		value := value
		t.Run(value.Name, func(t *testing.T) {
			var args []json.RawMessage
			if json.Unmarshal([]byte(value.ArgsJSON), &args) != nil {
				t.Fatal("bad args")
			}
			var got any
			switch value.Fn {
			case "taskPrompt":
				var input fixtureInput
				var path string
				_ = json.Unmarshal(args[0], &input)
				_ = json.Unmarshal(args[1], &path)
				got = BuildTaskPrompt(Input{
					Workspace: input.Workspace, ParentSessionID: input.ParentSessionID,
					UserPrompt: input.UserPrompt, AdditionalContext: input.AdditionalContext,
				}, path)
			case "reminder":
				var path string
				_ = json.Unmarshal(args[0], &path)
				got = BuildReminder(path).Text
			case "retryReminder":
				var attempt int
				var path, reason string
				_ = json.Unmarshal(args[0], &attempt)
				_ = json.Unmarshal(args[1], &path)
				_ = json.Unmarshal(args[2], &reason)
				if attempt <= 1 {
					got = nil
				} else {
					got = BuildRetryReminder(attempt, path, reason).Text
				}
			default:
				t.Fatalf("unknown fn %s", value.Fn)
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
