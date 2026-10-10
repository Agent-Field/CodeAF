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
	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/config"
)

// automationCall is one scripted `automation` call.
func automationCall(id string, body map[string]any) step {
	raw, _ := json.Marshal(body)
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, "automation", string(raw)), nil
	}
}

func automationStoreFor(t *testing.T) *automation.Store {
	t.Helper()
	store, err := automation.Open(filepath.Join(t.TempDir(), "automations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// automationAgent is a conversation somebody is in, with an automations store.
func automationAgent(t *testing.T, completer Completer, store *automation.Store, mutate func(*Config)) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Automations = &Automations{Store: store, Zone: "UTC"}
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 0
		if mutate != nil {
			mutate(config)
		}
	})
	return agent
}

// drainAnsweringAutomation drains a turn, answering every automation card.
func drainAnsweringAutomation(t *testing.T, events <-chan Event, answer func(Event)) []Event {
	t.Helper()
	var collected []Event
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				return collected
			}
			collected = append(collected, event)
			if event.Kind == EventAutomationProposal && answer != nil {
				answer(event)
			}
		case <-deadline:
			t.Fatalf("the turn never finished; events so far: %v", kinds(collected))
			return nil
		}
	}
}

func automationBeltHas(tools []string, name string) bool {
	for _, tool := range tools {
		if tool == name {
			return true
		}
	}
	return false
}

func automationBelt(a *Agent) []string {
	var names []string
	for _, tool := range a.belt() {
		names = append(names, tool.Name)
	}
	return names
}

// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN, and NOTHING AN AUTOMATION
// DOES MAY ARM ANOTHER: the verb is on a conversation's belt only when a store
// is behind it, and never on a task's or a run's; the report is on a run's only.
func TestTheAutomationVerbsAreWhereTheyCanWork(t *testing.T) {
	store := automationStoreFor(t)
	cases := []struct {
		name           string
		config         Config
		verb, reported bool
	}{
		{"no store", Config{Workspace: t.TempDir()}, false, false},
		{"a conversation", Config{Workspace: t.TempDir(), Automations: &Automations{Store: store}}, true, false},
		{"a task", Config{Workspace: t.TempDir(), Automations: &Automations{Store: store}, InTask: true}, false, false},
		{"a run", Config{Workspace: t.TempDir(), Automations: &Automations{Store: store}, AutomationRun: &AutomationRun{}}, false, true},
	}
	for _, c := range cases {
		names := automationBelt(&Agent{config: c.config})
		if automationBeltHas(names, "automation") != c.verb {
			t.Errorf("%s: automation on the belt = %v, want %v", c.name, !c.verb, c.verb)
		}
		if automationBeltHas(names, "automation_report") != c.reported {
			t.Errorf("%s: automation_report on the belt = %v, want %v", c.name, !c.reported, c.reported)
		}
	}
}

func TestSavingAnAutomationKeepsWhatTheCardShowed(t *testing.T) {
	store := automationStoreFor(t)
	completer := &scriptedCompleter{steps: []step{
		automationCall("a1", map[string]any{
			"op": "propose", "title": "weekly update", "words": "every Monday at 9, draft the weekly update",
			"when": map[string]any{"every": "0 9 * * 1"}, "do": "draft the weekly update from the git log",
		}),
		finalText("saved"),
	}}
	agent := automationAgent(t, completer, store, nil)
	events, err := agent.Submit(context.Background(), "every Monday at 9, draft the weekly update")
	if err != nil {
		t.Fatal(err)
	}
	collected := drainAnsweringAutomation(t, events, func(event Event) {
		agent.ResolveAutomation(event.Automation.ID, AutomationAnswer{Save: true})
	})
	all, _ := store.List()
	if len(all) != 1 {
		t.Fatalf("a yes saved %d automations", len(all))
	}
	saved := all[0]
	if saved.Words != "every Monday at 9, draft the weekly update" || saved.Title != "weekly update" || saved.Kind() != automation.KindWork {
		t.Fatalf("saved %+v", saved)
	}
	if saved.Workspace == "" || saved.Origin.SessionID == "" || saved.Schedule.Zone != "UTC" {
		t.Fatalf("saved without its place: %+v", saved)
	}
	card, found := firstOfKind(collected, EventAutomationProposal)
	if !found || card.Automation.WhenWords != "every Monday at 09:00" || !strings.Contains(card.Automation.Exact, "cron 0 9 * * 1") || card.Automation.Next.IsZero() {
		t.Fatalf("the card did not carry the schedule: %+v", card.Automation)
	}
	if len(card.Automation.Options) != 3 || card.Automation.Options[1].Label != "Save and run it now" {
		t.Fatalf("the card's answers are %+v", card.Automation.Options)
	}
	if update, found := firstOfKind(collected, EventAutomationUpdate); !found || update.Automation.Update != "saved" {
		t.Fatalf("nothing reported it saved: %v", kinds(collected))
	}
	if output := toolOutput(t, collected, "automation"); !strings.Contains(output, "saved "+saved.ID) || !strings.Contains(output, "now: ") {
		t.Fatalf("tool result = %q", output)
	}
}

