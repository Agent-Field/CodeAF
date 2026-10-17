package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/teams"
)

const creationTeamID = "aaaaaaaaaaaa"

// creationAgent is a conversation that can create automations and read the
// teams in a profile it owns. The profile holds one open team, and that team
// holds this conversation (its journal is its key) beside two others.
func creationAgent(t *testing.T, store *automation.Store, calls ...step) (*Agent, string) {
	t.Helper()
	profile, journal := creationProfile(t)
	completer := &scriptedCompleter{steps: append(calls, finalText("done"))}
	agent := automationAgent(t, completer, store, func(c *Config) {
		c.ProfileDir = profile
		c.SessionFile = journal
	})
	return agent, profile
}

// creationProfile is a profile whose one team holds a fresh journal's
// conversation, and that journal's path.
func creationProfile(t *testing.T) (string, string) {
	t.Helper()
	profile := t.TempDir()
	journal := filepath.Join(t.TempDir(), "conversation.jsonl")
	members := []teams.Member{{Key: journal, File: journal, Handle: "self"}, {Key: "m1", File: "m1", Handle: "one"}, {Key: "m2", File: "m2", Handle: "two"}}
	if err := teams.Save(profile, []teams.Team{{ID: creationTeamID, Name: "crew", Members: members}}); err != nil {
		t.Fatal(err)
	}
	return profile, journal
}

// inCrew is a creation context naming the test's team.
func inCrew() *CreationContext { return &CreationContext{Team: creationTeamID} }

const createWords = "every Monday at 9, draft the weekly update"

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
	events, err := agent.SubmitMessage(context.Background(), Submission{Text: createWords, Creation: inCrew()})
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

// A PLAIN SUBMIT STATES NOTHING, SO ITS AUTOMATION IS UNKNOWN — NOT NONE —
// EVEN FROM A CONVERSATION THAT MANAGES A TEAM RIGHT NOW. An older client, the
// terminal surface and every door that never heard of the field land here.
func TestCreationTeamPlainSubmitIsUnknownEvenForACurrentManager(t *testing.T) {
	store := automationStoreFor(t)
	agent, profile := creationAgent(t, store, createCall())
	if err := teams.Update(profile, func(f *teams.File) error { return f.SetManager(creationTeamID, agent.config.SessionFile) }); err != nil {
		t.Fatal(err)
	}
	events, err := agent.Submit(context.Background(), createWords)
	if err != nil {
		t.Fatal(err)
	}
	saveCreated(t, agent, events)
	if got := onlyAutomation(t, store).Origin.Team; got != nil {
		t.Fatalf("origin team = %+v, want unknown (nil)", got)
	}
}

// AN EXPLICIT NONE IS NONE, AND MANAGING A TEAM NOW DOES NOT MAKE IT THAT TEAM.
// A submission whose only extra is ClientID still states nothing.
func TestCreationTeamExplicitNoneIsNoneAndAbsenceIsNot(t *testing.T) {
	store := automationStoreFor(t)
	agent, profile := creationAgent(t, store, createCall())
	if err := teams.Update(profile, func(f *teams.File) error { return f.SetManager(creationTeamID, agent.config.SessionFile) }); err != nil {
		t.Fatal(err)
	}
	events, err := agent.SubmitMessage(context.Background(), Submission{Text: createWords, Creation: &CreationContext{}})
	if err != nil {
		t.Fatal(err)
	}
	saveCreated(t, agent, events)
	if got := onlyAutomation(t, store).Origin.Team; got == nil || !got.Known || got.ID != "" {
		t.Fatalf("origin team = %+v, want known none", got)
	}

	other := automationStoreFor(t)
	absent, _ := creationAgent(t, other, createCall())
	events, err = absent.SubmitMessage(context.Background(), Submission{Text: createWords, ClientID: "c-1"})
	if err != nil {
		t.Fatal(err)
	}
	saveCreated(t, absent, events)
	if got := onlyAutomation(t, other).Origin.Team; got != nil {
		t.Fatalf("a submission with no creation context recorded %+v, want unknown", got)
	}
}

