package session

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── (1) the tool exists only when something is behind it ────────────────────

func TestGenerateVideoIsOnTheBeltOnlyWithAClientAndAVideoModel(t *testing.T) {
	media := &scriptedMedia{video: []byte("MP4")}
	for _, testCase := range []struct {
		name   string
		mutate func(*Config)
		want   bool
	}{
		{"nothing wired", func(*Config) {}, false},
		{"a resolver with no client", func(config *Config) {
			config.MediaModel = mediaModels(map[string]string{modalityVideo: "film/model"})
		}, false},
		{"a client with no resolver", func(config *Config) { config.Media = media }, false},
		{"a client whose resolver only paints", func(config *Config) {
			config.Media = media
			config.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
		}, false},
		{"both", func(config *Config) {
			config.Media = media
			config.MediaModel = mediaModels(map[string]string{modalityVideo: "film/model"})
		}, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, testCase.mutate)
			if got := hasTool(agent, "generate_video"); got != testCase.want {
				t.Fatalf("generate_video on the belt = %v, want %v", got, testCase.want)
			}
		})
	}
}

// ── (2) THE ASYNC LAW ───────────────────────────────────────────────────────

// The whole point of the verb: the call comes back with a job id while the
// render is still running, and the finished file arrives later as a note.
//
// The render is HELD open for the first half of this test, so "it returned
// before the render finished" is a fact rather than a race the test usually
// wins.
func TestGenerateVideoReturnsAJobBeforeTheRenderFinishes(t *testing.T) {
	film := []byte("MP4 and then some bytes standing in for a render")
	hold := make(chan struct{})
	media := &scriptedMedia{video: film, hold: hold}
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	agent, workspace := newMediaAgent(t, media, func(config *Config) {
		config.ArtifactsIndex = index
	})

	result, isError := runTool(t, agent, "generate_video", `{"prompt":"A ferry crossing at dawn"}`)
	if isError {
		t.Fatalf("generate_video failed: %s", result)
	}
	// The tool answered while the provider is still inside the call.
	if !strings.Contains(result, "job 1 started") {
		t.Fatalf("result %q does not hand back a job id", result)
	}
	if !strings.Contains(result, "film/model") {
		t.Fatalf("result %q does not say what is filming", result)
	}
	if strings.Contains(result, ".mp4") {
		t.Fatalf("result %q names a file that does not exist yet", result)
	}
	waitFor(t, "the render to start", func() bool { return media.films() == 1 })
	if notesContain(agent, "job 1") {
		t.Fatal("a note landed before the render finished")
	}
	// While it runs it is a job like any other: listable, and running.
	listed, _ := runTool(t, agent, "jobs", `{"action":"list"}`)
	if !strings.Contains(listed, "job 1 · video · running") {
		t.Fatalf("jobs list = %q, want a running video row", listed)
	}

	close(hold)

	// The ending is the note, and the note names the file that landed.
	waitFor(t, "the completion note", func() bool { return notesContain(agent, "job 1 finished") })
	directory := filepath.Join(workspace, ".aforge-v3", "video")
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("%s holds %v (%v), want one file", directory, entries, err)
	}
	name := entries[0].Name()
	if !strings.HasSuffix(name, "-a-ferry-crossing-at-dawn.mp4") {
		t.Fatalf("generated name %q lost the timestamp-slug shape", name)
	}
	written, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil || !bytes.Equal(written, film) {
		t.Fatalf("the saved file is not the video the provider sent (%v)", err)
	}
	if !notesContain(agent, ".aforge-v3/video/"+name) {
		t.Fatalf("the note does not name the landed file; notes = %v", sessionNotes(agent))
	}
	if !notesContain(agent, "film/model") {
		t.Fatalf("the note does not say what filmed it; notes = %v", sessionNotes(agent))
	}

	// And the row: a deliverable nobody can find again is not a deliverable.
	rows := ReadArtifacts(index)
	if len(rows) != 1 || rows[0].Kind != "video" {
		t.Fatalf("artifact rows = %+v, want one video row", rows)
	}
	if rows[0].Title != "a ferry crossing at dawn" {
		t.Fatalf("row title = %q, want the prompt's own words", rows[0].Title)
	}

	// A finished render reads as finished, not as an exit code nobody set.
	waitFor(t, "the job to settle", func() bool {
		listed, _ := runTool(t, agent, "jobs", `{"action":"list"}`)
		return strings.Contains(listed, "finished")
	})
}

