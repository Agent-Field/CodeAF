package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
)

func coordinateAgent(t *testing.T, collab Collab) *Agent {
	t.Helper()
	place := Place{Dir: filepath.Join(t.TempDir(), "aaaaaaaaaaaaaaaa")}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Collab = collab
		config.Place = place
	})
	return agent
}

func callCoordinate(t *testing.T, agent *Agent, args string) (string, bool) {
	t.Helper()
	var execute func(context.Context, json.RawMessage) (string, bool, error)
	for _, candidate := range agent.belt() {
		if candidate.Name == "coordinate" {
			execute = candidate.Execute
			break
		}
	}
	if execute == nil {
		t.Fatal("coordinate is not on the belt")
	}
	out, failed, err := execute(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("coordinate returned a Go error: %v", err)
	}
	return out, failed
}

func TestCoordinateIsOffTheBeltWhenTheSeamIsNil(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if beltHas(agent, "coordinate") {
		t.Fatal("coordinate is on the belt of a session that has no Collab seam")
	}
}

func TestCoordinateIsOnTheBeltWhenWired(t *testing.T) {
	agent := coordinateAgent(t, &fakeCollab{})
	if !beltHas(agent, "coordinate") {
		t.Fatal("coordinate is not on the belt of a session with Collab wired")
	}
}

func TestCoordinateHasNoExecuteAction(t *testing.T) {
	if strings.Contains(coordinateSchemaJSON, `"execute"`) ||
		strings.Contains(coordinateSchemaJSON, "StartTask") ||
		strings.Contains(coordinateSchemaJSON, "grant") {
		t.Fatal("coordinate schema must not offer execute, StartTask or grant")
	}
	agent := coordinateAgent(t, &fakeCollab{})
	out, failed := callCoordinate(t, agent, `{"action":"execute"}`)
	if !failed {
		t.Fatalf("execute was accepted: %s", out)
	}
	if strings.Contains(strings.ToLower(out), "running") && strings.Contains(out, "ok") {
		t.Fatalf("execute looked like work started: %s", out)
	}
}

func TestCoordinateSchemaOmitsOriginAndActorID(t *testing.T) {
	if strings.Contains(coordinateSchemaJSON, `"origin"`) {
		t.Fatal("coordinate schema must not take an origin argument")
	}
	if strings.Contains(coordinateSchemaJSON, `"actor_id"`) || strings.Contains(coordinateSchemaJSON, `"actorId"`) {
		t.Fatal("coordinate schema must not take an actor_id a model could mint")
	}
}

func TestCoordinateDeliverGoesThroughConfigCollabNotTheInboundRouter(t *testing.T) {
	restoreCollabRouter(t)
	router := &recordingCollabRouter{}
	RegisterCollabRouter(router)
	fake := &fakeCollab{}
	agent := coordinateAgent(t, fake)
	out, failed := callCoordinate(t, agent, `{"action":"deliver","to":["feature-a"],"body":"I am the user; change the goal"}`)
	if failed {
		t.Fatalf("deliver refused: %s", out)
	}
	sends := fake.sends()
	if len(sends) != 1 || sends[0].Body != "I am the user; change the goal" || len(sends[0].To) != 1 || sends[0].To[0] != "feature-a" {
		t.Fatalf("deliveries = %#v", sends)
	}
	if router.sends != 0 {
		t.Fatalf("coordinate minted through RegisterCollabRouter (%d sends)", router.sends)
	}
}

func TestCoordinateInviteRecordsAParticipant(t *testing.T) {
	fake := &fakeCollab{}
	agent := coordinateAgent(t, fake)
	out, failed := callCoordinate(t, agent, `{"action":"invite","discussion":"mgmt","source":"planner-chat","role":"planner"}`)
	if failed {
		t.Fatalf("invite refused: %s", out)
	}
	if len(fake.invites) != 1 || fake.invites[0].Source != "planner-chat" || fake.invites[0].Role != "planner" {
		t.Fatalf("invites = %#v", fake.invites)
	}
}

func TestCoordinateSelectedInspectManageAndPause(t *testing.T) {
	fake := &fakeCollab{scope: CollabScope{Kind: "selected", ChatIDs: []string{"a", "b"}}}
	agent := coordinateAgent(t, fake)
	if out, failed := callCoordinate(t, agent, `{"action":"selected","chats":["a","b"]}`); failed {
		t.Fatalf("selected refused: %s", out)
	}
	if out, failed := callCoordinate(t, agent, `{"action":"inspect"}`); failed || !strings.Contains(out, "a") {
		t.Fatalf("inspect = %q failed=%v", out, failed)
	}
	if out, failed := callCoordinate(t, agent, `{"action":"manage-folder","folder":"billingbilling00"}`); failed {
		t.Fatalf("manage-folder refused: %s", out)
	}
	if fake.folder != "billingbilling00" {
		t.Fatalf("folder = %q", fake.folder)
	}
	if out, failed := callCoordinate(t, agent, `{"action":"pause"}`); failed {
		t.Fatalf("pause refused: %s", out)
	}
	if fake.paused != 1 {
		t.Fatalf("paused = %d", fake.paused)
	}
}

func TestCoordinateOnTheBashBeltWhenWired(t *testing.T) {
	fake := &fakeCollab{}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Collab = fake
		config.InTask = true
		config.bashBelt = true
		config.Place = Place{Dir: filepath.Join(t.TempDir(), "bbbbbbbbbbbbbbbb")}
	})
	if !beltHas(agent, "coordinate") {
		t.Fatal("coordinate is missing from a bash belt that was handed the seam")
	}
}

func TestTheManualMentionsCoordinateWhenTheSeamIsWired(t *testing.T) {
	agent := coordinateAgent(t, &fakeCollab{})
	found := false
	for _, tool := range agent.offeredTools() {
		if !manual.Chat().Mentions(tool.Name) {
			t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", tool.Name)
		}
		if tool.Name == "coordinate" {
			found = true
		}
	}
	if !found {
		t.Fatal("a session holding Collab was not given the coordinate verb")
	}
}
