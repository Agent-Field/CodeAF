package session

// THE WORKER THAT WAS ASKED WHAT TO DO NEXT WHILE ITS COMMAND WAS STILL RUNNING.
//
// Every test here is written from ONE REAL RUN. A task child ran its suite as an
// ordinary foreground `bash` call. The call outlived the session's
// background-after clock, so the promotion road adopted the process and the tool
// answered `still running as job 1; log at …` (promote.go) — which is the right
// answer to the question "is the work lost", and no answer at all to the
// question the turn loop asked one instant later, which was "what next". The
// worker had a running suite, no result from it, and a model waiting for a move.
// So it manufactured one: `sleep 28; tail <log>`, again, and again. The loop
// detector read that for what it looked like — the same call producing nothing —
// and the node was stopped for going in circles while the suite it was waiting
// for was healthy and eight seconds from green.
//
// So the law these pin is one sentence, and it is task_park_test.go's own law
// pointed at the other thing a worker can be waiting for: WAITING IS NOT WORKING,
// AND IT IS NOT SPINNING EITHER. A foreground call taken over by the promotion
// road is still THE CALL THE WORKER MADE — it is owed its ending — so the worker
// is parked on it, not asked, not counted, not clocked, and THE FIRST TURN AFTER
// THE PROMOTION IS THE TURN THAT READS THE ENDING.
//
// AND ONLY THAT ONE. A call that asked for `background: true` was never a call
// anybody was waiting for: it went to [jobRegistry.start] and was a job from its
// first instant (promote.go states the branch), and a worker held open by one of
// those would be a worker that can never hand its work back.
//
// ── WHAT THESE MEASURE, AND WHAT THEY DELIBERATELY DO NOT ──
//
// THE LOAD-BEARING CLAIM IS AN ORDER AND NEVER A CLOCK. "The worker was asked
// again before its command ended" reads as a duration, and a duration is what a
// first draft of this file asserted — against a watcher that recorded the ending
// when it NOTICED the state change. Once the wait existed the true gap fell to
// under a millisecond, which is the scale at which a poller is reporting its own
// scheduling and nothing else. The order does not have that problem: the request
// after the promotion either carries the command's ending or it does not, and on
// a tree without the wait it carries nothing at all. The exact instant is still
// read where a duration is worth printing, and it is read from [job.settle]'s own
// stamp ([waitSettled]) rather than from anybody watching.
//
// AND THE ORDER IS NOT LEFT TO A CLOCK EITHER — that is what [heldCommand] is
// for, and its comment is the second law of this file: A TEST MUST NOT RACE A
// CLOCK IT CAN OWN.
//
// AND THE LOOP DETECTOR IS LEFT ALONE. A worker that polls a log after its
// ending has arrived is repeating itself, and being stopped for it is the reader
// working correctly — #568 says so in as many words. So the nudge is asserted
// about only over the stretch the worker was PARKED, and everything the model
// chooses afterwards is its own.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── harness ─────────────────────────────────────────────────────────────────

// jobNest is [newNest] with the session's background-after clock wound down to
// one second. That figure is the whole premise of this file: the promotion has
// to fire while the command is still running — otherwise there is no wait to be
// wrong about — and one second is short enough that a test does not sit through
// it. HOW LONG THE COMMAND THEN RUNS IS NOT THIS NUMBER'S BUSINESS: it is the
// test's, and [heldCommand] is how the test holds it.
//
// The namer is answered off the queue because a promoted command becomes a job,
// and a job is named by a small call on a goroutine of its own (jobname.go). Left
// alone it takes whichever scripted step it lands on, which is a red that has
// nothing to do with the wait ([answerTheNamerOffTheQueue]).
func jobNest(t *testing.T, completer *scriptedCompleter) *nest {
	t.Helper()
	answerTheNamerOffTheQueue(completer)
	here := newNest(t, completer, nil)
	// Written before the run's goroutine exists, which is the only instant at
	// which this field has a single reader.
	here.node.config.BashBackgroundAfterSeconds = 1
	return here
}