// The render is paid for on the SESSION's pocket, minutes after the turn that
// asked for it has ended — which is exactly why no turn may be charged for it.
func TestGenerateVideoUsageFoldsIntoTheSessionTotal(t *testing.T) {
	cost := 0.75
	media := &scriptedMedia{
		video:     []byte("MP4"),
		videoCost: &ai.Usage{PromptTokens: 30, Cost: &cost},
	}
	agent, _ := newMediaAgent(t, media, nil)

	if result, isError := runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`); isError {
		t.Fatalf("generate_video failed: %s", result)
	}
	waitFor(t, "the render to be accounted", func() bool { return agent.Usage().CostUSD == cost })
	// The tokens are folded too. The count is a floor rather than an equality
	// because the completion note WAKES the session (agent.go), and the turn it
	// starts to read that note is a real turn with a real bill — which is the
	// design: the render's own money is what must not be charged to a turn, and
	// $0.75 landing on the session total is that fact.
	if usage := agent.Usage(); usage.Input < 30 {
		t.Fatalf("session usage = %+v, want at least the render's 30 tokens", usage)
	}
}

// ── (3) frames and references ───────────────────────────────────────────────

// The first frame, the last frame, and the style references: three arguments,
// two shapes on the wire, and the difference between them is the frame_type the
// provider reads.
func TestGenerateVideoCarriesFramesAndReferences(t *testing.T) {
	media := &scriptedMedia{video: []byte("MP4")}
	agent, workspace := newMediaAgent(t, media, nil)

	opening := writeReference(t, workspace, "art/open.png", pngOfSize(t, 4, 4))
	closing := writeReference(t, workspace, "art/close.png", pngOfSize(t, 5, 5))
	style := writeReference(t, workspace, "art/style.png", pngOfSize(t, 6, 6))

	result, isError := runTool(t, agent, "generate_video", fmt.Sprintf(
		`{"prompt":"a ferry","duration":6,"aspect_ratio":"16:9","frame_paths":["%s","%s"],"reference_paths":["%s"]}`,
		opening, closing, style))
	if isError {
		t.Fatalf("generate_video failed: %s", result)
	}
	waitFor(t, "the render to start", func() bool { return media.films() == 1 })

	request := media.film(0)
	if request.Model != "film/model" || request.Prompt != "a ferry" ||
		request.Duration != 6 || request.AspectRatio != "16:9" {
		t.Fatalf("video request = %+v", request)
	}
	if len(request.FrameImages) != 2 {
		t.Fatalf("the request carried %d frames, want 2", len(request.FrameImages))
	}
	if request.FrameImages[0].FrameType != "first_frame" || request.FrameImages[1].FrameType != "last_frame" {
		t.Fatalf("frame types = %q / %q", request.FrameImages[0].FrameType, request.FrameImages[1].FrameType)
	}
	if !bytes.Equal(dataURLBytes(t, request.FrameImages[0].ImageURL.URL), pngOfSize(t, 4, 4)) {
		t.Fatal("the opening frame did not carry its file's bytes")
	}
	if len(request.InputReferences) != 1 || request.InputReferences[0].FrameType != "" {
		t.Fatalf("style references = %+v, want one with no frame type", request.InputReferences)
	}
	if !bytes.Equal(dataURLBytes(t, request.InputReferences[0].ImageURL.URL), pngOfSize(t, 6, 6)) {
		t.Fatal("the style reference did not carry its file's bytes")
	}
}

// Everything the model can get wrong about its own arguments is a tool error
// answered in the same beat — and NOTHING is submitted, because a typo
// discovered inside a ten-minute render is a note that arrives minutes late to
// say nothing happened.
func TestGenerateVideoArgumentFaultsAreToolErrorsAndCostNothing(t *testing.T) {
	media := &scriptedMedia{video: []byte("MP4")}
	agent, workspace := newMediaAgent(t, media, nil)
	writeReference(t, workspace, "art/open.png", pngOfSize(t, 4, 4))

	for _, testCase := range []struct {
		name string
		args string
		want string
	}{
		{"no prompt", `{"prompt":"   "}`, "prompt is required"},
		{"arguments that do not parse", `{"prompt":`, "Invalid arguments"},
		{"a negative duration", `{"prompt":"a ferry","duration":-3}`, "cannot be negative"},
		{"three frames", `{"prompt":"a ferry","frame_paths":["art/open.png","art/open.png","art/open.png"]}`, "at most 2"},
		{"a frame that is not there", `{"prompt":"a ferry","frame_paths":["art/missing.png"]}`, "could not read art/missing.png"},
		{"a reference that is not a picture", `{"prompt":"a ferry","reference_paths":["go.mod"]}`, "not a picture"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result, isError := runTool(t, agent, "generate_video", testCase.args)
			if !isError {
				t.Fatalf("a bad call reported success: %s", result)
			}
			if !strings.Contains(result, testCase.want) {
				t.Fatalf("result %q does not say %q", result, testCase.want)
			}
		})
	}
	if media.films() != 0 {
		t.Fatalf("a refused call still started %d renders", media.films())
	}
}

// ── (4) a render that fails, and one that is stopped ────────────────────────

// A failure reaches the model where the success would have: on the steering
// lane, in one sentence naming the model and the cause. The turn that submitted
// it is long over and cannot be failed.
func TestGenerateVideoFailureArrivesAsANote(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		media *scriptedMedia
		want  string
	}{
		{"the provider refused", &scriptedMedia{videoErr: errors.New("the model is overloaded")}, "video generation failed (film/model): the model is overloaded"},
		{"it timed out", &scriptedMedia{videoErr: provider.ErrVideoTimeout}, "video generation timed out (film/model)"},
		{"no video came back", &scriptedMedia{}, "returned no video"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			agent, workspace := newMediaAgent(t, testCase.media, nil)
			if result, isError := runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`); isError {
				t.Fatalf("generate_video refused to submit: %s", result)
			}
			waitFor(t, "the failure note", func() bool { return notesContain(agent, "job 1 failed") })
			if !notesContain(agent, testCase.want) {
				t.Fatalf("notes %v do not say %q", sessionNotes(agent), testCase.want)
			}
			if _, err := os.Stat(filepath.Join(workspace, ".aforge-v3", "video")); err == nil {
				t.Fatal("a failed render left a directory behind")
			}
		})
	}
}

// A render this session KILLED says nothing on the way out, exactly as a killed
// background job does: the caller who asked for it already knows.
func TestGenerateVideoKilledSaysNothing(t *testing.T) {
	hold := make(chan struct{})
	defer close(hold)
	media := &scriptedMedia{video: []byte("MP4"), hold: hold}
	agent, _ := newMediaAgent(t, media, nil)

	if result, isError := runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`); isError {
		t.Fatalf("generate_video failed: %s", result)
	}
	waitFor(t, "the render to start", func() bool { return media.films() == 1 })

	killed, isError := runTool(t, agent, "jobs", `{"action":"kill","id":1}`)
	if isError {
		t.Fatalf("jobs kill failed: %s", killed)
	}
	if !strings.Contains(killed, "no video was saved") {
		t.Fatalf("kill said %q, want it to say nothing was saved", killed)
	}
	for _, note := range sessionNotes(agent) {
		if strings.Contains(note, "job 1") {
			t.Fatalf("a killed render reported its own death: %q", note)
		}
	}
}
