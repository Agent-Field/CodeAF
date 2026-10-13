package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/standing"
)

func TestAppStandingProposalUsesDurableCardAndRatifiesEditedFields(t *testing.T) {
	t.Setenv(config.AppModeEnv, "1")
	store, err := standing.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	args := `{"op":"propose","words":"report each minute","when":{"kind":"every","every":"1m"},"does":{"kind":"say","say":"Original"},"wont":"Push"}`
	completer := &scriptedCompleter{steps: []step{standCall("app-stand", args), finalText("It stands")}}
	agent, _ := newTestAgent(t, completer, func(cfg *Config) { cfg.Standing = &Standing{Store: store}; cfg.AskConsent = true })
	stream, err := agent.Submit(context.Background(), "report each minute")
	if err != nil {
		t.Fatal(err)
	}
	drainAnsweringStanding(t, stream, func(event Event) {
		item, err := store.Get(event.Standing.Item.ID)
		if err != nil || item.Status != standing.StatusProposed || item.App.Wont != "Push" {
			t.Fatalf("dormant card: %+v %v", item, err)
		}
		questions := agent.OpenQuestions()
		found := false
		for _, q := range questions {
			if q.Kind == QuestionStanding && q.Subject.Ref == item.ID {
				found = true
			}
		}
		if !found {
			t.Fatal("card has no durable subject")
		}
		_, err = store.ChangeApp(item.ID, func(it *standing.Item) error { it.Does.Say = "Edited"; return nil })
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.RatifyApp(item.ID); err != nil {
			t.Fatal(err)
		}
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})
	items, err := store.List()
	if err != nil || len(items) != 1 || items[0].Status != standing.StatusActive || items[0].Does.Say != "Edited" {
		t.Fatalf("ratification lost edited card: %+v %v", items, err)
	}
}

func TestAppReminderRegistersWithoutRatificationOrTrust(t *testing.T) {
	t.Setenv(config.AppModeEnv, "1")
	store, err := standing.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	completer := &scriptedCompleter{steps: []step{standCall("app-reminder", aReminder()), finalText("Reminder set")}}
	agent, _ := newTestAgent(t, completer, func(cfg *Config) { cfg.Standing = &Standing{Store: store}; cfg.AskConsent = true })
	stream, err := agent.Submit(context.Background(), "Remind me later")
	if err != nil {
		t.Fatal(err)
	}
	events := drainAnsweringStanding(t, stream, func(Event) { t.Fatal("a reminder asked for ratification") })
	for _, event := range events {
		if event.Kind == EventStandingProposal {
			t.Fatal("reminder drew a trust card")
		}
	}
	items, err := store.List()
	if err != nil || len(items) != 1 || items[0].Status != standing.StatusActive || items[0].When.Kind != standing.WhenAt || items[0].App.ApprovedRuns != 0 {
		t.Fatalf("reminder: %+v %v", items, err)
	}
}

