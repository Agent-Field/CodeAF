package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// A BRIEF CHANGED IN PLACE REACHES ONLY ITS MEMBER, AT ITS NEXT STEP: it is a
// delivered line that wakes nobody, the manager never reads it back, and the
// surface reads it as a brief line naming who changed it and its log entry.
func TestBriefChangedIsDeliveredToItsMemberAndWakesNobody(t *testing.T) {
	member := teamRole{id: "t", name: "Release", handle: "review", managed: true}
	other := teamRole{id: "t", name: "Release", handle: "backend", managed: true}
	manager := teamRole{id: "t", name: "Release", handle: "release", manager: true, managed: true}
	entry := teams.Entry{ID: "000000000012", Kind: teams.KindBrief, From: teams.FromYou, To: "review", ToKey: "krev", Text: "Check the retry backoff.\nSkip the e2e suite."}
	line := teamLine(member, entry)
	if line != "◆ brief changed by the person #12: Check the retry backoff.\n    Skip the e2e suite." {
		t.Fatalf("line = %q", line)
	}
	if teamLine(other, entry) != "" || teamLine(manager, entry) != "" {
		t.Fatal("a brief reached someone else")
	}
	for _, r := range []teamRole{member, manager} {
		if teamWakes(r, entry) {
			t.Fatalf("%s was woken by a brief", r.handle)
		}
	}
	// What the surface reads back from the delivery.
	got := teamNewsLines(teamNewsGroup(member, []string{line}))
	want := TeamLine{Team: "Release", From: teams.FromYou, Kind: teams.KindBrief, Text: "Check the retry backoff.\nSkip the e2e suite.", Thread: "000000000012"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("read back %+v, want %+v", got, want)
	}
	byManager := entry
	byManager.From = teams.FromManager
	if got := teamNewsLines(teamNewsGroup(member, []string{teamLine(member, byManager)})); len(got) != 1 || got[0].From != teams.FromManager || got[0].Kind != teams.KindBrief {
		t.Fatalf("manager's change read back %+v", got)
	}
}

// THE REQUEST A MEMBER WITH A ROLE AND A CHANGED BRIEF ACTUALLY SENDS. The app
// gives a role as cmd/codeaf's tellRole does: the member's own message, the
// person's line. The brief comes as team traffic, the start and then the change
// in place, each under the delivery's authority sentence (not the person's
// words; the person's own words outrank the manager). After the change both
// are in the request: the role's words untouched as the person's line, before
// the change; the start brief still in the history; the change as a team line.
// Nothing withdraws the role or ranks the brief above it.
func TestARoleAndAChangedBriefAreBothInTheMembersRequest(t *testing.T) {
	fixture := newTeamFixture(t, true)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindStart, From: teams.FromManager, To: "parser", Text: "Rewrite the lexer."})
	time.Sleep(2 * time.Millisecond)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("on it"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("on the new brief"), nil
		},
	}}
	parser := teamAgent(t, fixture, fixture.parser, completer, nil)
	const role = "Your role in harbor is reviewer.\n\nReview diffs; never change code."
	events, err := parser.Submit(context.Background(), role)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if _, err := teams.ReviseBrief(fixture.profile, fixture.teamID, convKeyOf(t, fixture.parser), teams.BriefChange{By: teams.FromYou, Text: "Rewrite the lexer; skip the e2e suite."}); err != nil {
		t.Fatal(err)
	}
	events, err = parser.Submit(context.Background(), "carry on")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)

	at := func(messages []ai.Message, has string) (int, string) {
		for i, m := range messages {
			if text := messageText(m); m.Role == "user" && strings.Contains(text, has) {
				return i, text
			}
		}
		return -1, ""
	}
	const authority = "The person's own words in this conversation outrank the manager"
	for n, request := range [][]ai.Message{completer.request(0), completer.request(1)} {
		i, said := at(request, "Review diffs; never change code.")
		if i < 0 || strings.Contains(said, "Team traffic") || strings.Contains(said, "not the person's words") {
			t.Fatalf("request %d: the role is not the person's own line:\n%q", n, said)
		}
		if j, start := at(request, teamBriefWord+" #1: Rewrite the lexer."); j < 0 || !strings.Contains(start, "not the person's words") || !strings.Contains(start, authority) {
			t.Fatalf("request %d: the start brief is not team traffic under the authority sentence:\n%q", n, start)
		}
	}
	request := completer.request(1)
	role0, _ := at(request, "Review diffs; never change code.")
	k, change := at(request, teamBriefChangedWord+"the person #")
	if k < 0 || !strings.Contains(change, ": Rewrite the lexer; skip the e2e suite.") || !strings.Contains(change, "not the person's words") || !strings.Contains(change, authority) {
		t.Fatalf("the change is not a team line under the authority sentence:\n%q", change)
	}
	if role0 > k {
		t.Fatalf("the role (%d) came after the change (%d)", role0, k)
	}
}
