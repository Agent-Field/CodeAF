package session

// generate_music is the session's composer: a description of a piece of music
// in, an mp3 on disk, a path back.
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
// THE BYTES NEVER ENTER THE TRANSCRIPT, for tools_speak.go's reason and more so:
// a minute of music is a megabyte and base64 inflates it by a third. The result
// is one line naming the file, and the file is what the person plays.
//
// IT HAS NO LENGTH ARGUMENT because the endpoint has none. A call composes
// whatever the model decides to write — around a minute in practice — and costs
// the same whether the piece is eight seconds or eighty, so there is no cheap
// call to offer and no knob to pretend there is (design-law §EMPTINESS applied
// to an argument: better absent than accepted and ignored).

import (
	"context"
	"encoding/json"
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

// The description says the two things a model cannot guess: that the prompt is a
// DESCRIPTION OF MUSIC and not lyrics to be sung or words to be read out, and
// that what comes back is a path rather than audio it can listen to. The pointer
// to speak is there because a model reaching for this to make a voiceover would
// get a music model trying to sing an announcement.
const generateMusicDescription = "Compose music from a description and save it as an audio file. Returns the path it was written to and how big the file is — never the audio itself, which stays on disk for the user to play. The prompt describes the MUSIC: genre, instruments, tempo, mood, structure — it is not lyrics to sing and not text to read out. For a voiceover or for text spoken aloud use speak instead, which is a different model. There is no length argument: the model writes a piece of its own choosing, around a minute, and the call costs the same however long it turns out — so ask for one piece and iterate on the description rather than calling this repeatedly for a shorter one. Give a path to choose the name and the folder; leave it out and the file is saved under a timestamped name derived from the description."

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
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed generateMusicArguments
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			prompt := strings.TrimSpace(parsed.Prompt)
			if prompt == "" {
				return "Invalid arguments: prompt is required", true, nil
			}
			// The call's own choice, resolved before anything is paid for
			// (tools_image.go states the shape).
			model, refusal := a.mediaPick(modalityMusic, parsed.Model, defaultModel)
			if refusal != "" {
				return "Invalid arguments: " + refusal, true, nil
			}

			// Every failure is a TOOL ERROR and never a Go error, exactly as
			// generate_image's and speak's are: a refused prompt, a model having
			// a bad minute and an expired key are all things the model can act
			// on, and none of them is a reason to fail the turn.
			//
			// This is GenerateMusic and never Speak: the two lanes share a
			// family and not an endpoint (media_contract.go, and
			// internal/provider/music.go for why).
			response, err := client.GenerateMusic(ctx, provider.MusicRequest{
				Model: model, Prompt: prompt,
			})
			if err != nil {
				return "Music generation failed (" + model + "): " + err.Error(), true, nil
			}
			if response == nil || len(response.Audio) == 0 {
				return "Music generation returned no audio (" + model + ")", true, nil
			}
			// Paid for before it is saved, on the session's pocket and no turn's
			// — tools_image.go states the reason.
			a.addAuxiliaryUsage(&ai.Response{Usage: response.Usage}, model, 1)

			path, err := a.mediaDestination(parsed.Path, prompt, musicFileExtension(response.Format),
				MusicDir(a.config.Place, a.config.Workspace))
			if err != nil {
				return "Could not save the generated music: " + err.Error(), true, nil
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return "Could not save the generated music: " + err.Error(), true, nil
			}
			if err := os.WriteFile(path, response.Audio, 0o644); err != nil {
				return "Could not save the generated music: " + err.Error(), true, nil
			}
			RecordArtifact(a.config.ArtifactsIndex, Artifact{
				Path:    path,
				Session: a.journalID(),
				Title:   mediaTitle(prompt, path),
				Kind:    "audio",
				Created: time.Now(),
			})
			return describeGeneratedMusic(a.config.Workspace, path, len(response.Audio), model), false, nil
		},
	}
}

// describeGeneratedMusic is the whole result: where it is, how big it is, who
// composed it. There is no duration, for [describeGeneratedAudio]'s reason —
// nothing here decodes the file, and a guessed length is worse than none
// (design-law §EMPTINESS). The format is read off the NAME the file was
// actually saved under rather than restated, so the sentence cannot describe a
// file that is not there.
func describeGeneratedMusic(workspace, path string, size int, model string) string {
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	return fmt.Sprintf("%s — %s of %s audio, composed by %s",
		displayMediaPath(workspace, path), mediaByteSize(size), format, model)
}
