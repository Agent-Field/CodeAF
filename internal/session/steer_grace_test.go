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
	"sync"
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

// AN EXPLICIT STOP DOES NOT WAIT OUT THE GRACE. The command is half a second
// old — far too young to adopt for an ordinary correction — and `stop` reaches
// it at once, because the grace exists to let a short command FINISH and that
// is the one thing the person has said they do not want.
func TestAnExplicitStopReachesAYoungCommandAtOnce(t *testing.T) {
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
	calls := agent.inFlightBash.snapshot()
	if age := calls[0].RunningFor(); age >= steerBashAge {
		t.Fatalf("the bash was already %s old, so this is not the young case", age)
	}
	sent := time.Now()
	steered := mustSteer(t, agent, "stop")
	// THE ACT IS DONE BY THE TIME Steer RETURNS, under the same lock — so this
	// is a bound on the whole stop and not on a poll of its effects.
	if waited := time.Since(sent); waited >= steerBashAge {
		t.Fatalf("the stop took %s, want it not to wait out the %s grace", waited, steerBashAge)
	}
	// AND NO WATCH IS LEFT BEHIND to fire into commands this stop already ended.
	agent.mu.Lock()
	armed := agent.steerGrace
	agent.mu.Unlock()
	if armed != nil {
		t.Fatal("an explicit stop left a second look armed")
	}
	collect(t, turn)
	if got := steerLanding(collect(t, steered)); got != "stopped the running command" {
		t.Fatalf("steer landing = %q", got)
	}
	if got := roleText(completer.request(1), "tool"); !strings.Contains(got, "stopped by the person: stop") {
		t.Fatalf("stopped tool result = %q", got)
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
}

// THE DELAYED LOOK CARRIES THE NEWEST DIRECTION AND NOTHING OLDER. A `stop`
// that is still sitting on the queue behind a later instruction is a sentence
// the person has moved on from, and it must not hold authority over a batch it
// never acted on — the model reads both, in order, and decides.
func TestTheDelayedSteerLookTakesTheNewestDirection(t *testing.T) {
	t.Parallel()
	waiting := func(words ...string) *Agent {
		agent := &Agent{}
		agent.steering = append(agent.steering, userMessage{message: textMessage("user", "job 1 exited 0")})
		for _, one := range words {
			agent.steering = append(agent.steering, steerMessage(&turnSteer{note: SteerNote{Words: one}}))
		}
		return agent
	}
	cases := []struct {
		name  string
		queue []string
		want  string
	}{
		{"a superseded stop is not standing authority", []string{"stop", "keep it running, just tell me the date"}, "keep it running, just tell me the date"},
		{"the newest is a stop", []string{"check the parser", "stop"}, "stop"},
		{"one correction is its own newest", []string{"check the parser"}, "check the parser"},
	}
	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			steer := waiting(one.queue...).pendingSteerLocked()
			if steer == nil {
				t.Fatalf("no pending steer found in %v", one.queue)
			}
			if steer.note.Words != one.want {
				t.Fatalf("the delayed look acts for %q, want %q", steer.note.Words, one.want)
			}
		})
	}
	if steer := waiting().pendingSteerLocked(); steer != nil {
		t.Fatalf("a queue with no correction on it answered %q", steer.note.Words)
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
	agent.mu.Unlock()
	if watch == nil {
		t.Fatal("no second look was armed for a young bash")
	}
	// The timer is taken off the clock so that every firing below is one this
	// test made, in the order it wrote them.
	watch.timer.Stop()
	// It fires late enough that the age itself is no longer what refuses it.
	time.Sleep(steerBashAge + 2*steerGraceMargin)

	// Each is INSTALLED before it is fired, so what refuses it is the identity
	// under test and not the guard that answers a watch the agent let go of
	// (which is TestAReplacedSteerGraceIsInertInItsOwnTurn's subject).
	fire := func(one *steerWatch) {
		agent.mu.Lock()
		agent.steerGrace = one
		agent.mu.Unlock()
		agent.steerGraceFired(one)
	}
	fire(&steerWatch{timer: watch.timer, turn: watch.turn + 1, calls: watch.calls})
	if list := agent.jobs.list(); list != "No background jobs." {
		t.Fatalf("a timer from another turn adopted a command: %q", list)
	}
	fire(&steerWatch{timer: watch.timer, turn: watch.turn, calls: nil})
	if list := agent.jobs.list(); list != "No background jobs." {
		t.Fatalf("a timer adopted a command it was never armed for: %q", list)
	}

	fire(watch)
	waitFor(t, "the armed second look to adopt its own command", func() bool { return agent.jobs.find(1) != nil })
	// AND FIRING IT AGAIN CHANGES NOTHING: the agent let go of it as it fired,
	// the correction has landed, and the call is no longer in flight.
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

// A WATCH THAT WAS REPLACED IS INERT INSIDE ITS OWN TURN. The turn number and
// the calls are identical between the two — the only difference is which one
// the agent is holding — so nothing but that check can refuse the first.
func TestAReplacedSteerGraceIsInertInItsOwnTurn(t *testing.T) {
	t.Parallel()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("replaced-bash", "bash", `{"command":"sleep 6; echo replaced-finished"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("changed course"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("and it finished"), nil },
	}}
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, nil)
	turn := mustSubmit(t, agent, "run the suite")
	waitFor(t, "the foreground bash to start", func() bool { return len(agent.inFlightBash.snapshot()) == 1 })
	first := mustSteer(t, agent, "check the parser while that runs")
	agent.mu.Lock()
	replaced := agent.steerGrace
	agent.mu.Unlock()
	second := mustSteer(t, agent, "and the lexer too")
	agent.mu.Lock()
	current := agent.steerGrace
	agent.mu.Unlock()
	if replaced == nil || current == nil || replaced == current {
		t.Fatalf("two steers did not replace one watch: %p then %p", replaced, current)
	}
	if replaced.turn != current.turn {
		t.Fatal("the second steer arrived in a different turn, so this is not the replacement case")
	}
	// Both timers come off the clock so that every firing below is one this test
	// made, and it is made late enough that the age is not what refuses it.
	replaced.timer.Stop()
	current.timer.Stop()
	time.Sleep(steerBashAge + 2*steerGraceMargin)

	agent.steerGraceFired(replaced)
	if list := agent.jobs.list(); list != "No background jobs." {
		t.Fatalf("a watch the agent had already let go of adopted a command: %q", list)
	}
	agent.steerGraceFired(current)
	waitFor(t, "the watch the agent is holding to adopt its command", func() bool { return agent.jobs.find(1) != nil })
	if agent.jobs.find(2) != nil {
		t.Fatal("one command was adopted twice")
	}
	collect(t, first)
	collect(t, second)
	collect(t, turn)
}

// THE WINDOW INSIDE Interrupt. It takes a.mu, drops the follow-ups, LETS GO OF
// THE LOCK, and only then cancels the turn — so between those two moments the
// turn is still running, the watch is still armed, and the command's context is
// still alive. A firing there would promote the foreground command into a job,
// and a background job deliberately survives an interrupt: the person who
// pressed stop would be left with the command detached and still running.
//
// The gate is the whole test. a.cancel is wrapped so that Interrupt blocks
// exactly where the window is, which is the only way to fire into an interval
// the scheduler otherwise closes in microseconds.
func TestASteerGraceCannotDetachWorkInsideAnInterrupt(t *testing.T) {
	t.Parallel()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("detach-bash", "bash", `{"command":"sleep 30"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("unreachable"), nil },
	}}
	agent, _ := newTestAgent(t, completer, nil)
	turn := mustSubmit(t, agent, "start the long one")
	waitFor(t, "the foreground bash to start", func() bool { return len(agent.inFlightBash.snapshot()) == 1 })
	steered := mustSteer(t, agent, "actually look at the log")

	entered := make(chan struct{})
	release := make(chan struct{})
	var arrived, opened sync.Once
	letGo := func() { opened.Do(func() { close(release) }) }
	// THE GATE IS OPENED WHATEVER HAPPENS BELOW, including a t.Fatal, or the
	// interrupting goroutine would hold this turn open until the test binary's
	// own timeout.
	t.Cleanup(letGo)

	agent.mu.Lock()
	watch := agent.steerGrace
	real := agent.cancel
	// Close cancels too, so both sides are once-only: the gate is about the ONE
	// pass Interrupt makes through here.
	agent.cancel = func() {
		arrived.Do(func() { close(entered) })
		<-release
		real()
	}
	agent.mu.Unlock()
	if watch == nil {
		t.Fatal("no second look was armed for a young bash")
	}
	// Off the clock, so the only firing is the one this test makes.
	watch.timer.Stop()

	go agent.Interrupt()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("Interrupt never reached the cancel")
	}
	// Interrupt is past its lock boundary and the turn it is stopping is still
	// alive: this is the interval, and the grace is now behind us.
	time.Sleep(steerBashAge + 2*steerGraceMargin)
	if len(agent.inFlightBash.snapshot()) != 1 {
		t.Fatal("the command was already gone, so this is not the window under test")
	}

	agent.steerGraceFired(watch)
	if list := agent.jobs.list(); list != "No background jobs." {
		t.Fatalf("a stop the person asked for detached the command instead: %q", list)
	}
	if queued := steeringQueue(agent); !containsString(queued, "actually look at the log") {
		t.Fatalf("the correction left the queue inside an interrupt: %v", queued)
	}

	letGo()
	collect(t, turn)
	events := collect(t, steered)
	for _, event := range events {
		if event.Kind == EventSteerConsumed {
			t.Fatal("a steer landed inside a turn the person stopped")
		}
	}
	if list := agent.jobs.list(); list != "No background jobs." {
		t.Fatalf("an interrupted turn left a job behind: %q", list)
	}
}
