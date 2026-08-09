package architecturegate

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

type promptFixtureInput struct {
	Workspace        string  `json:"workspace"`
	ParentSessionID  string  `json:"parentSessionID"`
	UserPrompt       string  `json:"userPrompt"`
	PRDPath          *string `json:"prdPath"`
	ArchitecturePath string  `json:"architecturePath"`
	ReviewPath       string  `json:"reviewPath"`
	Feedback         *string `json:"feedback"`
	RevisionNumber   int     `json:"revisionNumber"`
}

type schemaFixtureResult struct {
	Success bool                `json:"success"`
	Data    *ArchitectureReview `json:"data,omitempty"`
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
				got = ReviewFallback
			case "schema":
				parsed := (ReviewSchema{}).SafeParse(args[0])
				result := schemaFixtureResult{Success: parsed.Success()}
				if parsed.Success() {
					data := parsed.Data
					result.Data = &data
				}
				got = result
			case "architectPrompt":
				input := decodePromptInput(t, args[0])
				got = BuildArchitectPrompt(input.public(), input.ArchitecturePath, input.Feedback)
			case "reminder":
				input := decodePromptInput(t, args[0])
				var attempt int
				_ = json.Unmarshal(args[1], &attempt)
				got = BuildArchitectReminder(
					input.ArchitecturePath, input.Feedback,
					input.RevisionNumber, attempt,
				).Text
			case "techPrompt":
				input := decodePromptInput(t, args[0])
				got = BuildTechLeadPrompt(
					input.public(), input.ArchitecturePath,
					input.ReviewPath, input.RevisionNumber,
				)
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

func decodePromptInput(t *testing.T, raw json.RawMessage) promptFixtureInput {
	t.Helper()
	var input promptFixtureInput
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	return input
}

func (input promptFixtureInput) public() Input {
	return Input{
		Workspace: input.Workspace, ParentSessionID: input.ParentSessionID,
		UserPrompt: input.UserPrompt, PRDPath: input.PRDPath,
	}
}
