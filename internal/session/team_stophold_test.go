package session

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
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