// SAVE AND RUN IT NOW saves it, then hands the work back to do in this turn,
// where any approval it needs reaches the person.
func TestSaveAndRunItNowHandsTheWorkBack(t *testing.T) {
	store := automationStoreFor(t)
	completer := &scriptedCompleter{steps: []step{
		automationCall("a1", map[string]any{
			"op": "propose", "title": "tidy branches", "when": map[string]any{"every": "1d"}, "do": "delete merged local branches",
		}),
		finalText("ok"),
	}}
	agent := automationAgent(t, completer, store, nil)
	events, _ := agent.Submit(context.Background(), "every day, tidy my merged branches")
	collected := drainAnsweringAutomation(t, events, func(event Event) {
		agent.ResolveAutomation(event.Automation.ID, AutomationAnswer{Save: true, RunNow: true})
	})
	if all, _ := store.List(); len(all) != 1 {
		t.Fatalf("save and run saved %d", len(all))
	}
	if output := toolOutput(t, collected, "automation"); !strings.Contains(output, "Do the work ONCE, here, now: delete merged local branches") {
		t.Fatalf("tool result = %q", output)
	}
}

// A REMINDER OFFERS NO "RUN IT NOW": its whole content is the moment.
func TestAReminderCardHasNoRunNow(t *testing.T) {
	reminder := automation.Automation{Action: automation.Action{Say: "leave"}, Schedule: automation.Schedule{At: time.Now().Add(time.Hour)}}
	for _, option := range AutomationOptions(reminder) {
		if option.Key == AutomationRunNowKey {
			t.Fatal("a reminder's card offers to run it now")
		}
	}
	isolated := automation.Automation{Action: automation.Action{Do: "x"}, Worktree: true}
	if AutomationRunsNow(isolated) {
		t.Fatal("worktree work offers an attended run in the checkout")
	}
}

