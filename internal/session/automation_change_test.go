package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
)

// finishedReminder is a one-time reminder that has already gone off, which is
// what "move my reminder to 5pm" usually finds: the store's clock is put three
// hours back to make it and take it, then given back to the real one.
func finishedReminder(t *testing.T, store *automation.Store) automation.Automation {
	t.Helper()
	past := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	store.SetClock(func() time.Time { return past })
	made, err := store.Create(automation.Automation{
		Title: "drink water", Words: "remind me in an hour to drink water", Workspace: t.TempDir(),
		Schedule: automation.Schedule{At: past.Add(time.Hour)}, Action: automation.Action{Say: "drink water"},
	})
	if err != nil {
		t.Fatal(err)
	}
	due, err := store.Due(past.Add(2 * time.Hour))
	if err != nil || len(due) != 1 {
		t.Fatalf("due = %v, %v", due, err)
	}
	if _, err := store.Take(due[0], past.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	store.SetClock(time.Now)
	finished, err := store.Get(made.ID)
	if err != nil || finished.Status != automation.StatusFinished {
		t.Fatalf("the reminder did not finish: %+v, %v", finished, err)
	}
	return finished
}

// nextFivePM is the "5pm" a person means: today's, or tomorrow's once today's
// has gone.
func nextFivePM(now time.Time) time.Time {
	at := time.Date(now.Year(), now.Month(), now.Day(), 17, 0, 0, 0, now.Location())
	if !at.After(now.Add(time.Minute)) {
		at = at.AddDate(0, 0, 1)
	}
	return at
}

// "MOVE MY REMINDER TO 5PM" CHANGES THE ONE THERE IS. The card is the change
// card, a yes goes through [automation.Store.Update], and the finished reminder
// wakes at its new moment — rather than a second reminder being proposed beside
// a finished one, which was the only door before `change`.
func TestChangingAFinishedReminderWakesItAtItsNewTime(t *testing.T) {
	store := automationStoreFor(t)
	reminder := finishedReminder(t, store)
	fivePM := nextFivePM(time.Now())
	completer := &scriptedCompleter{steps: []step{
		automationCall("a1", map[string]any{
			"op": "change", "id": reminder.ID, "when": map[string]any{"at": fivePM.Format(time.RFC3339)},
		}),
		finalText("moved it to 5pm"),
	}}
	agent := automationAgent(t, completer, store, nil)
	events, err := agent.Submit(context.Background(), "move my water reminder to 5pm")
	if err != nil {
		t.Fatal(err)
	}
	collected := drainAnsweringAutomation(t, events, func(event Event) {
		if err := agent.ResolveQuestion(Answer{Kind: QuestionAutomation, ID: event.Automation.ID, Key: AutomationSaveKey}); err != nil {
			t.Errorf("resolve: %v", err)
		}
	})

	card, found := firstOfKind(collected, EventAutomationProposal)
	if !found {
		t.Fatalf("no card was raised: %v", kinds(collected))
	}
	notice := *card.Automation
	if notice.Automation.ID != reminder.ID || notice.Automation.Action.Say != "drink water" {
		t.Fatalf("the card is not the reminder that exists: %+v", notice.Automation)
	}
	if head := AutomationQuestion(notice).Head; head != AutomationChangeHead {
		t.Fatalf("the card is headed %q, want %q", head, AutomationChangeHead)
	}
	if len(notice.Options) != 2 || notice.Options[0].Key != AutomationSaveKey || notice.Options[1].Key != AutomationNoKey ||
		notice.Options[0].Consequence != "The automation runs this way from its next time." {
		t.Fatalf("the change card's answers are %+v", notice.Options)
	}
	if !notice.Next.Equal(fivePM) {
		t.Fatalf("the card says it next runs %v, want %v", notice.Next, fivePM)
	}

	all, _ := store.List()
	if len(all) != 1 {
		t.Fatalf("a change left %d automations, want the one it changed", len(all))
	}
	got := all[0]
	if got.ID != reminder.ID || got.Status != automation.StatusActive {
		t.Fatalf("the reminder is %s %s, want %s active", got.ID, got.Status, reminder.ID)
	}
	if !got.Next.Equal(fivePM) || !got.Schedule.At.Equal(fivePM) {
		t.Fatalf("the reminder next runs %v (at %v), want %v", got.Next, got.Schedule.At, fivePM)
	}
	if got.Title != "drink water" || got.Action.Say != "drink water" || got.Words != reminder.Words || got.Workspace != reminder.Workspace {
		t.Fatalf("a change of time changed more than the time: %+v", got)
	}
	if update, found := firstOfKind(collected, EventAutomationUpdate); !found || update.Automation.Update != "changed" {
		t.Fatalf("nothing reported it changed: %v", kinds(collected))
	}
	if output := toolOutput(t, collected, "automation"); !strings.Contains(output, "changed "+reminder.ID) || !strings.Contains(output, "next run: ") {
		t.Fatalf("tool result = %q", output)
	}
}

// A CHANGE THE PERSON DOES NOT SAVE CHANGES NOTHING, and a correction is sent
// back in their words for the model to change it again with.
func TestAnUnsavedChangeLeavesTheAutomationAsItWas(t *testing.T) {
	for _, c := range []struct {
		name   string
		answer Answer
		says   string
	}{
		{"don't save", Answer{Key: AutomationNoKey}, "nothing was changed: the person said no."},
		{"a correction", Answer{Change: "make it 6 instead"}, "the person changed it: make it 6 instead"},
	} {
		t.Run(c.name, func(t *testing.T) {
			store := automationStoreFor(t)
			made, err := store.Create(automation.Automation{Title: "weekly update", Workspace: t.TempDir(),
				Schedule: automation.Schedule{Every: "0 9 * * 1", Zone: "UTC"}, Action: automation.Action{Do: "draft the weekly update"}})
			if err != nil {
				t.Fatal(err)
			}
			completer := &scriptedCompleter{steps: []step{
				automationCall("a1", map[string]any{"op": "change", "id": made.ID, "when": map[string]any{"every": "0 9 * * 2"}}),
				finalText("ok"),
			}}
			agent := automationAgent(t, completer, store, nil)
			events, _ := agent.Submit(context.Background(), "make the weekly update Tuesdays")
			collected := drainAnsweringAutomation(t, events, func(event Event) {
				answer := c.answer
				answer.Kind, answer.ID = QuestionAutomation, event.Automation.ID
				if err := agent.ResolveQuestion(answer); err != nil {
					t.Errorf("resolve: %v", err)
				}
			})
			if output := toolOutput(t, collected, "automation"); !strings.Contains(output, c.says) {
				t.Fatalf("tool result = %q", output)
			}
			got, _ := store.Get(made.ID)
			if got.Revision != made.Revision || got.Schedule.Every != "0 9 * * 1" {
				t.Fatalf("an unsaved change was saved: %+v", got)
			}
		})
	}
}

// WHAT IS NOT SENT STAYS, AND WHAT IS SENT REPLACES ONLY ITSELF.
func TestAChangeLaysOnlyWhatWasSentOverWhatIsSaved(t *testing.T) {
	store := automationStoreFor(t)
	agent := automationAgent(t, &scriptedCompleter{}, store, nil)
	now := time.Now()
	change := func(stored automation.Automation, body map[string]any) (automation.Automation, bool, string) {
		t.Helper()
		raw, _ := json.Marshal(body)
		var parsed automationArguments
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatal(err)
		}
		return agent.automationChanged(stored, parsed, now)
	}

	watch := automation.Automation{ID: "w", Title: "ci", Workspace: t.TempDir(),
		Schedule: automation.Schedule{Every: "0 * * * *", Zone: "America/Toronto"},
		Look:     &automation.Look{Command: "gh run list -L1", Condition: "it failed", Once: true},
		Action:   automation.Action{Say: "CI is red"}, Limits: automation.Limits{Time: time.Minute, USD: 1}}
	got, rescheduled, problem := change(watch, map[string]any{"look": map[string]any{"condition": "the latest run on main failed"}})
	if problem != "" || rescheduled || got.Look.Command != "gh run list -L1" || !got.Look.Once || got.Look.Condition != "the latest run on main failed" {
		t.Fatalf("a new condition = %+v (rescheduled %v, %q)", got.Look, rescheduled, problem)
	}
	if watch.Look.Condition != "it failed" {
		t.Fatal("a change wrote through to the automation it was laid over")
	}
	got, rescheduled, problem = change(watch, map[string]any{"when": map[string]any{"every": "*/30 * * * *"}, "limits": map[string]any{"usd": 2}})
	if problem != "" || !rescheduled || got.Schedule.Zone != "America/Toronto" || got.Limits.USD != 2 || got.Limits.Time != time.Minute {
		t.Fatalf("a new rhythm = %+v %+v (rescheduled %v, %q)", got.Schedule, got.Limits, rescheduled, problem)
	}

	work := automation.Automation{ID: "k", Title: "tidy", Workspace: t.TempDir(), Worktree: true,
		Schedule: automation.Schedule{Every: "1d"}, Action: automation.Action{Do: "tidy branches"}}
	got, _, problem = change(work, map[string]any{"say": "time to tidy your branches"})
	if problem != "" || got.Action.Do != "" || got.Action.Say != "time to tidy your branches" || got.Worktree {
		t.Fatalf("work turned into a line = %+v (%q)", got, problem)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("work turned into a line does not validate: %v", err)
	}

	if _, _, problem := change(work, map[string]any{"when": map[string]any{"words": "every day"}}); !strings.Contains(problem, "send only what changes") {
		t.Fatalf("a change with nothing in it = %q", problem)
	}
	if _, _, problem := change(work, map[string]any{"when": map[string]any{"at": now.Add(-time.Hour).Format(time.RFC3339)}}); !strings.Contains(problem, "has already passed") {
		t.Fatalf("a moment that has passed = %q", problem)
	}
}

