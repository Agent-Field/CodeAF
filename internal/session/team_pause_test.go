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

	said := sendDirective(t, manager, "web")
	if !strings.Contains(said, "@web did NOT start: "+teamPausedWhy) || !strings.Contains(said, teamNotUnderWayRule) {
		t.Fatalf("the send does not say the member is paused: %s", said)
	}
	if strings.Contains(said, "started a turn") {
		t.Fatalf("the send reports a start for a paused member: %s", said)
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
	// The manager's own view says so, through the tool it reads its team with.
	if status, failed := callTool(t, manager.teamStatusTool, `{}`); failed || !strings.Contains(status, "- @web, Frontend developer \"web frontend\": "+teams.DigestStandby) {
		t.Fatalf("team_status does not show the member on standby:\n%s", status)
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
	// The surface draws the role line as the manager's start, not as nothing.
	drawn := false
	for _, line := range teamNewsLines(asked) {
		drawn = drawn || (line.Kind == teams.KindStart && line.From == teams.FromManager && strings.Contains(line.Text, "Owns the web frontend"))
	}
	if !drawn {
		t.Fatalf("the delivery card lost the standing role line: %+v", teamNewsLines(asked))
	}
	// Standby is over once the line is handed to it, and the manager's view stops saying it.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		file, err := teams.Load(fixture.profile)
		if err != nil {
			t.Fatal(err)
		}
		if member, _ := file.Teams[0].ByHandle("web"); !member.Standby {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status, _ := callTool(t, manager.teamStatusTool, `{}`); strings.Contains(status, teams.DigestStandby) {
		t.Fatalf("a member that has been handed its role still reads as on standby:\n%s", status)
	}
}

// STANDBY OUTLIVES A RESTART. A crew made today and first given work after the
// app was relaunched still hands each helper its role line with the directive,
// however old the start line is by then.
func TestTeamStandbyRoleLineSurvivesALateStart(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	long := time.Now().Add(-3 * time.Hour)
	start, err := teams.AppendTrafficID(fixture.profile, fixture.teamID, teams.Entry{
		Kind: teams.KindStart, From: teams.FromManager, To: "web", Text: "Owns the web frontend and its tests.", At: long,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A directive sent while nothing had the member open, also long ago.
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Fix the header.", At: long.Add(time.Minute)})
	err = teams.Update(fixture.profile, func(file *teams.File) error {
		team := &file.Teams[0]
		for i := range team.Members {
			if team.Members[i].Handle == "web" {
				team.Members[i].Standby = true
				team.Members[i].Started, team.Members[i].StartEntry, team.Members[i].StartID = true, start, start
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	file, err := teams.Load(fixture.profile)
	if err != nil {
		t.Fatal(err)
	}
	if !TeamOpenPending(fixture.profile, fixture.web, file, teams.Defaults{Wake: true}, time.Now()) {
		t.Fatal("a directive to a standby member that was never opened does not ask for it to be opened")
	}
	webCalls := oneAnswer(1)
	teamAgent(t, fixture, fixture.web, webCalls, nil)
	waitRequests(t, webCalls, 1)
	asked := userTextIn(webCalls.request(0))
	for _, want := range []string{teamStandbyBriefWord, "Owns the web frontend and its tests.", "Fix the header."} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the late first turn lacks %q:\n%s", want, asked)
		}
	}
}

// A HELD DIRECTIVE OUTLIVES A RESTART TOO, for a member that had never taken a
// turn in the team and so had no cursor written down.
func TestTeamHeldDirectiveSurvivesARestartOfAMemberThatNeverRan(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	setPaused(t, fixture, fixture.web, true)
	manager := teamAgent(t, fixture, fixture.manager, oneAnswer(1), nil)
	first := teamAgent(t, fixture, fixture.web, oneAnswer(1), nil)
	sendDirective(t, manager, "web")
	quietFor()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	// Much later, in another process: the directive is older than anything a
	// fresh conversation would look back to.
	setPaused(t, fixture, fixture.web, false)
	file, err := teams.Load(fixture.profile)
	if err != nil {
		t.Fatal(err)
	}
	if !TeamOpenPending(fixture.profile, fixture.web, file, teams.Defaults{Wake: true}, time.Now().Add(time.Hour)) {
		t.Fatal("the held directive does not ask for the resumed member to be opened")
	}
	webCalls := oneAnswer(1)
	teamAgent(t, fixture, fixture.web, webCalls, func(config *Config) { config.TeamListeningSince = time.Now().Add(time.Hour) })
	waitRequests(t, webCalls, 1)
	if asked := userTextIn(webCalls.request(0)); strings.Count(asked, "Fix the header.") != 1 {
		t.Fatalf("the member reopened after the resume was not handed the held directive once:\n%s", asked)
	}
}

// A DIRECTIVE SENT IN THE SAME BREATH AS THE PAUSE IS HELD, not started on a
// reading of the teams file from before the pause.
func TestTeamDirectiveRightAfterThePauseIsHeld(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	manager := teamAgent(t, fixture, fixture.manager, oneAnswer(1), nil)
	webCalls := oneAnswer(1)
	teamAgent(t, fixture, fixture.web, webCalls, nil)
	quietFor() // both conversations have read the teams file with nobody paused
	setPaused(t, fixture, fixture.web, true)
	if said := sendDirective(t, manager, "web"); !strings.Contains(said, "@web did NOT start: "+teamPausedWhy) {
		t.Fatalf("the send does not say the member is paused: %s", said)
	}
	quietFor()
	if got := webCalls.requests(); got != 0 {
		t.Fatalf("the member paused a moment before the directive made %d model calls", got)
	}
	if woke := trafficEvents(t, fixture, "woke @web"); len(woke) != 0 {
		t.Fatalf("the traffic says it woke: %+v", woke)
	}
	setPaused(t, fixture, fixture.web, false)
	waitRequests(t, webCalls, 1)
	if asked := userTextIn(webCalls.request(0)); !strings.Contains(asked, "Fix the header.") {
		t.Fatalf("the resumed member was not handed the directive:\n%s", asked)
	}
}

// THE PERSON'S STOP OF A TURN THE TEAM STARTED REACHES ITS MANAGER, which
// handed that work out and is told nothing else about its ending.
func TestTeamPersonStopWakesTheManager(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	managerCalls := oneAnswer(1)
	manager := teamAgent(t, fixture, fixture.manager, managerCalls, nil)
	streaming := make(chan struct{})
	web := teamAgent(t, fixture, fixture.web, &scriptedCompleter{steps: []step{thinkingStep(streaming)}}, nil)
	sendDirective(t, manager, "web")
	select {
	case <-streaming:
	case <-time.After(5 * time.Second):
		t.Fatal("the member did not start on the directive")
	}
	web.Interrupt()
	waitRequests(t, managerCalls, 1)
	asked := userTextIn(managerCalls.request(0))
	if !strings.Contains(asked, "@web"+teamStoppedByPersonLine) {
		t.Fatalf("the manager was not told the person stopped the work it handed out:\n%s", asked)
	}
	if stopped := trafficEvents(t, fixture, teams.StoppedByPerson); len(stopped) != 1 || stopped[0].State != teams.StateIdle {
		t.Fatalf("the stop was logged as %+v", stopped)
	}
}

// A TURN THE PERSON OPENED AND STOPPED IS THEIR OWN REQUEST, which the manager
// never handed out: it ends "stopped" and wakes nobody.
func TestTeamPersonStopOfTheirOwnTurnTellsTheManagerNothing(t *testing.T) {
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
	web.settleTeamEvents()
	quietFor()
	if got := managerCalls.requests(); got != 0 {
		t.Fatalf("the manager was woken %d times by a stop of the person's own turn", got)
	}
	if said := trafficEvents(t, fixture, teams.StoppedByPerson); len(said) != 0 {
		t.Fatalf("the person's own turn was logged as work stopped by the person: %+v", said)
	}
	if said := trafficEvents(t, fixture, "stopped"); len(said) != 1 {
		t.Fatalf("the ending was logged %d times, want once as stopped", len(said))
	}
}

// AN INTERFACE PERFORMS A MANAGER'S STOP THROUGH THE PERSON'S DOOR (the app, the
// terminal and a remote window all call Interrupt on reading the stop line),
// usually before the member's own session reads that line. The ending is the
// manager's all the same: the log says who stopped it.
func TestTeamManagerStopPerformedByAnInterfaceIsNotThePersons(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	managerCalls := oneAnswer(1)
	manager := teamAgent(t, fixture, fixture.manager, managerCalls, nil)
	streaming := make(chan struct{})
	web := teamAgent(t, fixture, fixture.web, &scriptedCompleter{steps: []step{thinkingStep(streaming)}}, nil)
	sendDirective(t, manager, "web")
	select {
	case <-streaming:
	case <-time.After(5 * time.Second):
		t.Fatal("the member did not start on the directive")
	}
	// The manager's stop line, then the interface's cancel, in one breath.
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindStop, From: teams.FromManager, To: "web", Member: convKeyOf(t, fixture.web), Text: "wrong way", At: time.Now()})
	web.Interrupt()
	deadline := time.Now().Add(5 * time.Second)
	for len(trafficEvents(t, fixture, "stopped")) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	web.settleTeamEvents()
	quietFor()
	if said := trafficEvents(t, fixture, teams.StoppedByPerson); len(said) != 0 {
		t.Fatalf("the manager's own stop was logged as the person's: %+v", said)
	}
	if got := managerCalls.requests(); got != 0 {
		t.Fatalf("the manager was woken %d times by its own stop", got)
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