// heldCommand is a command WHOSE ENDING BELONGS TO THE TEST. It prints its
// opening line, spins on a path that does not exist until the test creates it,
// and then prints its last word and exits.
//
// A TEST MUST NOT RACE A CLOCK IT CAN OWN. The first draft of this file wrote
// `sleep 3` and asked it to outlast a one-second handoff clock, which is a
// margin — and a margin is a bug waiting for a busy box: the handoff timer fires
// late, or the sleep finishes before the promotion lands, and then there is no
// promotion, no wait, and nothing for anything below to be right or wrong about.
// The failure that produces is a lie about the mechanism, because the mechanism
// was never reached.
//
// A command that ends WHEN THE TEST SAYS SO has no margin to lose. It runs for
// exactly as long as the promotion takes, however long the machine makes that;
// the test releases it once it has SEEN the promotion, and the release is the
// ending. Nothing here is timed and nothing here is hoped.
//
// The spin is `until [ -f … ]` and not `wait` or a fifo because it is the
// portable shape: it needs one test of one path and a short sleep, and it holds
// under any /bin/sh this suite could be handed.
type heldCommand struct {
	// gate is the path whose appearance ends the command. It lives under the
	// test's own temporary directory, so it cannot exist before the test writes
	// it and it is gone when the test is.
	gate string
	// text is the command as the model would have asked for it.
	text string
}

// holdACommand builds one of those, printing `opening` at once and `ending` on
// its way out — the two words the promotion's own tail and the ending note are
// then read for.
func holdACommand(t *testing.T, opening, ending string) *heldCommand {
	t.Helper()
	gate := filepath.Join(t.TempDir(), "release")
	return &heldCommand{
		gate: gate,
		text: fmt.Sprintf("echo %s; until [ -f '%s' ]; do sleep 0.05; done; echo %s",
			opening, gate, ending),
	}
}

// release ends the command. A command that is never released runs until the
// registry kills it, which is what a test wants from something that has to be
// STILL RUNNING at the moment it looks.
func (h *heldCommand) release(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(h.gate, nil, 0o600); err != nil {
		t.Fatalf("the command could not be released: %v", err)
	}
}

// bashStep is one scripted turn that makes a foreground `bash` call and records
// the instant it was asked for.
func bashStep(record *askLog, id, command string) step {
	arguments := fmt.Sprintf(`{"command":%q}`, command)
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		record.ask(command)
		return toolResponse(id, "bash", arguments), nil
	}
}

// sayStep is one scripted turn that says its piece and ends the run. It records
// no command, because a turn that only spoke ran nothing.
func sayStep(record *askLog, said string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		record.ask("")
		return textResponse(said), nil
	}
}

// askLog is when the worker was asked for each of its steps, and what each of
// those steps asked to run.
//
// UNDER ONE MUTEX, because it is written from the worker's turn and read from
// the test, and it is read against an instant a third goroutine stamped. A clock
// taken on one side of a race and a counter on the other is not an ordering; it
// is two numbers.
type askLog struct {
	mu       sync.Mutex
	asked    []time.Time
	commands []string
}

func (l *askLog) ask(command string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.asked = append(l.asked, time.Now())
	l.commands = append(l.commands, command)
}

func (l *askLog) askedAt(index int) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if index < 0 || index >= len(l.asked) {
		return time.Time{}, false
	}
	return l.asked[index], true
}

func (l *askLog) asks() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.asked)
}

// ranBefore is every command the worker asked to run before `ending`, PAST THE
// FIRST — the first being the command `ending` belongs to, which is the one call
// this worker was legitimately in the middle of.
//
// It counts the ask and not the tool row, because the ask is the instant the
// step was spent and it is stamped by the same clock the ending is: a row
// counted off the transcript would have to be sampled by somebody watching, and
// what that measures is the watcher.
func (l *askLog) ranBefore(ending time.Time) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var ran []string
	for index := 1; index < len(l.asked); index++ {
		if l.commands[index] == "" || !l.asked[index].Before(ending) {
			continue
		}
		ran = append(ran, l.commands[index])
	}
	return ran
}

// generously is the instant a wait for something another goroutine will do must
// give up at: the asked-for stretch, cut short of the test's own deadline if
// there is one, so that a wait reports what it was waiting for instead of being
// killed mid-poll and blamed on whichever test was running.
//
// THE NUMBER IS ONLY EVER A CEILING. Every wait below returns the moment the
// thing it names has happened, so a generous bound costs nothing on any machine
// where it happens — and where it does not happen, the bound is the difference
// between a named failure and a hang.
func generously(t *testing.T, want time.Duration) time.Time {
	t.Helper()
	until := time.Now().Add(want)
	if deadline, set := t.Deadline(); set {
		if slack := deadline.Add(-5 * time.Second); slack.Before(until) {
			return slack
		}
	}
	return until
}

