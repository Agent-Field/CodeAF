package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

func TestTaskDailyBudgetAdmission(t *testing.T) {
	for _, road := range []string{"typed", "proposal", "bash"} {
		for _, choice := range []string{"stop", "raise"} {
			t.Run(road+"/"+choice, func(t *testing.T) {
				t.Setenv("CODEAF_TASK_BELT", "")
				if road == "bash" {
					t.Setenv("CODEAF_TASK_BELT", "bash")
					registerBeltRunEngine(t, newBeltRunDouble("done"))
				}
				profile, place := t.TempDir(), t.TempDir()
				ledger := filepath.Join(t.TempDir(), "usage.jsonl")
				if err := config.WriteDailyBudgetUSD(profile, 0.01); err != nil {
					t.Fatal(err)
				}
				now := time.Now()
				if road != "proposal" {
					recordUsage(t, ledger, UsageLine{At: now, Day: localDay(now), Calls: 1, USD: 0.011})
				}
				client := &scriptedCompleter{}
				agent, _ := newTestAgent(t, client, func(c *Config) {
					c.ProfileDir, c.usageLedger = profile, ledger
					c.Workspace = newTestRepo(t)
					c.Place = Place{Dir: place}
					c.SessionFile = filepath.Join(place, placeTranscript)
					c.AskConsent = true
					c.TaskAutoApproveSeconds = 0
				})
				if road == "proposal" {
					// Interactive proposals wait for the answer instead of headless auto-approval.
					agent.hub = newEventHub()
				}
				graph := stubbedGraph(agent, func(n *TaskNode) { n.graph.complete(n, TaskDone) })
				questions, stop := agent.WatchQuestions()
				defer stop()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				type outcome struct {
					id   uint64
					text string
					err  error
				}
				result := make(chan outcome, 1)
				go func() {
					if road == "proposal" {
						args, _ := json.Marshal(taskArguments{Title: "Document main", Summary: "Add the comment", Brief: "add a one-line doc comment to main.go\n" + taskBriefMark, Deliverable: "main.go", Acceptance: "main has a doc comment"})
						text, _, err := agent.proposeTask(ctx, args)
						result <- outcome{text: text, err: err}
						return
					}
					id, _, _, err := agent.StartTask(ctx, "add a one-line doc comment to main.go", true)
					result <- outcome{id: id, err: err}
				}()
				var q Question
				deadline := time.After(5 * time.Second)
			asking:
				for {
					select {
					case event := <-questions:
						if event.Kind != EventQuestion || event.Question == nil {
							continue
						}
						if event.Question.Kind == QuestionTask {
							// The day can cross its limit while an approval card is open.
							recordUsage(t, ledger, UsageLine{At: now, Day: localDay(now), Calls: 1, USD: 0.011})
							agent.ResolveTask(event.Question.ID, TaskAnswer{Approved: true})
							continue
						}
						if event.Question.Kind == QuestionDailyBudget {
							q = *event.Question
							break asking
						}
					case got := <-result:
						t.Fatalf("task admission returned before a daily-budget card: %+v", got)
					case <-deadline:
						t.Fatal("no daily-budget card")
					}
				}
				if admitted(graph) != 0 || len(agent.PlanTasks()) != 0 || client.requests() != 0 {
					t.Fatal("work started before the daily-budget answer")
				}
				if q.Options[0].Label != "Raise to $0.02" || q.Options[1].Label != "Stop for today" {
					t.Fatalf("wrong choices: %+v", q.Options)
				}
				select {
				case got := <-result:
					t.Fatalf("premature started receipt: %+v", got)
				default:
				}
				key := "2"
				if choice == "raise" {
					key = "1"
				}
				if err := agent.ResolveQuestion(Answer{Kind: q.Kind, ID: q.ID, Key: key}); err != nil {
					t.Fatal(err)
				}
				var got outcome
				select {
				case got = <-result:
				case <-deadline:
					t.Fatal("task start did not settle")
				}
				if choice == "stop" {
					refusal := got.text
					if got.err != nil {
						refusal = got.err.Error()
					}
					if refusal != DailySpendAction(0.01) || got.id != 0 {
						t.Fatalf("stop = %+v, want exact daily refusal", got)
					}
					if admitted(graph) != 0 || len(agent.PlanTasks()) != 0 || client.requests() != 0 {
						t.Fatal("stop admitted work")
					}
				} else {
					if got.err != nil {
						t.Fatal(got.err)
					}
					if road == "bash" {
						if len(agent.PlanTasks()) != 1 {
							t.Fatal("raise did not start the run")
						}
					} else if admitted(graph) != 1 {
						t.Fatal("raise did not admit exactly one task")
					}
					if daily, err := DailySpendAt(profile, now, ledger); err != nil || daily.Limit != 0.02 {
						t.Fatalf("daily = %+v, %v", daily, err)
					}
				}
			})
		}
	}
}

func TestTaskDailyBudgetWaitEndsWithCaller(t *testing.T) {
	for _, ending := range []string{"cancel", "close"} {
		t.Run(ending, func(t *testing.T) {
			profile := t.TempDir()
			ledger := filepath.Join(t.TempDir(), "usage.jsonl")
			if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			recordUsage(t, ledger, UsageLine{At: now, Day: localDay(now), Calls: 1, USD: 1.01})
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.ProfileDir, c.usageLedger = profile, ledger })
			questions, stop := agent.WatchQuestions()
			defer stop()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			go func() { _, _, _, err := agent.StartTask(ctx, "add a doc comment", true); result <- err }()
			deadline := time.After(5 * time.Second)
			var q Question
		asking:
			for {
				select {
				case event := <-questions:
					if event.Kind == EventQuestion && event.Question != nil && event.Question.Kind == QuestionDailyBudget {
						q = *event.Question
						break asking
					}
				case err := <-result:
					t.Fatalf("returned before asking: %v", err)
				case <-deadline:
					t.Fatal("no daily-budget question")
				}
			}
			want := context.Canceled
			if ending == "close" {
				want = errAgentClosed
				if err := agent.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case err := <-result:
				if err != want {
					t.Fatalf("ending = %v, want %v", err, want)
				}
			case <-deadline:
				t.Fatal("task admission outlived its caller")
			}
			if len(agent.OpenQuestions()) != 0 {
				t.Fatal("budget card survived its caller")
			}
			if err := agent.ResolveQuestion(Answer{Kind: q.Kind, ID: q.ID, Key: "1"}); err == nil {
				t.Fatal("stale raise resumed a cancelled task")
			}
			if daily, err := DailySpendAt(profile, now, ledger); err != nil || daily.Limit != 1 {
				t.Fatalf("cancelled task raised the limit: %+v, %v", daily, err)
			}
		})
	}
}