func TestAutomationCorrectionAndRefusals(t *testing.T) {
	store := automationStoreFor(t)
	t.Run("a correction saves nothing and sends the words back", func(t *testing.T) {
		completer := &scriptedCompleter{steps: []step{
			automationCall("a1", map[string]any{"op": "propose", "title": "leave", "when": map[string]any{"in": "2h"}, "say": "time to leave"}),
			finalText("ok"),
		}}
		agent := automationAgent(t, completer, store, nil)
		events, _ := agent.Submit(context.Background(), "remind me in 2h to leave")
		collected := drainAnsweringAutomation(t, events, func(event Event) {
			// The question lane is how every surface answers.
			if err := agent.ResolveQuestion(Answer{Kind: QuestionAutomation, ID: event.Automation.ID, Change: "make it 3h"}); err != nil {
				t.Errorf("resolve: %v", err)
			}
		})
		if all, _ := store.List(); len(all) != 0 {
			t.Fatalf("a correction saved %d", len(all))
		}
		if output := toolOutput(t, collected, "automation"); !strings.Contains(output, "the person changed it: make it 3h") {
			t.Fatalf("tool result = %q", output)
		}
	})
	t.Run("a key on the question lane saves", func(t *testing.T) {
		completer := &scriptedCompleter{steps: []step{
			automationCall("a1", map[string]any{"op": "propose", "title": "leave", "when": map[string]any{"in": "2h"}, "say": "time to leave"}),
			finalText("ok"),
		}}
		agent := automationAgent(t, completer, store, nil)
		events, _ := agent.Submit(context.Background(), "remind me in 2h to leave")
		drainAnsweringAutomation(t, events, func(event Event) {
			if err := agent.ResolveQuestion(Answer{Kind: QuestionAutomation, ID: event.Automation.ID, Key: AutomationSaveKey}); err != nil {
				t.Errorf("resolve: %v", err)
			}
		})
		if all, _ := store.List(); len(all) != 1 {
			t.Fatalf("key 1 saved %d", len(all))
		}
	})
	t.Run("nobody watching", func(t *testing.T) {
		completer := &scriptedCompleter{steps: []step{
			automationCall("a1", map[string]any{"op": "propose", "title": "leave", "when": map[string]any{"in": "2h"}, "say": "time to leave"}),
			finalText("ok"),
		}}
		agent := automationAgent(t, completer, automationStoreFor(t), func(c *Config) { c.AskConsent = false })
		events, _ := agent.Submit(context.Background(), "remind me")
		collected := drainAnsweringAutomation(t, events, nil)
		if output := toolOutput(t, collected, "automation"); !strings.Contains(output, "nobody is here to say yes") {
			t.Fatalf("tool result = %q", output)
		}
	})
	t.Run("a moment that has passed", func(t *testing.T) {
		completer := &scriptedCompleter{steps: []step{
			automationCall("a1", map[string]any{"op": "propose", "title": "leave", "when": map[string]any{"at": time.Now().Add(-2 * time.Hour).Format(time.RFC3339)}, "say": "time to leave"}),
			finalText("ok"),
		}}
		agent := automationAgent(t, completer, automationStoreFor(t), nil)
		events, _ := agent.Submit(context.Background(), "remind me")
		collected := drainAnsweringAutomation(t, events, nil)
		if output := toolOutput(t, collected, "automation"); !strings.Contains(output, "has already passed") {
			t.Fatalf("tool result = %q", output)
		}
	})
}

