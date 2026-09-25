package run_test

// A NOTE IS HANDED TO A TASK ONCE.
//
// Two ways the channel handed the same words over twice: a worker was handed
// back the note it had just written itself, and a worker launched again on the
// same task — a parent woken to integrate its children, a parked task woken —
// started from nothing and was handed the task's older notes a second time.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/run"
)

// echoesUntilTheCap keeps a worker moving with a different command every step,
// so the step cap is what ends it and every boundary before the cap is one the
// channel could hand a note over at.
func echoesUntilTheCap() step {
	done := 0
	return func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
		done++
		return toolReply(`{"command":` + jsonString(keepsMoving("echo working", done)) + `}`), nil
	}
}

// TestAWorkerIsNotHandedItsOwnNote: the worker leaves a note on its own task,
// and at the next boundary the channel handed its own sentence back to it as
// something "left on your task".
func TestAWorkerIsNotHandedItsOwnNote(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	const own = "the parser lives in cmd/parse and not in internal"
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":` + jsonString("plandb task note t-root '"+own+"'") + `}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":"echo one"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":"echo two"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":"echo three"}`), nil
		},
	}}
	worker := run.NewBashWorker(store, filepath.Dir(store.Path()), "test/model", seat)
	_, _ = worker.Run(run.WithStepsPerTask(runContext(t), 4), *store.Task(store.RootID()))

	written := false
	for _, note := range store.Notes(store.RootID(), 0) {
		written = written || note.Body == own
	}
	if !written {
		t.Fatalf("the worker's note never reached the store; what it was asked:\n%s", seatTranscript(seat))
	}
	if seatSaw(seat, "a note was left on your task") {
		t.Fatalf("the worker was handed its own note back:\n%s", seatTranscript(seat))
	}
}

// TestAWorkerLaunchedAgainIsNotHandedNotesItsTaskAlreadyHad: a person's note is
// handed to the task's first worker; a second worker on the same task — a wake —
// must not be handed it again.
func TestAWorkerLaunchedAgainIsNotHandedNotesItsTaskAlreadyHad(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	const said = "the fixture regenerates itself, do not commit it"
	if _, err := store.AddPersonNote(store.RootID(), said); err != nil {
		t.Fatalf("leave the note: %v", err)
	}

	first := &seat{ever: echoesUntilTheCap()}
	worker := run.NewBashWorker(store, filepath.Dir(store.Path()), "test/model", first)
	_, _ = worker.Run(run.WithStepsPerTask(runContext(t), 3), *store.Task(store.RootID()))
	if !seatSaw(first, said) {
		t.Fatalf("the first worker was never handed the note:\n%s", seatTranscript(first))
	}

	again := &seat{ever: echoesUntilTheCap()}
	woken := run.NewBashWorker(store, filepath.Dir(store.Path()), "test/model", again)
	_, _ = woken.Run(run.WithStepsPerTask(runContext(t), 3), *store.Task(store.RootID()))
	if seatSaw(again, said) {
		t.Fatalf("the task was handed a note it already had, a second time:\n%s", seatTranscript(again))
	}
}
