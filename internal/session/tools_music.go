package session

// generate_music is the session's composer: a description of a piece of music
// in, an mp3 on disk, a note naming it when it lands.
//
// It is `speak`'s sibling on the belt (tools_speak.go) and NOT its twin on the
// wire, which is the one thing worth knowing about it. Speech posts to
// /audio/speech; music has no media endpoint on the router at all and is
// composed through streaming chat completions asking for an audio modality back
// (internal/provider/music.go states the three wire rules and how each was
// learned). Two models, two lanes, one family.
//
// SO IT IS A SECOND VERB AND NOT AN ARGUMENT ON speak. A voice name is
// meaningless to a music model and a composition brief is meaningless to a TTS
// model; one verb taking both would be a verb whose arguments contradict each
// other depending on a model slot the model cannot see.
//
// IT ANSWERS BEFORE IT IS FINISHED, by generate_video's law (tools_video.go
// states it whole). A compose is one long streaming call — most of a minute is
// normal — and a turn that sat inside it was a conversation nobody could use
// while a file nobody could hear yet was written. So the call submits, comes
// back in a breath with a job id, and the composing happens in a goroutine
// whose ending is a note naming the file — the same id space, the same log,
// the same row in `jobs list`, the same `jobs kill`, the same death at Close.
// A model that has learned what a video render is has learned what this is,
// and it keeps working — on the clips, on the stitch, on the conversation —
// while the score is written.
//
// THE BYTES NEVER ENTER THE TRANSCRIPT, for tools_speak.go's reason and more so:
// a minute of music is a megabyte and base64 inflates it by a third. The note
// is one line naming the file, and the file is what the person plays.
//
// IT HAS NO LENGTH ARGUMENT because the endpoint has none. A call composes
// whatever the model decides to write — half a minute to a minute in practice,
// and Lyria's clips land nearer thirty seconds — and costs the same whether
// the piece is eight seconds or eighty, so there is no cheap call to offer and
// no knob to pretend there is (design-law §EMPTINESS applied to an argument:
// better absent than accepted and ignored). The one consequence worth the
// model knowing: a score for anything timed — a video, a slideshow — is cut
// from what comes back, looped or trimmed, and the file must be measured
// before anything is laid against it.

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

// musicDirectory is where a composed file lands when the model does not say and
// the session has no folder of its own — [audioDirectory]'s sibling, and
// [MusicDir] holds the whole rule.
//
// It is its OWN leaf and not audio/ for the reason the images and video leaves
// are their own: a person who asked for twenty takes of a theme wants them in
// one place they can listen through, not interleaved with the voiceovers the
// same session recorded.
const musicDirectory = ".aforge-v3/music"

// musicExtension is what the file is called when the provider does not say what
// it sent. It is mp3 because mp3 is what the lane returns; a named format is
// preferred over it whenever one arrives ([musicFileExtension]), so the constant
// is a fallback rather than an assumption the file has to live up to.
const musicExtension = ".mp3"

// musicLabel is what the job answers to in `jobs list` and in its kill line,
// [videoLabel]'s sibling: a constant word rather than a slug of the prompt,
// because the prompt is already the row's detail and a label is what a person
// says out loud ("kill the music").
const musicLabel = "music"

// musicFileExtension is the suffix a composed clip is saved under: what the
// provider said it sent, or [musicExtension] when it said nothing. A file named
// for a format it is not is a file the person's player refuses, so the
// provider's own word wins wherever there is one.
func musicFileExtension(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "":
		return musicExtension
	case "mpeg", "mpga":
		// The two spellings of an mp3 that a player will not open under their
		// own names.
		return ".mp3"
	default:
		return "." + strings.ToLower(strings.TrimSpace(format))
	}
}

