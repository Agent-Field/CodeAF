package run_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// The planner must hear the actual CLI refusal in its next model request.
// A scripted shell loop keeps the word and quoting that caused issue_.go.
func TestAFanOutPlannerIsSentBackFromAPlaceholderCheckAndNoFixIsSpawned(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/issues\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 20; n++ {
		file := filepath.Join(workspace, fmt.Sprintf("issue_%02d.go", n))
		if err := os.WriteFile(file, []byte("package main\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store := startOpenStore(t, "Reverse the issue helpers")
	var observation string
	var plannerCalls atomic.Int32
	planner := &seat{ever: func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		if len(messages) > 0 && strings.HasPrefix(messageContent(messages[0]), "You write one short status sentence") {
			return textReply("run | checking numbered issue helpers in workspace"), nil
		}
		switch plannerCalls.Add(1) {
		case 1:
			command := `for n in $(seq -w 1 20); do plandb add "Reverse issue_$n.go" --parent t-root --description "reverse the string helper in issue_$n.go" --check 'gofmt -l issue_.go' || true; done`
			return toolReply(bashArguments(t, command)), nil
		case 2:
			var words strings.Builder
			for _, message := range messages {
				words.WriteString(messageContent(message))
				words.WriteByte('\n')
			}
			observation = words.String()
			command := "true"
			if strings.Contains(observation, "Nothing was added.") {
				command = `for n in $(seq -w 1 20); do plandb add "Reverse issue_$n.go" --parent t-root --description "reverse the string helper in issue_$n.go" --check "gofmt -l issue_$n.go"; done`
			}
			return toolReply(bashArguments(t, command)), nil
		case 3:
			return toolReply(bashArguments(t, "plandb wait root --agent root || true")), nil
		case 4:
			return toolReply(finishCommand("root", "the numbered parts are complete")), nil
		default:
			return textReply(""), nil
		}
	}}
	factory := func(task plandb.Task) run.Worker {
		if task.ID == store.RootID() {
			return run.NewBashWorker(store, workspace, "test/model", planner)
		}
		if task.Role == plandb.RoleCheck {
			return funcWorker(func(_ context.Context, check plandb.Task) (run.Report, error) {
				for _, command := range check.Checks {
					words := strings.Fields(command)
					if len(words) == 0 {
						continue
					}
					file := words[len(words)-1]
					if _, err := os.Stat(filepath.Join(workspace, file)); os.IsNotExist(err) {
						return run.Report{Result: fmt.Sprintf("does not hold: %s names %s, which does not exist.", command, file), Steps: 1}, nil
					}
				}
				recordDeclaredCheck(t, store, check)
				return run.Report{Result: "holds: gofmt and go test are clean.", Steps: 1}, nil
			})
		}
		return funcWorker(func(_ context.Context, task plandb.Task) (run.Report, error) {
			return run.Report{Result: "finished " + task.Title, Steps: 1}, nil
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	outcome, summary := run.Start(ctx, run.Spec{
		Store: store, Workspace: workspace, Title: "Reverse the issue helpers",
		Brief: "Reverse every numbered issue helper", Slots: 6,
		Limits: run.Limits{ReviewRound: true, StepsPerTask: 9}, Factory: factory,
	})
	for _, want := range []string{"gofmt -l issue_.go", "issue_.go", "issue_01.go", "Nothing was added."} {
		if !strings.Contains(observation, want) {
			excerpt := observation
			if len(excerpt) > 1000 {
				excerpt = excerpt[len(excerpt)-1000:]
			}
			t.Errorf("planner's next observation misses %q; last bytes: %q", want, excerpt)
		}
	}
	parts := 0
	for _, task := range store.Tasks() {
		if strings.HasPrefix(task.Title, "fix: ") {
			t.Errorf("a fix task was spawned: %s", task.Title)
		}
		for _, check := range task.Checks {
			if strings.Contains(check, "issue_.go") {
				t.Errorf("stored check on %s kept the placeholder: %q", task.Title, check)
			}
		}
		if task.ID == store.RootID() || task.Role == plandb.RoleCheck || strings.HasPrefix(task.Title, "fix: ") {
			continue
		}
		parts++
		words := strings.Fields(task.Title)
		file := words[len(words)-1]
		if len(task.Checks) != 1 || task.Checks[0] != "gofmt -l "+file {
			t.Errorf("part %s has checks %q, want its own %s", task.Title, task.Checks, file)
		}
	}
	if parts != 20 {
		t.Errorf("part tasks = %d, want 20", parts)
	}
	if outcome != run.OutcomeDone {
		t.Errorf("outcome = %q, want done; summary=%+v", outcome, summary)
	}
}

func TestACheckOnAFileNothingMakesStartsNoFix(t *testing.T) {
	for _, tc := range []struct {
		name, file, check string
		parts             int
		seed              []string
	}{
		{"shared missing file", "shared.go", "gofmt -l shared.go", 3, nil},
		{"number missing", "issue_.go", "gofmt -l issue_.go", 1, []string{"issue_01.go", "issue_02.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			for _, file := range tc.seed {
				if err := os.WriteFile(filepath.Join(workspace, file), []byte("package main\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			store := runOpenStore(t)
			leaves := make([]plandb.TaskSpec, tc.parts)
			for i := range leaves {
				leaves[i] = plandb.TaskSpec{ID: fmt.Sprintf("part%d", i), Title: fmt.Sprintf("part %d", i), Description: "finish the work", Checks: []string{tc.check}}
			}
			seat := newFakeSeat()
			seat.actions["root"] = splitRoot(t, store, leaves...)
			factory := func(task plandb.Task) run.Worker {
				if task.Role == plandb.RoleCheck {
					return funcWorker(func(_ context.Context, check plandb.Task) (run.Report, error) {
						recordDeclaredCheck(t, store, check)
						return run.Report{Result: "does not hold: " + tc.check + " names " + tc.file + ", which does not exist.", Steps: 1}, nil
					})
				}
				return seat.workerFor(task)
			}
			outcome := run.NewSupervisor(store, workspace, 4, run.Limits{ReviewRound: true}, factory).Run(runContext(t))
			if outcome != run.OutcomeDone {
				t.Fatalf("outcome = %q, want done", outcome)
			}
			for _, task := range store.Tasks() {
				if strings.HasPrefix(task.Title, "fix: ") {
					t.Errorf("a fix task was spawned: %s", task.Title)
				}
			}
			for _, leaf := range leaves {
				notes := store.Notes(leaf.ID, 10)
				wantFinding := tc.check + " names " + tc.file + ", which does not exist."
				wantStop := "No fix was started: the check names " + tc.file + ", a file nothing in this run makes."
				if len(notes) != 2 || notes[0].Body != wantFinding || notes[1].Body != wantStop || notes[0].Agent != "check" || notes[1].Agent != "check" {
					t.Errorf("notes on %s = %+v, want finding then %q", leaf.ID, notes, wantStop)
				}
			}
		})
	}
}

func TestAMissingFileOnlyOnePartsCheckNamesStillGetsAFix(t *testing.T) {
	assertMissingCheckGetsFixes(t, []plandb.TaskSpec{{ID: "part1", Title: "write the guide", Description: "write the readme", Checks: []string{"test -f README.md"}}}, "does not hold: README.md does not exist.")
}

func TestAMissingFileAPartNamesInBackticksStillGetsItsFix(t *testing.T) {
	assertMissingCheckGetsFixes(t, []plandb.TaskSpec{
		{ID: "part1", Title: "write the helpers", Description: "write `shared.go` with the helpers.", Checks: []string{"gofmt -l shared.go"}},
		{ID: "part2", Title: "use the helpers", Description: "use the helpers after they land", Checks: []string{"gofmt -l shared.go"}},
	}, "does not hold: shared.go does not exist.")
}

func TestASharedPackageCheckStillGetsItsFixes(t *testing.T) {
	assertMissingCheckGetsFixes(t, []plandb.TaskSpec{
		{ID: "part1", Title: "first package part", Checks: []string{"go test ./internal/rank"}},
		{ID: "part2", Title: "second package part", Checks: []string{"go test ./internal/rank"}},
	}, "does not hold: go test ./internal/rank: no such directory.")
}

// A real finding on an individual output or a package still buys its fix.
func assertMissingCheckGetsFixes(t *testing.T, leaves []plandb.TaskSpec, finding string) {
	t.Helper()
	store := runOpenStore(t)
	seat := newFakeSeat()
	seat.actions["root"] = splitRoot(t, store, leaves...)
	factory := func(task plandb.Task) run.Worker {
		if task.Role == plandb.RoleCheck {
			return funcWorker(func(_ context.Context, check plandb.Task) (run.Report, error) {
				recordDeclaredCheck(t, store, check)
				return run.Report{Result: finding, Steps: 1}, nil
			})
		}
		return seat.workerFor(task)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if outcome := run.NewSupervisor(store, t.TempDir(), 1, run.Limits{ReviewRound: true}, factory).Run(ctx); outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want done", outcome)
	}
	for _, leaf := range leaves {
		if fixes := tasksTitled(store, "fix: "+leaf.Title); len(fixes) != 1 {
			t.Errorf("fix tasks for %s = %d, want one", leaf.Title, len(fixes))
		}
	}
}
