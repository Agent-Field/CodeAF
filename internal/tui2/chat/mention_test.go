package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
)

// 5.18's data half: what this room can address, read off the rail rather than
// out of a second query.
func TestTheMentionFilterOffersTheRailsOwnWork(t *testing.T) {
	app, _ := boardApp(t)
	targets := app.mentionTargets()
	if len(targets) != 2 {
		t.Fatalf("targets = %d, want the board's two jobs: %#v", len(targets), targets)
	}
	live, settled := targets[0], targets[1]
	if live.Word != "wisp-parity" || live.Title != "wisp-parity" {
		t.Fatalf("the live target reads %q / %q", live.Word, live.Title)
	}
	if live.Settled {
		t.Fatal("a running job was offered under history")
	}
	if !settled.Settled || settled.Word != "perf-audit" {
		t.Fatalf("the settled target reads %q, settled=%v", settled.Word, settled.Settled)
	}
	// The hue is the rail's assignment and not a second hash of the row id: one
	// task, one colour, everywhere (5.16).
	if want := blocks.Seed("job-1"); live.Seed != want {
		t.Fatalf("the live target's seed is %d, want the rail's %d", live.Seed, want)
	}
}

// A question waiting on a human is the one thing 5.16 paints amber, and the
// filter reads the same count the card does.
func TestAnAddressableTargetCarriesItsAttention(t *testing.T) {
	app, backend := boardApp(t)
	backend.questions = []store.AgentQuestion{{OriginNodeID: "job-1/h2"}}
	backend.journal++
	poll(t, app)
	for _, target := range app.mentionTargets() {
		if target.Word == "wisp-parity" && !target.Attention {
			t.Fatal("a job with an open question is not marked as wanting a human")
		}
	}
}

// The word is a handle: typed, matched, and read back out of the draft. A title
// that is a sentence still has to reduce to something a person can type.
func TestTheTaskWordIsAHandleAndNotATitle(t *testing.T) {
	cases := map[string]string{
		"wisp-parity":                         "wisp-parity",
		"rework NavCtx after the worker died": "rework-navctx-after",
		"  Ship   the   importer  ":           "ship-the-importer",
		"!!!":                                 "",
		"aVeryLongSingleWordThatKeepsOnGoing": "averylongsinglewordth",
	}
	for title, want := range cases {
		if got := taskWord(title); got != want && !(want != "" && strings.HasPrefix(got, want[:min(len(want), 8)])) {
			t.Fatalf("taskWord(%q) = %q, want %q", title, got, want)
		}
		if strings.ContainsAny(taskWord(title), " \t") {
			t.Fatalf("taskWord(%q) has whitespace in it and can never be matched back", title)
		}
	}
}

// Two tasks may not answer to one token: a duplicate would silently address
// whichever came first, and the other would be unreachable.
func TestTwoTasksNeverShareOneWord(t *testing.T) {
	seen := map[string]int{}
	first := uniqueWord("importer", seen)
	second := uniqueWord("importer", seen)
	if first == second {
		t.Fatalf("two targets share the word %q", first)
	}
}

// 5.18's send half, live branch: an addressed send takes the steer door — the
// same journal verb the steer line uses, so a worker's mailbox stays one
// mailbox. The reader is NOT teleported.
func TestALiveDispatchSteersTheTaskAndStaysPut(t *testing.T) {
	app, backend := boardApp(t)
	before := app.view
	run(t, app.dispatchCmd(composer.Dispatch{
		TargetID: rowTaskPrefix + "job-1",
		Text:     "@wisp-parity skip H2 for now",
	}))
	posted := backend.posted[len(backend.posted)-1]
	if posted.NodeID != "job-1" {
		t.Fatalf("the dispatch landed on node %q", posted.NodeID)
	}
	if posted.Role != store.RoleUser || posted.Body != "@wisp-parity skip H2 for now" {
		t.Fatalf("the steered row reads %q as %q", posted.Body, posted.Role)
	}
	if app.view != before {
		t.Fatal("a plain send moved the reader's context")
	}
}

// 5.18's blunt rule: a settled target NEVER receives direct injection. The
// message goes to the main head with the task named as context, because a dead
// thread has nobody to absorb it.
func TestASettledDispatchGoesToTheHeadAsContext(t *testing.T) {
	app, backend := boardApp(t)
	run(t, app.dispatchCmd(composer.Dispatch{
		TargetID: rowTaskPrefix + "job-2",
		Settled:  true,
		Text:     "@perf-audit what did it find?",
	}))
	posted := backend.posted[len(backend.posted)-1]
	if posted.NodeID != "" {
		t.Fatalf("a settled target was injected directly, at node %q", posted.NodeID)
	}
	if want := "about perf-audit: what did it find?"; posted.Body != want {
		t.Fatalf("the routed body is %q, want %q", posted.Body, want)
	}
}

// The power chord: enter sends and stays, ctrl+enter sends and follows.
func TestFollowEntersTheAddressedTasksRoom(t *testing.T) {
	app, backend := boardApp(t)
	runAll(t, app.dispatchCmd(composer.Dispatch{
		TargetID: rowTaskPrefix + "job-1",
		Follow:   true,
		Text:     "@wisp-parity keep going",
	}))
	if len(backend.posted) == 0 {
		t.Fatal("the followed send journaled nothing")
	}
	if app.view == nil || app.view.kind != viewNode || app.view.node != "job-1" {
		t.Fatalf("follow did not enter the task's room: %#v", app.view)
	}
}

// A target that left the rail while the draft was being typed must not take the
// words with it (5.20, 12.5): the send falls back to the room it was typed in.
func TestADispatchToSomethingGoneStillLandsSomewhere(t *testing.T) {
	app, backend := boardApp(t)
	run(t, app.dispatchCmd(composer.Dispatch{TargetID: "gone", Text: "did it work?"}))
	posted := backend.posted[len(backend.posted)-1]
	if posted.NodeID != "" || posted.Body != "did it work?" {
		t.Fatalf("the orphaned dispatch landed as %q at node %q", posted.Body, posted.NodeID)
	}
}