// The description carries the async law in the model's own terms, exactly as
// generate_video's does and for its reason: a model that does not know the
// call returns before the file exists reads the job line as a failure and pays
// twice. It also says the two things a model cannot guess — that the prompt is
// a DESCRIPTION OF MUSIC and not lyrics to be sung or words to be read out,
// and that the piece's length is the model's own choice.
const generateMusicDescription = "Compose music from a description and save it as an audio file. This tool RETURNS IMMEDIATELY with a background job id, because a compose takes most of a minute: the work keeps going while you and the user carry on talking, and when it lands you are told in a note naming the file — you do not wait for it, poll it, or call it twice. The prompt describes the MUSIC: genre, instruments, tempo, mood, structure — it is not lyrics to sing and not text to read out. For a voiceover or for text spoken aloud use speak instead, which is a different model. There is no length argument: the model writes a piece of its own choosing — half a minute to a minute in practice — and the call costs the same however long it turns out, so ask for one piece and iterate on the description rather than calling this repeatedly for a shorter one. To lay the piece under anything timed, measure the file first and loop or trim it: its length is the model's choice, not yours. Give a path to choose the name and the folder; leave it out and the file is saved under a timestamped name derived from the description. Watch it with the jobs tool and stop it with jobs kill; a stopped compose saves nothing."

const generateMusicSchemaJSON = `{"type":"object","properties":{` +
	`"prompt":{"type":"string","description":"The music to compose, described the way a brief would describe it: genre, instruments, tempo, key or mood, and how it should develop. The whole prompt reaches the music model, so detail is worth writing. It is a description of a piece, not lyrics and not words to be spoken."},` +
	`"path":{"type":"string","description":"Where to save it, relative to the workspace. Leave it out for a timestamped name derived from the description, saved where this session keeps its music. An existing file at this path is overwritten, as with the write tool."}` +
	`},"required":["prompt"],"additionalProperties":false}`

// musicTools is the composing verb, CONDITIONAL by the belt's law: a nil media
// client or a resolver with no music model for this machine leaves the tool off
// rather than on and refusing ([Agent.mediaHand]).
//
// It is asked for "music" and never for "speech", which is what keeps the two
// verbs independent: a machine whose catalog advertises a TTS model and no music
// model gets speak and not this, and the model is never handed a verb that would
// send a composition brief to something that can only pronounce it.
func (a *Agent) musicTools() []bare.Tool {
	client, model, ready := a.mediaHand(modalityMusic)
	if !ready {
		return nil
	}
	return []bare.Tool{a.generateMusicTool(client, model)}
}

type generateMusicArguments struct {
	Prompt string `json:"prompt"`
	Path   string `json:"path"`
	Model  string `json:"model"`
}

func (a *Agent) generateMusicTool(client MediaGenerator, defaultModel string) bare.Tool {
	return bare.Tool{
		Name:        "generate_music",
		Description: generateMusicDescription,
		Schema:      a.mediaSchema(generateMusicSchemaJSON, "music"),
		Execute: func(_ context.Context, args json.RawMessage) (string, bool, error) {
			var parsed generateMusicArguments
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			prompt := strings.TrimSpace(parsed.Prompt)
			if prompt == "" {
				return "Invalid arguments: prompt is required", true, nil
			}
			// The call's own choice, resolved before the job starts, so a word
			// that matches nothing costs neither money nor a job row
			// (tools_image.go states the shape).
			model, refusal := a.mediaPick(modalityMusic, parsed.Model, defaultModel)
			if refusal != "" {
				return "Invalid arguments: " + refusal, true, nil
			}

			started, ctx, err := a.jobs.startRender(musicLabel, prompt)
			if err != nil {
				return "Could not start the compose: " + err.Error(), true, nil
			}
			fmt.Fprintf(started.sink, "composing on %s: %s\n", model, firstLine(prompt))

			go a.composeMusic(ctx, client, started, provider.MusicRequest{
				Model: model, Prompt: prompt,
			}, parsed.Path, model)

			// The id, the model and the log, and nothing else — the RESULT
			// arrives as a note, by tools_video.go's law exactly.
			return fmt.Sprintf("job %d started; composing on %s — the finished piece arrives as a note naming the file. Log at %s",
				started.id, model, started.logPath), false, nil
		},
	}
}

