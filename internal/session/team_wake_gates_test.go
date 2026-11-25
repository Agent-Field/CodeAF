package session

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	procexec "github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// refusingSeat is a seat that may not work now ([Agent.CheckWork]).
type refusingSeat struct{ procexec.Seat }

func (refusingSeat) CheckWork() error { return errors.New("this seat is someone else's now") }

// LINES THE BOUNDARY HANDED OVER ARE NOT DROPPED BY A GATE THAT CLOSED SINCE.
// The watch decided a wake could start, the boundary moved and saved the
// delivery cursor past the directive, and by the time the turn was to start
// the seat could no longer work: the text was thrown away and no turn ever read
// the directive. It is kept as an ordinary line for the next turn, and starts
// nothing now.
func TestTeamWakeWithKeepsTheLinesWhenTheSeatClosedSinceTheDecision(t *testing.T) {
	calls := &scriptedCompleter{}
	agent, _ := newTestAgent(t, calls, func(config *Config) { config.Seat = refusingSeat{Seat: procexec.Host} })
	d := agent.wakeWith("Your manager started this turn.\n\nFix the header.")
	if d.gate != wakeGateSeat {
		t.Fatalf("the wake was decided %+v, want the seat's gate", d)
	}
	agent.mu.Lock()
	running, steering := agent.running, append([]userMessage(nil), agent.steering...)
	agent.mu.Unlock()
	if running || calls.requests() != 0 {
		t.Fatal("a seat that may not work started a turn")
	}
	if len(steering) != 1 || steering[0].wake || !strings.Contains(steering[0].text(), "Fix the header.") {
		t.Fatalf("the handed-over lines were not kept for the next turn: %+v", steering)
	}
}

// A MANAGER'S STOP READ IN THE SAME LOOK AS THE DIRECTIVE BEFORE IT STOPS THAT
// DIRECTIVE. The member waited on a command; the manager sent a directive and
// stopped it at once, and one idle look read both: the stop held what the
// member waits on, and then a turn was started on the stopped directive. No
// turn starts now, and a directive after the stop still wakes the member.
func TestTeamManagerStopAfterADirectiveInOneLookStartsNoTurn(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	var web *Agent
	started := make(chan *job, 1)
	calls := &scriptedCompleter{steps: []step{waitedStep(func() *Agent { return web }, started), okStep(), okStep()}}
	web = teamAgent(t, fixture, fixture.web, calls, func(config *Config) { config.teamWatchManual = true })
	collect(t, mustSubmit(t, web, "start the build and wait for it"))
	waitQuiet(t, web)
	<-started

	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Fix the header."})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindStop, From: teams.FromManager, To: "web", Text: "stop"})
	web.teamWatchTick(fixture.profile, time.Now())
	if !web.team.stopHold.held.Load() {
		t.Fatal("the member did not hold on its manager's stop")
	}
	quietFor()
	if got := calls.requests(); got != 1 {
		t.Fatalf("the member was asked %d times, want only the person's turn: the directive was stopped", got)
	}

	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Now the footer."})
	web.teamWatchTick(fixture.profile, time.Now())
	waitRequests(t, calls, 2)
	waitQuiet(t, web)
	web.settleTeamEvents()
}

// A CONVERSATION IN NO TEAM PAYS NOTHING FOR THE PAUSE. The pause a command's
// ending might be held by was read afresh for every conversation with a
// profile, a stat of the teams file and a read of it whenever it had moved.
// Only a conversation with a role in a team can be paused.
func TestNoTeamNoteDoesNotReadTheTeamsFile(t *testing.T) {
	fixture := newWakingTeamFixture(t)
	solo := filepath.Join(t.TempDir(), "solo", "transcript.jsonl")
	agent := teamAgent(t, fixture, solo, &scriptedCompleter{}, func(config *Config) { config.teamWatchManual = true })
	if len(agent.teamRoles()) != 0 || agent.team.stopHold.seated.Load() {
		t.Fatal("the fixture's conversation has a seat in a team")
	}
	agent.team.mu.Lock()
	before := agent.team.teamsAt
	agent.team.mu.Unlock()
	if err := teams.Update(fixture.profile, func(file *teams.File) error {
		file.Teams = append(file.Teams, teams.Team{ID: teams.NewID(), Name: "elsewhere"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// No turn may start and read the file at its own boundary.
	agent.mu.Lock()
	agent.workStopped = true
	agent.mu.Unlock()
	agent.enqueueJobNote("job 1 exited 0")
	agent.team.mu.Lock()
	after := agent.team.teamsAt
	agent.team.mu.Unlock()
	if after != before {
		t.Fatal("a command's ending in a conversation in no team read the teams file")
	}
}

// A PAUSE BETWEEN A FAILED COMMAND'S ENDING AND ITS WAKE'S CLOCK HOLDS THE
// WAKE. The ending was queued before the person paused the member, and the
// clock that would start its turn fired after: the turn is the one the pause is
// there to keep from starting. Resume starts it, and it reads the ending.
func TestTeamPauseBeforeAFailedCommandsWakeFiresHoldsIt(t *testing.T) {
	fixture, _, web, calls, clock, started := waitingMember(t, okStep(), okStep())
	web.jobs.settleExit(<-started, 7)
	setPaused(t, fixture, fixture.web, true)
	clock.advance(jobWakeDelay)
	quietFor()
	if got := calls.requests(); got != 1 {
		t.Fatalf("the paused member was asked %d times, want only its first turn", got)
	}

	setPaused(t, fixture, fixture.web, false)
	waitRequests(t, calls, 2)
	if asked := userTextIn(calls.request(1)); !strings.Contains(asked, "job 1 exited 7") {
		t.Fatalf("the turn Resume started did not read the failed command's ending:\n%s", asked)
	}
	waitQuiet(t, web)
}
