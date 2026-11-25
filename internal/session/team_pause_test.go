package session

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/plandb"
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

// A DIRECTIVE READ INSIDE A TURN THE PERSON OPENED is handed-out work all the
// same: stopping that turn leaves it undone, and the manager was told the
// member would read it. The ending is the person's stop of that work.
func TestTeamPersonStopOfATurnThatWasHandedADirective(t *testing.T) {
	fastTeamWake(t)
	// Auto-wake off, so the directive waits for the turn the person starts.
	fixture := makeTeamFixture(t, true, false)
	streaming := make(chan struct{})
	web := teamAgent(t, fixture, fixture.web, &scriptedCompleter{steps: []step{thinkingStep(streaming)}}, nil)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Fix the header.", At: time.Now()})
	events := mustSubmit(t, web, "what are you working on?")
	select {
	case <-streaming:
	case <-time.After(5 * time.Second):
		t.Fatal("the member did not start")
	}
	web.Interrupt()
	collect(t, events)
	web.settleTeamEvents()
	if said := trafficEvents(t, fixture, teams.StoppedByPerson); len(said) != 1 {
		t.Fatalf("a stopped turn that had been handed a directive was logged as %+v", trafficEvents(t, fixture, "stopped"))
	}
}

// THE APP'S OWN STOPS ARE NOT THE PERSON'S. Quitting, and ending a paused
// member's stray wake, cancel through another door; the manager is told
// nothing about a choice the person did not make.
func TestTeamMachineryStopIsNotThePersons(t *testing.T) {
	for _, door := range []StopDoor{StopByClosing, StopByWorkStopped, StopByTakeover} {
		t.Run(string(door), func(t *testing.T) {
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
			web.InterruptFor(door)
			deadline := time.Now().Add(5 * time.Second)
			for len(trafficEvents(t, fixture, "stopped")) == 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			web.settleTeamEvents()
			quietFor()
			if said := trafficEvents(t, fixture, teams.StoppedByPerson); len(said) != 0 {
				t.Fatalf("a stop through %q was logged as the person's: %+v", door, said)
			}
			if got := managerCalls.requests(); got != 0 {
				t.Fatalf("the manager was woken %d times by a stop through %q", got, door)
			}
		})
	}
}

// A STANDBY MEMBER RENAMED BEFORE ITS FIRST WORK still gets its role line: the
// start that made the membership is its own by identity, not by the handle it
// was addressed to.
func TestTeamStandbyRoleLineSurvivesARename(t *testing.T) {
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
				team.Members[i].Standby = true
				team.Members[i].Started, team.Members[i].StartEntry, team.Members[i].StartID = true, start, start
			}
		}
		return file.SetHandle(fixture.teamID, convKeyOf(t, fixture.web), "front")
	})
	if err != nil {
		t.Fatal(err)
	}
	manager := teamAgent(t, fixture, fixture.manager, oneAnswer(1), nil)
	webCalls := oneAnswer(1)
	teamAgent(t, fixture, fixture.web, webCalls, nil)
	sendDirective(t, manager, "front")
	waitRequests(t, webCalls, 1)
	asked := userTextIn(webCalls.request(0))
	for _, want := range []string{teamStandbyBriefWord, "Owns the web frontend and its tests.", "Fix the header."} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the renamed member's first turn lacks %q:\n%s", want, asked)
		}
	}
}

// A DIRECTIVE HELD FOR A PAUSED MEMBER NOTHING HAD OPEN outlives a restart: the
// sender's "held" line is the record that it is owed.
func TestTeamHeldDirectiveSurvivesForAMemberThatWasNeverOpen(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	setPaused(t, fixture, fixture.web, true)
	manager := teamAgent(t, fixture, fixture.manager, oneAnswer(1), nil)
	sendDirective(t, manager, "web")
	setPaused(t, fixture, fixture.web, false)
	file, err := teams.Load(fixture.profile)
	if err != nil {
		t.Fatal(err)
	}
	// Opened long after: nothing but the held line says the directive is owed.
	late := time.Now().Add(time.Hour)
	if !TeamOpenPending(fixture.profile, fixture.web, file, teams.Defaults{Wake: true}, late) {
		t.Fatal("the held directive does not ask for the resumed member to be opened")
	}
	webCalls := oneAnswer(1)
	teamAgent(t, fixture, fixture.web, webCalls, func(config *Config) { config.TeamListeningSince = late })
	waitRequests(t, webCalls, 1)
	if asked := userTextIn(webCalls.request(0)); strings.Count(asked, "Fix the header.") != 1 {
		t.Fatalf("the member opened after the resume was not handed the held directive once:\n%s", asked)
	}
}

