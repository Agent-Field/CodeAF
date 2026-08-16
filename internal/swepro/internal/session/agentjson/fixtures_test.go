package agentjson

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

type decision struct {
	Decision string    `json:"decision"`
	Reason   string    `json:"reason"`
	Items    []float64 `json:"items,omitempty"`
}

type decisionWithItems struct {
	Decision string    `json:"decision"`
	Reason   string    `json:"reason"`
	Items    []float64 `json:"items"`
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
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
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

func fixtureResult(data decision, firstTry, usedFallback bool) Result[decision] {
	return Result[decision]{Data: data, FirstTry: firstTry, UsedFallback: usedFallback}
}

func callFixture(t *testing.T, fixture fixtureLine) any {
	t.Helper()
	const outputPath = "/tmp/codex-agentjson-fixtures/result.json"
	switch fixture.Fn {
	case "initialRequest":
		return struct {
			Reminder string           `json:"reminder"`
			Tools    ToolSettings     `json:"tools"`
			Model    Model            `json:"model"`
			Result   Result[decision] `json:"result"`
		}{
			Reminder: BuildSystemReminder(BuildPromptArgs{
				TaskPrompt: "Decide whether the merge is safe.",
				OutputPath: outputPath, Workspace: "/fixture/workspace", Label: "merger",
			}),
			Tools: MergeTools([]ToolSetting{
				{Name: "edit", Enabled: true}, {Name: "custom", Enabled: false},
			}),
			Model:  SplitModelID("openrouter/vendor/model/x"),
			Result: fixtureResult(decision{Decision: "yes", Reason: "clean"}, true, false),
		}
	case "retryRequest":
		var args struct {
			LastErrorKind string `json:"lastErrorKind"`
		}
		if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
			t.Fatal(err)
		}
		last := ""
		result := fixtureResult(decision{Decision: "no", Reason: "fixture fallback"}, false, true)
		switch args.LastErrorKind {
		case "parse":
			last = "parse error after 0 fix turn(s): JSON Parse error: Expected '}'"
			result = fixtureResult(decision{Decision: "no", Reason: "second"}, false, false)
		case "missing":
			last = "no output file at " + outputPath
		case "empty":
			last = "output file empty at " + outputPath
		case "schema-fix-broke-parse":
			last = CompactSchemaErrors(schemaFixtureIssues())
			note := "Your previous JSON failed validation: " + last +
				". Rewrite the file at the output path EXACTLY matching the schema. " +
				"Do not add fields not in the schema."
			return struct {
				Reminder string                    `json:"reminder"`
				Result   Result[decisionWithItems] `json:"result"`
			}{
				Reminder: BuildSystemReminder(BuildPromptArgs{
					TaskPrompt: "Decide whether the merge is safe.",
					OutputPath: outputPath, Workspace: "/fixture/workspace",
					Label: "merger", RetryNote: &note,
				}),
				Result: Result[decisionWithItems]{
					Data: decisionWithItems{
						Decision: "yes", Reason: "retry", Items: []float64{},
					},
					FirstTry: false,
				},
			}
		default:
			t.Fatalf("unknown lastErrorKind %q", args.LastErrorKind)
		}
		note := "Your previous JSON failed validation: " + last +
			". Rewrite the file at the output path EXACTLY matching the schema. " +
			"Do not add fields not in the schema."
		return struct {
			Reminder string           `json:"reminder"`
			Result   Result[decision] `json:"result"`
		}{
			Reminder: BuildSystemReminder(BuildPromptArgs{
				TaskPrompt: "Decide whether the merge is safe.",
				OutputPath: outputPath, Workspace: "/fixture/workspace",
				Label: "merger", RetryNote: &note,
			}),
			Result: result,
		}
	case "parseFixRequest":
		var args struct {
			Raw     string `json:"raw"`
			Attempt int    `json:"attempt"`
			Budget  int    `json:"budget"`
		}
		if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
			t.Fatal(err)
		}
		return struct {
			Reminder    string           `json:"reminder"`
			SameSession bool             `json:"sameSession"`
			Result      Result[decision] `json:"result"`
		}{
			Reminder: BuildParseFixReminder(ParseFixReminderArgs{
				OutputPath: outputPath, ParseError: JSONParseError([]byte(args.Raw)),
				Attempt: args.Attempt, Budget: args.Budget, Label: "merger",
			}),
			SameSession: true,
			Result:      fixtureResult(decision{Decision: "yes", Reason: "fixed"}, true, false),
		}
	case "schemaFixRequest":
		issues := schemaFixtureIssues()
		return struct {
			Reminder    string           `json:"reminder"`
			SameSession bool             `json:"sameSession"`
			Result      Result[decision] `json:"result"`
		}{
			Reminder: BuildSchemaFixReminder(SchemaFixReminderArgs{
				OutputPath: outputPath, SchemaErrors: FormatSchemaErrors(issues),
				Attempt: 1, Budget: 1, Label: "merger",
			}),
			SameSession: true,
			Result: fixtureResult(
				decision{Decision: "no", Reason: "fixed", Items: []float64{1}},
				true, false,
			),
		}
	case "dispatchResult":
		return fixtureResult(decision{Decision: "no", Reason: "fixture fallback"}, false, true)
	case "dispatchError":
		return "agent-json: merger failed after 1 attempts; last error: no output file at " + outputPath
	default:
		t.Fatalf("unknown fixture fn %q", fixture.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) != 13 {
		t.Fatalf("fixture corpus count = %d, want 13", len(fixtures))
	}
	counts := map[string]int{}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
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
		"initialRequest", "retryRequest", "parseFixRequest", "schemaFixRequest",
		"dispatchResult", "dispatchError",
	} {
		if counts[fn] == 0 {
			t.Errorf("no fixtures for %s", fn)
		}
	}
}

func schemaFixtureIssues() []Issue {
	return []Issue{
		{Path: []string{"decision"}, Message: `Invalid option: expected one of "yes"|"no"`},
		{Path: []string{"reason"}, Message: "Too small: expected string to have >=3 characters"},
		{Path: []string{"items", "0"}, Message: "Invalid input: expected number, received string"},
		{Message: `Unrecognized key: "extra"`},
	}
}
