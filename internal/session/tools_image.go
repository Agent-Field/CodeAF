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
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
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

// ImageGenerator is the one call generate_image makes: a text prompt in, an
// encoded picture out.
//
// It is [provider.MediaClient.GenerateImage]'s signature verbatim rather than a
// simplified one of this package's own, so the wiring wave hands the media
// client over directly and no adapter type exists to drift. It is an interface
// rather than the concrete client for the reason [Completer] is: a test drives a
// scripted painter and never opens a socket.
type ImageGenerator interface {
	GenerateImage(context.Context, provider.ImageRequest) (*provider.ImageResponse, error)
}

// And the claim above, checked by the compiler rather than by a reader: the
// media client goes in here with no adapter between. It is stated because the
// signature is the ONLY thing holding the two sides together — a change to
// [provider.MediaClient.GenerateImage] would otherwise be found by the door
// that wires it (cmd/aforge's v3ImageGen) rather than by the file that made
// the promise.
var _ ImageGenerator = (*provider.MediaClient)(nil)

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

// imageStampFormat is the sortable half of a generated file's name. Seconds are
// enough resolution because the slug and the collision suffix carry the rest;
// the point of the stamp is that `ls` reads in the order the pictures were made.
const imageStampFormat = "20060102-150405"

// imageSlugWords and imageSlugLimit bound the readable half. Six words is enough
// to recognise which prompt made which file at a glance, which is the whole job
// the slug has.
const (
	imageSlugWords = 6
	imageSlugLimit = 48
)

// The tool's own words name no directory, because the answer is not one
// directory any more ([ImagesDir]) and a description that named the wrong one
// would be teaching the model a path it cannot use. What the model needs is
// that the picture is saved and that the result says where — both of which the
// result actually does.
const generateImageDescription = "Generate an image from a text prompt and save it. Returns the path it was written to and the picture's dimensions — never the image itself, which stays on disk: this conversation carries the path, and the file is what you and the user both refer to afterwards. Give a path to choose the name and the folder; leave it out and the image is saved under a timestamped name derived from the prompt, and the result says where it went."

const generateImageSchemaJSON = `{"type":"object","properties":{"prompt":{"type":"string","description":"What to draw, as a full description: subject, composition, style, lighting. The whole prompt reaches the image model, so detail is worth writing."},"path":{"type":"string","description":"Where to save it, relative to the workspace. Leave it out for a timestamped name derived from the prompt, saved where this session keeps its pictures. An existing file at this path is overwritten, as with the write tool."}},"required":["prompt"],"additionalProperties":false}`

// imageTools is the picture-making half of the belt, and it is CONDITIONAL by
// the same law tools_search.go states at length: a tool with nothing behind it
// is left OFF rather than added and made to refuse.
//
// Two things must both be present. The client is the hand, and the model is what
// it asks for; a client with no model would send a request naming nothing, and a
// model with no client is a name with nowhere to send it.
func (a *Agent) imageTools() []bare.Tool {
	client := a.config.ImageGenClient
	if client == nil {
		return nil
	}
	model := a.imageGenModel()
	if model == "" {
		return nil
	}
	return []bare.Tool{a.generateImageTool(client, model)}
}

// imageGenModel answers which model paints: the one the surface wired, or the
// one the person pinned.
//
// It reads the PIN alone — [roles.Pinned], not [roles.Resolve] — and that is the
// deliberate part. The tier ladder holds TEXT models: a person who sets
// tiers.high to a chat model has said nothing about painting, and resolving
// imagegen through that rung would send an image request to a model that has no
// images endpoint, which comes back as a 404 nobody reading it can act on. A pin
// is the only rung that can mean "this model paints", so it is the only rung
// consulted, and roles.RoleImageGen is deliberately never registered for a tier.
func (a *Agent) imageGenModel() string {
	if model := strings.TrimSpace(a.config.ImageGenModel); model != "" {
		return model
	}
	if model, ok := roles.Pinned(roles.Source(a.config.RolesSource), roles.RoleImageGen); ok {
		return model
	}
	return ""
}

