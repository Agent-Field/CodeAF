package session

// The tool layer's half of one refusal: a frame asked for past the end of the
// clip.
//
// It has a file of its own because it is a REGRESSION AND NOT A FEATURE. ffmpeg
// exits 0 when it is seeked past the last frame, having decoded nothing and
// written nothing, so every layer above it agreed the frame had been saved and
// the model was handed a path to an empty png — which it then passes to
// generate_video as the opening frame of the next shot. internal/video refuses
// it now; this proves the refusal travels all the way to the model, which is
// the only place it matters.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAFramePastTheEndOfTheClipIsRefusedToTheModelRatherThanSavedEmpty(t *testing.T) {
	requireFfmpeg(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	clip := madeVideo(t, workspace, "clip.mp4", 2, false)

	result, isError := runTool(t, agent, "edit_video",
		fmt.Sprintf(`{"action":"frame","video":%q,"at":"9"}`, clip))
	if !isError {
		t.Fatalf("a frame nine seconds into a two-second clip must be refused, got %q", result)
	}
	// The sentence has to carry the clip's REAL length, because that is the
	// fact the model needs to ask again correctly; "could not save the frame"
	// on its own gets the same wrong number tried twice.
	if !strings.Contains(result, "clip.mp4") || !strings.Contains(result, "runs 2") {
		t.Errorf("refusal %q does not name the clip and how long it actually runs", result)
	}

	// And no picture landed. The destination's NAME is claimed as an empty file
	// before ffmpeg is started, so that two calls in the same second cannot
	// race for it (tools_media.go), which is why the assertion is about bytes
	// rather than about entries: what must never exist is a file with something
	// in it, because that is the one the answer would have handed on.
	entries, err := os.ReadDir(filepath.Join(workspace, ".aforge-v3", "images"))
	if err != nil {
		return
	}
	for _, entry := range entries {
		if written, err := entry.Info(); err == nil && written.Size() > 0 {
			t.Errorf("the refusal wrote %s anyway, %d bytes of it", entry.Name(), written.Size())
		}
	}
}

func TestTheClosingFrameOfTheSameClipIsStillSaved(t *testing.T) {
	// The refusal above is on a moment measured from the START. [video.Closing]
	// is a seek from the end, cannot be past one, and is the default — so a
	// refusal that caught it would take the frame-chaining trick, which is the
	// whole reason this action exists, off the belt.
	requireFfmpeg(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	clip := madeVideo(t, workspace, "clip.mp4", 2, false)

	result, isError := runTool(t, agent, "edit_video",
		fmt.Sprintf(`{"action":"frame","video":%q}`, clip))
	if isError {
		t.Fatalf("the closing frame must still be saved: %s", result)
	}
	if !strings.Contains(result, "the closing frame of clip.mp4") {
		t.Errorf("the answer %q does not say which frame of which clip it is", result)
	}
	saved := filepath.Join(workspace, ".aforge-v3", "images", onlyFileIn(t, filepath.Join(workspace, ".aforge-v3", "images")))
	if info, err := os.Stat(saved); err != nil || info.Size() == 0 {
		t.Errorf("the saved frame is empty, which is the thing that must never be reported as saved: %v", err)
	}
}
