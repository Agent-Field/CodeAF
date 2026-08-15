package plannertranslate

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
	var fixtures []fixture
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var value fixture
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		fixtures = append(fixtures, value)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return fixtures
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixtures, got %d", len(fixtures))
	}
	for _, value := range fixtures {
		value := value
		t.Run(value.Name, func(t *testing.T) {
			var arguments []json.RawMessage
			if err := json.Unmarshal([]byte(value.ArgsJSON), &arguments); err != nil {
				t.Fatalf("decode args: %v", err)
			}
			var got any
			switch value.Fn {
			case "normalizeRawDAG":
				if len(arguments) != 1 {
					t.Fatalf("normalizeRawDAG args=%d", len(arguments))
				}
				got = NormalizeRawDAG(arguments[0])
			case "buildPrompt":
				if len(arguments) < 1 || len(arguments) > 3 {
					t.Fatalf("buildPrompt args=%d", len(arguments))
				}
				var outputPath string
				_ = json.Unmarshal(arguments[0], &outputPath)
				var frontier *string
				var tick *float64
				if len(arguments) > 1 {
					var value string
					_ = json.Unmarshal(arguments[1], &value)
					frontier = &value
				}
				if len(arguments) > 2 {
					var value float64
					_ = json.Unmarshal(arguments[2], &value)
					tick = &value
				}
				got = BuildPrompt(outputPath, frontier, tick)
			default:
				t.Fatalf("unexpected fixture function %s", value.Fn)
			}
			encoded, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(encoded) != value.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", value.ArgsJSON, encoded, value.OutJSON)
			}
		})
	}
}
