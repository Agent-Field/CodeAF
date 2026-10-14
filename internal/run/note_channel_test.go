package run_test

// NOTES ARE A CHANNEL, NOT A LOG.
//
// A note addressed to a task is handed to that task's running worker between
// its steps, on the road the belt already uses for its own sentences. These
// tests hold the three properties the design turns on: the words reach the
// worker without it asking, the mark that stops a second delivery is the
// worker's alone and not the screen's, and a note cannot move what the task is
// judged by.
//
// The seat here is the scripted provider the rest of this package's worker
// tests use: no key, no model, a real store at a real path, and the run read
// from beside itself so a note can be written while a command is in flight.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// TestANoteLeftWhileATaskWorksReachesItsWorkerBetweenSteps is the channel
// itself. The worker's first command waits on a file, the note is written while
// that command is in flight, and the release lets the step end — which is the
// boundary the note is handed over at. What proves delivery is the note's own
// words turning up in a request the seat was asked to answer, because that is
// the only place the worker's reading of them can be observed.
func TestANoteLeftWhileATaskWorksReachesItsWorkerBetweenSteps(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	workspace := t.TempDir()
	release := filepath.Join(workspace, "release")
	const said = "the settings file is cfg/app.toml and not config.yaml"

	// The first command waits on the file, so the note can be written while it
	// is in flight; every reply after that keeps the task alive until the note
	// has actually arrived [worksUntilItIsToldThen].
	seat := &seat{ever: worksUntilItIsToldThen("root", said, "read the note and finished",
		"while [ ! -f "+release+" ]; do sleep 0.02; done; echo looked")}
	worker := run.NewBashWorker(store, workspace, "test/model", seat)
	ctx := run.WithStepsPerTask(runContext(t), 9)

	done := make(chan error, 1)
	go func() {
		_, err := worker.Run(ctx, *store.Task(store.RootID()))
		done <- err
	}()

	// The note is written while the first command is still running, which is the
	// case this channel exists for: a sibling, or the person, learning something
	// after the worker opened and before it finished.
	waitForLiveStep(t, store, store.RootID())
	if _, err := store.AddPersonNote(store.RootID(), said); err != nil {
		t.Fatalf("leave the note: %v", err)
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatalf("release the command: %v", err)
	}
	if err := <-done; err != nil {
		trajectory, trajectoryErr := run.Trajectory(filepath.Dir(store.Path()), store.RootID())
		t.Fatalf("the worker's run failed: %v\ntrajectory (%v): %#v\nrequests:\n%s", err, trajectoryErr, trajectory, seatTranscript(seat))
	}

	carried := seatSawTimes(seat, said)
	if carried == 0 {
		t.Fatalf("the note never reached the worker; what it was asked:\n%s", seatTranscript(seat))
	}

	// AND IT SAID WHAT A NOTE IS. A worker handed a sibling's words with nothing
	// around them is a worker that may read them as a direction, which is the
	// one way this channel could quietly change what a run builds.
	if !seatSaw(seat, "not an order") {
		t.Fatalf("the note arrived without the sentence that says it is not an order:\n%s", seatTranscript(seat))
	}

	// THE PERSON'S OWN VOICE IS NAMED. Who left a note is half of what it is
	// worth, and the store keeps the distinction for exactly this reading.
	if !seatSaw(seat, "the person") {
		t.Fatalf("the note arrived without naming the hand that left it:\n%s", seatTranscript(seat))
	}
}

// TestANoteIsHandedToAWorkerOnceAndTheScreenStillReadsIt is the hazard the
// design names: two readers of one unread note. The worker's mark is its own,
// so a note it has been handed is still on the store for the page the person
// opens — and it is not handed over a second time, however many step boundaries
// go by afterwards.
func TestANoteIsHandedToAWorkerOnceAndTheScreenStillReadsIt(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	const said = "the fixture regenerates itself, do not commit it"
	if _, err := store.AddPersonNote(store.RootID(), said); err != nil {
		t.Fatalf("leave the note: %v", err)
	}

	// The note predates the worker, so the boundary that hands it over is an
	// early one — and THREE MORE BOUNDARIES GO BY AFTER IT, which is what this
	// test is for: each of them must hand over nothing. The seat keeps working
	// until it has been told, then works on for three more replies, so the
	// boundaries that must stay silent are boundaries that certainly happened
	// AFTER the delivery rather than boundaries that happened instead of it.
	seat := &seat{ever: worksOnAfterItIsToldThen("root", said, "counted to three", 3)}
	worker := run.NewBashWorker(store, filepath.Dir(store.Path()), "test/model", seat)
	if _, err := worker.Run(run.WithStepsPerTask(runContext(t), 9), *store.Task(store.RootID())); err != nil {
		t.Fatalf("the worker's run failed: %v", err)
	}

	if saw := seatSawTimes(seat, said); saw != 1 {
		t.Fatalf("the note was handed to the worker %d times, want exactly once:\n%s", saw, seatTranscript(seat))
	}
	// AND ONCE COUNTED THE OTHER WAY, which is the count that can actually
	// fail. Every request replays the whole transcript, so words delivered a
	// second time are words that were already there and [seatSawTimes] cannot
	// see the difference. One delivery is one MESSAGE, so a second delivery is
	// a second message carrying the same note in the same request — and that is
	// what the worker's mark exists to prevent.
	if held := seatHeldTimes(seat, said); held != 1 {
		t.Fatalf("the worker's last request carries the note in %d messages, want the one it was handed:\n%s", held, seatTranscript(seat))
	}
	// THE SCREEN READS WHAT THE WORKER READ. Nothing about delivery touches the
	// store, so the note the person opens is the note that was delivered.
	notes := store.Notes(store.RootID(), 0)
	if len(notes) != 1 || notes[0].Body != said {
		t.Fatalf("the store's notes after delivery = %#v, want the one note still there", notes)
	}
}