// waitPromoted waits for the promotion road to have adopted a running command
// and answers the job it became. It is the signal a test needs before it can say
// anything at all about the wait: "the worker has not been asked again" is not a
// claim anybody can make before the promotion that would ask it.
//
// AND IT IS WAITED FOR RATHER THAN ASSUMED. The handoff clock is one second, and
// a test that took that for the truth would be reading its own optimism on a box
// where the timer fires late. If the promotion never comes at all, THAT is a real
// failure and it is said plainly.
func waitPromoted(t *testing.T, node *Agent) *job {
	t.Helper()
	for until := generously(t, 30*time.Second); time.Now().Before(until); {
		if all := node.jobs.all(); len(all) > 0 && all[0].running() {
			return all[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the foreground command was never taken over by the promotion road, so there was no running job for this worker to be waiting on")
	return nil
}

// parkedOnItsCommand reads whether this worker has TAKEN THE GENERATION it parks
// on ([Agent.taskNewsWait]).
//
// THAT READING IS THE ORDERING AND NOT A GUESS AT IT. The park takes its channel
// BEFORE it asks whether anything is still owed, precisely so that news landing
// from that instant onward cannot be missed (task_job_park.go states the law) —
// so a test that has seen the channel exist knows that an ending it causes from
// here will reach the worker, which is the only fact it needs before causing
// one.
func parkedOnItsCommand(a *Agent) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.taskNews != nil
}

// waitParkedOnItsCommand returns once the worker has either parked on the
// command it started or PROVED IT HAS NOT, by asking for something else. It is
// what a test calls between seeing the promotion and ending the command.
//
// ── WHY THE PROOF OF THE WRONG ANSWER IS PART OF THE WAIT ──
//
// The claim under test is an order: the worker's next request either comes
// before its command's ending or after it. A test that ended the command on a
// timer would be deciding that order with a stopwatch — and on a tree without
// the wait, a slow enough box could let the ending overtake the very request
// that proves the wait is missing, which is a red test going green for the worst
// reason there is.
//
// So the ending is caused by an OBSERVED fact either way. Parked, and the test
// releases knowing the worker is holding; asked again, and the test releases
// knowing the request that should not exist has already been made and snapshot.
// The bound underneath is only a ceiling for the case where neither is ever
// true, and it returns rather than failing, because the assertion the caller is
// about to make is the one entitled to judge that.
func waitParkedOnItsCommand(t *testing.T, node *Agent, completer *scriptedCompleter, asked int) {
	t.Helper()
	for until := generously(t, 30*time.Second); time.Now().Before(until); {
		if parkedOnItsCommand(node) || completer.requests() > asked {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// waitSettled waits for one job to be final and answers THE EXACT INSTANT its
// process ended — [job.settle]'s own stamp, read back off the same copy a row is
// drawn from, where a settled job's elapsed is its ending minus its start
// ([job.info]).
//
// THE POLL IS ONLY THE WAIT AND NEVER THE READING. A watcher that stamped
// time.Now() when it NOTICED the state change would be reporting how often it
// looked, and the gaps this file prints are sub-millisecond once the wait exists.
func waitSettled(t *testing.T, node *Agent, id int) time.Time {
	t.Helper()
	for until := generously(t, 30*time.Second); time.Now().Before(until); {
		if one := node.jobs.find(id); one != nil && !one.running() {
			settled := one.info()
			return settled.started.Add(settled.elapsed)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("job %d never ended", id)
	return time.Time{}
}

// afterThePromotion answers the index of the first request the worker made once
// the promotion's own sentence was in its transcript — the one turn this whole
// file is about.
//
// IT IS FOUND BY THE SENTENCE AND NOT COUNTED TO. "The turn after the promotion"
// is what the law names; a hard-coded index would only be that turn by
// coincidence, and would go on passing while naming a different one.
func afterThePromotion(t *testing.T, completer *scriptedCompleter) int {
	t.Helper()
	for index := 0; index < completer.requests(); index++ {
		if strings.Contains(roleText(completer.request(index), "tool"), BashPromotedLead) {
			return index
		}
	}
	t.Fatal("the worker was never asked anything after its command was taken over, so there is no turn here to be right or wrong about")
	return 0
}

// nudgedWhileParked answers whether the worker was told off for repeating itself
// BEFORE its command's ending reached it, and with what.
//
// THE STRETCH IS THE POINT. A worker that goes on polling a log after the ending
// is in front of it is repeating itself, and a detector that stops it is working
// exactly as #568 says it must go on working. What must never happen is a worker
// DRIVEN into that repetition by a wait it was never given, and that is the only
// stretch this reads: the transcript up to the message carrying the ending.
func nudgedWhileParked(a *Agent) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, message := range a.messages {
		text := messageText(message)
		if message.Role == "user" && strings.Contains(text, "while you worked:") {
			return "", false
		}
		if strings.Contains(text, "[stuck]") {
			return text, true
		}
	}
	return "", false
}

// theCallAlone is how many requests this worker has made when its command is
// promoted: the one that made the call, and nothing else. Every test below hands
// it to [waitParkedOnItsCommand] as the count a second request would exceed.
const theCallAlone = 1

// ── the wait ────────────────────────────────────────────────────────────────

// THE MEASURED FAILURE, PINNED. A foreground command outlives the handoff clock,
// the promotion road takes the process, and the first thing the worker is asked
// after that must be the turn that carries the command's ending.
//
// The command is a real one in a real shell, because what broke was the ordering
// between a call and a turn and a fake would test the fake (promote_test.go
// makes the same choice for the same reason). It prints, it outlives the handoff
// because the test holds it open across it, and its last word is the word the
// ending has to carry.
func TestATaskParksOnItsPromotedForegroundCommandUntilItExits(t *testing.T) {
	record := &askLog{}
	suite := holdACommand(t, "starting the suite", "PASS")
	completer := &scriptedCompleter{steps: []step{
		bashStep(record, "the-suite", suite.text),
		sayStep(record, "the suite passed"),
	}}
	here := jobNest(t, completer)

	done, stopped := runParent(t, here, taskLimits{maxSteps: 200, noProgress: 6})

	// THE COMMAND ENDS WHERE THE TEST SAYS AND NOT WHERE A TIMER DOES: once the
	// promotion has actually happened, and once the worker has either parked on
	// it or shown that it did not.
	waitPromoted(t, here.node)
	waitParkedOnItsCommand(t, here.node, completer, theCallAlone)
	suite.release(t)

	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("the worker never finished")
	}
	if *stopped != "" {
		t.Fatalf("the worker was stopped with %q, want a worker that waited on its own command to be left alone", *stopped)
	}

	ended := waitSettled(t, here.node, 1)
	next := afterThePromotion(t, completer)
	woken := userTextIn(completer.request(next))

	// THE WHOLE FINDING, AS AN ORDER. Nothing may fall between the promotion's
	// result and the ending of the command that result is about: the very next
	// thing the worker is asked is the turn that reads how its command went. The
	// gap is printed because it is worth reading, and it is not what is asserted.
	if !strings.Contains(woken, "job 1 exited 0: PASS") {
		gap := ""
		if asked, made := record.askedAt(next); made {
			gap = fmt.Sprintf(" — asked %v before the command ended", ended.Sub(asked))
		}
		t.Fatalf("the first turn after %q carries no ending%s; it reads:\n%s",
			BashPromotedLead+"…", gap, woken)
	}

	// AND THE ENDING ARRIVES WHOLE, which is the only thing that makes the wait
	// worth taking. Trimmed to its headline it would say no more than the row at
	// the foot of the promotion result already said — which is the reading the
	// wait was taken INSTEAD of — so it carries its last lines and the path to
	// the rest ([Agent.jobNote] states the law, [jobRegistry.settleExit]
	// composes it, [batchSessionNotes] wraps it).
	for _, want := range []string{
		"while you worked:",
		"starting the suite\nPASS",
		"full log:",
	} {
		if !strings.Contains(woken, want) {
			t.Fatalf("the turn that read the ending is missing %q; it reads:\n%s", want, woken)
		}
	}
}

// ── the person's stop ───────────────────────────────────────────────────────

// AND A STOP IS AN ENDING LIKE ANY OTHER. The wait is owed the command's result,
// not the command's success: a person who reaches the job's row and stops it
// (jobstop.go's [Agent.cancelJob]) has ended the thing the worker is waiting for,
// and the worker is owed exactly one turn about it.
//
// This is the half that would hang. A wait released only by a clean exit leaves a
// worker parked on a process nobody is going to see exit, until its bound runs
// out, with the person's own stop as the last thing that happened.
//
// The command here is never released, so THE PERSON'S STOP IS THE ONLY ENDING IT
// CAN HAVE. A `sleep 30` stood here once and was the same test with a ceiling on
// it: nothing about this is supposed to depend on the stop arriving within half
// a minute.
func TestAStoppedCommandWakesTheWaitWithItsOwnEnding(t *testing.T) {
	record := &askLog{}
	long := holdACommand(t, "the long one", "never")
	completer := &scriptedCompleter{steps: []step{
		bashStep(record, "the-long-one", long.text),
		sayStep(record, "somebody stopped it; here is where I got to"),
	}}
	here := jobNest(t, completer)

	// Taken after the nest is built and before the run's goroutines exist, so
	// what this counts is the RUN's goroutines and not the session's.
	before := runtime.NumGoroutine()
	done, stopped := runParent(t, here, taskLimits{maxSteps: 200, noProgress: 6})

	promoted := waitPromoted(t, here.node)
	pid := promoted.cmd.Process.Pid
	// The stop is made once the worker is holding — or once it has shown it is
	// not — so what follows is about the stop and never about which of the two
	// goroutines the machine ran first.
	waitParkedOnItsCommand(t, here.node, completer, theCallAlone)
	answer, err := here.node.Cancel(fmt.Sprintf("%s:%d", CancelJob, promoted.id))
	if err != nil {
		t.Fatalf("the person's stop was refused: %v", err)
	}
	if !strings.HasPrefix(answer, "stopped ") {
		t.Fatalf("the person's stop answered %q, want the line that says it stopped", answer)
	}

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the worker never finished after its command was stopped: the wait outlived the thing it was waiting for")
	}
	if *stopped != "" {
		t.Fatalf("the worker was stopped with %q, want a stopped command to be news rather than an ending", *stopped)
	}
	// EXACTLY ONCE. A stop that woke the wait twice would buy a second turn about
	// an ending the first one already carried.
	if got := completer.requests(); got != 2 {
		t.Fatalf("the worker was asked %d times, want the call and the one turn that reads its stop", got)
	}
	if record.asks() != 2 {
		t.Fatalf("the script was reached %d times, want twice", record.asks())
	}

	// NOTHING IS LEFT RUNNING. The registry's kill reaches the whole process
	// group and the reaper has already taken the corpse, so the pid is gone
	// rather than a zombie somebody has to explain.
	if one := here.node.jobs.find(promoted.id); one == nil || one.running() {
		t.Fatalf("job %d is still running after the person stopped it", promoted.id)
	}
	waitFor(t, "the stopped process to be gone", func() bool {
		return syscall.Kill(pid, syscall.Signal(0)) != nil
	})

	// AND NOBODY IS STILL PARKED. A wait released by a road nothing closes is a
	// goroutine per stopped command, which is the shape that survives every test
	// that only asks whether the run ended.
	settled := before
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
		if settled = runtime.NumGoroutine(); settled <= before+2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%d goroutines are still parked after the stop, want no more than %d", settled, before+2)
}

// ── the job nobody is waiting for ───────────────────────────────────────────

// AND ONLY A CALL SOMEBODY WAS WAITING FOR IS OWED. `background: true` is the
// model saying it does not want to wait; that call went to [jobRegistry.start]
// and was a job from its first instant, never a foreground call and never
// promoted (promote.go states the branch, [job.owed] states the provenance). A
// worker held open by one of those could never hand its work back — a dev server
// started as a job would keep its node alive until the session closed.
//
// This one passed before the wait existed and must go on passing after it: it is
// the guard against a fix that reads "a job is running" where the law says "the
// call this worker made has not ended".
//
// The command is held rather than slept for the reason the others are, and here
// the reason is the LAST assertion: "the job really is still running" is the
// evidence that what was measured is a worker letting go, and a sleep can expire
// out from under that evidence on a slow enough box. A held command cannot. The
// two clock readings that remain are what they always were — two scripted turns
// that do no work at all, against ten seconds — and they are a bound on a
// worker that never blocks rather than a race between two things that both take
// about as long as each other.
func TestAnExplicitBackgroundJobDoesNotHoldATaskOpen(t *testing.T) {
	record := &askLog{}
	server := holdACommand(t, "the server is up", "never")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			record.ask(server.text)
			return toolResponse("the-server", "bash",
				fmt.Sprintf(`{"command":%q,"background":true}`, server.text)), nil
		},
		sayStep(record, "it runs while I carry on"),
	}}
	here := jobNest(t, completer)

	started := time.Now()
	done, stopped := runParent(t, here, taskLimits{maxSteps: 200, noProgress: 6})
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the worker was held open by a job nobody was waiting for")
	}
	// WELL WITHIN A BOUND THE WORK CANNOT REACH. The worker's two turns run no
	// command of their own, so ten seconds is a ceiling on a run that should take
	// milliseconds, and the command it started is still going.
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("the worker took %v, want it done long before its background command", elapsed)
	}
	if asked, made := record.askedAt(1); !made || asked.Sub(started) > 10*time.Second {
		t.Fatalf("the worker's second turn came %v after it started (reached: %v), want it asked promptly",
			asked.Sub(started), made)
	}
	if *stopped != "" {
		t.Fatalf("the worker was stopped with %q", *stopped)
	}
	if got := completer.requests(); got != 2 {
		t.Fatalf("the worker was asked %d times, want the call and the turn that carries on without it", got)
	}
	// And the job really is still running, so what was measured above is a
	// worker that let go rather than a command that finished early.
	if one := here.node.jobs.find(1); one == nil || !one.running() {
		t.Fatal("the background command was not still running, so nothing here says the worker let go of it")
	}
}

