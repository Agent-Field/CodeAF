package session

// generate_video: a verb that ANSWERS BEFORE IT IS FINISHED — the belt's first;
// generate_music (tools_music.go) now keeps the same law.
//
// /videos is submit-then-poll on the wire, and a render is minutes rather than
// seconds — ten of them, at the provider's own ceiling. Every other tool on this
// belt returns what it did; this one cannot, because a turn that waited would be
// a conversation the person cannot use until a file they cannot see yet exists.
// So THE ASYNC LAW (docs/MULTIMODAL.md, Decision 7): the tool submits and comes
// back in seconds with a job id, the poll runs where jobs run, and the finished
// video reaches the model the way every other job's ending does — a note on the
// steering lane, drained into the transcript at the next step boundary.
//
// It is `bash background:true` with a provider on the other end instead of a
// process (tools_jobs.go, jobs.go), and deliberately the same in every visible
// way: the same id space, the same log file, the same row in `jobs list`, the
// same `jobs kill`, and the same death at Close. A model that has learned what a
// background job is has learned what this is.
//
// The landing happens INSIDE the job, not in the tool call: the bytes arrive
// minutes after the call returned, so the destination, the artifact row and the
// auxiliary spend are all the goroutine's work, and the note names the path.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// videoDirectory is where a render lands when the model does not say and the
// session has no folder of its own — [imageDirectory]'s sibling, and [VideoDir]
// holds the whole rule.
const videoDirectory = ".aforge-v3/video"

// videoExtension is what the provider sends and what the file is called. The
// endpoint has no format argument, so there is nothing to keep in step with.
const videoExtension = ".mp4"

// videoFrameLimit is how many frame_paths mean anything: the FIRST frame and the
// LAST one. A third has no slot on the wire (provider.VideoRequest's
// FrameImages carries frame_type first_frame / last_frame and nothing else), so
// a third is refused rather than silently dropped — a model told its third frame
// was used would plan the rest of the shot around it.
const videoFrameLimit = 2

// videoLabel is what the job answers to in `jobs list` and in its note. It is a
// constant word rather than a slug of the prompt because the prompt is already
// the row's detail, and a label is what a person says out loud ("kill the
// video").
const videoLabel = "video"

// The description carries the async law in the model's own terms, because a
// model that does not know the call returns before the file exists will read the
// job line as a failure and try again — twice the money, twice the wait.
const generateVideoDescription = "Generate a video from a text prompt. This tool RETURNS IMMEDIATELY with a background job id, because a render takes minutes: the work keeps going while you and the user carry on talking, and when it lands you are told in a note naming the file — you do not wait for it, poll it, or call it twice. Give frame_paths to pin the motion (the first image is the opening frame, a second is the closing one) and reference_paths for pictures that set the style. EVERY CALL IS AN INDEPENDENT RENDER: the video model sees only this prompt and these images — no earlier clip, none of this conversation — so anything that must hold across clips (a face, a costume, a setting) must be described or passed as a picture on every call, and one clip continues from another only when the earlier clip's final frame is handed in as the next call's opening frame, which makes connected clips a sequence rather than a parallel batch. The landing note reports the clip's measured length and whether it carries sound, whenever the file can be measured. Watch it with the jobs tool and stop it with jobs kill; a stopped render saves nothing."

const generateVideoSchemaJSON = `{"type":"object","properties":{` +
	`"prompt":{"type":"string","description":"What happens in the shot: subject, action, camera movement, style, lighting. The whole prompt reaches the video model, so detail is worth writing — every dimension the prompt leaves open, the model fills with its statistical average, and that average is what generic AI footage looks like. Decide the medium, the light, the lens and the mood yourself and write them down; specificity is the difference between the shot you meant and a generic one. A prompt assembled from the genre's usual clichés lands on that same average by choice — escape a genre by naming a real filmmaking tradition (a film stock, a documentary setup, an animation technique), not by recoloring it. And specify positively: video models barely read negation, so describe what IS in the frame until the default has no room."},` +
	`"duration":{"type":"integer","description":"How many seconds long, if the model takes a length. Leave it out for the model's own default."},` +
	`"aspect_ratio":{"type":"string","description":"Shape of the frame, as the video model spells it (for example 16:9 or 9:16). Passed through untouched; leave it out for the model's own default."},` +
	`"resolution":{"type":"string","description":"How sharp the render is, as the video model spells it (for example 720p or 1080p). Passed through untouched; leave it out for the model's own default, and name one when sharpness is part of the brief — a higher resolution is a slower, costlier render."},` +
	`"seed":{"type":"integer","description":"Fixed number that makes the render's randomness repeatable, if the video model takes one. The same seed with the same prompt and pictures re-renders close to the same shot, so hold it steady to change one thing about a shot you mostly liked. Passed through untouched; leave it out for a fresh roll."},` +
	`"frame_paths":{"type":"array","items":{"type":"string"},"description":"One or two pictures in the workspace (png, jpeg, webp or gif) that the shot must start and end on: the first is the opening frame, a second is the closing one. More than two is refused. An image generate_image just made is a valid frame, and a frame saved out of an earlier clip is how one shot carries into the next."},` +
	`"reference_paths":{"type":"array","items":{"type":"string"},"description":"Pictures whose look the render should follow — style, palette, a character's face. They set the appearance, not the motion; use frame_paths for that. The same references handed to every clip keep a character steady across renders that otherwise share nothing."},` +
	`"path":{"type":"string","description":"Where to save it, relative to the workspace. Leave it out for a timestamped name derived from the prompt, saved where this session keeps its video. An existing file at this path is overwritten, as with the write tool."}` +
	`},"required":["prompt"],"additionalProperties":false}`