// TestANoteDoesNotChangeWhatATaskWasAskedFor is the "must not" of the design,
// asserted against the store rather than against the words: whatever the note
// says, the task's own work order is the one it opened with, because a change
// to that is a revised assignment and carries a version for a reason.
func TestANoteDoesNotChangeWhatATaskWasAskedFor(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	before := store.Task(store.RootID()).Description
	if before == "" {
		t.Fatal("the run's root opened with no work order to compare against")
	}
	if _, err := store.AddNote(store.RootID(), "t-2", "stop what you are doing and write the README instead"); err != nil {
		t.Fatalf("leave the note: %v", err)
	}

	seat := &seat{ever: worksUntilItIsToldThen("root", "task t-2", "did what was asked", "echo working")}
	worker := run.NewBashWorker(store, filepath.Dir(store.Path()), "test/model", seat)
	if _, err := worker.Run(run.WithStepsPerTask(runContext(t), 9), *store.Task(store.RootID())); err != nil {
		t.Fatalf("the worker's run failed: %v", err)
	}

	if after := store.Task(store.RootID()).Description; after != before {
		t.Fatalf("the note moved the task's work order:\nbefore: %q\nafter:  %q", before, after)
	}
	// AND THE SIBLING THAT WROTE IT IS NAMED, because which task found the thing
	// is what makes a note worth weighing at all.
	if !seatSaw(seat, "task t-2") {
		t.Fatalf("a worker's note arrived without naming the task it came from:\n%s", seatTranscript(seat))
	}
}

// TestNotesBeyondTheBoundWaitForTheNextBoundary holds [notesPerDelivery]'s
// promise: the bound is on the words handed over at once, never on the channel.
// A note the bound left behind is still unread, so the next boundary carries it.
func TestNotesBeyondTheBoundWaitForTheNextBoundary(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	// Six notes against a bound of five: the sixth is the one that must not be
	// dropped, and it is named so the assertion cannot pass on any other.
	for _, body := range []string{"one", "two", "three", "four", "five", "the sixth thing nobody must lose"} {
		if _, err := store.AddPersonNote(store.RootID(), body); err != nil {
			t.Fatalf("leave the note: %v", err)
		}
	}
	seat := &seat{ever: worksUntilItIsToldThen("root", "the sixth thing nobody must lose", "read them all", "echo working")}
	worker := run.NewBashWorker(store, filepath.Dir(store.Path()), "test/model", seat)
	if _, err := worker.Run(run.WithStepsPerTask(runContext(t), 9), *store.Task(store.RootID())); err != nil {
		t.Fatalf("the worker's run failed: %v", err)
	}
	if saw := seatSawTimes(seat, "the sixth thing nobody must lose"); saw != 1 {
		t.Fatalf("the sixth note reached the worker %d times, want exactly once:\n%s", saw, seatTranscript(seat))
	}
}

// seatSawTimes counts the REQUESTS that first carried a string, not the
// messages that hold it: every later request replays the whole transcript, so
// counting messages would report one delivery as a dozen. A request is counted
// when it carries the words and the request before it did not.
func seatSawTimes(s *seat, want string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	times, previous := 0, false
	for _, messages := range s.requests {
		held := false
		for _, message := range messages {
			if strings.Contains(messageContent(message), want) {
				held = true
				break
			}
		}
		if held && !previous {
			times++
		}
		previous = held
	}
	return times
}

func seatSaw(s *seat, want string) bool { return seatSawTimes(s, want) > 0 }