// A CONTEXT NO DOOR STAMPED STAYS UNKNOWN, NEVER NONE.
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

// A TEAM THIS ENGINE CANNOT PROVE NEVER STOPS THE MESSAGE AND NEVER BECOMES
// PROVENANCE: the message is accepted and answered exactly as before, and the
// automation made in its turn is UNKNOWN — not the claimed team and not none.
// That covers a team that does not exist here, a team this conversation is not
// in, a malformed id, an id padded with spaces (never trimmed to fit), and a
// conversation that keeps no teams.
func TestCreationTeamUnprovenTeamIsAcceptedAsUnknown(t *testing.T) {
	for _, team := range []string{"bbbbbbbbbbbb", "otherteam01", "../escape", "a b", " " + creationTeamID} {
		store := automationStoreFor(t)
		agent, profile := creationAgent(t, store, createCall())
		if err := teams.Update(profile, func(f *teams.File) error {
			f.Teams = append(f.Teams, teams.Team{ID: "otherteam01", Name: "other", Members: []teams.Member{{Key: "m9", File: "m9", Handle: "nine"}}})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if got := agent.provenCreation(&CreationContext{Team: team}); got != nil {
			t.Fatalf("team %q was proven as %+v", team, got)
		}
		events, err := agent.SubmitMessage(context.Background(), Submission{Text: createWords, Creation: &CreationContext{Team: team}})
		if err != nil {
			t.Fatalf("team %q refused the message: %v", team, err)
		}
		saveCreated(t, agent, events)
		if got := onlyAutomation(t, store).Origin.Team; got != nil {
			t.Fatalf("team %q recorded %+v, want unknown", team, got)
		}
	}
	store := automationStoreFor(t)
	teamless := automationAgent(t, &scriptedCompleter{steps: []step{createCall(), finalText("done")}}, store, nil)
	events, err := teamless.SubmitMessage(context.Background(), Submission{Text: createWords, Creation: inCrew()})
	if err != nil {
		t.Fatalf("a conversation with no teams refused a crew message: %v", err)
	}
	saveCreated(t, teamless, events)
	if got := onlyAutomation(t, store).Origin.Team; got != nil {
		t.Fatalf("a conversation with no teams recorded %+v, want unknown", got)
	}
	// Absence stays absence and an explicit none stays none, with no lookup.
	if teamless.provenCreation(nil) != nil {
		t.Fatal("absence was proven as something")
	}
	if got := teamless.provenCreation(&CreationContext{}); got == nil || got.Team != "" {
		t.Fatalf("an explicit none was proven as %+v", got)
	}
}

// A CLOSED CREW IS NOT A REASON TO DROP THE CREW: the door decides who may be
// sent a message, and provenance records what it accepted. The automation made in a
// closed crew's turn saves, with that crew's id.
func TestCreationTeamClosedCrewIsAcceptedAndRecorded(t *testing.T) {
	store := automationStoreFor(t)
	agent, profile := creationAgent(t, store, createCall())
	if err := teams.Update(profile, func(f *teams.File) error { return f.Disband(creationTeamID, time.Now(), "") }); err != nil {
		t.Fatal(err)
	}
	events, err := agent.SubmitMessage(context.Background(), Submission{Text: createWords, Creation: inCrew()})
	if err != nil {
		t.Fatalf("a closed crew's message was refused: %v", err)
	}
	saveCreated(t, agent, events)
	if got := onlyAutomation(t, store).Origin.Team; got == nil || got.ID != creationTeamID {
		t.Fatalf("origin team = %+v, want %s", got, creationTeamID)
	}
}

// THE ID IS CAPTURED AT ACCEPTANCE: a manager change, the conversation leaving
// the team and the team closing between the message and the save do not stop
// or move it, and a later change through the tool keeps it.
func TestCreationTeamCapturedThenChangedNeverRetargets(t *testing.T) {
	store := automationStoreFor(t)
	changed := make(chan struct{})
	afterTheMove := func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return createCall()(ctx, messages)
	}
	agent, profile := creationAgent(t, store, afterTheMove)
	events, err := agent.SubmitMessage(context.Background(), Submission{Text: createWords, Creation: inCrew()})
	if err != nil {
		t.Fatal(err)
	}
	// The team changes hands, loses this conversation and closes after the
	// message was accepted and before the automation is proposed: what was
	// accepted stays what is recorded.
	if err := teams.Update(profile, func(f *teams.File) error {
		if err := f.SetManager(creationTeamID, "m2"); err != nil {
			return err
		}
		if err := f.RemoveMember(creationTeamID, agent.config.SessionFile); err != nil {
			return err
		}
		return f.Disband(creationTeamID, time.Now(), "")
	}); err != nil {
		t.Fatal(err)
	}
	close(changed)
	saveCreated(t, agent, events)
	made := onlyAutomation(t, store)
	if made.Origin.Team == nil || made.Origin.Team.ID != creationTeamID {
		t.Fatalf("origin team after the team moved = %+v", made.Origin.Team)
	}
	// The change goes through the tool, from a plain turn, and the stored
	// creation team must survive it.
	changer := automationAgent(t, &scriptedCompleter{steps: []step{
		automationCall("c1", map[string]any{"op": "change", "id": made.ID, "title": "renamed"}),
		finalText("renamed"),
	}}, store, nil)
	changeEvents, err := changer.Submit(context.Background(), "rename it")
	if err != nil {
		t.Fatal(err)
	}
	saveCreated(t, changer, changeEvents)
	if got := onlyAutomation(t, store); got.Title != "renamed" || got.Origin.Team == nil || got.Origin.Team.ID != creationTeamID {
		t.Fatalf("after the change: title %q, origin team %+v", got.Title, got.Origin.Team)
	}
}

// A CREW TURN TAKEN OVER BEFORE IT ANSWERED IS ASKED AGAIN IN THE SAME CREW: the
// window that takes it reads the context back from the person's journaled line,
// so the automation the resumed turn makes records the crew, not unknown.
func TestCreationTeamSurvivesATakeoverResume(t *testing.T) {
	store := automationStoreFor(t)
	profile, journal := creationProfile(t)
	streaming := make(chan struct{})
	first := automationAgent(t, &scriptedCompleter{steps: []step{thinkingStep(streaming)}}, store, func(c *Config) {
		c.ProfileDir, c.SessionFile = profile, journal
	})
	events, err := first.SubmitMessage(context.Background(), Submission{Text: createWords, Creation: inCrew()})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-streaming:
	case <-time.After(10 * time.Second):
		t.Fatal("the first turn never streamed")
	}
	first.InterruptFor(StopByTakeover)
	collect(t, events)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := automationAgent(t, &scriptedCompleter{steps: []step{createCall(), finalText("done")}}, store, func(c *Config) {
		c.ProfileDir, c.SessionFile = profile, journal
	})
	resumed, ok := second.ResumeStoppedTurn(context.Background())
	if !ok {
		t.Fatal("the taken-over crew turn was not asked again")
	}
	saveCreated(t, second, resumed)
	if got := onlyAutomation(t, store).Origin.Team; got == nil || got.ID != creationTeamID {
		t.Fatalf("the resumed turn recorded %+v, want %s", got, creationTeamID)
	}
}

// A QUEUED FOLLOW-UP KEEPS ITS CONTEXT UNTIL ITS OWN TURN OPENS.
func TestCreationTeamFollowUpKeepsItsContext(t *testing.T) {
	store := automationStoreFor(t)
	release := make(chan struct{})
	busy := func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return textResponse("first done"), nil
	}
	agent, _ := creationAgent(t, store, busy, createCall())
	first, err := agent.Submit(context.Background(), "hold on")
	if err != nil {
		t.Fatal(err)
	}
	queued, err := agent.FollowUpMessage(Submission{Text: createWords, Creation: inCrew()})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	collect(t, first)
	saveCreated(t, agent, queued)
	if got := onlyAutomation(t, store).Origin.Team; got == nil || got.ID != creationTeamID {
		t.Fatalf("the follow-up's automation recorded %+v, want %s", got, creationTeamID)
	}
}
