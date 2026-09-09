package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The real turn boundary must accept outcomes outside the explicit file tools.
// A correct shell-created artifact previously incurred two more inspections and
// a false stop because the session's creation ledger did not know about it.
func TestOrdinaryCompletionDoesNotRequireLocalFileToolActivity(t *testing.T) {
	for _, tc := range []struct {
		name, tool, args, ask, final string
		exists                       bool
	}{
		{"shell artifact", "bash", `{"command":"python3 -c 'import json; json.dump({\"count\":3}, open(\"answer.json\", \"w\"))'"}`, "Save answer.json containing integer count 3.", "Saved answer.json with count 3.", true},
		{"read only", "read", `{"path":"input.txt"}`, "Read input.txt and give its contents without changing any file.", "The input says unchanged.", false},
		{"deletion", "bash", `{"command":"python3 -c 'from pathlib import Path; Path(\"obsolete.txt\").unlink()'"}`, "Delete obsolete.txt and leave input.txt alone.", "Deleted obsolete.txt.", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponse("call_0", tc.tool, tc.args), nil
				},
				func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(tc.final), nil },
			}}
			agent, dir := newTestAgent(t, completer, func(c *Config) { c.Unattended = true; c.Budget = Budget{Wall: time.Hour} })
			for _, name := range []string{"input.txt", "obsolete.txt"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("unchanged"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			events := collect(t, mustSubmit(t, agent, tc.ask))
			if completer.requests() != 2 {
				t.Fatalf("completion bought another model round: %d", completer.requests())
			}
			for _, event := range events {
				if strings.Contains(event.Text, "nothing has been finished") || strings.Contains(event.Text, "carrying on rather than stopping") {
					t.Fatalf("finished result was reopened: %s", event.Text)
				}
			}
			if got, _ := os.ReadFile(filepath.Join(dir, "input.txt")); string(got) != "unchanged" {
				t.Fatal("input file changed")
			}
			if tc.exists {
				var got struct{ Count int }
				data, err := os.ReadFile(filepath.Join(dir, "answer.json"))
				if err != nil {
					t.Fatal(err)
				}
				if json.Unmarshal(data, &got) != nil || got.Count != 3 {
					t.Fatalf("shell artifact incorrect: %s", data)
				}
			}
			if tc.name == "deletion" {
				if _, err := os.Stat(filepath.Join(dir, "obsolete.txt")); !os.IsNotExist(err) {
					t.Fatalf("requested deletion did not happen: %v", err)
				}
			}
			if len(agent.createdList()) != 0 {
				t.Fatal("fixture accidentally supplied a file-tool completion witness")
			}
		})
	}
}

// Running a failed declared check must not clear the prior unmet fingerprint
// through an intermediate done decision, or every repeated ending retries it.
func TestRepeatedFailedDeclaredCheckStopsAtTheEnding(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	s := agent.steward()
	s.hear("Finish the calculation.")
	s.setAcceptanceContract(s.Ask(), "the declared check passes", []string{"false"})
	recordCoveredBaseline(agent, "false")
	first := agent.decideRemains(context.Background(), "The calculation is complete.")
	if first.Verb != DecideCarryOn || !strings.Contains(first.Brief, "false does not pass") {
		t.Fatalf("actual failed check was lost: %+v", first)
	}
	second := agent.decideRemains(context.Background(), "The calculation is complete.")
	if second.Verb != DecideStop || !strings.Contains(second.Reason, "false does not pass") {
		t.Fatalf("repeated failed check was reset by an intermediate done: %+v", second)
	}
}

// Outside the workspace does not mean disposable: a person's requested absolute
// output survives both a completed turn and every factual reason to stop it.
func TestCompletionPreservesRequestedFilesOutsideTheWorkspace(t *testing.T) {
	for _, mode := range []string{"attended", "unattended", "canceled", "budget", "latched stop"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "requested-report.txt")
			args, _ := json.Marshal(map[string]string{"path": path, "content": "requested result"})
			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponse("write-report", "write", string(args)), nil
				},
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return textResponse("Saved the requested report."), nil
				},
			}}
			agent, _ := newTestAgent(t, completer, func(c *Config) {
				c.Unattended = mode != "attended"
				if c.Unattended {
					c.Budget = Budget{USD: 1, Wall: time.Hour}
				}
			})
			if mode == "attended" || mode == "unattended" {
				collect(t, mustSubmit(t, agent, "Write the report to "+path))
				if completer.requests() != 2 {
					t.Fatalf("unexpected approval round: %d", completer.requests())
				}
			} else {
				// Exercise the ending with the creation fact recorded after a
				// successful write; the stop has already been observed.
				if err := os.WriteFile(path, []byte("requested result"), 0600); err != nil {
					t.Fatal(err)
				}
				agent.rememberCreated(fileChange{path: path, shown: path, created: true})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				switch mode {
				case "canceled":
					cancel()
				case "budget":
					agent.mu.Lock()
					agent.usage.CostUSD = 2
					agent.mu.Unlock()
				case "latched stop":
					agent.steward().mu.Lock()
					agent.steward().stopped = "already stopped"
					agent.steward().mu.Unlock()
				}
				if got := agent.decideRemains(ctx, "Saved the requested report."); got.Verb != DecideStop {
					t.Fatalf("stop lost: %+v", got)
				}
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != "requested result" {
				t.Fatalf("completion deleted or changed requested external output: %q, %v", got, err)
			}
		})
	}
}

// A retained branch is context for the main model's report. Without an
// explicit destination contract it does not require an automatic integration
// job or another model call to classify the person's request.
func TestRetainedCompletionDoesNotBuyADestinationClassifier(t *testing.T) {
	completer := &scriptedCompleter{}
	a, _ := newTestAgent(t, completer, func(c *Config) { c.Unattended = true; c.Budget = Budget{Wall: time.Hour} })
	a.steward().hear("Finish the requested work.")
	a.openAcceptance(context.Background(), nil)
	node := landOne(a, TaskDone, "the finished work", "The result is retained on task/result.")
	node.graph.mu.Lock()
	node.branch, node.merge = "task/result", mergeKept
	node.changed = []string{"result.txt"}
	node.graph.mu.Unlock()
	before := a.remainsFor("The result is on task/result.")
	if len(before.Landings) != 1 || before.Landings[0].Retained != "task/result" {
		t.Fatalf("retained location was lost: %+v", before.Landings)
	}
	if got := a.decideRemains(context.Background(), "The result is on task/result."); got.Verb != DecideDone {
		t.Fatalf("retained result became an implicit integration job: %+v", got)
	}
	if completer.requests() != 0 || a.steward().declaredDelivery().Kind != "" {
		t.Fatal("ending classified or invented a destination")
	}
}

// Budget exhaustion wins before the terminal check process is started; keeping
// actual checks does not authorize spending after the run has already stopped.
func TestAnExpiredCompletionDoesNotRunDeclaredChecks(t *testing.T) {
	a, dir := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Unattended = true; c.Budget = Budget{Wall: time.Nanosecond} })
	marker := filepath.Join(dir, "check-ran")
	s := a.steward()
	s.hear("Finish the calculation.")
	s.setAcceptanceContract(s.Ask(), "the declared check passes", []string{"touch " + marker})
	got := a.decideRemains(context.Background(), "Done.")
	if got.Verb != DecideStop {
		t.Fatalf("expired run continued: %+v", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("expired completion ran its check: %v", err)
	}
}
