package session

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

func setPaused(t *testing.T, fixture teamFixture, transcript string, paused bool) {
	t.Helper()
	err := teams.Update(fixture.profile, func(file *teams.File) error {
		return file.SetPaused(fixture.teamID, convKeyOf(t, transcript), paused)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A DIRECTIVE TO A MEMBER THE PERSON PAUSED STARTS NOTHING, the manager is told
// so in the answer, the traffic says it once, and the directive is still there
// when the pause is lifted: the member starts on it then.
func TestTeamDirectiveToAPausedMemberWaitsForResume(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	setPaused(t, fixture, fixture.web, true)
	manager := teamAgent(t, fixture, fixture.manager, oneAnswer(1), nil)
	webCalls := oneAnswer(1)
	teamAgent(t, fixture, fixture.web, webCalls, nil)

	begun := time.Now()
	said := sendDirective(t, manager, "web")
	if !strings.Contains(said, "@web did NOT start: "+teamPausedWhy) || !strings.Contains(said, teamNotUnderWayRule) {
		t.Fatalf("the send does not say the member is paused: %s", said)
	}
	if strings.Contains(said, "started a turn") {
		t.Fatalf("the send reports a start for a paused member: %s", said)
	}
	if took := time.Since(begun); took > teamSendSettle/2 {
		t.Fatalf("a paused member is known from the teams file, yet the send waited %s", took)
	}
	quietFor()
	if got := webCalls.requests(); got != 0 {
		t.Fatalf("the paused member made %d model calls", got)
	}
	if woke := trafficEvents(t, fixture, "woke @web"); len(woke) != 0 {
		t.Fatalf("the traffic says a paused member woke: %+v", woke)
	}
	held := trafficEvents(t, fixture, teamHeldPausedText("web"))
	if len(held) != 1 || held[0].From != teams.FromSystem || held[0].Answers == "" {
		t.Fatalf("the hold was said %d times, want once, answering the directive: %+v", len(held), held)
	}
	// THE MEASURED CASE SENT A SECOND DIRECTIVE to the same paused member a few
	// minutes later and was told it had started again. It is told the same
	// thing as the first time, and the member still makes no call.
	if again := sendDirective(t, manager, "web"); !strings.Contains(again, "@web did NOT start: "+teamPausedWhy) {
		t.Fatalf("the second send does not say the member is paused: %s", again)
	}
	quietFor()
	if got := webCalls.requests(); got != 0 {
		t.Fatalf("the paused member made %d model calls after the second directive", got)
	}
	if held := trafficEvents(t, fixture, teamHeldPausedText("web")); len(held) != 2 {
		t.Fatalf("two held directives were said %d times", len(held))
	}
	// And the manager's own view of its team says why nothing is moving.
	status, failed := callTool(t, manager.teamStatusTool, `{}`)
	if failed || !strings.Contains(status, teams.DigestPaused) || !strings.Contains(status, "has not started on #") {
		t.Fatalf("team_status does not show the pause and the waiting directive:\n%s", status)
	}

	setPaused(t, fixture, fixture.web, false)
	waitRequests(t, webCalls, 1)
	asked := userTextIn(webCalls.request(0))
	if strings.Count(asked, "Fix the header.") != 2 || !strings.Contains(asked, teamWakeMemberLead) {
		t.Fatalf("the resumed member was not handed both directives that waited, once each:\n%s", asked)
	}
	if woke := trafficEvents(t, fixture, "woke @web"); len(woke) != 1 {
		t.Fatalf("the resumed member's wake was logged %d times", len(woke))
	}
}

// A paused member that is not open is not a reason to open it.
func TestTeamOpenPendingLeavesAPausedMemberClosed(t *testing.T) {
	fixture := newWakingTeamFixture(t)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Fix the header."})
	pending := func() bool {
		file, err := teams.Load(fixture.profile)
		if err != nil {
			t.Fatal(err)
		}
		return TeamOpenPending(fixture.profile, fixture.web, file, teams.Defaults{Wake: true}, time.Now().Add(-time.Minute))
	}
	if !pending() {
		t.Fatal("a directive to an idle member is not pending")
	}
	setPaused(t, fixture, fixture.web, true)
	if pending() {
		t.Fatal("a paused member's directive asks for its conversation to be opened")
	}
	setPaused(t, fixture, fixture.web, false)
	if !pending() {
		t.Fatal("the directive is no longer pending after the resume")
	}
}

// THE MANAGER IS NOT PAUSED: the person talks to it directly.
func TestTeamManagerIsNotPaused(t *testing.T) {
	fixture := newWakingTeamFixture(t)
	err := teams.Update(fixture.profile, func(file *teams.File) error {
		return file.SetPaused(fixture.teamID, convKeyOf(t, fixture.manager), true)
	})
	if err == nil || !strings.Contains(err.Error(), "is not paused") {
		t.Fatalf("pausing the manager answered %v", err)
	}
}

// A MEMBER MADE ON STANDBY STARTS NOTHING ON ITS START LINE, and its first
// directive hands it the role and the work together.
func TestTeamStandbyMemberStartsOnItsFirstDirective(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	start, err := teams.AppendTrafficID(fixture.profile, fixture.teamID, teams.Entry{
		Kind: teams.KindStart, From: teams.FromManager, To: "web", Text: "Owns the web frontend and its tests.", At: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = teams.Update(fixture.profile, func(file *teams.File) error {
		team := &file.Teams[0]
		for i := range team.Members {
			if team.Members[i].Handle == "web" {
				team.Members[i].Standby, team.Members[i].Role = true, "Frontend developer"
				team.Members[i].Started, team.Members[i].StartEntry, team.Members[i].StartID = true, start, start
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	manager := teamAgent(t, fixture, fixture.manager, oneAnswer(1), nil)
	webCalls := oneAnswer(1)
	teamAgent(t, fixture, fixture.web, webCalls, nil)
	quietFor()
	if got := webCalls.requests(); got != 0 {
		t.Fatalf("the standby member began %d turns on its start line", got)
	}
	if said := trafficEvents(t, fixture, teamStandbyText("web")); len(said) != 1 {
		t.Fatalf("standby was said %d times in the traffic, want once", len(said))
	}

	if said := sendDirective(t, manager, "web"); !strings.Contains(said, "@web started a turn on it.") {
		t.Fatalf("the first directive did not start the standby member: %s", said)
	}
	waitRequests(t, webCalls, 1)
	asked := userTextIn(webCalls.request(0))
	for _, want := range []string{teamStandbyBriefWord, "Owns the web frontend and its tests.", "Fix the header.", "Your role in this team: Frontend developer."} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the standby member's first turn lacks %q:\n%s", want, asked)
		}
	}
	if strings.Contains(asked, teamBriefWord+" #") {
		t.Fatalf("a standby start was handed over as a brief to begin:\n%s", asked)
	}
}

// THE PERSON'S STOP OF A MEMBER'S TURN REACHES ITS MANAGER, which handed that
// work out and is told nothing else about its ending.
func TestTeamPersonStopWakesTheManager(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	managerCalls := oneAnswer(1)
	teamAgent(t, fixture, fixture.manager, managerCalls, nil)
	streaming := make(chan struct{})
	web := teamAgent(t, fixture, fixture.web, &scriptedCompleter{steps: []step{thinkingStep(streaming)}}, nil)
	events := mustSubmit(t, web, "work on the header")
	select {
	case <-streaming:
	case <-time.After(5 * time.Second):
		t.Fatal("the member did not start")
	}
	web.Interrupt()
	collect(t, events)
	waitRequests(t, managerCalls, 1)
	asked := userTextIn(managerCalls.request(0))
	if !strings.Contains(asked, "@web's turn was "+teams.StoppedByPerson+" before it finished") {
		t.Fatalf("the manager was not told the person stopped its member:\n%s", asked)
	}
	if stopped := trafficEvents(t, fixture, teams.StoppedByPerson); len(stopped) != 1 || stopped[0].State != teams.StateIdle {
		t.Fatalf("the stop was logged as %+v", stopped)
	}
}

// AND ITS OWN STOP DOES NOT: the manager performed it and knows.
func TestTeamEventWakesManager(t *testing.T) {
	for _, c := range []struct {
		state, text string
		want        bool
	}{
		{teams.StateFinished, "finished", true},
		{teams.StateFailed, "failed: x", true},
		{teams.StateAsking, "asks: x", true},
		{teams.StateIdle, teams.StoppedByPerson, true},
		{teams.StateIdle, "stopped", false},
		{teams.StateRunning, "woke @web", false},
	} {
		if got := teamEventWakesManager(c.state, c.text); got != c.want {
			t.Errorf("%s %q wakes the manager: %v, want %v", c.state, c.text, got, c.want)
		}
	}
}
