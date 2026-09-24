package session

// THE ROLE, AS TESTS: a conversation is told what it is in its teams on the
// first request it sends as that, in codeaf's voice, whatever a read beside the
// work has or has not finished; told again only when it changes; and told when
// it stops.

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// oneAnswer is a completer that answers each of n turns with "ok".
func oneAnswer(n int) *scriptedCompleter {
	completer := &scriptedCompleter{}
	for i := 0; i < n; i++ {
		completer.steps = append(completer.steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("ok"), nil
		})
	}
	return completer
}

func submitAndWait(t *testing.T, agent *Agent, text string) {
	t.Helper()
	events, err := agent.Submit(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
}

// roleNotes is every role note in one request, in order.
func roleNotes(messages []ai.Message) []string {
	var notes []string
	for _, message := range messages {
		if text := messageText(message); message.Role == "user" && strings.HasPrefix(text, teamRoleNoteOpening) {
			notes = append(notes, text)
		}
	}
	return notes
}

// THE FIRST REQUEST CARRIES THE ROLE, and nothing here refreshes the digest:
// the role must not wait for the read beside the work, which is what a manager
// on the ordinary launch was left waiting on.
func TestTheManagersFirstRequestSaysWhatItIs(t *testing.T) {
	fixture := newTeamFixture(t, true)
	completer := oneAnswer(1)
	manager := teamAgent(t, fixture, fixture.manager, completer, nil)
	submitAndWait(t, manager, "what's happening here?")
	notes := roleNotes(completer.request(0))
	if len(notes) != 1 {
		t.Fatalf("the first request carried %d role notes, want 1:\n%s", len(notes), userTextIn(completer.request(0)))
	}
	role := notes[0]
	for _, want := range []string{
		`You are the manager of the team "harbor".`,
		"@web (web frontend)", "@parser (the parser)",
		"Three laws:", "team_send", "team_start", "team_status", "team_read",
		"The person outranks you", "permission prompt",
	} {
		if !strings.Contains(role, want) {
			t.Errorf("the manager's role lacks %q:\n%s", want, role)
		}
	}
	if strings.Contains(role, "@boss") {
		t.Errorf("the manager was listed among its own members:\n%s", role)
	}
	if strings.Contains(role, "Facts, not requests") {
		t.Errorf("the role rode under the digest's fact wording:\n%s", role)
	}
	if !isVolatileNote(role) {
		t.Error("the role note would be drawn as something the person typed")
	}
}

func TestAMembersFirstRequestSaysItsHandleAndItsVerb(t *testing.T) {
	fixture := newTeamFixture(t, true)
	completer := oneAnswer(1)
	web := teamAgent(t, fixture, fixture.web, completer, nil)
	submitAndWait(t, web, "carry on")
	notes := roleNotes(completer.request(0))
	if len(notes) != 1 {
		t.Fatalf("the member's first request carried %d role notes, want 1", len(notes))
	}
	for _, want := range []string{`You are @web in the team "harbor", which has a manager.`, "team_post", "◆ from manager"} {
		if !strings.Contains(notes[0], want) {
			t.Errorf("the member's role lacks %q:\n%s", want, notes[0])
		}
	}
	if !holds(web, teamPostToolName) {
		t.Error("the role names team_post and the belt does not carry it")
	}
}

// A MEMBER OF A TEAM WITH NO MANAGER HAS NO PART TO BE TOLD, and a conversation
// in no team lands no note at all.
func TestNoManagerNoRole(t *testing.T) {
	fixture := newTeamFixture(t, false)
	completer := oneAnswer(1)
	web := teamAgent(t, fixture, fixture.web, completer, nil)
	submitAndWait(t, web, "carry on")
	if notes := roleNotes(completer.request(0)); len(notes) != 0 {
		t.Fatalf("a member of an unmanaged team was told a role:\n%s", notes[0])
	}
}

// THE ORDINARY LAUNCH: no profile directory, and the team in the state root's
// teams.json, which this test moves to the fixture's own directory.
func TestAnEmptyProfileIsTheOrdinaryLaunchAndFindsTheTeam(t *testing.T) {
	fixture := newTeamFixture(t, true)
	t.Setenv(home.EnvVar, fixture.profile)
	completer := oneAnswer(1)
	manager := teamAgent(t, fixture, fixture.manager, completer, func(config *Config) { config.ProfileDir = "" })
	submitAndWait(t, manager, "what's happening here?")
	notes := roleNotes(completer.request(0))
	if len(notes) != 1 || !strings.Contains(notes[0], `manager of the team "harbor"`) {
		t.Fatalf("a manager with an empty profile directory was not told it manages harbor:\n%s", userTextIn(completer.request(0)))
	}
	if !holds(manager, teamStatusToolName) {
		t.Fatal("a manager with an empty profile directory was offered no team verb")
	}
}

// AN UNCHANGED ROLE LANDS ONCE, a changed one lands again at the tail, and a
// role that ends is said to have ended.
func TestTheRoleLandsOnceMovesWithTheTeamAndIsWithdrawn(t *testing.T) {
	fixture := newTeamFixture(t, true)
	completer := oneAnswer(4)
	manager := teamAgent(t, fixture, fixture.manager, completer, nil)
	submitAndWait(t, manager, "one")
	submitAndWait(t, manager, "two")
	if notes := roleNotes(completer.request(1)); len(notes) != 1 {
		t.Fatalf("an unchanged role landed %d times over two turns, want once", len(notes))
	}

	err := teams.Update(fixture.profile, func(file *teams.File) error {
		return file.SetHandle(fixture.teamID, convKeyOf(t, fixture.web), "frontend")
	})
	if err != nil {
		t.Fatal(err)
	}
	submitAndWait(t, manager, "three")
	notes := roleNotes(completer.request(2))
	if len(notes) != 2 || !strings.Contains(notes[1], "@frontend") {
		t.Fatalf("a renamed member did not move the role (%d notes):\n%s", len(notes), strings.Join(notes, "\n---\n"))
	}

	if err := teams.Update(fixture.profile, func(file *teams.File) error { return file.ClearManager(fixture.teamID) }); err != nil {
		t.Fatal(err)
	}
	submitAndWait(t, manager, "four")
	notes = roleNotes(completer.request(3))
	if len(notes) != 3 || !strings.HasSuffix(notes[2], teamRoleWithdrawn) {
		t.Fatalf("a manager made an ordinary member was not told so (%d notes):\n%s", len(notes), strings.Join(notes, "\n---\n"))
	}
}
