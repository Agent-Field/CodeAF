package tool

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

func TestTaskFixtureParity(t *testing.T) {
	fixtures := loadToolFixtures(t, "testdata/task_fixtures.json")
	if len(fixtures) < 50 {
		t.Fatalf("fixture count = %d, want at least 50", len(fixtures))
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
				t.Fatal(err)
			}
			var got any
			switch fixture.Fn {
			case "issueFilePointerLines":
				var issue string
				_ = json.Unmarshal(args[0], &issue)
				got = IssueFilePointerLines(issue)
			case "compact":
				var text string
				var limit int
				_ = json.Unmarshal(args[0], &text)
				_ = json.Unmarshal(args[1], &limit)
				got = CompactTaskText(text, limit)
			case "inferAccess", "inferTaskRole", "inferOutputs", "buildPackage":
				var params TaskParams
				if err := json.Unmarshal(args[0], &params); err != nil {
					t.Fatal(err)
				}
				switch fixture.Fn {
				case "inferAccess":
					got = InferTaskAccess(params)
				case "inferTaskRole":
					var access string
					_ = json.Unmarshal(args[1], &access)
					got = InferTaskRole(params, access)
				case "inferOutputs":
					var access, role string
					_ = json.Unmarshal(args[1], &access)
					_ = json.Unmarshal(args[2], &role)
					got = InferTaskOutputs(params, access, role)
				case "buildPackage":
					var parent, session, agent, excerpt string
					_ = json.Unmarshal(args[1], &parent)
					_ = json.Unmarshal(args[2], &session)
					_ = json.Unmarshal(args[3], &agent)
					_ = json.Unmarshal(args[4], &excerpt)
					got = BuildTaskPackageDescription(params, parent, session, agent, excerpt)
				}
			case "planDBInfoFromText":
				var text string
				_ = json.Unmarshal(args[0], &text)
				got = ParseTaskPlanDBInfo(text)
			case "stripPolicyLines":
				var text string
				_ = json.Unmarshal(args[0], &text)
				got = StripTaskPolicyLines(text)
			case "contextLines":
				var raw any
				if err := json.Unmarshal(args[0], &raw); err != nil {
					t.Fatal(err)
				}
				var ids []string
				_ = json.Unmarshal(args[1], &ids)
				if array, ok := raw.([]any); ok {
					items := make([]TaskContextItem, 0, len(array))
					for _, value := range array {
						data, _ := json.Marshal(value)
						var item TaskContextItem
						_ = json.Unmarshal(data, &item)
						items = append(items, item)
					}
					got = TaskContextLines(items, ids)
				} else {
					got = []string{}
				}
			case "childPrompt":
				var prompt, issue string
				_ = json.Unmarshal(args[0], &prompt)
				_ = json.Unmarshal(args[1], &issue)
				got = BuildTaskChildPrompt(prompt, issue)
			case "reminder":
				var input TaskReminderInput
				if err := json.Unmarshal(args[0], &input); err != nil {
					t.Fatal(err)
				}
				got = BuildTaskPlanReminder(input)
			case "resultOutput":
				var session, taskID, result string
				_ = json.Unmarshal(args[0], &session)
				_ = json.Unmarshal(args[1], &taskID)
				_ = json.Unmarshal(args[2], &result)
				got = BuildTaskResultOutput(session, taskID, result)
			case "pickTier":
				var category, agent string
				_ = json.Unmarshal(args[0], &category)
				_ = json.Unmarshal(args[1], &agent)
				got = PickTaskTier(category, agent)
			default:
				t.Fatalf("unknown fixture function %q", fixture.Fn)
			}
			data, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != fixture.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fixture.ArgsJSON, data, fixture.OutJSON)
			}
		})
	}
}
