package merger

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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

type fixtureInput struct {
	Workspace       string `json:"workspace"`
	ParentSessionID string `json:"parentSessionID"`
	Worktree        string `json:"worktree"`
	MergeAt         string `json:"mergeAt"`
	TaskID          string `json:"taskID"`
	TaskTitle       string `json:"taskTitle"`
	SourceBranch    string `json:"sourceBranch"`
	TargetBranch    string `json:"targetBranch"`
	UserGoal        string `json:"userGoal"`
	PriorFailure    string `json:"priorFailure"`
}

func (input fixtureInput) mergerInput() Input {
	return Input{
		Workspace: input.Workspace, ParentSessionID: input.ParentSessionID,
		PromptOps: struct{}{}, Worktree: input.Worktree, MergeAt: input.MergeAt,
		TaskID: input.TaskID, TaskTitle: input.TaskTitle,
		SourceBranch: input.SourceBranch, TargetBranch: input.TargetBranch,
		UserGoal: input.UserGoal, PriorFailure: input.PriorFailure,
	}
}

type dispatchProjection struct {
	Agent      string         `json:"agent"`
	Workspace  string         `json:"workspace"`
	TaskPrompt string         `json:"taskPrompt"`
	OutputPath string         `json:"outputPath"`
	Fallback   MergerDecision `json:"fallback"`
	MaxRetries int            `json:"maxRetries"`
	Label      string         `json:"label"`
	Tools      ToolOverrides  `json:"tools"`
	TimeoutMS  int            `json:"timeoutMs"`
}

func loadFixtures(t *testing.T) []fixtureLine {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	out := []fixtureLine{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
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
	case "MERGER_FALLBACK":
		return MergerFallback
	case "dispatchRequest":
		var raw fixtureInput
		var status, conflicts string
		if err := json.Unmarshal(args[0], &raw); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &status); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[2], &conflicts); err != nil {
			t.Fatal(err)
		}
		call := 0
		runner := RunnerFunc(func([]string, string) (ProcessResult, error) {
			call++
			if fixture.Name == "status and diff rejection degrade" {
				return ProcessResult{}, errors.New("rejected")
			}
			if call == 1 {
				return ProcessResult{Stdout: []byte(status)}, nil
			}
			return ProcessResult{Stdout: []byte(conflicts)}, nil
		})
		var captured DispatchRequest
		dispatcher := DispatcherFunc(func(
			_ context.Context,
			request DispatchRequest,
		) (DispatchResult, error) {
			captured = request
			return fallbackResult(), nil
		})
		DispatchMerger(raw.mergerInput(), Dependencies{
			Runner: runner, Dispatcher: dispatcher,
		})
		return dispatchProjection{
			Agent: captured.Agent, Workspace: captured.Workspace,
			TaskPrompt: captured.TaskPrompt, OutputPath: captured.OutputPath,
			Fallback: captured.Fallback, MaxRetries: captured.MaxRetries,
			Label: captured.Label, Tools: captured.Tools,
			TimeoutMS: captured.TimeoutMS,
		}
	case "verifyMergeResolved":
		var worktree, stdout string
		var reject bool
		if err := json.Unmarshal(args[0], &worktree); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &stdout); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[2], &reject); err != nil {
			t.Fatal(err)
		}
		runner := RunnerFunc(func([]string, string) (ProcessResult, error) {
			if reject {
				return ProcessResult{}, errors.New("grep exploded")
			}
			return ProcessResult{Stdout: []byte(stdout)}, nil
		})
		return VerifyMergeResolved(worktree, runner)
	default:
		t.Fatalf("unknown fixture fn %q", fixture.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 14 {
		t.Fatalf("expected fixture corpus, got %d", len(fixtures))
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Fn+"/"+fixture.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callFixture(t, fixture))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != fixture.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fixture.ArgsJSON, got, fixture.OutJSON)
			}
		})
	}
}