func TestManagingAutomationsByTheirIDs(t *testing.T) {
	store := automationStoreFor(t)
	saved, err := store.Create(automation.Automation{Title: "ci", Workspace: t.TempDir(), Schedule: automation.Schedule{Every: "5m"},
		Look: &automation.Look{Command: "true", Condition: "it failed"}, Action: automation.Action{Say: "CI is red"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := automationAgent(t, &scriptedCompleter{}, store, nil)
	call := func(body map[string]any) string {
		raw, _ := json.Marshal(body)
		out, _, err := agent.automationTool(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := call(map[string]any{"op": "list"}); !strings.Contains(out, saved.ID+" · ci · watch · every 5 minutes") {
		t.Fatalf("list = %q", out)
	}
	if out := call(map[string]any{"op": "pause", "id": saved.ID}); !strings.HasPrefix(out, "paused") {
		t.Fatalf("pause = %q", out)
	}
	if got, _ := store.Get(saved.ID); got.Status != automation.StatusPaused {
		t.Fatalf("status %s", got.Status)
	}
	if out := call(map[string]any{"op": "resume", "id": saved.ID}); !strings.HasPrefix(out, "resumed") {
		t.Fatalf("resume = %q", out)
	}
	if out := call(map[string]any{"op": "run", "id": saved.ID}); !strings.Contains(out, "to run now") {
		t.Fatalf("run = %q", out)
	}
	if out := call(map[string]any{"op": "history", "id": saved.ID}); !strings.Contains(out, "queued") {
		t.Fatalf("history = %q", out)
	}
	if out := call(map[string]any{"op": "delete", "id": saved.ID}); !strings.HasPrefix(out, "deleted") {
		t.Fatalf("delete = %q", out)
	}
	if out := call(map[string]any{"op": "pause", "id": "nope"}); !strings.Contains(out, "no automation with that id") {
		t.Fatalf("an unknown id = %q", out)
	}
}

// ── the runner ──────────────────────────────────────────────────────────────

// A COMMAND LOOK IS A SHELL WITHOUT THE PROVIDER KEYS, and a failing command is
// evidence: its exit status rides with its output.
func TestACommandLookKeepsTheExitAndHidesTheKeys(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-secret-do-not-leak")
	sight, err := automationLookCommand(context.Background(), t.TempDir(), `echo "key=[$OPENROUTER_API_KEY]"; exit 3`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sight.Text, "sk-secret") {
		t.Fatalf("the look saw the provider key: %q", sight.Text)
	}
	if !strings.Contains(sight.Text, "key=[]") || !strings.Contains(sight.Text, "(exit status 3)") {
		t.Fatalf("sight = %q", sight.Text)
	}
}

func TestAFilesLookSaysWhatChangedSinceTheLastLook(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "invoices", "2026"), 0o700))
	must(os.WriteFile(filepath.Join(root, "invoices", "2026", "a.pdf"), []byte("a"), 0o600))
	must(os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o600))

	first, err := automationLookFiles(root, "invoices/**/*.pdf", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.Text, "1 file(s) match") || !strings.Contains(first.Text, "first look") || first.Memo == "" {
		t.Fatalf("first look = %q / memo %q", first.Text, first.Memo)
	}
	must(os.WriteFile(filepath.Join(root, "invoices", "2026", "b.pdf"), []byte("b"), 0o600))
	must(os.WriteFile(filepath.Join(root, "invoices", "2026", "a.pdf"), []byte("a2"), 0o600))
	second, err := automationLookFiles(root, "invoices/**/*.pdf", first.Memo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.Text, "+ invoices/2026/b.pdf (new)") || !strings.Contains(second.Text, "~ invoices/2026/a.pdf (changed)") {
		t.Fatalf("second look = %q", second.Text)
	}
	third, _ := automationLookFiles(root, "invoices/**/*.pdf", second.Memo)
	if !strings.Contains(third.Text, "Nothing changed") {
		t.Fatalf("third look = %q", third.Text)
	}
	if _, err := automationLookFiles(root, "../elsewhere/*", ""); err == nil {
		t.Fatal("a glob outside the project was read")
	}
}

func TestGlobsSpanFoldersOnlyWithDoubleStar(t *testing.T) {
	cases := []struct {
		glob, name string
		match      bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "cmd/main.go", false},
		{"**/*.go", "cmd/main.go", true},
		{"**/*.go", "main.go", true},
		{"cmd/**", "cmd/a/b/c.txt", true},
		{"invoices/*.pdf", "invoices/2026/a.pdf", false},
	}
	for _, c := range cases {
		if got := automationGlobMatch(c.glob, c.name); got != c.match {
			t.Errorf("%q vs %q = %v, want %v", c.glob, c.name, got, c.match)
		}
	}
}

func TestTheVerdictIsTheFirstWholeWord(t *testing.T) {
	cases := map[string]string{
		"yes — the last run on main failed": "yes",
		"No. Nothing changed.":              "no",
		"yesterday the run failed":          "yesterday",
		"unknown: the output was empty":     "unknown",
	}
	for reply, want := range cases {
		if word, _ := automationVerdictWords(reply); word != want {
			t.Errorf("%q reads %q, want %q", reply, word, want)
		}
	}
	if _, line := automationVerdictWords("yes — the last run on main failed"); line != "the last run on main failed" {
		t.Errorf("line = %q", line)
	}
}

func TestOnlyARefusalForWantOfThePersonStopsARun(t *testing.T) {
	stops := []string{
		"refused in a task: bash default — nobody to ask",
		"needs approval but no resolver is attached: default",
		"this needs the person to say yes, and nobody is watching",
	}
	for _, line := range stops {
		if !automationNeedsPerson(line) {
			t.Errorf("%q should stop the run", line)
		}
	}
	for _, line := range []string{"denied by approval rule: rm -rf", "file not found", "exit status 1"} {
		if automationNeedsPerson(line) {
			t.Errorf("%q should not stop the run", line)
		}
	}
}

