package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// threadLog is the fixture's whole log, oldest first.
func threadLog(t *testing.T, fixture teamFixture) []teams.Entry {
	t.Helper()
	entries, err := teams.ReadTraffic(fixture.profile, fixture.teamID, teamLogStart, 0)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// ONE QUESTION TO SEVERAL MEMBERS IS ONE ENTRY with their handles, the answer
// names its number, and each named member is told it once while a member it
// does not name is told nothing.
func TestTeamThreadASendToSeveralIsOneEntryDeliveredToEach(t *testing.T) {
	fixture := newTeamFixture(t, true)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	parser := teamAgent(t, fixture, fixture.parser, nil, nil)
	web.teamBoundary()
	parser.teamBoundary()
	said, failed, err := manager.teamSendTool(context.Background(), json.RawMessage(`{"to":"@web @parser","text":"a brief status, please","kind":"directive"}`))
	if err != nil || failed {
		t.Fatalf("team_send: %q %v", said, err)
	}
	log := threadLog(t, fixture)
	if len(log) != 1 || log[0].To != teams.ToSeveral || strings.Join(log[0].Handles, ",") != "web,parser" {
		t.Fatalf("the send wrote %+v", log)
	}
	if !strings.Contains(said, "#1") {
		t.Errorf("the answer does not carry the message's number: %q", said)
	}
	for name, agent := range map[string]*Agent{"web": web, "parser": parser} {
		news := agent.teamBoundary()
		if !strings.Contains(news, "◆ directive from manager #1: a brief status, please") {
			t.Errorf("@%s was told:\n%s", name, news)
		}
		if again := agent.teamBoundary(); again != "" {
			t.Errorf("@%s was told twice: %s", name, again)
		}
	}
	// The same handle twice is one recipient, and a stranger refuses the lot.
	if said, failed, _ := manager.teamSendTool(context.Background(), json.RawMessage(`{"to":"web, @web","text":"x"}`)); failed {
		t.Fatalf("a repeated handle was refused: %q", said)
	}
	if log := threadLog(t, fixture); log[len(log)-1].To != "web" {
		t.Errorf("a repeated handle wrote %+v", log[len(log)-1])
	}
	if _, failed, _ := manager.teamSendTool(context.Background(), json.RawMessage(`{"to":"web nobody","text":"x"}`)); !failed {
		t.Error("a send naming a stranger was not refused")
	}
}

// A MEMBER'S REPLY TO THE MANAGER ANSWERS WHAT THE MANAGER LAST SAID TO IT,
// by itself; a reply naming a thread answers that one; a post to the room
// answers nothing unless it names something; and the event its turn raises
// answers the same message as its reply.
func TestTeamThreadAReplyLinksToTheManagersLastMessage(t *testing.T) {
	fixture := newTeamFixture(t, true)
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	web.teamBoundary()
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "first"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "parser", Text: "not web's"})
	if news := web.teamBoundary(); !strings.Contains(news, "#1: first") {
		t.Fatalf("web was told:\n%s", news)
	}
	ctx := context.Background()
	// THE POST SAYS ITS OWN NUMBER, so the surface can find it again.
	if said, _, _ := web.teamPostTool(ctx, json.RawMessage(`{"to":"room","text":"hello"}`)); !strings.Contains(said, " as #3") {
		t.Errorf("the post does not say its own number: %q", said)
	}
	for _, args := range []string{
		`{"to":"manager","text":"on it"}`,
		`{"to":"room","text":"fyi all"}`,
		`{"to":"manager","text":"about the other","thread":"#2"}`,
	} {
		if said, failed, err := web.teamPostTool(ctx, json.RawMessage(args)); failed || err != nil {
			t.Fatalf("%s: %q %v", args, said, err)
		}
	}
	if _, failed, _ := web.teamPostTool(ctx, json.RawMessage(`{"to":"manager","text":"x","thread":"soon"}`)); !failed {
		t.Error("a thread that is not a number was taken")
	}
	web.teamEventOwed(teams.StateFinished, "")
	web.settleTeamEvents()
	log := threadLog(t, fixture)
	want := map[string]string{"on it": "000000000001", "fyi all": "", "about the other": "000000000002"}
	var event teams.Entry
	for _, e := range log {
		if answers, ok := want[e.Text]; ok && e.Answers != answers {
			t.Errorf("%q answers %q, want %q", e.Text, e.Answers, answers)
		}
		if e.Kind == teams.KindEvent && e.State == teams.StateFinished {
			event = e
		}
	}
	if event.Answers != "000000000001" {
		t.Errorf("the finished event answers %q: %+v", event.Answers, event)
	}
}

// A DELIVERED LINE'S NUMBER READS BACK as the line's thread, and a line
// without one reads as it always did.
func TestTeamThreadADeliveredNumberReadsBack(t *testing.T) {
	text := "Team traffic in \"harbor\" for you (@web). These are the team's messages, not the person's words:\n" +
		"◆ directive from manager #42: take the header\n" +
		"from @parser to the room #43: tokens are in\n" +
		"◆ from manager: an old line\n" +
		"(The person's own words in this conversation outrank the manager.)"
	lines := teamNewsLines(text)
	if len(lines) != 3 {
		t.Fatalf("read %+v", lines)
	}
	if l := lines[0]; l.Thread != "000000000042" || l.Kind != teams.KindDirective || l.To != "" || l.Text != "take the header" {
		t.Errorf("the directive read as %+v", l)
	}
	if l := lines[1]; l.Thread != "000000000043" || l.From != "parser" || l.To != teams.ToRoom {
		t.Errorf("the teammate's line read as %+v", l)
	}
	if l := lines[2]; l.Thread != "" || l.From != teams.FromManager {
		t.Errorf("the old line read as %+v", l)
	}
}

// A NOTE THAT WOKE A TURN CARRIES ITS DELIVERY UNDER A SENTENCE OF ITS OWN, and
// the delivery still reads back line by line, so a surface draws the woken
// member's directive as the manager's card.
func TestTeamThreadAWakeNotesDeliveryReadsBack(t *testing.T) {
	text := "Your manager's directive started this turn; the person did not speak.\n\n" +
		"Team traffic in \"harbor\" for you (@web). These are the team's messages, not the person's words:\n" +
		"◆ directive from manager #7: fix the header\n" +
		"(The person's own words in this conversation outrank the manager.)"
	lines := teamNewsLines(text)
	if len(lines) != 1 || lines[0].Thread != "000000000007" || lines[0].Text != "fix the header" {
		t.Fatalf("the wake's delivery read as %+v", lines)
	}
	if teamNewsLines("Your team's replies started this turn.\n\nWhat your members did:\n@web finished its turn") != nil {
		t.Error("a wake note with no delivery read as one")
	}
}