// videoTools is the filming verb, CONDITIONAL by the belt's law: a nil media
// client or a resolver with no video model for this machine leaves the tool off
// rather than on and refusing ([Agent.mediaHand]).
func (a *Agent) videoTools() []bare.Tool {
	client, model, ready := a.mediaHand(modalityVideo)
	if !ready {
		return nil
	}
	return []bare.Tool{a.generateVideoTool(client, model)}
}

// generateVideoArguments is the wire form. Duration is a plain int because zero
// and absent mean the same thing here — the model's own default length — and
// provider.VideoRequest omits a zero on the way out. Seed is a pointer because
// zero is a seed a model may legitimately ask for, and collapsing it into
// "absent" would quietly re-roll a shot the model was trying to hold still.
type generateVideoArguments struct {
	Prompt         string   `json:"prompt"`
	Duration       int      `json:"duration"`
	AspectRatio    string   `json:"aspect_ratio"`
	Resolution     string   `json:"resolution"`
	Seed           *int     `json:"seed"`
	FramePaths     []string `json:"frame_paths"`
	ReferencePaths []string `json:"reference_paths"`
	Path           string   `json:"path"`
	Model          string   `json:"model"`
}

func (a *Agent) generateVideoTool(client MediaGenerator, defaultModel string) bare.Tool {
	return bare.Tool{
		Name:        "generate_video",
		Description: generateVideoDescription,
		Schema:      a.mediaSchema(generateVideoSchemaJSON, "video"),
		Execute: func(_ context.Context, args json.RawMessage) (string, bool, error) {
			var parsed generateVideoArguments
			if err := decodeToolArguments(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			prompt := strings.TrimSpace(parsed.Prompt)
			if prompt == "" {
				return "Invalid arguments: prompt is required", true, nil
			}
			// The call's own choice, resolved before the job starts, so a word
			// that matches nothing costs neither money nor a job row
			// (tools_image.go states the shape).
			model, refusal := a.mediaPick(modalityVideo, parsed.Model, defaultModel)
			if refusal != "" {
				return "Invalid arguments: " + refusal, true, nil
			}
			if parsed.Duration < 0 {
				return "Invalid arguments: duration is a number of seconds and cannot be negative", true, nil
			}
			if len(parsed.FramePaths) > videoFrameLimit {
				return fmt.Sprintf("Invalid arguments: frame_paths takes at most %d — the opening frame and the closing one; pictures that set the look go in reference_paths", videoFrameLimit), true, nil
			}
			// EVERY picture is read here, in the call, and not in the job. A
			// path with a typo in it must cost nothing: discovering it inside
			// the render would mean a note minutes later saying the render
			// never started, which is the worst of both shapes.
			frames, refusal := a.videoFrames(parsed.FramePaths)
			if refusal != "" {
				return "Invalid arguments: frame " + refusal, true, nil
			}
			// A style reference is the envelope WITHOUT a frame type, which is
			// how the wire tells a look from a keyframe — so it is exactly what
			// the shared builder makes, with nothing added.
			styles, refusal := a.mediaReferences(parsed.ReferencePaths)
			if refusal != "" {
				return "Invalid arguments: reference " + refusal, true, nil
			}

			request := provider.VideoRequest{
				Model: model, Prompt: prompt, Duration: parsed.Duration,
				AspectRatio:     strings.TrimSpace(parsed.AspectRatio),
				Resolution:      strings.TrimSpace(parsed.Resolution),
				Seed:            parsed.Seed,
				FrameImages:     frames,
				InputReferences: styles,
			}
			started, ctx, err := a.jobs.startRender(videoLabel, prompt)
			if err != nil {
				return "Could not start the video render: " + err.Error(), true, nil
			}
			fmt.Fprintf(started.sink, "filming on %s: %s\n", model, firstLine(prompt))

			go a.renderVideo(ctx, client, started, request, parsed.Path, model)

			// The id, the model and the log, and nothing else. There is no
			// output yet by construction, and the three things the model needs
			// next — what it is called, what is making it, where to look — are
			// all here. The RESULT is not here and does not belong here: it
			// arrives as a note.
			return fmt.Sprintf("job %d started; filming on %s — the finished video arrives as a note naming the file. Log at %s",
				started.id, model, started.logPath), false, nil
		},
	}
}

// videoFrames turns the frame paths into the wire's first/last frame envelopes:
// the same envelope [Agent.mediaReferences] builds for every other endpoint,
// with the one slot only a video has filled in.
func (a *Agent) videoFrames(paths []string) ([]provider.ImageReference, string) {
	frameTypes := [videoFrameLimit]string{"first_frame", "last_frame"}
	frames, refusal := a.mediaReferences(paths)
	if refusal != "" {
		return nil, refusal
	}
	for index := range frames {
		frames[index].FrameType = frameTypes[index]
	}
	return frames, ""
}

// renderVideo is the job's whole middle: wait for the provider, land the bytes,
// record the row, and say one sentence about how it went.
//
// It runs on the registry's background context, so it survives the turn that
// started it and dies to `jobs kill` and to Close like any other job. Nothing
// here returns an error to anybody: the caller is a goroutine, and the only
// audience for a failure is the model, which hears about it in the note.
func (a *Agent) renderVideo(ctx context.Context, client MediaGenerator, rendering *job, request provider.VideoRequest, asked, model string) {
	response, err := client.GenerateVideo(ctx, request)
	if err != nil {
		a.failVideo(rendering, videoFailure(model, err))
		return
	}
	if response == nil || len(response.Video) == 0 {
		a.failVideo(rendering, "video generation returned no video ("+model+")")
		return
	}
	// The render is paid for whether or not it is any good, and on the SESSION's
	// pocket rather than a turn's: no turn asked for a video at this price, and
	// the turn that submitted it probably ended minutes ago (tools_image.go
	// states the law).
	a.addAuxiliaryUsage(&ai.Response{Usage: response.Usage}, model, 1)

	path, err := a.mediaDestination(asked, request.Prompt, videoExtension,
		VideoDir(a.config.Place, a.config.Workspace))
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o755)
	}
	if err == nil {
		err = os.WriteFile(path, response.Video, 0o644)
	}
	if err != nil {
		a.failVideo(rendering, "could not save the generated video: "+err.Error())
		return
	}
	RecordArtifact(a.config.ArtifactsIndex, Artifact{
		Path:    path,
		Session: a.journalID(),
		Title:   mediaTitle(request.Prompt, path),
		Kind:    "video",
		Created: time.Now(),
	})

	landed := describeGeneratedVideo(a.config.Workspace, path, response.Video, model)
	fmt.Fprintln(rendering.sink, landed)
	a.jobs.finish(rendering, 0, fmt.Sprintf("job %d finished: %s", rendering.id, landed))
}

