package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
)

// fakeStartDoor is a run door that can also start, and says who manages.
type fakeStartDoor struct {
	fakeRunDoor
	started []string
	line    string
	refuse  error
	manages bool
}

func (d *fakeStartDoor) StartRun(_ context.Context, conversation string) (string, error) {
	d.started = append(d.started, conversation)
	return d.line, d.refuse
}

func (d *fakeStartDoor) Manages(string) bool { return d.manages }

func startToolCall(t *testing.T, a *Agent) (string, bool) {
	t.Helper()
	out, isErr, err := a.factoryStartTool().Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	return out, isErr
}

// `factory_start` IS ON THE BELT ONLY WHERE THE RUN DOOR CAN START, asks the
// door with its own conversation, answers the door's line or its refusal, and
// is the manual's word.
func TestFactoryStartIsTheManagersStart(t *testing.T) {
	plain := &Agent{config: Config{Workspace: t.TempDir(), FactoryRun: &fakeRunDoor{}}}
	plain.tools = plain.belt()
	for _, tool := range plain.offeredTools() {
		if tool.Name == "factory_start" {
			t.Fatal("a run door that cannot start put factory_start on the belt")
		}
	}
	door := &fakeStartDoor{line: "#12 started"}
	a := &Agent{config: Config{Workspace: t.TempDir(), FactoryRun: door, SessionFile: "/tmp/manager/transcript.jsonl"}}
	a.tools = a.belt()
	found := false
	for _, tool := range a.offeredTools() {
		if tool.Name == "factory_start" {
			found = true
			for _, want := range []string{"only when the person clearly said so", "nothing to look up first"} {
				if !strings.Contains(tool.Description, want) {
					t.Errorf("the description does not say %q", want)
				}
			}
		}
	}
	if !found {
		t.Fatal("a run door that can start did not put factory_start on the belt")
	}
	if out, isErr := startToolCall(t, a); isErr || out != "#12 started" || len(door.started) != 1 || door.started[0] != "/tmp/manager/transcript.jsonl" {
		t.Fatalf("answered %q (%v), door asked by %v", out, isErr, door.started)
	}
	door.refuse = errors.New("#12 is already running")
	if out, isErr := startToolCall(t, a); !isErr || out != "nothing started: #12 is already running" {
		t.Fatalf("a refusal answered %q (%v)", out, isErr)
	}
	// A SHAPING TURN'S LENT DOOR CANNOT START: the runner's own turn never
	// launches anything.
	a.running, a.teamTurnSerial = true, 3
	turnRunDoors.Store(a, turnRunDoor{door: &fakeRunDoor{}, serial: 3})
	t.Cleanup(func() { turnRunDoors.Delete(a) })
	if out, isErr := startToolCall(t, a); !isErr || !strings.HasPrefix(out, "nothing started") || len(door.started) != 2 {
		t.Fatalf("the lent turn answered %q (%v) after %d starts", out, isErr, len(door.started))
	}
	if !manual.Chat().Mentions("factory_start") {
		t.Error("no chat manual page mentions factory_start")
	}
	if got := ActionCategoryForTool("factory_start"); got != ActionEdit {
		t.Errorf("factory_start's family is %q, want %q", got, ActionEdit)
	}
}

// THE RUNNER'S WORDS CARRY NO SKILLS BLOCK: a block is chosen from a
// message's words, and `Shape the run for this item now.` is no request a
// skill serves.
func TestARunnerNoteCarriesNoSkills(t *testing.T) {
	note := runnerNote("Shape the run for this item now.")
	if !note.noSkills || !note.authored {
		t.Fatalf("runner note %+v", note)
	}
	a := &Agent{}
	before := messageContentText(note.message)
	a.attachTurnSkillsLocked(&note)
	if got := messageContentText(note.message); got != before || len(note.skills) != 0 {
		t.Fatalf("the runner's note was given skills: %q", got)
	}
}

// A FACTORY ITEM'S MANAGER LEADS ITS TEAM WITH THE READS ALONE: the members
// are the steps the runner runs, so no directive, stop or start reaches the
// belt, and its role says the runner runs them.
func TestAFactoryLeadHasTheTeamsReadsAlone(t *testing.T) {
	door := &fakeStartDoor{manages: true}
	a := &Agent{config: Config{Workspace: t.TempDir(), FactoryRun: door, SessionFile: "/tmp/manager/transcript.jsonl"}}
	a.tools = a.belt()
	if !a.factoryLead() {
		t.Fatal("a conversation its door says manages an item is no factory lead")
	}
	roles := []teamRole{{id: "t1", name: "#12 · fix the ledger", manager: true, managed: true}}
	a.armTeamTools(roles, true)
	names := map[string]bool{}
	for _, tool := range a.offeredTools() {
		names[tool.Name] = true
	}
	if !names[teamStatusToolName] || !names[teamReadToolName] {
		t.Fatalf("the lead lacks the reads: %v", names)
	}
	for _, verb := range []string{teamSendToolName, teamStopToolName, teamStartToolName, teamAddToolName, teamRemoveToolName, teamRaiseToolName} {
		if names[verb] {
			t.Errorf("the factory lead carries %s", verb)
		}
	}
	role := teamRoleBlock(roles, nil, true)
	if !strings.Contains(role, "the factory's runner runs them") || strings.Contains(role, "hand the work out") {
		t.Fatalf("role = %q", role)
	}
	// AN ORDINARY MANAGER keeps every verb.
	b := &Agent{config: Config{Workspace: t.TempDir()}}
	b.tools = b.belt()
	b.armTeamTools(roles, false)
	got := map[string]bool{}
	for _, tool := range b.offeredTools() {
		got[tool.Name] = true
	}
	if !got[teamSendToolName] || !got[teamStartToolName] {
		t.Fatalf("an ordinary manager lost its verbs: %v", got)
	}
	if (&Agent{config: Config{FactoryRun: &fakeRunDoor{}}}).factoryLead() {
		t.Fatal("a door that cannot say made a factory lead")
	}
}
