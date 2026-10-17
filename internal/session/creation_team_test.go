package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/teams"
)

const creationTeamID = "aaaaaaaaaaaa"

// creationAgent is a conversation that can create automations and read the
// teams in a profile it owns. The profile holds one open team.
func creationAgent(t *testing.T, store *automation.Store, calls ...step) (*Agent, string) {
	t.Helper()
	profile := t.TempDir()
	members := []teams.Member{{Key: "m1", File: "m1", Handle: "one"}, {Key: "m2", File: "m2", Handle: "two"}}
	if err := teams.Save(profile, []teams.Team{{ID: creationTeamID, Name: "crew", Members: members}}); err != nil {
		t.Fatal(err)
	}
	completer := &scriptedCompleter{steps: append(calls, finalText("done"))}
	agent := automationAgent(t, completer, store, func(c *Config) {
		c.ProfileDir = profile
		c.SessionFile = filepath.Join(t.TempDir(), "conversation.jsonl")
	})
	return agent, profile
}

func createCall() step {
	return automationCall("a1", map[string]any{
		"op": "propose", "title": "weekly update", "words": "every Monday at 9, draft the weekly update",
		"when": map[string]any{"every": "0 9 * * 1"}, "do": "draft the weekly update",
	})
}

func saveCreated(t *testing.T, agent *Agent, events <-chan Event) {
	t.Helper()
	drainAnsweringAutomation(t, events, func(event Event) {
		agent.ResolveAutomation(event.Automation.ID, AutomationAnswer{Save: true})
	})
}

func onlyAutomation(t *testing.T, store *automation.Store) automation.Automation {
	t.Helper()
	all, err := store.List()
	if err != nil || len(all) != 1 {
		t.Fatalf("store holds %d automations, %v", len(all), err)
	}
	return all[0]
}

// AN ACCEPTED CREW TURN'S EXACT TEAM SURVIVES THE STORE BEING REOPENED.
func TestCreationTeamAcceptedCrewTurnRecordsExactTeamAfterReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "automations")
	store, err := automation.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent, _ := creationAgent(t, store, createCall())
	events, err := agent.SubmitMessage(context.Background(), Submission{Text: "every Monday at 9, draft the weekly update", CreationTeam: creationTeamID})
	if err != nil {
		t.Fatal(err)
	}
	saveCreated(t, agent, events)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := automation.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	got := onlyAutomation(t, reopened).Origin
	if got.Team == nil || !got.Team.Known || got.Team.ID != creationTeamID {
		t.Fatalf("origin team = %+v, want exactly %s", got.Team, creationTeamID)
	}
	if got.SessionID == "" {
		t.Fatalf("origin lost its session: %+v", got)
	}
}

// AN ORDINARY ACCEPTED MESSAGE IS EXPLICIT NONE, AND BEING A MANAGER NOW DOES
// NOT MAKE IT ANYTHING ELSE.
func TestCreationTeamOrdinaryTurnIsKnownNoneEvenForACurrentManager(t *testing.T) {
	store := automationStoreFor(t)
	agent, profile := creationAgent(t, store, createCall())
	if err := teams.Update(profile, func(f *teams.File) error { return f.SetManager(creationTeamID, "m1") }); err != nil {
		t.Fatal(err)
	}
	events, err := agent.Submit(context.Background(), "every Monday at 9, draft the weekly update")
	if err != nil {
		t.Fatal(err)
	}
	saveCreated(t, agent, events)
	if got := onlyAutomation(t, store).Origin.Team; got == nil || !got.Known || got.ID != "" {
		t.Fatalf("origin team = %+v, want known none", got)
	}
}

// A CONTEXT NO ACCEPTED MESSAGE STAMPED STAYS UNKNOWN, NEVER NONE.
func TestCreationTeamUnstampedContextStaysUnknown(t *testing.T) {
	agent, _ := creationAgent(t, automationStoreFor(t))
	origin, problem := agent.automationOrigin(context.Background())
	if problem != "" || origin.Team != nil {
		t.Fatalf("unstamped context gave team %+v, problem %q; want unknown", origin.Team, problem)
	}
	stamped, problem := agent.automationOrigin(withCreationTeam(context.Background(), ""))
	if problem != "" || stamped.Team == nil || !stamped.Team.Known || stamped.Team.ID != "" {
		t.Fatalf("stamped none gave %+v, %q", stamped.Team, problem)
	}
}

// A ROW SAVED BEFORE THE FIELD READS BACK UNKNOWN AND IS NOT WRITTEN AS NONE.
func TestCreationTeamLegacyRowReadsUnknown(t *testing.T) {
	var origin automation.Origin
	if err := json.Unmarshal([]byte(`{"sessionId":"s","transcript":"t"}`), &origin); err != nil {
		t.Fatal(err)
	}
	if origin.Team != nil {
		t.Fatalf("legacy origin decoded as %+v", origin.Team)
	}
	raw, err := json.Marshal(origin)
	if err != nil || string(raw) != `{"sessionId":"s","transcript":"t"}` {
		t.Fatalf("legacy origin re-encoded as %s, %v", raw, err)
	}
}

// A STAMP THAT NAMES NO OPEN TEAM REFUSES THE CREATE AND SAVES NOTHING.
func TestCreationTeamInvalidStampRefusesCreate(t *testing.T) {
	for _, stamp := range []string{"bbbbbbbbbbbb", "../escape", "a b"} {
		store := automationStoreFor(t)
		agent, _ := creationAgent(t, store, createCall())
		events, err := agent.SubmitMessage(context.Background(), Submission{Text: "every Monday at 9, draft the weekly update", CreationTeam: stamp})
		if err != nil {
			t.Fatal(err)
		}
		saveCreated(t, agent, events)
		if all, _ := store.List(); len(all) != 0 {
			t.Fatalf("stamp %q still saved %+v", stamp, all)
		}
	}
}

// A MANAGER OR MEMBERSHIP CHANGE NEVER RETARGETS A SAVED ROW, AND A CHANGE KEEPS IT.
func TestCreationTeamLaterChangesNeverRetarget(t *testing.T) {
	store := automationStoreFor(t)
	agent, profile := creationAgent(t, store, createCall())
	events, err := agent.SubmitMessage(context.Background(), Submission{Text: "every Monday at 9, draft the weekly update", CreationTeam: creationTeamID})
	if err != nil {
		t.Fatal(err)
	}
	saveCreated(t, agent, events)
	made := onlyAutomation(t, store)
	if err := teams.Update(profile, func(f *teams.File) error { return f.SetManager(creationTeamID, "m2") }); err != nil {
		t.Fatal(err)
	}
	if err := teams.Update(profile, func(f *teams.File) error { return f.Disband(creationTeamID, time.Now(), "") }); err != nil {
		t.Fatal(err)
	}
	// The change goes through the tool, from a plain (ordinary) turn, and the
	// stored creation team must survive both that and the disbanded team.
	changer := automationAgent(t, &scriptedCompleter{steps: []step{
		automationCall("c1", map[string]any{"op": "change", "id": made.ID, "title": "renamed"}),
		finalText("renamed"),
	}}, store, nil)
	changeEvents, err := changer.Submit(context.Background(), "rename it")
	if err != nil {
		t.Fatal(err)
	}
	saveCreated(t, changer, changeEvents)
	if got := onlyAutomation(t, store); got.Title != "renamed" {
		t.Fatalf("the change did not apply: %+v", got)
	}
	if got := onlyAutomation(t, store).Origin.Team; got == nil || got.ID != creationTeamID {
		t.Fatalf("stored origin team = %+v", got)
	}
}
