package session

// A MEASUREMENT IS NOT A LANDING.
//
// edit_video is on [savingTools] because a node ordered to land the film it has
// been cutting must still be handed the joining verb. But the map answers for a
// VERB and three readers were asking it about a CALL, and the verb's fourth
// action writes nothing: a standing firing whose whole night's work was
// `{"action":"measure"}` reported as landed — the one outcome the sweep may
// never reap, so its run folder was kept for ever — and a node measuring the
// same clip over and over had every measurement counted as the work moving,
// which is precisely the spin the counter exists to catch.
//
// [producedAFile] is the answer both of them ask now, and these are the two
// readers held to it.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── what a firing came to ───────────────────────────────────────────────────

// A WORDLESS FIRING THAT ONLY MEASURED CAME TO NOTHING, and the same firing that
// cut something landed. The pair matters more than either half: the fix is worth
// nothing if it took the verb's real work down with the reading.
func TestAFiringThatOnlyMeasuredCameToNothing(t *testing.T) {
	requireFfmpeg(t)

	measured := editVideoFiring(t, `{"action":"measure","video":"clip.mp4"}`)
	if measured.Kind != standing.OutcomeNothing {
		t.Fatalf("a firing whose only act was a measure came to %q, wanted %q",
			measured.Kind, standing.OutcomeNothing)
	}

	joined := editVideoFiring(t, `{"action":"join","clips":["clip.mp4","clip.mp4"],"path":"cut.mp4"}`)
	if joined.Kind != "landed" {
		t.Fatalf("a firing that cut a film came to %q, wanted landed", joined.Kind)
	}
}

// editVideoFiring runs one headless standing firing whose whole life is a single
// edit_video call and not one word of report, and answers what it came to. The
// child, its belt and its loop are the product's own ([standingChildRunner]);
// only the model is scripted.
func editVideoFiring(t *testing.T, arguments string) standing.Outcome {
	t.Helper()
	root, workspace := t.TempDir(), t.TempDir()
	madeVideo(t, workspace, "clip.mp4", 1, true)

	cuts := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "edit_video", arguments), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(""), nil },
	}}
	outcome, err := standingChildRunner(t, root, cuts).Run(context.Background(),
		nightly(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return outcome
}

// ── what counts as the work moving ──────────────────────────────────────────

// THE SAME CLIP MEASURED TWICE IS A TURN GOING NOWHERE. The first reading is
// information — nobody had asked — and the second brings back the answer it
// already has, so neither of them may reset the clock that says when the work
// last changed. The join that follows both does.
func TestARepeatedMeasureIsNotTheWorkMoving(t *testing.T) {
	watch := newLoopWatch()
	measure := ai.ToolCall{Function: ai.ToolCallFunction{
		Name: "edit_video", Arguments: `{"action":"measure","video":"clip.mp4"}`}}
	measured := []toolResult{{text: "clip.mp4 — 6 seconds, sound, 1280×720, 4.1 MB of mp4"}}

	if !watch.count(measure, measured, 0) {
		t.Fatal("the first measurement of a clip told the node nothing")
	}
	if watch.count(measure, measured, 0) {
		t.Fatal("the same clip measured again counted as the turn moving forward")
	}
	if watch.clock.changedAt != 0 {
		t.Fatalf("the work is recorded as having changed at step %d; nothing was written",
			watch.clock.changedAt)
	}

	join := ai.ToolCall{Function: ai.ToolCallFunction{
		Name: "edit_video", Arguments: `{"action":"join","clips":["clip.mp4","clip.mp4"],"path":"cut.mp4"}`}}
	if !watch.count(join, []toolResult{{text: "cut.mp4 — 12 seconds, sound, joined from 2 clips"}}, 0) {
		t.Fatal("a joined cut did not count as the work moving")
	}
	if watch.clock.changedAt == 0 {
		t.Fatal("the joined cut left the work's clock saying nothing had ever changed")
	}

	// AND A FAILED CUT IS NOT A CUT. Nothing was written, whatever the arguments
	// asked for — the same rule a failed edit has always been held to.
	failed := newLoopWatch()
	failed.count(join, []toolResult{{text: "Could not join the clips", isError: true}}, 0)
	if failed.clock.changedAt != 0 {
		t.Fatal("a join that failed was counted as the work moving")
	}
}

// AND WHAT A MEASUREMENT IS INSTEAD IS A READING, which the node's own counter
// has to keep taking as one. Closing the landing hole by itself would have made
// a measure neither a save nor a look at the world — and a node measuring its
// three clips before joining them would have been three steps nearer being
// stopped for making no progress, which is the punishment for exploring that
// [knowledgeTools] was widened to the whole read-only belt to end.
func TestAMeasureStillCountsAsTheNodeLearningSomething(t *testing.T) {
	ledger := newProgressLedger()
	measured := Event{Kind: EventToolEnd, Tool: "edit_video",
		Args:   `{"action":"measure","video":"clip.mp4"}`,
		Output: "clip.mp4 — 6 seconds, sound, 1280×720, 4.1 MB of mp4"}

	if !addedSomething(measured, false, false, ledger) {
		t.Fatal("measuring a clip for the first time added nothing to the run")
	}
	// And the second reading of the same unchanged clip adds nothing, which is
	// the whole point of asking the ledger rather than the hand.
	if addedSomething(measured, false, false, ledger) {
		t.Fatal("the same clip measured again added something")
	}
}

// AND THE STRONGEST SIGNAL THIS WATCH HAS IS NOT SPENT ON A READING EITHER.
// [loopWatch.materialProgress] is what breaks a silent streak and gives a spent
// note back, and it used to read the hand's NAME — so admitting edit_video to
// the mutating machinery for the write scope's sake would have handed a
// read-only measure the one signal that resets the ladder.
func TestAMeasureIsNotMaterialProgress(t *testing.T) {
	watch := newLoopWatch()
	measure := ai.ToolCall{Function: ai.ToolCallFunction{
		Name: "edit_video", Arguments: `{"action":"measure","video":"clip.mp4"}`}}
	if watch.materialProgress([]ai.ToolCall{measure}, []toolResult{{text: "clip.mp4 — 6 seconds"}}) {
		t.Fatal("measuring a clip counted as this turn putting something in the world")
	}

	saved := editVideoJoinCall("c1", "cut.mp4", "clip.mp4", "clip.mp4")
	if !watch.materialProgress([]ai.ToolCall{saved}, []toolResult{{text: "cut.mp4 — 12 seconds"}}) {
		t.Fatal("cutting a film did not count as this turn putting something in the world")
	}
}