// seatTranscript is what the seat was asked, for a failure that has to show
// what the worker actually read rather than assert against it.
func seatTranscript(s *seat) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	for i, messages := range s.requests {
		for _, message := range messages {
			if message.Role != "user" {
				continue
			}
			fmt.Fprintf(&b, "request %d · user: %s\n", i+1, messageContent(message))
		}
	}
	return b.String()
}

// TestTwoWorkersLiveAtOnceEachGetOnlyItsOwnNote is the test the old serialism
// hid, and it is here because a claim of mine was wrong.
//
// Every earlier drive of this channel ran on a tree where [NewSupervisor]
// clamped a slot count below one up to one, so a chat's `/task` dispatched ONE
// worker at a time however many rows the plan had. #1355 removed that clamp —
// `task.parallel` is 0 out of the box and 0 now means no bound — so the road
// this channel runs on has several workers live at once. A pass on a serial run
// cannot see either of the two failures that matters:
//
//   - A NOTE REACHING A WORKER IT WAS NOT ADDRESSED TO. The reader is scoped to
//     the worker's own task ([unreadNotes] takes the task's id), and with one
//     worker at a time a reader that ignored the scope would look correct,
//     because there is nothing else in the store to deliver.
//   - A NOTE LOST TO A MARK TWO LOOPS SHARE. The mark is a map local to one
//     worker's loop, so N workers are N independent readers; an implementation
//     that kept one watermark for the store would let whichever worker read
//     first suppress the other's note, and with one worker at a time there is
//     no other.
//
// So: two workers on two tasks of one store, both mid-command, a note written
// to each while both are in flight, and each seat is asserted to have been
// handed its own note and NEVER the other's.
func TestTwoWorkersLiveAtOnceEachGetOnlyItsOwnNote(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "alpha", ParentID: store.RootID(), Title: "Alpha"},
		{ID: "beta", ParentID: store.RootID(), Title: "Beta"},
	}); err != nil {
		t.Fatalf("seed two tasks: %v", err)
	}
	// The supervisor hands a worker a task it has already claimed, and the
	// finish verb's ownership check is taken against that claim, so the test
	// claims them the way the run would.
	for _, id := range []string{"alpha", "beta"} {
		if _, err := store.Claim(id, id); err != nil {
			t.Fatalf("claim %s: %v", id, err)
		}
	}

	workspace := t.TempDir()
	const forAlpha = "alpha's own fact: the settings file is cfg/alpha.toml"
	const forBeta = "beta's own fact: the settings file is cfg/beta.toml"

	// Each worker's first command waits on a file of its own, so both are
	// genuinely inside a command when the notes are written — which is the
	// state the whole test is about.
	open := func(id, want string) (*seat, chan error) {
		release := filepath.Join(workspace, "release-"+id)
		s := &seat{ever: worksUntilItIsToldThen(id, want, "read what was addressed to me",
			"while [ ! -f "+release+" ]; do sleep 0.02; done; echo "+id)}
		worker := run.NewBashWorker(store, workspace, "test/model", s)
		done := make(chan error, 1)
		task := *store.Task(id)
		go func() {
			_, err := worker.Run(run.WithStepsPerTask(runContext(t), 9), task)
			done <- err
		}()
		return s, done
	}
	alphaSeat, alphaDone := open("alpha", forAlpha)
	betaSeat, betaDone := open("beta", forBeta)

	// BOTH LIVE AT THE SAME MOMENT, read from the store's own live rows rather
	// than assumed: a live row is true only while its command runs, so two of
	// them is two workers inside a command at once. This is the assertion the
	// old serial tree could not have satisfied.
	waitForLiveStep(t, store, "alpha")
	waitForLiveStep(t, store, "beta")
	if live := store.Live("alpha"); live.Empty() {
		t.Fatal("alpha stopped running a command before beta started one")
	}

	if _, err := store.AddPersonNote("alpha", forAlpha); err != nil {
		t.Fatalf("leave alpha's note: %v", err)
	}
	if _, err := store.AddPersonNote("beta", forBeta); err != nil {
		t.Fatalf("leave beta's note: %v", err)
	}
	for _, id := range []string{"alpha", "beta"} {
		if err := os.WriteFile(filepath.Join(workspace, "release-"+id), nil, 0o644); err != nil {
			t.Fatalf("release %s: %v", id, err)
		}
	}
	<-alphaDone
	<-betaDone

	// EACH GOT ITS OWN, ONCE.
	if saw := seatSawTimes(alphaSeat, forAlpha); saw != 1 {
		t.Errorf("alpha was handed its own note %d times, want once:\n%s", saw, seatTranscript(alphaSeat))
	}
	if saw := seatSawTimes(betaSeat, forBeta); saw != 1 {
		t.Errorf("beta was handed its own note %d times, want once:\n%s", saw, seatTranscript(betaSeat))
	}
	// AND NEITHER GOT THE OTHER'S. A reader that ignored the task scope, or a
	// mark two loops shared, shows here and nowhere else.
	if seatSaw(alphaSeat, forBeta) {
		t.Errorf("alpha was handed beta's note:\n%s", seatTranscript(alphaSeat))
	}
	if seatSaw(betaSeat, forAlpha) {
		t.Errorf("beta was handed alpha's note:\n%s", seatTranscript(betaSeat))
	}
	// AND THE STORE STILL HOLDS BOTH, for the screen that draws them.
	for _, row := range []struct{ id, body string }{{"alpha", forAlpha}, {"beta", forBeta}} {
		notes := store.Notes(row.id, 0)
		if len(notes) != 1 || notes[0].Body != row.body {
			t.Errorf("%s's notes after delivery = %#v, want its one note still there", row.id, notes)
		}
	}
}