func TestAppStandingScopeDeliversToCurrentCrewAndAlwaysInbox(t *testing.T) {
	for _, test := range []struct {
		scope string
		want  []string
	}{{"this_chat", []string{"origin"}}, {"this_crew", []string{"origin", "mate"}}, {"everywhere", []string{"origin", "mate", "other"}}, {"not_here", nil}} {
		t.Run(test.scope, func(t *testing.T) {
			engineHome := t.TempDir()
			root := filepath.Join(engineHome, "v3", "standing")
			dirs := map[string]string{}
			records := map[string]any{}
			for _, id := range []string{"origin", "mate", "other", "archived"} {
				dir := filepath.Join(engineHome, "chats", id)
				dirs[id] = dir
				crew := "crew"
				if id == "other" {
					crew = "other-crew"
				}
				state := "closed"
				if id == "archived" {
					state = "archived"
				}
				records[id] = map[string]any{"agent": map[string]any{"id": id, "crew_id": crew, "state": state}, "session_file": filepath.Join(dir, "transcript.jsonl")}
			}
			raw, err := json.Marshal(map[string]any{"agents": records})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(filepath.Join(engineHome, "v3", "app"), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(engineHome, "v3", "app", "agents.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			item := nightly(engineHome)
			item.App = &standing.AppOrder{Scope: test.scope, CrewID: "crew"}
			item.Origin = standing.Origin{SessionID: "origin", Transcript: filepath.Join(dirs["origin"], "transcript.jsonl")}
			runner := &standingRunner{root: root}
			pending := standing.Pending{ID: "scoped-note", Kind: standing.ActionSay, Text: "CI failed"}
			if _, err = runner.Deliver(context.Background(), item, pending); err != nil {
				t.Fatal(err)
			}
			for id, dir := range dirs {
				notes, err := drainStanding(t, dir)
				if err != nil {
					t.Fatal(err)
				}
				want := false
				for _, allowed := range test.want {
					if allowed == id {
						want = true
					}
				}
				if (len(notes) == 1) != want {
					t.Fatalf("%s delivery: %+v", id, notes)
				}
			}
			notes, err := drainStanding(t, standing.AppInboxDir(root, item.ID))
			if err != nil || len(notes) != 1 {
				t.Fatalf("Inbox copy: %+v %v", notes, err)
			}
			if _, err = runner.Deliver(context.Background(), item, pending); err != nil {
				t.Fatal(err)
			}
			notes, err = drainStanding(t, standing.AppInboxDir(root, item.ID))
			if err != nil || len(notes) != 0 {
				t.Fatal("spent Inbox identity was delivered again")
			}
		})
	}
}

func TestAppStandingBudgetStopsAndAsksAtPerRunCap(t *testing.T) {
	item := nightly(t.TempDir())
	item.App = &standing.AppOrder{Scope: "this_chat", Wont: "Push or merge"}
	item.Rails.PerRunUSD = standing.DefaultPerRunUSD
	agent := &Agent{usage: Usage{CostUSD: item.Rails.PerRunUSD}}
	watch := &standingRunWatch{limit: 100}
	watch.finished(agent, item)
	if !watch.capped || !strings.Contains(watch.needs, "Budget per run reached") || !strings.Contains(watch.needs, "$0.40") {
		t.Fatalf("run cap: %+v", watch)
	}
	outcome := standingNeedsOutcome(standing.Outcome{Kind: "landed"}, watch.needs)
	if outcome.NeedsPerson == "" {
		t.Fatal("budget stopped without an ask")
	}
	reply := &standingRunWatch{limit: 100}
	stream := make(chan Event)
	close(stream)
	reply.drain(stream, agent, item)
	if !reply.capped || reply.needs == "" {
		t.Fatal("word-only reply escaped the run cap")
	}
	if !strings.Contains(standingRunBrief(item, ""), "Push or merge") {
		t.Fatal("It won't missing from the run")
	}
}

func TestAppStandingDeclinePersistsAfterTurnAndRestart(t *testing.T) {
	t.Setenv(config.AppModeEnv, "1")
	root := t.TempDir()
	store, err := standing.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	args := `{"op":"propose","words":"report each minute","when":{"kind":"every","every":"1m"},"does":{"kind":"say","say":"Original"}}`
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{standCall("app-decline", args), finalText("Declined")}}, func(cfg *Config) { cfg.Standing = &Standing{Store: store}; cfg.AskConsent = true })
	stream, err := agent.Submit(context.Background(), "report each minute")
	if err != nil {
		t.Fatal(err)
	}
	drainAnsweringStanding(t, stream, func(event Event) { agent.ResolveStanding(event.Standing.ID, StandingAnswer{}) })
	reopened, err := standing.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	items, err := reopened.List()
	if err != nil || len(items) != 1 || items[0].Status != standing.StatusDeclined {
		t.Fatalf("declined card: %+v %v", items, err)
	}
}

func TestAppScheduledWorkProposesInsteadOfRegisteringAsReminder(t *testing.T) {
	t.Setenv(config.AppModeEnv, "1")
	store, err := standing.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var args map[string]any
	if err = json.Unmarshal([]byte(aReminder()), &args); err != nil {
		t.Fatal(err)
	}
	args["does"] = map[string]any{"kind": "task", "brief": "Read CI failures"}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{standCall("scheduled-app-work", string(raw)), finalText("Proposed")}}, func(cfg *Config) { cfg.Standing = &Standing{Store: store}; cfg.AskConsent = true })
	events, err := agent.Submit(context.Background(), "Read CI failures later")
	if err != nil {
		t.Fatal(err)
	}
	asked := false
	drainAnsweringStanding(t, events, func(event Event) {
		asked = true
		item, err := store.Get(event.Standing.Item.ID)
		if err != nil || item.Status != standing.StatusProposed {
			t.Fatal("work armed without ratification")
		}
		if StandingOnceIsAnAnswer(item) {
			t.Fatal("app proposal offers a once-now bypass")
		}
		options := StandingOptions(item)
		if len(options) != 2 || options[0].Label != "Ratify" || options[1].Label != "Not now" {
			t.Fatalf("app buttons: %+v", options)
		}
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{})
	})
	if !asked {
		t.Fatal("scheduled work registered without a card")
	}
}
