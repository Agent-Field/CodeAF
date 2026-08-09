package replangate

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

type schemaFixtureResult struct {
	Success bool            `json:"success"`
	Data    *ReplanDecision `json:"data,omitempty"`
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
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
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
	fixtures := loadFixtures(t)
	if len(fixtures) < 20 {
		t.Fatalf("fixture count=%d", len(fixtures))
	}
	for _, value := range fixtures {
		value := value
		t.Run(value.Name, func(t *testing.T) {
			var args []json.RawMessage
			_ = json.Unmarshal([]byte(value.ArgsJSON), &args)
			var got any
			switch value.Fn {
			case "fallback":
				got = ReplanFallback
			case "defaultMax":
				got = DefaultMaxReplans
			case "schema":
				parsed := (DecisionSchema{}).SafeParse(args[0])
				result := schemaFixtureResult{Success: parsed.Success()}
				if parsed.Success() {
					data := parsed.Data
					result.Data = &data
				}
				got = result
			case "prompt":
				var input DispatchReplannerInput
				var snapshot PlanDBSnapshot
				var history []ReplanHistoryEntry
				var outputPath string
				_ = json.Unmarshal(args[0], &input)
				_ = json.Unmarshal(args[1], &snapshot)
				_ = json.Unmarshal(args[2], &history)
				_ = json.Unmarshal(args[3], &outputPath)
				got = BuildReplannerPrompt(input, snapshot, history, outputPath)
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