// THE PAUSE IS READ AGAIN BEFORE A WAKE HANDS ANYTHING OVER, whatever the look
// that chose the wake had read: called with roles from before the pause, the
// wake holds.
func TestTeamWakeMemberReadsThePauseAfresh(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	webCalls := oneAnswer(1)
	web := teamAgent(t, fixture, fixture.web, webCalls, func(config *Config) { config.teamWatchManual = true })
	var stale []teamRole
	for _, role := range web.teamRoles() {
		if !role.manager {
			stale = append(stale, role)
		}
	}
	if len(stale) != 1 || stale[0].paused {
		t.Fatalf("the member's roles before the pause are %+v", stale)
	}
	setPaused(t, fixture, fixture.web, true)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Fix the header.", At: time.Now()})
	web.teamWakeMember(fixture.profile, stale, time.Now())
	quietFor()
	if got := webCalls.requests(); got != 0 {
		t.Fatalf("a wake chosen before the pause started %d turns", got)
	}
	if woke := trafficEvents(t, fixture, "woke @web"); len(woke) != 0 {
		t.Fatalf("the traffic says it woke: %+v", woke)
	}
	// And the directive is still there for the resume.
	setPaused(t, fixture, fixture.web, false)
	web.teamWatchTick(fixture.profile, time.Now())
	waitRequests(t, webCalls, 1)
	if asked := userTextIn(webCalls.request(0)); !strings.Contains(asked, "Fix the header.") {
		t.Fatalf("the resumed member was not handed the directive:\n%s", asked)
	}
}

// A PAUSE HOLDS THE CONVERSATION IN EVERY TEAM, a resume lifts it wherever it
// is pressed, and a conversation that manages a team is not paused at all.
func TestTeamPauseIsTheConversations(t *testing.T) {
	fixture, sharedTeam, _ := linkedFixture(t)
	key := convKeyOf(t, fixture.web)
	roles := func() []teamRole {
		file, err := teams.Load(fixture.profile)
		if err != nil {
			t.Fatal(err)
		}
		return rolesFor(file, []string{key}, teams.Defaults{Wake: true})
	}
	if got := len(roles()); got != 2 {
		t.Fatalf("the fixture's web member is in %d teams, want two", got)
	}
	setPaused(t, fixture, fixture.web, true)
	for _, role := range roles() {
		if !role.manager && !role.paused {
			t.Fatalf("paused in one team, the member is not held in %q", role.name)
		}
	}
	// Resumed from the other team.
	if err := teams.Update(fixture.profile, func(file *teams.File) error { return file.SetPaused(sharedTeam, key, false) }); err != nil {
		t.Fatal(err)
	}
	for _, role := range roles() {
		if role.paused {
			t.Fatalf("resumed from another team, the member is still held in %q", role.name)
		}
	}
}

func sendTeamNote(t *testing.T, manager *Agent, to string) {
	t.Helper()
	if said, failed := callTool(t, manager.teamSendTool, `{"to":"`+to+`","text":"The header is blue now.","kind":"note"}`); failed {
		t.Fatalf("team_send refused the note: %s", said)
	}
}

// NOTHING BUT THE PERSON STARTS A PAUSED MEMBER, AND THE ENGINE DECIDES IT.
// A command's ending woke the paused member, and only the desktop app, where
// it held the member, ended that turn after it began; an earlier fault had the
// turn's first request carry the held directive, so the resume started
// nothing. Here no interface is in the loop: a command's ending and a watch's
// firing start no turn, each is kept in the conversation as a record that says
// why, the held directive stays held, and the turn Resume starts reads the
// directive and the records together. After that a command's ending starts a
// turn again.
func TestTeamPausedMembersOtherWakeLeavesTheDirectiveHeld(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	setPaused(t, fixture, fixture.web, true)
	manager := teamAgent(t, fixture, fixture.manager, oneAnswer(1), nil)
	webCalls := oneAnswer(4)
	web := teamAgent(t, fixture, fixture.web, webCalls, nil)
	sendDirective(t, manager, "web")
	quietFor()

	web.enqueueJobNote("job 8 exited 0")
	web.enqueueWatchNote("checks", "the checks settled", true)
	quietFor()
	if got := webCalls.requests(); got != 0 {
		t.Fatalf("a paused member was started %d times by a command's ending and a watch", got)
	}
	journal, err := os.ReadFile(fixture.web)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"job 8 exited 0", "the checks settled"} {
		if !strings.Contains(string(journal), want) {
			t.Fatalf("%q was not kept as a record:\n%s", want, journal)
		}
	}
	if got := strings.Count(string(journal), teamPauseHeldRule); got != 2 {
		t.Fatalf("the record said why it started no turn %d times, want 2", got)
	}

	setPaused(t, fixture, fixture.web, false)
	waitRequests(t, webCalls, 1)
	asked := userTextIn(webCalls.request(0))
	if strings.Count(asked, "Fix the header.") != 1 || !strings.Contains(asked, teamWakeMemberLead) {
		t.Fatalf("the resumed member was not started on the directive that waited, once:\n%s", asked)
	}
	for _, want := range []string{"job 8 exited 0", "the checks settled", teamPauseHeldRule} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the turn Resume started did not read %q:\n%s", want, asked)
		}
	}
	waitQuiet(t, web)
	web.enqueueJobNote("job 9 exited 0")
	waitRequests(t, webCalls, 2)
}