// A CHANGE NEEDS THE ID OF SOMETHING THAT EXISTS, and nobody watching is told
// in the change's own words.
func TestAChangeNeedsAnAutomationAndSomebodyToSayYes(t *testing.T) {
	store := automationStoreFor(t)
	agent := automationAgent(t, &scriptedCompleter{}, store, nil)
	call := func(body map[string]any) string {
		t.Helper()
		raw, _ := json.Marshal(body)
		out, _, err := agent.automationTool(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := call(map[string]any{"op": "change", "say": "hi"}); !strings.Contains(out, "id is required") {
		t.Fatalf("no id = %q", out)
	}
	if out := call(map[string]any{"op": "change", "id": "nope", "say": "hi"}); !strings.Contains(out, "no automation with that id") {
		t.Fatalf("an unknown id = %q", out)
	}
	if out := call(map[string]any{"op": "rename"}); !strings.Contains(out, "propose, change, list") {
		t.Fatalf("an unknown op = %q", out)
	}

	reminder := finishedReminder(t, store)
	completer := &scriptedCompleter{steps: []step{
		automationCall("a1", map[string]any{"op": "change", "id": reminder.ID, "when": map[string]any{"in": "2h"}}),
		finalText("ok"),
	}}
	unwatched := automationAgent(t, completer, store, func(c *Config) { c.AskConsent = false })
	events, _ := unwatched.Submit(context.Background(), "move my reminder to later")
	if output := toolOutput(t, drainAnsweringAutomation(t, events, nil), "automation"); !strings.Contains(output, "can only be changed in a conversation somebody is in") {
		t.Fatalf("tool result = %q", output)
	}
	if got, _ := store.Get(reminder.ID); got.Status != automation.StatusFinished {
		t.Fatalf("a change nobody saw was saved: %+v", got)
	}
}