// composeMusic is the job's whole middle, [Agent.renderVideo]'s twin: wait for
// the provider, land the bytes, record the row, and say one sentence about how
// it went. It runs on the registry's background context, so it survives the
// turn that started it and dies to `jobs kill` and to Close like any other
// job; the only audience for a failure is the model, which hears it in the
// note.
func (a *Agent) composeMusic(ctx context.Context, client MediaGenerator, composing *job, request provider.MusicRequest, asked, model string) {
	response, err := client.GenerateMusic(ctx, request)
	if err != nil {
		a.failMusic(composing, musicFailure(model, err))
		return
	}
	if response == nil || len(response.Audio) == 0 {
		a.failMusic(composing, "music generation returned no audio ("+model+")")
		return
	}
	// Paid for before it is saved, on the SESSION's pocket and no turn's — the
	// turn that submitted it has usually ended by now (tools_image.go states
	// the law).
	a.addAuxiliaryUsage(&ai.Response{Usage: response.Usage}, model, 1)

	path, err := a.mediaDestination(asked, request.Prompt, musicFileExtension(response.Format),
		MusicDir(a.config.Place, a.config.Workspace))
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o755)
	}
	if err == nil {
		err = os.WriteFile(path, response.Audio, 0o644)
	}
	if err != nil {
		a.failMusic(composing, "could not save the generated music: "+err.Error())
		return
	}
	RecordArtifact(a.config.ArtifactsIndex, Artifact{
		Path:    path,
		Session: a.journalID(),
		Title:   mediaTitle(request.Prompt, path),
		Kind:    "audio",
		Created: time.Now(),
	})

	landed := describeGeneratedMusic(a.config.Workspace, path, len(response.Audio), model)
	fmt.Fprintln(composing.sink, landed)
	a.jobs.finish(composing, 0, fmt.Sprintf("job %d finished: %s", composing.id, landed))
}

// failMusic ends a compose that produced nothing, [Agent.failVideo]'s twin:
// exit code 1, the reason in the log, the same sentence as the note.
func (a *Agent) failMusic(composing *job, reason string) {
	fmt.Fprintln(composing.sink, reason)
	a.jobs.finish(composing, 1, fmt.Sprintf("job %d failed: %s", composing.id, reason))
}

// musicFailure is the one sentence a failed compose says, with the provider's
// own cause in it. A cancellation and a timeout are named plainly rather than
// as Go error strings, because "context canceled" and "Client.Timeout exceeded
// while awaiting headers" are sentences about plumbing, and "was stopped" and
// "timed out" are sentences about what happened. The timeout is the media
// client's own request bound (provider.Config.Timeout) — there is no music
// poll and so no separate deadline of the video kind.
func musicFailure(model string, err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "music generation was stopped (" + model + "); no music was saved"
	case errors.Is(err, context.DeadlineExceeded):
		return "music generation timed out (" + model + "); no music was saved"
	default:
		return "music generation failed (" + model + "): " + clip(firstLine(err.Error()), jobExitNoteLimit)
	}
}

// describeGeneratedMusic is what the note and the log both say: where it is,
// how big it is, who composed it. There is no duration, for
// [describeGeneratedAudio]'s reason — nothing here decodes an mp3, whose
// framing carries no stated length the way an mp4's movie header does, and a
// guessed length is worse than none (design-law §EMPTINESS). The format is
// read off the NAME the file was actually saved under rather than restated, so
// the sentence cannot describe a file that is not there.
func describeGeneratedMusic(workspace, path string, size int, model string) string {
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	return fmt.Sprintf("%s — %s of %s audio, composed by %s",
		displayMediaPath(workspace, path), mediaByteSize(size), format, model)
}
