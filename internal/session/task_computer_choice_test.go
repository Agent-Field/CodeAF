package session

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

func taskComputerChoiceHome(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CODEAF_HOME", root)
}

// DESIGN contracts 1 and 6 require the named computer to receive the task's
// chosen controls; asking for effort must not route a worker on the origin.
func TestANamedComputerReceivesTheEffortChosenForThatTask(t *testing.T) {
	for _, effort := range []string{"best", "cheap"} {
		t.Run(effort, func(t *testing.T) {
			taskComputerChoiceHome(t)
			reached := make(chan string, 1)
			remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
				reached <- spec.NamedEffort
				_ = spec.Store.CompleteRoot("export repaired")
				return RunSummary{Outcome: "done", Result: "export repaired"}
			}}
			a, local := remoteTaskAgent(t, remote, nil)
			if _, _, _, err := a.StartTaskOn(context.Background(), "Repair export", true, "work computer", effort); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-reached:
				if got != effort {
					t.Fatalf("the other computer received effort %q, want %q", got, effort)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the chosen computer did not receive the task")
			}
			waitRemoteRun(t, a)
			if local.didRun() {
				t.Fatal("the task ran on the origin")
			}
		})
	}
}

// DESIGN contracts 1 and 7 require a card's explicit computer to outrank
// both the original proposal and the standing default, through real admission.
func TestAProposalAnswerChoosesTheComputerThroughThePlacementResolver(t *testing.T) {
	for _, via := range []string{"task answer", "question answer", "correction"} {
		for _, computer := range []string{"work computer", "this computer"} {
			t.Run(via+"/"+computer, func(t *testing.T) {
				taskComputerChoiceHome(t)
				reached := make(chan string, 1)
				remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
					reached <- "work computer"
					_ = spec.Store.CompleteRoot("export repaired")
					return RunSummary{Outcome: "done", Result: "export repaired"}
				}}
				a, local := remoteTaskAgent(t, remote, func(c *Config) {
					c.TaskAutoApproveSeconds = 0
					c.AskConsent = true
					if computer == "this computer" {
						if err := config.WriteTaskRunsOn(c.ProfileDir, "dev_0123456789abcdef0123456789abcdef"); err != nil {
							t.Fatal(err)
						}
					}
				})
				var release sync.Once
				t.Cleanup(func() { release.Do(func() { close(local.release) }) })
				args := taskOnOtherComputer()
				args.Computer = ""
				if computer == "this computer" {
					args.Computer = "work computer"
				}
				raw, _ := json.Marshal(args)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				events := watched(a)
				staged := a.stageTask(ctx, raw)
				var id uint64
				for id == 0 {
					select {
					case event := <-events:
						if event.Kind == EventTaskProposal && event.Task != nil {
							id = event.Task.ID
						}
					case <-ctx.Done():
						t.Fatal("the proposal was not shown")
					}
				}
				if via == "task answer" {
					a.ResolveTask(id, TaskAnswer{Approved: true, Computer: computer})
				} else {
					answer := Answer{Kind: QuestionTask, ID: id, Key: "1", Blanks: map[string]string{TaskComputerBlank: computer}}
					if via == "correction" {
						answer.Key = ""
						answer.Change = "also cover an empty export"
					}
					if err := a.ResolveQuestion(answer); err != nil {
						t.Fatal(err)
					}
				}
				result, refused, err := staged.Commit(ctx)
				if err != nil || refused {
					t.Fatalf("proposal = %q, %v, %v", result, refused, err)
				}
				if computer == "work computer" {
					select {
					case <-reached:
					case <-ctx.Done():
						t.Fatal("the card's choice did not reach the other computer")
					}
					if local.didRun() {
						t.Fatal("the origin ran the explicitly remote task")
					}
				} else {
					select {
					case <-local.entered:
					case <-ctx.Done():
						t.Fatal("the card's local choice did not override placement")
					}
					release.Do(func() { close(local.release) })
					select {
					case <-reached:
						t.Fatal("the local choice also ran remotely")
					default:
					}
				}
				waitRemoteRun(t, a)
			})
		}
	}
}

// DESIGN contract 6 requires a corrected card choice to get the same
// correctable unknown-name answer as every other placement entry point.
func TestAnUnknownComputerChosenOnAProposalUsesThePlacementError(t *testing.T) {
	taskComputerChoiceHome(t)
	a, local := remoteTaskAgent(t, nil, func(c *Config) {
		c.TaskAutoApproveSeconds = 0
		c.AskConsent = true
	})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(local.release) }) })
	events := watched(a)
	args := taskOnOtherComputer()
	args.Computer = ""
	raw, _ := json.Marshal(args)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	staged := a.stageTask(ctx, raw)
	for {
		select {
		case ev := <-events:
			if ev.Kind != EventTaskProposal || ev.Task == nil {
				continue
			}
			a.ResolveTask(ev.Task.ID, TaskAnswer{Approved: true, Computer: "missing"})
			result, refused, err := staged.Commit(ctx)
			if err != nil || !refused || !strings.Contains(result, `no paired computer is named "missing"`) || !strings.Contains(result, "work computer") {
				t.Fatalf("proposal correction = %q, %v, %v", result, refused, err)
			}
			if local.didRun() {
				t.Fatal("an unknown choice started local work")
			}
			return
		case <-ctx.Done():
			t.Fatal("the proposal was not shown")
		}
	}
}