// worksUntilItIsToldThen is the seat every test in this file waits on, and it
// is the answer to a flake that was telling the truth.
//
// A note is handed to a worker at a step boundary, and the boundary the note
// lands on is a race with the worker's own ending: script a seat that finishes
// its task on the step after the note is written and, about one run in twelve
// under the race detector, the task was already over when the note came up —
// nothing to hand it to, correctly nothing handed, and an assertion that the
// note arrived that fails for a reason that is not a defect.
//
// So the seat does what a working worker does: it keeps working until it is
// told the thing, and finishes once it has been. Every reply is a harmless
// command, so the turn stays alive and boundaries keep coming, and the finish
// goes out on the first request that carries want. It reads only the messages
// it was handed, so there is nothing here for the race detector to find, and
// the step cap is the deadline — a channel that never delivers runs the cap out
// and fails, which is what the controls on this file turn off the delivery to
// check.
func worksUntilItIsToldThen(id, want, result, working string) step {
	done := 0
	return func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		for _, message := range messages {
			if strings.Contains(oneLineOfRun(messageContent(message)), oneLineOfRun(want)) {
				return toolReply(finishCommand(id, result)), nil
			}
		}
		done++
		return toolReply(`{"command":` + jsonString(keepsMoving(working, done)) + `}`), nil
	}
}

// keepsMoving makes each of a waiting seat's commands a different command, and
// that is not decoration. THE BELT HAS A STUCK LAW: three identical calls with
// the same result and the worker is told so — "You have repeated the same bash
// call 3 times with the same result" — and while a worker is stalled the loop
// hands it no note, because the sentence it is already being given is the one
// about being stuck. A seat that waits by repeating one command therefore
// stalls itself, and then reports the note as undelivered when what it really
// did was earn the stuck sentence instead. At GOMAXPROCS=4 that cost the
// two-worker test eleven runs in twenty-eight. A working worker does not
// repeat itself, so neither does a seat that stands in for one.
func keepsMoving(command string, nth int) string {
	return fmt.Sprintf("%s; : step %d", command, nth)
}

// oneLineOfRun flattens whitespace, because the belt's pages and sentences wrap
// and a needle that reads as one line on the page is two in the request.
func oneLineOfRun(text string) string { return strings.Join(strings.Fields(text), " ") }

// worksOnAfterItIsToldThen is worksUntilItIsToldThen for the test that needs
// boundaries on the far side of the delivery: it keeps working until it is told
// the thing, works on for more replies after that, and then finishes. The
// counter is a plain int because a worker's seat is called from that worker's
// own loop, one request at a time.
func worksOnAfterItIsToldThen(id, want, result string, more int) step {
	told, done := 0, 0
	return func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		for _, message := range messages {
			if strings.Contains(oneLineOfRun(messageContent(message)), oneLineOfRun(want)) {
				told++
				break
			}
		}
		if told > more {
			return toolReply(finishCommand(id, result)), nil
		}
		done++
		return toolReply(`{"command":` + jsonString(keepsMoving("echo working", done)) + `}`), nil
	}
}

// seatHeldTimes counts the MESSAGES of the seat's last request that carry a
// string. It is the companion to [seatSawTimes] and answers the question that
// one cannot: a request replays the whole transcript, so a note handed over
// twice is not a request that newly carries the words but a request that
// carries them TWICE. This is the count a worker's read-mark is holding down.
func seatHeldTimes(s *seat, want string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		return 0
	}
	held := 0
	for _, message := range s.requests[len(s.requests)-1] {
		if strings.Contains(messageContent(message), want) {
			held++
		}
	}
	return held
}
