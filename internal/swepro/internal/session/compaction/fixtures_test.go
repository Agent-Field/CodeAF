package compaction

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/ledgers"
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
		t.Fatal(err)
	}
	defer file.Close()
	var out []fixtureLine
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		var fixture fixtureLine
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatal(err)
		}
		out = append(out, fixture)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func fixtureArgs(t *testing.T, fixture fixtureLine) []json.RawMessage {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
		t.Fatal(err)
	}
	return args
}

func callFixture(t *testing.T, fixture fixtureLine) any {
	t.Helper()
	args := fixtureArgs(t, fixture)
	switch fixture.Fn {
	case "constants":
		return struct {
			PruneMinimum    int    `json:"pruneMinimum"`
			PruneProtect    int    `json:"pruneProtect"`
			SummaryTemplate string `json:"summaryTemplate"`
		}{
			PruneMinimum: PruneMinimum, PruneProtect: PruneProtect,
			SummaryTemplate: SummaryTemplate,
		}
	case "buildPrompt":
		var input struct {
			PreviousSummary *string  `json:"previousSummary"`
			Context         []string `json:"context"`
		}
		if err := json.Unmarshal(args[0], &input); err != nil {
			t.Fatal(err)
		}
		return BuildPrompt(input.PreviousSummary, input.Context)
	case "buildDurableBlockerPin":
		var blockers []ledgers.OpenBlocker
		if err := json.Unmarshal(args[0], &blockers); err != nil {
			t.Fatal(err)
		}
		return BuildDurableBlockerPin(blockers)
	case "isSyntheticUser":
		var message msgmodel.WithParts
		if err := json.Unmarshal(args[0], &message); err != nil {
			t.Fatal(err)
		}
		return IsSyntheticUser(message)
	case "turns":
		var messages []msgmodel.WithParts
		if err := json.Unmarshal(args[0], &messages); err != nil {
			t.Fatal(err)
		}
		return Turns(messages)
	case "workingSetDrift":
		var messages []msgmodel.WithParts
		if err := json.Unmarshal(args[0], &messages); err != nil {
			t.Fatal(err)
		}
		var options struct {
			WindowTurns *int `json:"windowTurns"`
		}
		if err := json.Unmarshal(args[1], &options); err != nil {
			t.Fatal(err)
		}
		if options.WindowTurns == nil {
			return jscompat.JSNumber(WorkingSetDrift(messages))
		}
		return jscompat.JSNumber(WorkingSetDrift(messages, *options.WindowTurns))
	case "headTailTruncate":
		var text string
		var max float64
		if err := json.Unmarshal(args[0], &text); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &max); err != nil {
			t.Fatal(err)
		}
		return HeadTailTruncate(text, max)
	case "evidenceBlocksFromMessages":
		var messages []msgmodel.WithParts
		var max float64
		if err := json.Unmarshal(args[0], &messages); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &max); err != nil {
			t.Fatal(err)
		}
		return EvidenceBlocksFromMessages(messages, max)
	case "tokenEstimate":
		var text string
		if err := json.Unmarshal(args[0], &text); err != nil {
			t.Fatal(err)
		}
		return jscompat.JSNumber(estimateTokens(text))
	case "evidenceCompactionEnabled":
		var value *string
		if err := json.Unmarshal(args[0], &value); err != nil {
			t.Fatal(err)
		}
		env := map[string]string{}
		if value != nil {
			env["CODEAF_COMPACT_EVIDENCE"] = *value
		}
		return EvidenceCompactionEnabled(env)
	default:
		t.Fatalf("unknown fixture function %q", fixture.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) != 36 {
		t.Fatalf("fixture count = %d, want 36", len(fixtures))
	}
	counts := map[string]int{}
	for _, fixture := range fixtures {
		t.Run(fixture.Fn+"/"+fixture.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callFixture(t, fixture))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != fixture.OutJSON {
				t.Fatalf("args=%s\n got: %s\nwant: %s", fixture.ArgsJSON, got, fixture.OutJSON)
			}
		})
		counts[fixture.Fn]++
	}
	for _, fn := range []string{
		"constants", "buildPrompt", "buildDurableBlockerPin", "isSyntheticUser",
		"turns", "workingSetDrift", "headTailTruncate",
		"evidenceBlocksFromMessages", "tokenEstimate", "evidenceCompactionEnabled",
	} {
		if counts[fn] == 0 {
			t.Errorf("no fixtures for %s", fn)
		}
	}
}
