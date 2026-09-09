package session

// EDIT_VIDEO IS A WRITE, AND EVERYTHING THAT WATCHES WRITES HAS TO KNOW IT.
//
// The verb arrived on the belt as a media hand and was read as one: [savingTools]
// knew it saved something, and nothing that resolves a PATH knew anything about
// it at all. So the citizens that stand between a model and a file it must not
// touch — the write scope (orchestrate.go), the tree claim (treehold.go) and the
// task's ground (taskoutside.go) — never saw an edit_video call, and neither did
// the ledger a revert is offered from. A node scoped to `assets/` could join two
// clips straight over `src/release.mp4`, and the harness would have watched it
// happen and then had nothing to put back.
//
// These hold the seam shut at both ends: the guard refuses the cut it should
// refuse, and the ledger can undo the cut that was allowed.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// editVideoCall is one edit_video call as the model sends it: the action, and
// the destination when it names one.
func editVideoCall(id, action, path string) ai.ToolCall {
	arguments := `{"action":"` + action + `"`
	if path != "" {
		arguments += `,"path":"` + path + `"`
	}
	return ai.ToolCall{ID: id, Type: "function", Function: ai.ToolCallFunction{
		Name: "edit_video", Arguments: arguments + `}`}}
}

// editVideoJoinCall is a join with its clips, which is the shape that actually
// writes something when it runs.
func editVideoJoinCall(id, path string, clips ...string) ai.ToolCall {
	return ai.ToolCall{ID: id, Type: "function", Function: ai.ToolCallFunction{
		Name:      "edit_video",
		Arguments: `{"action":"join","clips":["` + strings.Join(clips, `","`) + `"],"path":"` + path + `"}`}}
}

// ── the scope ───────────────────────────────────────────────────────────────

// A NODE CUTS INSIDE ITS SCOPE AND NOWHERE ELSE, and the action decides whether
// the question is even asked: three of the four write the file they name and
// `measure` writes nothing at all.
func TestTheWriteScopeBindsEditVideosDestination(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.writeScope = []string{"assets"}
	})
	guard := writeGuard{agent: agent}

	for _, c := range []struct {
		name    string
		call    ai.ToolCall
		allowed bool
	}{
		{"a cut inside the scope", editVideoCall("c1", editVideoJoin, "assets/film.mp4"), true},
		{"a cut over a file outside it", editVideoCall("c2", editVideoJoin, "src/release.mp4"), false},
		{"a frame outside it", editVideoCall("c3", editVideoFrame, "src/poster.png"), false},
		{"a score outside it", editVideoCall("c4", editVideoScore, "src/release.mp4"), false},
		// The reading is not a write and must not be refused as one — a node
		// planning a cut measures every clip it is about to lay end to end, and
		// those clips are usually somebody else's corner of the tree.
		{"a measure of a file outside it", editVideoCall("c5", editVideoMeasure, "src/release.mp4"), true},
		// AND THE UNNAMED DESTINATION IS OUT OF REACH, exactly as generate_image's
		// is: it lands under a timestamped name in the session's own video folder,
		// which is not a name anything can read out of these arguments
		// ([mutatedPath] says so in its own words).
		{"a cut that names no destination", editVideoCall("c6", editVideoJoin, ""), true},
	} {
		_, result, ok := guard.PreAction(context.Background(), nil, nil, c.call)
		if ok != c.allowed {
			t.Errorf("%s: allowed=%v, want %v", c.name, ok, c.allowed)
		}
		if !ok && (!result.isError || !strings.Contains(result.text, "write scope")) {
			t.Errorf("%s: the refusal reads %q", c.name, result.text)
		}
	}
}