func (a *Agent) generateImageTool(client ImageGenerator, model string) bare.Tool {
	return bare.Tool{
		Name:        "generate_image",
		Description: generateImageDescription,
		Schema:      json.RawMessage(generateImageSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Prompt string `json:"prompt"`
				Path   string `json:"path"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			prompt := strings.TrimSpace(parsed.Prompt)
			if prompt == "" {
				return "Invalid arguments: prompt is required", true, nil
			}

			// Every failure below is a TOOL ERROR and never a Go error, by
			// tools_search.go's rule: a refused prompt, an expired key, a model
			// having a bad minute are all things the model can act on, and none
			// of them is a reason to fail the turn.
			response, err := client.GenerateImage(ctx, provider.ImageRequest{
				Model: model, Prompt: prompt, N: 1, OutputFormat: "png",
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

			path, err := a.imageDestination(parsed.Path, prompt, response.Data[0].MediaType)
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
				Title:   imageTitle(prompt, path),
				Kind:    "image",
				Created: time.Now(),
			})
			return describeGeneratedImage(a.config.Workspace, path, data, model), false, nil
		},
	}
}

// imageDestination is the absolute path the bytes are about to be written to.
//
// A path the model gave is taken as given — relative to the workspace, the way
// every other tool on this belt resolves one, and overwritten if it exists the
// way write does. Only its extension is corrected, because a name with none is a
// file nothing on the machine will open by double-clicking it.
//
// A path the model did not give is derived: the directory, the timestamp, the
// prompt's slug, and a suffix if a file of that name is already there. The
// collision loop matters more than it looks — two pictures of the same prompt in
// the same second is what "make me three of these" produces, and the third one
// silently overwriting the second would lose work nobody asked to lose.
func (a *Agent) imageDestination(asked, prompt, mediaType string) (string, error) {
	extension := imageExtension(mediaType)
	if asked = strings.TrimSpace(asked); asked != "" {
		path := asked
		if !filepath.IsAbs(path) {
			path = filepath.Join(a.config.Workspace, path)
		}
		path = filepath.Clean(path)
		if filepath.Ext(path) == "" {
			path += extension
		}
		return path, nil
	}

	directory := ImagesDir(a.config.Place, a.config.Workspace)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}
	base := time.Now().Format(imageStampFormat)
	if slug := imageSlug(prompt); slug != "" {
		base += "-" + slug
	}
	for collision := 0; collision < 1000; collision++ {
		name := base
		if collision > 0 {
			name += fmt.Sprintf("-%d", collision+1)
		}
		path := filepath.Join(directory, name+extension)
		// The name is CLAIMED, not merely looked at. A tool batch runs its calls
		// in parallel (loop.go), so two pictures of the same prompt in the same
		// second are two goroutines racing for one name — and a check that only
		// asked whether the file existed would hand both the same answer.
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			if closeErr := file.Close(); closeErr != nil {
				return "", closeErr
			}
			return path, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("too many images share the name %s", base)
}

// imageSlug turns a prompt into the readable half of a file name: the first few
// words, lowercased, with everything that is not a letter or a digit collapsed
// to a single hyphen. "Sunset over the harbour, 35mm" becomes
// "sunset-over-the-harbour-35mm".
func imageSlug(prompt string) string {
	words := strings.FieldsFunc(strings.ToLower(prompt), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(words) > imageSlugWords {
		words = words[:imageSlugWords]
	}
	slug := strings.Join(words, "-")
	if len(slug) > imageSlugLimit {
		slug = strings.TrimRight(slug[:imageSlugLimit], "-")
	}
	return slug
}

// imageTitle is what a picker row says about one picture: the prompt's own
// slug, which is the phrase a person would search for, and the file's name when
// the prompt made no slug at all — a prompt of punctuation, or a picture that
// arrived rather than being asked for.
func imageTitle(prompt, path string) string {
	if slug := imageSlug(prompt); slug != "" {
		return strings.ReplaceAll(slug, "-", " ")
	}
	return filepath.Base(path)
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
	shown := displayImagePath(workspace, path)
	if config, format, err := stdimage.DecodeConfig(bytes.NewReader(data)); err == nil {
		return fmt.Sprintf("%s — %d×%d %s, %s, generated on %s",
			shown, config.Width, config.Height, format, imageByteSize(len(data)), model)
	}
	return fmt.Sprintf("%s — %s, generated on %s", shown, imageByteSize(len(data)), model)
}

// displayImagePath is the path as the model should say it back: relative to the
// workspace when it is inside it, so the read tool and the person's own shell
// both accept the string verbatim.
func displayImagePath(workspace, path string) string {
	relative, err := filepath.Rel(workspace, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

func imageByteSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
