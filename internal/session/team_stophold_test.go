package session

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/teams"
)

func okStep() step {
	return func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("ok"), nil }
}

// waitedStep starts a command the member asked to be told the ending of, and
// answers: the turn ends with its work waiting on that command.
func waitedStep(agent func() *Agent, started chan<- *job) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		registry := agent().jobs
		one, err := registry.newJob("fixture", jobKindBash)
		if err == nil {
			err = registry.add(one)
		}
		if err != nil {
			return nil, err
		}
		one.mu.Lock()
		one.notify = true
		one.mu.Unlock()
		started <- one
		return textResponse("ok"), nil
	}
}

// waitTeamEvents waits until the fixture's Traffic holds n events saying want.
func waitTeamEvents(t *testing.T, fixture teamFixture, want string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(trafficEvents(t, fixture, want)) < n {
		if time.Now().After(deadline) {
			t.Fatalf("the team's log holds %d %q events, want %d", len(trafficEvents(t, fixture, want)), want, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitManagerToldOfStop waits until the manager was woken with the line that
// says the person stopped work it had handed @web.
func waitManagerToldOfStop(t *testing.T, managerCalls *scriptedCompleter) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		for i := 0; i < managerCalls.requests(); i++ {
			if strings.Contains(userTextIn(managerCalls.request(i)), "@web"+teamStoppedByPersonLine) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the manager was not told the work it handed out was stopped")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitingMember is a member whose first turn, on the manager's directive,
// starts a command it waits on; later turns take the steps after.
func waitingMember(t *testing.T, after ...step) (teamFixture, *scriptedCompleter, *Agent, *scriptedCompleter, *jobWakeClock, chan *job) {
	t.Helper()
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	managerCalls := oneAnswer(8)
	manager := teamAgent(t, fixture, fixture.manager, managerCalls, nil)
	var web *Agent
	started := make(chan *job, 2)
	calls := &scriptedCompleter{steps: append([]step{waitedStep(func() *Agent { return web }, started)}, after...)}
	web = teamAgent(t, fixture, fixture.web, calls, nil)
	clock := &jobWakeClock{}
	web.jobWakeAfter = clock.after
	sendDirective(t, manager, "web")
	waitRequests(t, calls, 1)
	waitTeamEvents(t, fixture, "finished", 1)
	return fixture, managerCalls, web, calls, clock, started
}

// A STOP OF THE TURN A COMMAND'S ENDING STARTED IS THE STOP OF HANDED WORK. The
// member's first turn on the directive finished waiting on a command; its
// ending woke a second turn, still on the manager's work, and the person
// stopped that. The manager is told once, as for the turn the directive woke.
func TestTeamPersonStopOfATurnACommandEndingStartedTellsTheManager(t *testing.T) {
	streaming := make(chan struct{})
	fixture, managerCalls, web, _, clock, started := waitingMember(t, thinkingStep(streaming), okStep())
	web.jobs.settleExit(<-started, 0)
	clock.advance(jobWakeDelay)
	select {
	case <-streaming:
	case <-time.After(5 * time.Second):
		t.Fatal("the command's ending did not start the member's next turn")
	}
	web.Interrupt()
	waitQuiet(t, web)
	waitTeamEvents(t, fixture, teams.StoppedByPerson, 1)
	waitManagerToldOfStop(t, managerCalls)
	quietFor()
	web.settleTeamEvents()
	if said := trafficEvents(t, fixture, teams.StoppedByPerson); len(said) != 1 {
		t.Fatalf("the stop was logged %d times, want once", len(said))
	}
}

// AND SO IS A STOP WHILE IT WAITS ON A SECOND COMMAND. The turn the first
// command's ending started finished waiting on another; the stop then finds no
// turn, tells the manager, and the second command's ending starts nothing.
func TestTeamPersonStopWhileWaitingOnASecondCommandTellsTheManager(t *testing.T) {
	var web *Agent
	second := make(chan *job, 1)
	fixture, managerCalls, member, calls, clock, started := waitingMember(t, waitedStep(func() *Agent { return web }, second), okStep())
	web = member
	web.jobs.settleExit(<-started, 0)
	clock.advance(jobWakeDelay)
	waitRequests(t, calls, 2)
	waitTeamEvents(t, fixture, "finished", 2)

	web.Interrupt()
	waitTeamEvents(t, fixture, teams.StoppedByPerson, 1)
	waitManagerToldOfStop(t, managerCalls)
	web.jobs.settleExit(<-second, 0)
	clock.advance(jobWakeDelay)
	quietFor()
	web.settleTeamEvents()
	if got := calls.requests(); got != 2 {
		t.Fatalf("the stopped member was asked %d times, want its two turns before the stop", got)
	}
	if said := trafficEvents(t, fixture, teams.StoppedByPerson); len(said) != 1 {
		t.Fatalf("the stop was logged %d times, want once", len(said))
	}
}

// A STOP OF THE PERSON'S OWN TURN WHILE HANDED WORK WAITS IS SAID TOO. The
// member waits on a command for its manager's work; the person asked it
// something else and stopped the answer. The hold then keeps the command's
// ending from starting a turn, so the manager is told or the work would stall
// with nobody knowing.
func TestTeamPersonStopOfTheirOwnTurnWhileHandedWorkWaitsTellsTheManager(t *testing.T) {
	streaming := make(chan struct{})
	fixture, managerCalls, web, calls, clock, started := waitingMember(t, thinkingStep(streaming), okStep())
	events := mustSubmit(t, web, "status?")
	select {
	case <-streaming:
	case <-time.After(5 * time.Second):
		t.Fatal("the person's turn did not start")
	}
	web.Interrupt()
	collect(t, events)
	waitTeamEvents(t, fixture, teams.StoppedByPerson, 1)
	waitManagerToldOfStop(t, managerCalls)

	web.jobs.settleExit(<-started, 0)
	clock.advance(jobWakeDelay)
	quietFor()
	web.settleTeamEvents()
	if got := calls.requests(); got != 2 {
		t.Fatalf("the stopped member was asked %d times, want the directive's turn and the person's", got)
	}
	journal, err := os.ReadFile(fixture.web)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(journal), teamStopHeldRule) {
		t.Fatalf("the command's ending was not kept as a record:\n%s", journal)
	}
	if said := trafficEvents(t, fixture, teams.StoppedByPerson); len(said) != 1 {
		t.Fatalf("the stop was logged %d times, want once", len(said))
	}
}

// THE HOLD WITHDRAWS A WAKE ALREADY ON ITS CLOCK. A command the member waited
// on ended moments before the stop, and its turn was seconds from starting:
// the stop keeps that turn from starting, tells the manager, and the ending is
// read by the turn somebody next means.
func TestTeamPersonStopWithdrawsACommandEndingAboutToStartATurn(t *testing.T) {
	fixture, managerCalls, web, calls, clock, started := waitingMember(t, okStep(), okStep())
	web.jobs.settleExit(<-started, 7)
	web.Interrupt()
	clock.advance(jobWakeDelay)
	waitTeamEvents(t, fixture, teams.StoppedByPerson, 1)
	waitManagerToldOfStop(t, managerCalls)
	quietFor()
	if got := calls.requests(); got != 1 {
		t.Fatalf("the stopped member was asked %d times, want only its first turn", got)
	}
	events := mustSubmit(t, web, "what happened?")
	collect(t, events)
	if asked := userTextIn(calls.request(1)); !strings.Contains(asked, "job 1 exited 7") {
		t.Fatalf("the person's next turn did not read the ending the stop held back:\n%s", asked)
	}
}

// A STOP OF A MEMBER HOLDS. A command the stopped turn left running
// ends later, and its ending is a record that starts no turn: not the lane a
// failure takes, not the one a command the work waited on takes, not a watch's
// firing. The manager's next hand-off starts the member, and from then on a
// command's ending starts its turn again.
func TestTeamPersonStopKeepsACommandEndingFromStartingATurn(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	manager := teamAgent(t, fixture, fixture.manager, oneAnswer(8), nil)
	streaming := make(chan struct{})
	calls := &scriptedCompleter{steps: []step{thinkingStep(streaming), okStep(), okStep(), okStep()}}
	web := teamAgent(t, fixture, fixture.web, calls, nil)
	sendDirective(t, manager, "web")
	select {
	case <-streaming:
	case <-time.After(5 * time.Second):
		t.Fatal("the member did not start on the directive")
	}
	one := settlementJob(t, web.jobs)
	web.Interrupt()
	waitQuiet(t, web)

	web.jobs.settleExit(one, 7)
	web.enqueueJobNote("job 8 exited 0")
	web.enqueueWatchNote("checks", "the checks settled", true)
	quietFor()
	if got := calls.requests(); got != 1 {
		t.Fatalf("the stopped member was asked %d times, want only the turn the person stopped", got)
	}
	journal, err := os.ReadFile(fixture.web)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"job 1 exited 7", "job 8 exited 0", "the checks settled"} {
		if !strings.Contains(string(journal), want) {
			t.Fatalf("the ending %q was not kept as a record:\n%s", want, journal)
		}
	}
	if got := strings.Count(string(journal), teamStopHeldRule); got != 3 {
		t.Fatalf("the record said why it started no turn %d times, want 3", got)
	}

	sendDirective(t, manager, "web")
	waitRequests(t, calls, 2)
	waitQuiet(t, web)
	web.enqueueJobNote("job 9 exited 0")
	waitRequests(t, calls, 3)
}

// A CONVERSATION IN NO TEAM IS UNTOUCHED: the ending of a command still starts
// its turn after the person stopped the one before.
func TestPersonStopOutsideATeamStillHearsACommandEnd(t *testing.T) {
	a, client, _, wakes, _ := jobWakeFixture(t)
	streaming := make(chan struct{})
	client.steps = []step{thinkingStep(streaming), okStep()}
	events := mustSubmit(t, a, "think about this for a while")
	select {
	case <-streaming:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn did not start")
	}
	a.Interrupt()
	collect(t, events)
	a.enqueueJobNote("job 1 exited 0")
	select {
	case stream := <-wakes:
		collect(t, stream)
	case <-time.After(5 * time.Second):
		t.Fatal("a command's ending started no turn in a conversation that is in no team")
	}
}

// A STOP THAT FINDS NO TURN BUT A COMMAND STILL RUNNING HOLDS TOO. The member
// started its command, finished its turn and waits to be told how it ended;
// the person's stop then is the stop of that work. Its manager is told so in
// the words a stopped turn uses, and the command's ending starts no turn.
func TestTeamPersonStopOfAMemberWaitingOnACommandHolds(t *testing.T) {
	fixture, managerCalls, web, calls, clock, started := waitingMember(t, okStep())

	web.Interrupt()
	waitTeamEvents(t, fixture, teams.StoppedByPerson, 1)
	web.jobs.settleExit(<-started, 0)
	clock.advance(jobWakeDelay)
	web.enqueueJobNote("job 8 exited 0")
	quietFor()
	if got := calls.requests(); got != 1 {
		t.Fatalf("the stopped member was asked %d times, want only its first turn", got)
	}
	if said := trafficEvents(t, fixture, teams.StoppedByPerson); len(said) != 1 {
		t.Fatalf("the stop was logged %d times, want once", len(said))
	}
	waitManagerToldOfStop(t, managerCalls)
}

// A COMMAND NOBODY WAITS ON IS HELD AND SAID TO NOBODY. The member finished its
// directive and left a server running; a later stop found only that. Its ending
// starts no turn, and the manager is not told finished work "is not done".
func TestTeamPersonStopOfAMemberWithACommandNobodyWaitsOn(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	managerCalls := oneAnswer(8)
	manager := teamAgent(t, fixture, fixture.manager, managerCalls, nil)
	calls := oneAnswer(4)
	web := teamAgent(t, fixture, fixture.web, calls, nil)
	sendDirective(t, manager, "web")
	waitRequests(t, calls, 1)
	waitQuiet(t, web)
	web.settleTeamEvents()
	one := settlementJob(t, web.jobs)

	web.Interrupt()
	web.jobs.settleExit(one, 7)
	quietFor()
	web.settleTeamEvents()
	if got := calls.requests(); got != 1 {
		t.Fatalf("the stopped member was asked %d times, want only its first turn", got)
	}
	if said := trafficEvents(t, fixture, teams.StoppedByPerson); len(said) != 0 {
		t.Fatalf("finished work was logged as stopped by the person: %+v", said)
	}
	for i := 0; i < managerCalls.requests(); i++ {
		if strings.Contains(userTextIn(managerCalls.request(i)), teamStoppedByPersonLine) {
			t.Fatal("the manager was told finished work was stopped before it finished")
		}
	}
}

// A MANAGER'S STOP PERFORMED BY THE MEMBER'S OWN SESSION HOLDS AS THE PERSON'S
// DOES. A headless member stopped by team_stop was woken by the ending of the
// command its turn had started and took the stopped directive up again. The
// manager is not told of its own stop.
func TestTeamManagerStopByTheMembersSessionHolds(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	managerCalls := oneAnswer(8)
	manager := teamAgent(t, fixture, fixture.manager, managerCalls, nil)
	streaming := make(chan struct{})
	calls := &scriptedCompleter{steps: []step{thinkingStep(streaming), okStep(), okStep()}}
	web := teamAgent(t, fixture, fixture.web, calls, nil)
	sendDirective(t, manager, "web")
	select {
	case <-streaming:
	case <-time.After(5 * time.Second):
		t.Fatal("the member did not start on the directive")
	}
	one := settlementJob(t, web.jobs)
	if said, failed := callTool(t, manager.teamStopTool, `{"handle":"web"}`); failed {
		t.Fatalf("team_stop refused: %s", said)
	}
	waitQuiet(t, web)
	web.settleTeamEvents()

	web.jobs.settleExit(one, 7)
	quietFor()
	if got := calls.requests(); got != 1 {
		t.Fatalf("the member the manager stopped was asked %d times, want only the stopped turn", got)
	}
	journal, err := os.ReadFile(fixture.web)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(journal), "job 1 exited 7") || !strings.Contains(string(journal), teamStopHeldRule) {
		t.Fatalf("the command's ending was not kept as a record:\n%s", journal)
	}
	if said := trafficEvents(t, fixture, teams.StoppedByPerson); len(said) != 0 {
		t.Fatalf("the manager's own stop was logged as the person's: %+v", said)
	}
	if got := managerCalls.requests(); got != 0 {
		t.Fatalf("the manager was woken %d times by its own stop", got)
	}

	sendDirective(t, manager, "web")
	waitRequests(t, calls, 2)
}

// A STOP WITH NOTHING RUNNING AND NOTHING PENDING HOLDS NOTHING: a command the
// member starts in a later turn still wakes it.
func TestTeamPersonStopOfAnIdleMemberHoldsNothing(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	manager := teamAgent(t, fixture, fixture.manager, oneAnswer(8), nil)
	calls := oneAnswer(4)
	web := teamAgent(t, fixture, fixture.web, calls, nil)
	sendDirective(t, manager, "web")
	waitRequests(t, calls, 1)
	waitQuiet(t, web)
	web.Interrupt()
	web.enqueueJobNote("job 8 exited 0")
	waitRequests(t, calls, 2)
	if said := trafficEvents(t, fixture, teams.StoppedByPerson); len(said) != 0 {
		t.Fatalf("a stop of a member with nothing running was logged as work stopped: %+v", said)
	}
}

// A MANAGER'S STOP THAT FOUND THE MEMBER IDLE DOES NOT PASS FOR A LATER ONE.
// The member waits on a command for the first directive; the manager's stop
// finds no turn and stops nothing, but its line stays on the log. A second
// directive starts the member and the person stops that turn: the stop is the
// person's, and the manager is told.
func TestTeamAManagersEarlierStopDoesNotHideThePersonsStop(t *testing.T) {
	streaming := make(chan struct{})
	fixture, managerCalls, web, calls, _, _ := waitingMember(t, thinkingStep(streaming), okStep())
	said := func(entry teams.Entry) {
		t.Helper()
		if err := teams.AppendTraffic(fixture.profile, fixture.teamID, entry); err != nil {
			t.Fatal(err)
		}
	}
	said(teams.Entry{Kind: teams.KindStop, From: teams.FromManager, To: "web", Member: convKeyOf(t, fixture.web), Text: "hold on"})
	quietFor()
	if got := calls.requests(); got != 1 {
		t.Fatalf("a manager's stop of an idle member started %d more turns", got-1)
	}
	said(teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Fix the footer."})
	select {
	case <-streaming:
	case <-time.After(5 * time.Second):
		t.Fatal("the member did not start on the second directive")
	}
	web.Interrupt()
	waitQuiet(t, web)
	waitTeamEvents(t, fixture, teams.StoppedByPerson, 1)
	waitManagerToldOfStop(t, managerCalls)
}

// THE HOLD IS READ UNDER THE LOCK THE NOTE IS QUEUED UNDER. It was read first
// and the lock taken afterwards, so a command ending in the instant of a stop
// found no hold, waited for the stop to let go of the lock, and then queued
// its ending as news that started the stopped conversation again. The test
// holds the lock every stop takes, lets a command's ending arrive at it, and
// only then lets the stop's hold land: the ending must find it.
func TestTeamStopHoldIsReadWhereTheEndingIsQueued(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	calls := oneAnswer(1)
	web := teamAgent(t, fixture, fixture.web, calls, func(config *Config) { config.teamWatchManual = true })
	if roles := web.teamRoles(); len(roles) == 0 {
		t.Fatal("the member has no seat in its team")
	}
	web.mu.Lock()
	arrived := make(chan struct{})
	queued := make(chan struct{})
	go func() {
		close(arrived)
		web.enqueueJobNote("job 8 exited 0")
		close(queued)
	}()
	<-arrived
	// Long enough for the ending to reach the lock it has to wait at.
	time.Sleep(100 * time.Millisecond)
	web.team.stopHold.held.Store(true)
	web.mu.Unlock()
	<-queued
	quietFor()
	if got := calls.requests(); got != 0 {
		t.Fatalf("a command ending at the instant of a stop started %d turns", got)
	}
	journal, err := os.ReadFile(fixture.web)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(journal), "job 8 exited 0") || strings.Count(string(journal), teamStopHeldRule) != 1 {
		t.Fatalf("the ending was not kept as one record that says why it started no turn:\n%s", journal)
	}
}
