//go:build e2e

package e2e

// refusedargs_e2e_test.go is issue #890's e2e half, driven end to end: the real
// binary, a real terminal, and a real model asked to propose a task whose check
// is a composed shell command — the shape small models send routinely.
//
// WHAT IS ASSERTED. Two sentences and an absence. The refused proposal's card
// settles on `not started · the call was refused`; the refused call's row says
// `the call was refused`; and the screen NEVER contains `Invalid arguments:`,
// because that sentence is the schema's repair instruction for the model —
// field names, the quoted command — and it stays in the tool result and behind
// ctrl+o rather than being drawn to the person. A model that mends the call on
// its second turn is the fix working, not noise: what is asserted is that the
// refusal was never drawn, in whatever turn the model takes.

import (
	"strings"
	"testing"
	"time"
)

// testRefusedTaskProposal is the subtest the tmux suite runs. One prompt is
// engineered to make a composed check likely: the acceptance is phrased as a
// command that has to run in a subdirectory, and `cd X && make test` is the
// natural spelling a model reaches for.
func testRefusedTaskProposal(t *testing.T) {
	requireTmuxAndKey(t)
	home := newHome(t, nil)
	ws := newWorkspace(t, "refusedargsws", false)
	r := start(t, "afe2e_refused_args", home, ws, tuiWide, 45)
	statesPastTheDoor(t, r)

	r.lit(`use propose_task to start a task titled "check the gate". Its checks must run ./verify.sh from inside the subdirectory gate — send the one command cd gate && ./verify.sh as the check, exactly that, do not rewrite it into two commands, and once the call has been refused once keep sending it unchanged`)
	r.keys("Enter")

	// The refusal can arrive within seconds of the call being placed, and the
	// person's words for it settle right after. What must never appear is the
	// schema's own sentence, so the screen is read once the row has settled and
	// then again after the turn has finished, which is the harder case: the
	// model may repair and succeed, and the repair's history is exactly what
	// must stay undrawn.
	settled := r.waitFor(3*time.Minute, say(t, "refusedCallRowWord"))
	t.Logf("a refused proposal settled in the person's words:\n%s", settled)
	if strings.Contains(settled, "Invalid arguments:") {
		t.Errorf("the schema's refusal sentence is drawn to the person:\n%s", settled)
	}

	// The turn's end is where a spilled row would linger longest, and where a
	// second refusal — the model sending the composed check again — would draw
	// the sentence a second time. The suite's own way of knowing a model turn
	// has finished is waiting for a word that only comes back with it, and here
	// that word is the status line's `idle`.
	//
	// IT WAS [exchangeAnswerHint], AND THAT WORD CANNOT ARRIVE IN THIS SCENARIO.
	// That needle is the answer line a ONE-OFF REMINDER's card offers — `1 yes,
	// set it up · 0 no · c change` — which is what [testAskHere] is waiting for
	// when it waits for a turn to finish. #938 took the five-second sleep out of
	// this subtest and copied that call without its scenario: a proposal the tool
	// REFUSED has no answers to offer, so the card settles on `not started · the
	// call was refused` and the foot the suite was waiting for is one the product
	// is right never to draw. The wait burned ninety seconds of every run and
	// then failed, in front of two assertions that were passing.
	final := r.waitFor(modelPatience, say(t, "refusedCallRowWord"))
	final = r.waitFor(modelPatience, say(t, "idleWord"))
	if strings.Contains(final, "Invalid arguments:") {
		t.Errorf("the schema's refusal sentence is on the screen after the turn:\n%s", final)
	} else {
		t.Logf("the screen after the turn carries no repair sentence:\n%s", final)
	}
	r.quit()
}
