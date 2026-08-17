package session

// speak is the session's voice: text in, an audio file on disk, a path back.
//
// It is generate_image's twin in every way that matters (tools_image.go), and
// deliberately so — one landing law, one artifact row, one auxiliary pocket, one
// absence law — because a person who has learned where their pictures go has
// learned where their audio goes. What differs is only the endpoint and the
// extension: /audio/speech, mp3.
//
// THE BYTES NEVER ENTER THE TRANSCRIPT, for image.go's reason and more so: a
// minute of speech is a megabyte, base64 inflates it by a third, and a
// transcript carrying it re-sends it on every step of every turn afterwards. The
// result is one line naming the file, and the file is what the person plays.

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

// audioDirectory is where a spoken file lands when the model does not say and
// the session has no folder of its own — [imageDirectory]'s sibling, and
// [AudioDir] holds the whole rule.
const audioDirectory = ".aforge-v3/audio"

// speechFormat is what the endpoint is asked for and what the file is named
// with. One constant, because a request that asked for mp3 and a file called
// .wav is a file that will not play (design-law §ONE SOURCE OF TRUTH).
const (
	speechFormat    = "mp3"
	speechExtension = ".mp3"
)

// The description says the two things a model cannot guess: that voice is
// OPTIONAL — the provider has a default and naming a voice that does not exist
// is a failure it would have avoided by saying nothing — and that what comes
// back is a path, not audio it can listen to.
const speakDescription = "Turn text into spoken audio and save it as an mp3. Returns the path it was written to and how long the file is in bytes — never the audio itself, which stays on disk for the user to play. Leave voice out to get the speech model's own default voice; name one only when the user asked for a particular one, because a voice the model does not have is a failed generation. Give a path to choose the name and the folder; leave it out and the file is saved under a timestamped name derived from the text."

const speakSchemaJSON = `{"type":"object","properties":{` +
	`"text":{"type":"string","description":"The words to speak, exactly as they should be said. Punctuation shapes the delivery, so write it the way it should sound."},` +
	`"voice":{"type":"string","description":"Which voice to use, as the speech model spells it. Optional: leave it out and the provider's default voice speaks."},` +
	`"path":{"type":"string","description":"Where to save it, relative to the workspace. Leave it out for a timestamped name derived from the text, saved where this session keeps its audio. An existing file at this path is overwritten, as with the write tool."}` +
	`},"required":["text"],"additionalProperties":false}`

// speakTools is the speaking verb, CONDITIONAL by the belt's law: a nil media
// client or a resolver with no speech model for this machine leaves the tool off
// rather than on and refusing ([Agent.mediaHand]).
func (a *Agent) speakTools() []bare.Tool {
	client, model, ready := a.mediaHand(modalitySpeech)
	if !ready {
		return nil
	}
	return []bare.Tool{a.speakTool(client, model)}
}

type speakArguments struct {
	Text  string `json:"text"`
	Voice string `json:"voice"`
	Path  string `json:"path"`
}

func (a *Agent) speakTool(client MediaGenerator, model string) bare.Tool {
	return bare.Tool{
		Name:        "speak",
		Description: speakDescription,
		Schema:      json.RawMessage(speakSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed speakArguments
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			text := strings.TrimSpace(parsed.Text)
			if text == "" {
				return "Invalid arguments: text is required", true, nil
			}

			// Every failure is a TOOL ERROR and never a Go error, exactly as
			// generate_image's are: a refused text, an unknown voice and an
			// expired key are all things the model can act on.
			response, err := client.Speak(ctx, provider.SpeechRequest{
				Model: model, Input: text,
				Voice:          strings.TrimSpace(parsed.Voice),
				ResponseFormat: speechFormat,
			})
			if err != nil {
				return "Speech generation failed (" + model + "): " + err.Error(), true, nil
			}
			if response == nil || len(response.Audio) == 0 {
				return "Speech generation returned no audio (" + model + ")", true, nil
			}
			// Paid for before it is saved, on the session's pocket and no
			// turn's — tools_image.go states the reason.
			a.addAuxiliaryUsage(&ai.Response{Usage: response.Usage})

			path, err := a.mediaDestination(parsed.Path, text, speechExtension,
				AudioDir(a.config.Place, a.config.Workspace))
			if err != nil {
				return "Could not save the generated audio: " + err.Error(), true, nil
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return "Could not save the generated audio: " + err.Error(), true, nil
			}
			if err := os.WriteFile(path, response.Audio, 0o644); err != nil {
				return "Could not save the generated audio: " + err.Error(), true, nil
			}
			RecordArtifact(a.config.ArtifactsIndex, Artifact{
				Path:    path,
				Session: a.journalID(),
				Title:   mediaTitle(text, path),
				Kind:    "audio",
				Created: time.Now(),
			})
			return describeGeneratedAudio(a.config.Workspace, path, len(response.Audio), model), false, nil
		},
	}
}

// describeGeneratedAudio is the whole result: where it is, how big it is, who
// said it. There is no duration, because nothing here decodes an mp3 and a
// guessed length is worse than none (design-law §EMPTINESS).
func describeGeneratedAudio(workspace, path string, size int, model string) string {
	return fmt.Sprintf("%s — %s of mp3 audio, spoken by %s",
		displayMediaPath(workspace, path), mediaByteSize(size), model)
}