// WORK SAYS HOW IT WENT, OR IT IS INCOMPLETE. A run that reports done is done;
// one that only talks is handed back with no outcome — the clock reads that as
// incomplete — and its last words are detail, never the result.
func TestWorkOutcomesComeFromTheReport(t *testing.T) {
	store := automationStoreFor(t)
	item, err := store.Create(automation.Automation{Title: "weekly", Workspace: t.TempDir(), Schedule: automation.Schedule{Every: "1d"}, Action: automation.Action{Do: "draft it"}})
	if err != nil {
		t.Fatal(err)
	}
	run := func(steps ...step) automation.Report {
		t.Helper()
		runner := NewAutomationRunner(func(workspace string) (Config, error) {
			return Config{Workspace: workspace, Model: "test/model", System: "SYSTEM"}, nil
		}, store.Root()).(*automationRunner)
		runner.child = func(cfg Config) (*Agent, error) { return newAgent(cfg, &scriptedCompleter{steps: steps}) }
		report, err := runner.Work(context.Background(), item, automation.Run{ID: 7, AutomationID: item.ID}, "")
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	reportCall := func(status, summary string) step {
		raw, _ := json.Marshal(map[string]string{"status": status, "summary": summary})
		return func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("r1", "automation_report", string(raw)), nil
		}
	}
	done := run(reportCall("done", "drafted the update\nit is in notes/"), finalText("ok"))
	if done.Outcome != automation.OutcomeDone || done.Line != "drafted the update" || !strings.Contains(done.Transcript, filepath.Join("runs", item.ID, "7")) {
		t.Fatalf("a reported run = %+v", done)
	}
	quiet := run(finalText("I'll start by reading the git log."))
	if quiet.Outcome != "" || quiet.Line != "" || !strings.Contains(quiet.Detail, "I'll start by") {
		t.Fatalf("an unreported run = %+v", quiet)
	}
}

// THE PERSON'S DAILY LIMIT HOLDS OVER A RUN OF WORK. Once today's spending has
// reached it, a run does not start — no session is opened and nothing is spent
// — and it comes back as the person's call, naming the limit and the door that
// changes it.
func TestAWorkRunDoesNotStartPastTheDailyLimit(t *testing.T) {
	store := automationStoreFor(t)
	item, err := store.Create(automation.Automation{Title: "weekly", Workspace: t.TempDir(), Schedule: automation.Schedule{Every: "1d"}, Action: automation.Action{Do: "draft it"}})
	if err != nil {
		t.Fatal(err)
	}
	profile := t.TempDir()
	if err := config.WriteDailyBudgetUSD(profile, 1.00); err != nil {
		t.Fatal(err)
	}
	held := automationSpentToday
	t.Cleanup(func() { automationSpentToday = held })
	automationSpentToday = func() float64 { return 1.25 }

	runner := NewAutomationRunner(func(workspace string) (Config, error) {
		return Config{Workspace: workspace, Model: "test/model", System: "SYSTEM", ProfileDir: profile}, nil
	}, store.Root()).(*automationRunner)
	runner.child = func(Config) (*Agent, error) {
		t.Fatal("a run past the daily limit opened a session")
		return nil, nil
	}
	report, err := runner.Work(context.Background(), item, automation.Run{ID: 8, AutomationID: item.ID}, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != automation.OutcomeYourCall || !strings.Contains(report.Line, "today's spending limit is reached") ||
		!strings.Contains(report.Line, "/budget day changes it") {
		t.Fatalf("a run past the daily limit = %+v", report)
	}

	// AND UNDER IT, THE RUN GOES AHEAD.
	automationSpentToday = func() float64 { return 0.25 }
	opened := false
	runner.child = func(cfg Config) (*Agent, error) {
		opened = true
		return newAgent(cfg, &scriptedCompleter{steps: []step{finalText("ok")}})
	}
	if _, err := runner.Work(context.Background(), item, automation.Run{ID: 9, AutomationID: item.ID}, ""); err != nil {
		t.Fatal(err)
	}
	if !opened {
		t.Fatal("a run under the daily limit never started")
	}
}