// THE MEASURED SHAPE, THROUGH THE DOOR EVERY CALL PASSES THROUGH. A scoped node
// aims a join at a real video it does not own; the pre-action chain refuses
// before ffmpeg is started, and the file it was aimed at is byte for byte what
// it was.
func TestAScopedNodeCannotCutOverAVideoOutsideItsScope(t *testing.T) {
	requireFfmpeg(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.writeScope = []string{"assets"}
	})
	if err := os.MkdirAll(filepath.Join(workspace, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	release := madeVideo(t, filepath.Join(workspace, "src"), "release.mp4", 1, true)
	before := revertRead(t, release)

	_, refused, allowed := agent.newEpisode().preAction(context.Background(), nil,
		editVideoJoinCall("c1", "src/release.mp4", "src/release.mp4", "src/release.mp4"))
	if allowed {
		t.Fatal("a node scoped to assets/ was allowed to cut over src/release.mp4")
	}
	if !refused.isError || !strings.Contains(refused.text, "write scope") {
		t.Fatalf("the refusal reads %q", refused.text)
	}
	if after := revertRead(t, release); after != before {
		t.Fatalf("src/release.mp4 was rewritten anyway: %d bytes, was %d", len(after), len(before))
	}
}

// ── the ledger ──────────────────────────────────────────────────────────────

// AND WHAT A CUT LEAVES BEHIND CAN BE PUT BACK, which needs nothing said about
// video anywhere in the revert: a file the turn created is deleted and a tracked
// file it overwrote is checked out of the index, and neither of those cares
// whether the bytes are prose or an mp4.
func TestARevertUndoesEditVideosCutsAsItUndoesAWrite(t *testing.T) {
	requireFfmpeg(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	revertRepo(t, workspace)
	madeVideo(t, workspace, "clip.mp4", 1, true)
	madeVideo(t, workspace, "cut.mp4", 2, true)
	revertCommit(t, workspace, "the footage")
	committed := revertRead(t, filepath.Join(workspace, "cut.mp4"))

	episode := agent.newEpisode()
	// The tracked cut is overwritten and a second one is made from nothing, which
	// are the two halves the ledger has to tell apart and the only moment anybody
	// can: pre-action is where the file either exists or does not.
	for _, call := range []ai.ToolCall{
		editVideoJoinCall("c1", "cut.mp4", "clip.mp4", "clip.mp4"),
		editVideoJoinCall("c2", "trailer.mp4", "clip.mp4", "clip.mp4"),
	} {
		cutThrough(t, agent, episode, call)
	}
	if overwritten := revertRead(t, filepath.Join(workspace, "cut.mp4")); overwritten == committed {
		t.Fatal("the join did not actually rewrite cut.mp4, so this test proves nothing")
	}

	changes := episode.changes.list()
	if len(changes) != 2 {
		t.Fatalf("the ledger holds %d changes, want 2 (%+v)", len(changes), changes)
	}
	if changes[0].shown != "cut.mp4" || changes[0].created {
		t.Errorf("cut.mp4 recorded as %+v, want a modification", changes[0])
	}
	if changes[1].shown != "trailer.mp4" || !changes[1].created {
		t.Errorf("trailer.mp4 recorded as %+v, want a creation", changes[1])
	}

	outcome := agent.revert(episode.offerFor())
	if len(outcome.manual) > 0 || len(outcome.failed) > 0 {
		t.Fatalf("the revert could not finish: %+v", outcome)
	}
	if restored := revertRead(t, filepath.Join(workspace, "cut.mp4")); restored != committed {
		t.Errorf("cut.mp4 came back as %d bytes, want the committed %d", len(restored), len(committed))
	}
	if _, err := os.Stat(filepath.Join(workspace, "trailer.mp4")); !os.IsNotExist(err) {
		t.Errorf("the cut this turn made is still there: %v", err)
	}
}

// cutThrough runs one edit_video call the way a turn runs it — the pre-action
// chain, the tool itself, then the post-feedback chain — because the ledger's
// one fact is measured between the first two and recorded by the third.
func cutThrough(t *testing.T, agent *Agent, ep *episode, call ai.ToolCall) {
	t.Helper()
	ctx := context.Background()
	if _, refused, allowed := ep.preAction(ctx, nil, call); !allowed {
		t.Fatalf("%s was refused: %s", call.Function.Arguments, refused.text)
	}
	text, isError := runTool(t, agent, "edit_video", call.Function.Arguments)
	if isError {
		t.Fatalf("%s: %s", call.Function.Arguments, text)
	}
	ep.postFeedback(ctx, nil, []ai.ToolCall{call}, []toolResult{{text: text}})
}