// failVideo ends a render that produced nothing. The exit code is 1 for the
// same reason a failed command's is: `jobs list` renders a video's status as
// "finished" either way, and the code is what a reader of the log has left.
func (a *Agent) failVideo(rendering *job, reason string) {
	fmt.Fprintln(rendering.sink, reason)
	a.jobs.finish(rendering, 1, fmt.Sprintf("job %d failed: %s", rendering.id, reason))
}

// videoFailure is the one sentence a failed render says, with the provider's own
// cause in it. A timeout is named as a timeout because it is the one failure the
// model can act on differently — a shorter clip, a simpler prompt — and a
// cancellation says so plainly rather than as a Go error string.
func videoFailure(model string, err error) string {
	switch {
	case errors.Is(err, provider.ErrVideoTimeout):
		return "video generation timed out (" + model + "); no video was saved"
	case errors.Is(err, context.Canceled):
		return "video generation was stopped (" + model + "); no video was saved"
	default:
		return "video generation failed (" + model + "): " + clip(firstLine(err.Error()), jobExitNoteLimit)
	}
}

// describeGeneratedVideo is what the note and the log both say: where it is,
// how big it is, how long it runs, whether it carries sound, what made it.
//
// The length and the sound answer are MEASURED, read out of the file's own
// boxes (mp4.go) — they are what the model plans its next step from: a stitch
// is timed from its clips' lengths, and whether the join must carry audio is
// known before anything is joined. When the bytes do not parse as an mp4 the
// clause is simply absent rather than guessed (design-law §EMPTINESS), which
// is also why there are no dimensions: nothing here reads them.
func describeGeneratedVideo(workspace, path string, video []byte, model string) string {
	measured := ""
	if length, sound, ok := mp4Facts(video); ok {
		carrying := " with sound"
		if !sound {
			carrying = " without sound"
		}
		measured = ", " + mediaLength(length) + carrying
	}
	return fmt.Sprintf("%s — %s of mp4 video%s, filmed on %s",
		displayMediaPath(workspace, path), mediaByteSize(len(video)), measured, model)
}
