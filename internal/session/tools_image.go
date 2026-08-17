package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	stdimage "image"
	// The decoders are imported for their side effect alone: they register
	// themselves with image.DecodeConfig, which is how a saved picture's
	// dimensions are read back out of the bytes we just wrote. Only these three
	// are in the standard library; a webp comes back without a size rather than
	// with a guessed one (see [describeGeneratedImage]).
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The session's one hand for MAKING a picture, as opposed to reading one.
//
// It obeys image.go's law from the other side. A picture the person attaches is
// journaled by reference and never as bytes, because a transcript full of base64
// is a transcript that is re-sent on every step of every turn afterwards. A
// picture the MODEL makes is exactly the same object with the same cost, so it
// leaves this tool the same way: written to a file, named by its path. The tool
// result is one line of text — where it is, how big it is — and the bytes go to
// disk and nowhere else. A model that wants to look at what it made attaches the
// file like anybody else.
//
// The wire it rides is the media contract (media_contract.go): one client for
// every generation endpoint and one use-time resolver for which model serves
// which modality (docs/MULTIMODAL.md, Decisions 5-7). The pre-revision pair —
// a client and a model slug of this tool's own — is gone, and with it the pin
// ladder this file used to carry: the resolver owns the whole ladder now, all
// four rungs of it, capability-checked against the catalog before it answers.

// imageDirectory is where a generated picture lands when the model does not say
// and the session has no folder of its own. It sits under the workspace, in the
// surface's own dot directory, so a session that paints twenty drafts leaves
// twenty files in one place a person can delete in one gesture rather than
// twenty files in the root of their repository.
//
// A session WITH a folder answers differently and [ImagesDir] holds the whole
// rule: the workspace itself when the session owns it, the session's own
// artifacts/ when the workspace is somebody's repository.
const imageDirectory = ".aforge-v3/images"

// The tool's own words name no directory, because the answer is not one
// directory any more ([ImagesDir]) and a description that named the wrong one
// would be teaching the model a path it cannot use. What the model needs is
// that the picture is saved and that the result says where — both of which the
// result actually does.
//
// What the middle sentences do is TEACH (docs/MULTIMODAL.md, Decision 8). A
// model that knows only "prompt in, png out" writes a fresh prompt every time
// and cannot fix what it just drew; a model that knows the path it was handed is
// itself a valid reference can edit, restyle, combine, and iterate on its own
// last render until the thing is right. That is the whole difference between one
// picture and a working loop, and it costs three clauses to say.
const generateImageDescription = "Generate an image from a text prompt and save it. Returns the path it was written to and the picture's dimensions — never the image itself, which stays on disk: this conversation carries the path, and the file is what you and the user both refer to afterwards. Give reference_paths to work from pictures instead of from words alone — one reference edits or restyles it, several combine them — and remember that the path this tool just returned is itself a valid reference, so you can pass your own last render back in and iterate on it until the diagram, the character or the layout is right. Give a path to choose the name and the folder; leave it out and the image is saved under a timestamped name derived from the prompt, and the result says where it went."

const generateImageSchemaJSON = `{"type":"object","properties":{` +
	`"prompt":{"type":"string","description":"What to draw, as a full description: subject, composition, style, lighting. The whole prompt reaches the image model, so detail is worth writing. With reference_paths it is the instruction — what to change about the pictures you gave."},` +
	`"reference_paths":{"type":"array","items":{"type":"string"},"description":"Pictures to work from, as paths in the workspace (png, jpeg, webp or gif, each up to 10MB). One is an edit or a restyle of it; several are combined. A path this tool returned earlier is a valid reference, which is how you iterate on your own output."},` +
	`"aspect_ratio":{"type":"string","description":"Shape of the frame, as the image model spells it (for example 16:9 or 1:1). Passed through untouched; leave it out for the model's own default."},` +
	`"size":{"type":"string","description":"Exact pixel size as WIDTHxHEIGHT (for example 1024x1024). Passed through untouched; leave it out for the model's own default."},` +
	`"path":{"type":"string","description":"Where to save it, relative to the workspace. Leave it out for a timestamped name derived from the prompt, saved where this session keeps its pictures. An existing file at this path is overwritten, as with the write tool."}` +
	`},"required":["prompt"],"additionalProperties":false}`

// imageTools is the picture-making half of the belt, and it is CONDITIONAL by
// the same law tools_search.go states at length: a tool with nothing behind it
// is left OFF rather than added and made to refuse.
//
// Two things must both be present, and [Agent.mediaHand] asks for both at once:
// the client is the hand, and the resolver's answer is what it asks for. A
// client with no model would send a request naming nothing, and a model with no
// client is a name with nowhere to send it.
func (a *Agent) imageTools() []bare.Tool {
	client, model, ready := a.mediaHand(modalityImage)
	if !ready {
		return nil
	}
	return []bare.Tool{a.generateImageTool(client, model)}
}

