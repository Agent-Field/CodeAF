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
	deadline := time.Now().Add(5 * time.Second)
	for len(trafficEvents(t, fixture, teams.StoppedByPerson)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the manager's log was not told the person stopped the member's waiting work")
		}
		time.Sleep(10 * time.Millisecond)
	}
	web.jobs.settleExit(one, 0)
	web.enqueueJobNote("job 8 exited 0")
	quietFor()
	if got := calls.requests(); got != 1 {
		t.Fatalf("the stopped member was asked %d times, want only its first turn", got)
	}
	if said := trafficEvents(t, fixture, teams.StoppedByPerson); len(said) != 1 {
		t.Fatalf("the stop was logged %d times, want once", len(said))
	}
	told := false
	for i := 0; i < managerCalls.requests(); i++ {
		told = told || strings.Contains(userTextIn(managerCalls.request(i)), "@web"+teamStoppedByPersonLine)
	}
	if !told {
		t.Fatal("the manager was not told the work it handed out was stopped")
	}
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
