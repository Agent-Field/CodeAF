package session

// THE GRACE: a steer that arrived one second too early must not wait a minute.
//
// [steerBashAge] is a bargain about the COMMAND — a short one may finish its
// batch — and it was being read as a bargain about the PERSON: a correction
// typed half a second into a sixty-second build was skipped once and never
// looked at again, so it waited for the command to end or for the session's
// background clock (thirty seconds in a live chat) to end it. These tests are
// about the second look.

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A YOUNG BASH THAT TURNS OUT TO BE A LONG ONE. The steer is sent while the
// command is far too young to adopt, the command then runs well past
// [steerBashAge], and the person's words must reach the model at that bound —
// not at the command's own ending, and not at the background clock.
func TestASteerLandsWhenAYoungBashCrossesTheGrace(t *testing.T) {
	t.Parallel()
	var reached atomic.Int64
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("grace-bash", "bash", `{"command":"sleep 7; echo grace-bash-finished"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			reached.Store(time.Now().UnixNano())
			return textResponse("2026-09-05, and the suite is still running"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("and the suite has finished"), nil
		},
	}}
	// The adopted bash becomes a job, and a job is named on its own goroutine;
	// this test indexes into the requests it scripted (steer_test.go).
	answerTheNamerOffTheQueue(completer)
	// A live conversation arms the background clock, and the defect was the
	// steer waiting for it. Twenty seconds stands in for livechat's thirty and
	// is longer than anything this test does.
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.BashBackgroundAfterSeconds = 20 })
	turn := mustSubmit(t, agent, "run the suite")
	waitFor(t, "the foreground bash to start", func() bool { return len(agent.inFlightBash.snapshot()) == 1 })
	calls := agent.inFlightBash.snapshot()
	if age := calls[0].RunningFor(); age >= steerBashAge {
		t.Fatalf("the bash was already %s old, so this is not the young case", age)
	}
	sent := time.Now()
	steered := mustSteer(t, agent, "while that runs, what is today's date")

	waitFor(t, "the steer to reach the model", func() bool { return reached.Load() != 0 })
	waited := time.Unix(0, reached.Load()).Sub(sent)
	t.Logf("the steer reached the model %s after it was sent", waited)
	if bound := steerBashAge + 2*time.Second; waited > bound {
		t.Fatalf("the steer reached the model %s after it was sent, want it inside %s", waited, bound)
	}

	second := completer.request(1)
	if got, want := userLines(second), []string{"run the suite", "while that runs, what is today's date"}; !equalStrings(got, want) {
		t.Fatalf("second request users = %v, want %v", got, want)
	}
	if got := roleText(second, "tool"); !strings.Contains(got, "still running as job 1") ||
		!strings.Contains(got, "output via jobs output 1") {
		t.Fatalf("adopted tool result = %q", got)
	}
	// THE ACCEPTANCE STAYS TRUTHFUL. It was sent when the only true sentence was
	// that the step was still running, and no second acceptance rewrites it.
	events := collect(t, steered)
	if got := steerLanding(events); got != "waiting for the running step" {
		t.Fatalf("steer landing = %q", got)
	}

	// ONE PROCESS, ADOPTED ONCE, AND ITS ENDING STILL ARRIVES.
	waitFor(t, "the adopted job's exit note", func() bool { return notesContain(agent, "grace-bash-finished") })
	waitFor(t, "the owed exit request", func() bool { return completer.requests() >= 3 })
	if got := strings.Join(userLines(completer.request(2)), "\n"); !strings.Contains(got, "job 1 exited 0") {
		t.Fatalf("later request has no owed exit note: %q", got)
	}
	if agent.jobs.find(2) != nil {
		t.Fatal("the command was adopted twice")
	}
	collect(t, turn)
}

// A COMMAND THAT FINISHES ON ITS OWN IS NEVER TOUCHED, and the timer armed
// behind it is inert when it fires into a turn that has already ended.
func TestASteerGraceLeavesAQuickBashAlone(t *testing.T) {
	t.Parallel()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("quick-bash", "bash", `{"command":"sleep 0.5; echo quick-finished"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, _ := newTestAgent(t, completer, nil)
	turn := mustSubmit(t, agent, "run the quick check")
	waitFor(t, "the young foreground bash to start", func() bool { return len(agent.inFlightBash.snapshot()) == 1 })
	steered := mustSteer(t, agent, "then read the result")
	collect(t, turn)
	collect(t, steered)
	// Past the moment the watch was armed for, with the turn long over.
	time.Sleep(steerBashAge + 2*steerGraceMargin)
	if list := agent.jobs.list(); list != "No background jobs." {
		t.Fatalf("a command that finished on its own became a job: %q", list)
	}
	agent.mu.Lock()
	armed := agent.steerGrace
	agent.mu.Unlock()
	if armed != nil {
		t.Fatal("the watch outlived the turn that armed it")
	}
	if got := roleText(completer.request(1), "tool"); !strings.Contains(got, "quick-finished") {
		t.Fatalf("the quick command did not answer for itself: %q", got)
	}
}

// TWO CORRECTIONS, ONE WATCH, AND THE STOP WINS. The person said `stop` and
// then said something else; the command is killed rather than promoted into a
// healthy background job, and only one adoption happens.
func TestASteerGraceKeepsAStopAStop(t *testing.T) {
	t.Parallel()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("stop-me", "bash", `{"command":"sleep 30"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("stopped"), nil },
	}}
	agent, _ := newTestAgent(t, completer, nil)
	turn := mustSubmit(t, agent, "start the server")
	waitFor(t, "the foreground bash to start", func() bool { return len(agent.inFlightBash.snapshot()) == 1 })
	first := mustSteer(t, agent, "stop")
	second := mustSteer(t, agent, "look at the config instead")
	agent.mu.Lock()
	armed := agent.steerGrace
	agent.mu.Unlock()
	if armed == nil {
		t.Fatal("no second look was armed for a young bash")
	}

	turnEvents := collect(t, turn)
	collect(t, first)
	collect(t, second)
	if got := roleText(completer.request(1), "tool"); !strings.Contains(got, "stopped by the person: stop") {
		t.Fatalf("stopped tool result = %q", got)
	}
	if got, want := userLines(completer.request(1)), []string{"start the server", "stop", "look at the config instead"}; !equalStrings(got, want) {
		t.Fatalf("second request users = %v, want %v", got, want)
	}
	waitFor(t, "the adopted job to settle killed", func() bool {
		job := agent.jobs.find(1)
		return job != nil && !job.running()
	})
	if agent.jobs.find(2) != nil {
		t.Fatal("one command was adopted twice")
	}
	if notesContain(agent, "job 1 exited") {
		t.Fatal("a person-requested stop produced an owed exit note")
	}
	if kinds := steerEvents(turnEvents); len(kinds) == 0 {
		t.Fatal("the turn carried no steer events at all")
	}
}

// A WATCH IS ONLY EVER RIGHT ABOUT THE TURN AND THE COMMANDS IT WAS ARMED FOR.
// The two ways a late timer can be wrong are fired by hand, and then the real
// one is fired to prove the road they took was the live one.
func TestASteerGraceOnlyActsForWhatItWasArmedFor(t *testing.T) {
	t.Parallel()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("identity-bash", "bash", `{"command":"sleep 6; echo identity-finished"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("changed course"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("and it finished"), nil },
	}}
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, nil)
	turn := mustSubmit(t, agent, "run the suite")
	waitFor(t, "the foreground bash to start", func() bool { return len(agent.inFlightBash.snapshot()) == 1 })
	steered := mustSteer(t, agent, "check the parser while that runs")

	agent.mu.Lock()
	watch := agent.steerGrace
	agent.steerGrace = nil
	agent.mu.Unlock()
	if watch == nil {
		t.Fatal("no second look was armed for a young bash")
	}
	// The timer is taken off the clock so that every firing below is one this
	// test made, in the order it wrote them.
	watch.timer.Stop()
	// It fires late enough that the age itself is no longer what refuses it.
	time.Sleep(steerBashAge + 2*steerGraceMargin)

	agent.steerGraceFired(&steerWatch{timer: watch.timer, turn: watch.turn + 1, calls: watch.calls})
	if list := agent.jobs.list(); list != "No background jobs." {
		t.Fatalf("a timer from another turn adopted a command: %q", list)
	}
	agent.steerGraceFired(&steerWatch{timer: watch.timer, turn: watch.turn, calls: nil})
	if list := agent.jobs.list(); list != "No background jobs." {
		t.Fatalf("a timer adopted a command it was never armed for: %q", list)
	}

	agent.steerGraceFired(watch)
	waitFor(t, "the armed second look to adopt its own command", func() bool { return agent.jobs.find(1) != nil })
	// AND FIRING IT AGAIN CHANGES NOTHING: the correction has landed and the
	// call is no longer in flight.
	agent.steerGraceFired(watch)
	if agent.jobs.find(2) != nil {
		t.Fatal("a second firing adopted the command twice")
	}
	events := collect(t, steered)
	if got := steerEvents(events); len(got) == 0 || !strings.HasPrefix(got[len(got)-1], "consumed:") {
		t.Fatalf("steer events = %v, want the correction consumed", got)
	}
	collect(t, turn)
}

// A TURN SOMEBODY STOPPED IS STOPPED. The correction was typed while the
// command was young, esc came before the grace, and nothing the watch does may
// turn that into work carrying on.
func TestASteerGraceDoesNotOutliveAnInterruptedTurn(t *testing.T) {
	t.Parallel()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("interrupted-bash", "bash", `{"command":"sleep 8"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("unreachable"), nil },
	}}
	agent, _ := newTestAgent(t, completer, nil)
	turn := mustSubmit(t, agent, "start the long one")
	waitFor(t, "the foreground bash to start", func() bool { return len(agent.inFlightBash.snapshot()) == 1 })
	steered := mustSteer(t, agent, "actually look at the log")
	agent.Interrupt()

	collect(t, turn)
	events := collect(t, steered)
	for _, event := range events {
		if event.Kind == EventSteerConsumed {
			t.Fatal("a steer landed inside a turn the person stopped")
		}
	}
	// Past the moment the watch was armed for, with the turn already gone.
	time.Sleep(steerBashAge + 2*steerGraceMargin)
	agent.mu.Lock()
	armed, running := agent.steerGrace, agent.running
	agent.mu.Unlock()
	if armed != nil {
		t.Fatal("the watch outlived the turn that armed it")
	}
	if running {
		t.Fatal("the session is still running a turn the person stopped")
	}
	if list := agent.jobs.list(); list != "No background jobs." {
		t.Fatalf("an interrupted turn left a job behind: %q", list)
	}
}
