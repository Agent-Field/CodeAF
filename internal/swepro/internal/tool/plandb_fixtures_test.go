package tool

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
)

func TestPlanDBFixtureParity(t *testing.T) {
	fixtures := loadToolFixtures(t, "testdata/plandb_fixtures.json")
	if len(fixtures) < 60 {
		t.Fatalf("fixture count = %d, want at least 60", len(fixtures))
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
				t.Fatal(err)
			}
			var got any
			switch fixture.Fn {
			case "buildCommand":
				var params PlanDBParams
				if err := json.Unmarshal(args[0], &params); err != nil {
					t.Fatal(err)
				}
				var err error
				got, err = BuildPlanDBCommand(params)
				if err != nil {
					t.Fatal(err)
				}
			case "buildCommandError":
				var params PlanDBParams
				if err := json.Unmarshal(args[0], &params); err != nil {
					t.Fatal(err)
				}
				_, err := BuildPlanDBCommand(params)
				if err != nil {
					got = err.Error()
				} else {
					got = ""
				}
			case "withPolicy":
				var params PlanDBParams
				if err := json.Unmarshal(args[0], &params); err != nil {
					t.Fatal(err)
				}
				got = WithPlanPolicy(params)
			case "isIdKeyed":
				var tasks []PlanTaskInput
				if err := json.Unmarshal(args[0], &tasks); err != nil {
					t.Fatal(err)
				}
				got = IsIDKeyedPlan(tasks)
			case "validate":
				var tasks []PlanTaskInput
				var existing []string
				if err := json.Unmarshal(args[0], &tasks); err != nil {
					t.Fatal(err)
				}
				_ = json.Unmarshal(args[1], &existing)
				existingSet := stringBoolSet(existing)
				if len(args) == 3 {
					var limits PlanLimits
					if err := json.Unmarshal(args[2], &limits); err != nil {
						t.Fatal(err)
					}
					got = ValidatePlanDocument(tasks, existingSet, limits)
				} else {
					got = ValidatePlanDocument(tasks, existingSet)
				}
			case "apply":
				var order []NormalizedPlanTask
				var base PlanApplyBase
				var existing []string
				var chunkSize, failAt int
				_ = json.Unmarshal(args[0], &order)
				_ = json.Unmarshal(args[1], &base)
				_ = json.Unmarshal(args[2], &existing)
				_ = json.Unmarshal(args[3], &chunkSize)
				_ = json.Unmarshal(args[4], &failAt)
				got = ApplyPlanDocument(order, base, stringBoolSet(existing), fixturePlanRunner(failAt), chunkSize)
			case "ingest":
				var tasks []PlanTaskInput
				var existing []string
				var base PlanApplyBase
				var failAt int
				_ = json.Unmarshal(args[0], &tasks)
				_ = json.Unmarshal(args[1], &existing)
				_ = json.Unmarshal(args[2], &base)
				_ = json.Unmarshal(args[3], &failAt)
				got = IngestIDKeyedPlan(tasks, stringBoolSet(existing), base, fixturePlanRunner(failAt))
			case "expandScope":
				var description string
				var files []string
				_ = json.Unmarshal(args[0], &description)
				_ = json.Unmarshal(args[1], &files)
				got = ExpandPlanDBFileScope(description, files)
			case "mutationTitle":
				var files []string
				_ = json.Unmarshal(args[0], &files)
				got = PlanDBMutationTitle(files)
			case "isVerification":
				var command string
				_ = json.Unmarshal(args[0], &command)
				got = IsVerificationShellCommand(command)
			case "verificationTitle":
				var command string
				_ = json.Unmarshal(args[0], &command)
				got = PlanDBVerificationTitle(command)
			case "shellTitle":
				var command string
				_ = json.Unmarshal(args[0], &command)
				got = PlanDBShellTitle(command)
			case "fileScopeMatches":
				var description string
				var files, relative []string
				_ = json.Unmarshal(args[0], &description)
				_ = json.Unmarshal(args[1], &files)
				_ = json.Unmarshal(args[2], &relative)
				got = PlanDBFileScopeMatches(description, files, relative)
			case "fileScope":
				var description string
				_ = json.Unmarshal(args[0], &description)
				got = PlanDBFileScope(description)
			case "shellDescription":
				var input struct {
					SessionID string `json:"sessionID"`
					MessageID string `json:"messageID"`
					Agent     string `json:"agent"`
				}
				var command, role string
				_ = json.Unmarshal(args[0], &input)
				_ = json.Unmarshal(args[1], &command)
				_ = json.Unmarshal(args[2], &role)
				got = PlanDBShellDescription(PlanDBGuardContext{
					SessionID: input.SessionID,
					MessageID: input.MessageID,
					Agent:     input.Agent,
				}, command, role)
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

func stringBoolSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func fixturePlanRunner(failAt int) PlanRunFunc {
	count := 0
	return func(argv []string) plandb.RunResult {
		count++
		if count == failAt {
			return plandb.RunResult{Code: 1, Stdout: []byte{}, Stderr: []byte("boom\n")}
		}
		title := ""
		for i, arg := range argv {
			if arg == "add" && i+1 < len(argv) {
				title = argv[i+1]
				break
			}
		}
		data, _ := jscompat.Stringify(struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}{ID: fmt.Sprintf("real-%d", count), Title: title})
		return plandb.RunResult{Code: 0, Stdout: data, Stderr: []byte{}}
	}
}