// ── what the wait costs ─────────────────────────────────────────────────────

// THE WAIT SPENDS NOTHING, and that is the half of this the field bill was
// actually made of. The worker in the run had no result and a model waiting for
// a move, so it invented the only move available — look at the log, wait, look
// again — and every one of those was a model round-trip, a tool call, and a step
// on the counter that eventually killed it.
//
// The script below IS that invention, kept unconditional on purpose: it polls
// whether or not it has been given the ending. A worker that is parked cannot
// reach the polls until its command has ended, so over the stretch it was
// waiting it spends NO step and earns NO nudge — and everything it chooses to do
// after the ending is its own, including being stopped for repeating itself,
// which is the reader working correctly and is deliberately not asserted about.
func TestTheWaitOnItsCommandSpendsNoStepAndNoNudge(t *testing.T) {
	record := &askLog{}
	suite := holdACommand(t, "starting the suite", "PASS")
	const poll = "sleep 0.2; echo still running"
	steps := []step{bashStep(record, "the-suite", suite.text)}
	for round := 0; round < 3; round++ {
		steps = append(steps, bashStep(record, fmt.Sprintf("poll-%d", round), poll))
	}
	steps = append(steps, sayStep(record, "the suite passed"))
	completer := &scriptedCompleter{steps: steps}
	here := jobNest(t, completer)

	done, stopped := runParent(t, here, taskLimits{maxSteps: 200, noProgress: 6})

	// AND THE COMMAND OUTLIVES THE POLL THE WORKER WOULD HAVE INVENTED, BECAUSE
	// THE TEST HOLDS IT. Released only once the promotion has happened and the
	// worker has parked — or has reached for the first poll and proved it did
	// not — so the ending below is on the far side of whichever of those two
	// happened, and never on the near side by an accident of scheduling.
	waitPromoted(t, here.node)
	waitParkedOnItsCommand(t, here.node, completer, theCallAlone)
	suite.release(t)

	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("the worker never finished")
	}
	if *stopped != "" {
		t.Fatalf("the worker was stopped with %q, want the wait not to have spent its allowance", *stopped)
	}

	// NOT ONE STEP WHILE IT WAS WAITING. Both instants come from the same clock
	// and neither is sampled by a watcher: the asks are stamped as the worker
	// makes them, and the ending is [job.settle]'s own.
	ended := waitSettled(t, here.node, 1)
	if invented := record.ranBefore(ended); len(invented) > 0 {
		t.Fatalf("the worker ran %d commands while the one it was waiting for was still running: %q",
			len(invented), invented)
	}

	// AND IT WAS NEVER TOLD OFF FOR WAITING. A run of identical calls is exactly
	// what the loop detector is built to catch (looped.go), and a worker driven
	// into making them by a wait it was never given is scolded for the harness's
	// own omission.
	if note, nudged := nudgedWhileParked(here.node); nudged {
		t.Fatalf("the worker was nudged for repeating itself before its command's ending reached it:\n%s", note)
	}
}