// generateImageArguments is the wire form.
type generateImageArguments struct {
	Prompt         string   `json:"prompt"`
	ReferencePaths []string `json:"reference_paths"`
	AspectRatio    string   `json:"aspect_ratio"`
	Size           string   `json:"size"`
	Path           string   `json:"path"`
}

func (a *Agent) generateImageTool(client MediaGenerator, model string) bare.Tool {
	return bare.Tool{
		Name:        "generate_image",
		Description: generateImageDescription,
		Schema:      json.RawMessage(generateImageSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed generateImageArguments
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			prompt := strings.TrimSpace(parsed.Prompt)
			if prompt == "" {
				return "Invalid arguments: prompt is required", true, nil
			}
			// The references are read BEFORE the request is sent, so a picture
			// that cannot be read costs nothing: a refusal here is a typo the
			// model can fix, and paying for a generation that was going to be
			// wrong about its own inputs helps nobody.
			references, refusal := a.mediaReferences(parsed.ReferencePaths)
			if refusal != "" {
				return "Invalid arguments: reference " + refusal, true, nil
			}

			// Every failure below is a TOOL ERROR and never a Go error, by
			// tools_search.go's rule: a refused prompt, an expired key, a model
			// having a bad minute are all things the model can act on, and none
			// of them is a reason to fail the turn.
			response, err := client.GenerateImage(ctx, provider.ImageRequest{
				Model: model, Prompt: prompt, N: 1, OutputFormat: "png",
				AspectRatio:     strings.TrimSpace(parsed.AspectRatio),
				Size:            strings.TrimSpace(parsed.Size),
				InputReferences: references,
			})
			if err != nil {
				return "Image generation failed (" + model + "): " + err.Error(), true, nil
			}
			if response == nil || len(response.Data) == 0 {
				return "Image generation returned no image (" + model + ")", true, nil
			}
			data, decodeErr := base64.StdEncoding.DecodeString(response.Data[0].Base64)
			if decodeErr != nil || len(data) == 0 {
				return "Image generation returned an unreadable image (" + model + ")", true, nil
			}
			// The picture is paid for whether or not it is any good, so the
			// accounting lands before the write can fail. It folds into the
			// SESSION total and not the turn's, exactly as the title and the
			// compaction summary do: no turn asked for a picture at this price,
			// and charging one turn for it would make an ordinary question read
			// as the cost of a rendering (see [Agent.addAuxiliaryUsage]).
			a.addAuxiliaryUsage(&ai.Response{Usage: response.Usage})

			path, err := a.mediaDestination(parsed.Path, prompt,
				imageExtension(response.Data[0].MediaType),
				ImagesDir(a.config.Place, a.config.Workspace))
			if err != nil {
				return "Could not save the generated image: " + err.Error(), true, nil
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return "Could not save the generated image: " + err.Error(), true, nil
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return "Could not save the generated image: " + err.Error(), true, nil
			}
			// A picture the harness made is a DELIVERABLE, so it earns a row in
			// the index a person finds their work again by (artifacts.go). The
			// recording is silent in both directions: it happens after the bytes
			// are safely down, and a failure to write the lookup file is not news
			// the model can act on.
			RecordArtifact(a.config.ArtifactsIndex, Artifact{
				Path:    path,
				Session: a.journalID(),
				Title:   mediaTitle(prompt, path),
				Kind:    "image",
				Created: time.Now(),
			})
			return describeGeneratedImage(a.config.Workspace, path, data, model), false, nil
		},
	}
}

// imageExtension maps what the provider says it sent onto a file suffix. png is
// the default because png is what the request asked for: a provider that names
// no type sent the format it was told to.
func imageExtension(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

// describeGeneratedImage is the whole result the model reads: WHERE and HOW BIG,
// and nothing else.
//
// The dimensions are read back out of the bytes rather than echoed from the
// request, because the request did not ask for any: a model that has to plan a
// layout around what it just made needs the number the file actually has. A
// format the standard library cannot decode — webp — reports its size in bytes
// alone rather than a guessed geometry, which is [design-law §EMPTINESS] applied
// to a number: better absent than invented.
func describeGeneratedImage(workspace, path string, data []byte, model string) string {
	shown := displayMediaPath(workspace, path)
	if config, format, err := stdimage.DecodeConfig(bytes.NewReader(data)); err == nil {
		return fmt.Sprintf("%s — %d×%d %s, %s, generated on %s",
			shown, config.Width, config.Height, format, mediaByteSize(len(data)), model)
	}
	return fmt.Sprintf("%s — %s, generated on %s", shown, mediaByteSize(len(data)), model)
}