// NEWS A PAUSED MEMBER'S OWN TURN LEFT UNREAD STARTS NO TURN AT ITS END. The
// person typed into a paused member, which is theirs to do, and a command
// ended after that turn's last request had gone out. The turn's end records
// the ending and would start one more turn to answer it, a turn nobody typed
// in a conversation the person paused. It is kept as a record instead.
func TestTeamPausedMembersTurnEndStartsNoTurnOnACommandsEnding(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	setPaused(t, fixture, fixture.web, true)
	var web *Agent
	calls := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			web.enqueueJobNote("job 8 exited 0")
			return textResponse("started"), nil
		},
		okStep(),
	}}
	web = teamAgent(t, fixture, fixture.web, calls, nil)
	collect(t, mustSubmit(t, web, "start the build"))
	waitQuiet(t, web)
	quietFor()
	if got := calls.requests(); got != 1 {
		t.Fatalf("the paused member made %d model calls, want only the person's turn", got)
	}
	journal, err := os.ReadFile(fixture.web)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(journal), "job 8 exited 0") || strings.Count(string(journal), teamPauseHeldRule) != 1 {
		t.Fatalf("the ending was not kept as one record that says why it started no turn:\n%s", journal)
	}
}

// A TASK LANDING IN A PAUSED MEMBER IS KEPT AS ITS RECEIPT. The landing would
// start the turn that answers it; the person paused the conversation, so the
// result is recorded with its task's identity, as a late stop records it, and
// says why no turn began.
func TestTeamPausedMembersTaskLandingIsKeptAsARecord(t *testing.T) {
	fixture := newWakingTeamFixture(t)
	setPaused(t, fixture, fixture.web, true)
	calls := oneAnswer(1)
	web := teamAgent(t, fixture, fixture.web, calls, func(config *Config) { config.teamWatchManual = true })
	store := askStore(t, "repair parser")
	defer store.Close()
	question := "What did the repair find?"
	if _, err := store.Revise(store.RootID(), plandb.TaskPatch{Question: &question}); err != nil {
		t.Fatal(err)
	}
	run := &beltRun{store: store, root: store.RootID(), row: 98, title: "Parser", bringingHome: true}
	web.beltMu.Lock()
	web.beltRun = run
	web.beltMu.Unlock()
	t.Cleanup(func() { web.beltMu.Lock(); web.beltRun = nil; web.beltMu.Unlock() })
	web.deliverBeltRunLanding(run, RunSummary{Outcome: beltRunOutcomeDone, Result: "Preserved result."}, RunLanding{})
	web.mu.Lock()
	woke := web.running || len(web.steering) != 0
	web.mu.Unlock()
	if woke || calls.requests() != 0 {
		t.Fatal("a task landing started a paused member's turn")
	}
	entries := web.Transcript()
	if len(entries) != 1 || !entries[0].RoutineCompletion || len(entries[0].CompletionTasks) != 1 || entries[0].CompletionTasks[0].ID != run.row {
		t.Fatalf("the held landing lost its task's identity: %+v", entries)
	}
	journal, err := os.ReadFile(fixture.web)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(journal), "Preserved result.") || strings.Count(string(journal), teamPauseHeldRule) != 1 {
		t.Fatalf("the landing was not kept with why it started no turn:\n%s", journal)
	}
}

// A HOLD ENDS WHEN ITS LINES ARE READ. The person typed into the paused member
// and that turn read the held directive; a note sent afterwards wakes nobody,
// and the resume used to start a second turn on it because the hold still stood.
func TestTeamHeldDirectiveThePersonsTurnReadStartsNothingOnResume(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	setPaused(t, fixture, fixture.web, true)
	manager := teamAgent(t, fixture, fixture.manager, oneAnswer(1), nil)
	webCalls := oneAnswer(4)
	web := teamAgent(t, fixture, fixture.web, webCalls, nil)
	sendDirective(t, manager, "web")
	quietFor()

	collect(t, mustSubmit(t, web, "what is waiting for you?"))
	if asked := userTextIn(webCalls.request(0)); strings.Count(asked, "Fix the header.") != 1 {
		t.Fatalf("the person's own turn in a paused member did not read what was waiting:\n%s", asked)
	}
	waitQuiet(t, web)
	sendTeamNote(t, manager, "web")
	quietFor()

	setPaused(t, fixture, fixture.web, false)
	quietFor()
	if got := webCalls.requests(); got != 1 {
		t.Fatalf("the resumed member made %d model calls, want only the person's turn: a note wakes nobody", got)
	}
}
