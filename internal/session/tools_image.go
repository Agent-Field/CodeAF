package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/provider"
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

// And the claim the contract makes, checked by the compiler rather than by a
// reader: the media client goes into [Config.Media] with no adapter between.
var _ MediaGenerator = (*provider.MediaClient)(nil)

// imageDirectory is where a generated picture lands when the model does not say
// and the session has no folder of its own. It sits under the workspace, in the
// surface's own dot directory, so a session that paints twenty drafts leaves
// twenty files in one place a person can delete in one gesture rather than
// twenty files in the root of their repository.
//
// A session WITH a folder answers differently and [ImagesDir] holds the whole
// rule: the workspace itself when the session owns it, the session's own
// artifacts/ when the workspace is somebody's repository.
const imageDirectory = ".codeaf/images"

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
const generateImageDescription = "Generate an image from a text prompt and save it. Returns the path it was written to and the picture's dimensions — never the image itself, which stays on disk: this conversation carries the path, and the file is what you and the user both refer to afterwards. Give reference_paths to work from pictures instead of from words alone — one reference edits or restyles it, several combine them — and remember that the path this tool just returned is itself a valid reference, so you can pass your own last render back in and iterate on it until the diagram, the character or the layout is right. Set n (1 to 10) to get that many variants of one prompt from a single request; you choose among them yourself. A first render is a draft: look at the file before you build on it or hand it over, judge it against what was asked, and iterate when it fell short. Give a path to choose the name and the folder; leave it out and the image is saved under a timestamped name derived from the prompt, and the result says where it went."

const generateImageSchemaJSON = `{"type":"object","properties":{` +
	`"prompt":{"type":"string","description":"What to draw, as a full description: subject, composition, style, lighting. The whole prompt reaches the image model, so detail is worth writing — every dimension the prompt leaves open, the model fills with its statistical average, and that average is what generic AI imagery looks like. Decide the medium, the light, the palette and the mood yourself and write them down; specificity is the difference between the picture you meant and a generic one. A prompt assembled from the genre's usual clichés lands on that same average by choice — escape a genre by naming a real medium (a print process, a photographic setup, a drafting tradition), not by recoloring it. And specify positively: image models barely read negation, so describe the surface, material and light that ARE there until the default has no room. With reference_paths it is the instruction — what to change about the pictures you gave."},` +
	`"reference_paths":{"type":"array","items":{"type":"string"},"description":"Pictures to work from, as paths in the workspace (png, jpeg, webp or gif, each up to 10MB). One is an edit or a restyle of it; several are combined. A path this tool returned earlier is a valid reference, which is how you iterate on your own output."},` +
	`"aspect_ratio":{"type":"string","description":"Shape of the frame, as the image model spells it (for example 16:9 or 1:1). Passed through untouched; leave it out for the model's own default."},` +
	`"size":{"type":"string","description":"Exact pixel size as WIDTHxHEIGHT (for example 1024x1024). Passed through untouched; leave it out for the model's own default."},` +
	`"n":{"type":"integer","minimum":1,"maximum":10,"default":1,"description":"How many pictures ONE request should return, for a contact sheet of variants of the same prompt. Default 1. Each is saved as its own file and the cost is that of the whole request. If the model cannot return several, the call fails and nothing is retried."},` +
	`"path":{"type":"string","description":"Where to save it, relative to the workspace. Leave it out for a timestamped name derived from the prompt, saved where this session keeps its pictures. An existing file at this path is overwritten, as with the write tool; with n above 1 the files are numbered from it (name-1.png, name-2.png) and no existing file is overwritten."}` +
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

// GenerateImageArgs is the wire form, and the road's argument shape in one:
// the belt decodes into it, and the command line's image door fills it
// directly, so the two doors cannot disagree about what one call carries.
type GenerateImageArgs struct {
	Prompt         string   `json:"prompt"`
	ReferencePaths []string `json:"reference_paths"`
	AspectRatio    string   `json:"aspect_ratio"`
	Size           string   `json:"size"`
	Path           string   `json:"path"`
	Model          string   `json:"model"`
	// N is how many pictures ONE request asks for, 1 to [ImageVariantsMax]; zero
	// means one.
	N int `json:"n"`
}

// ImageGen is everything one generation needs from the surface it runs in:
// the client, the default model its resolver answers, the picker for a call's
// own word, where unnamed pictures land, and the two hooks the belt adds —
// the bill and the artifacts row. Account and Record are optional: a caller
// with no session passes neither, and the picture still lands.
type ImageGen struct {
	Client       MediaGenerator
	DefaultModel string
	Pick         func(modality, word string) (string, error)
	Workspace    string
	Directory    string
	Account      func(model string, usage *ai.Usage)
	Record       func(path, prompt string)
	// RecordMedia is Record with the structured fact of the file (mediafact.go);
	// it wins over Record when both are set.
	RecordMedia func(path, prompt string, fact MediaFact)
	// attempt writes the call's render record before the provider is asked
	// (render_attempt.go). The belt sets it; the command line's door has no
	// session and no ledger, so it leaves it nil and records nothing.
	attempt func(ctx context.Context, model string) (*renderAttempt, error)
}

// generateImageTool wraps the shared road ([generateImage]) in the belt's
// argument decode and its own two hooks.
func (a *Agent) generateImageTool(client MediaGenerator, defaultModel string) bare.Tool {
	return bare.Tool{
		Name:        "generate_image",
		Description: generateImageDescription,
		Schema:      a.mediaSchema(generateImageSchemaJSON, "image"),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed GenerateImageArgs
			if err := decodeToolArguments(args, &parsed); err != nil {
				return rejectToolArguments(ctx, "Invalid arguments: "+err.Error()), true, nil
			}
			resolvedDefault := defaultModel
			if strings.TrimSpace(parsed.Prompt) != "" && a.config.MediaAdmission != nil {
				admission := a.config.MediaAdmission(modalityImage, parsed.Model)
				if admission.Model == "" {
					return a.refuseMediaAdmission(ctx, modalityImage, admission)
				}
				resolvedDefault = admission.Model
				parsed.Model = "" // The invocation's exact choice was already resolved.
			}
			text, failed := GenerateImage(ctx, ImageGen{
				Client:       client,
				DefaultModel: resolvedDefault,
				Pick:         a.config.MediaPick,
				Workspace:    a.config.Workspace,
				Directory:    ImagesDir(a.config.Place, a.config.Workspace),
				Account:      a.accountImageRung,
				attempt:      a.beginImageAttempt,
				RecordMedia: func(path, prompt string, fact MediaFact) {
					a.recordImageMedia(path, prompt, fact.WithMade(madeBy(ctx, nil)))
				},
			}, parsed)
			return text, failed, nil
		},
	}
}

// accountImageRung is the road's bill door: the picture is paid for whether or
// not it is any good, so the accounting lands before the write can fail, and
// it folds into the SESSION total and not the turn's, exactly as the title
// call does — no turn asked for a picture at this price, and charging one turn
// for it would make an ordinary question read as the cost of a rendering (see
// [Agent.addAuxiliaryUsage]).
func (a *Agent) accountImageRung(model string, usage *ai.Usage) {
	a.addAuxiliaryUsage(&ai.Response{Usage: usage}, model, 1)
}

// recordImageMedia is the road's artifacts hook: a picture the harness made
// is a DELIVERABLE, so it earns a row in the index a person finds their work
// again by (artifacts.go). The recording is silent in both directions: it
// happens after the bytes are safely down, and a failure to write the lookup
// file is not news the model can act on.
func (a *Agent) recordImageMedia(path, prompt string, fact MediaFact) {
	a.recordMedia(Artifact{
		Path:    path,
		Title:   mediaTitle(prompt, path),
		Kind:    "image",
		Created: time.Now(),
	}, fact)
}

// GenerateImage is the whole road behind generate_image, as a plain function
// the command line's image door runs too: pick the model, read the
// references, send the request, decode, account, write, describe. It exists
// so the belt's tool and the command line's door cannot drift.
//
// Every failure is the tool error the belt returns and the command line
// prints, never a Go error: a refused prompt, an expired key, a model having
// a bad minute are all things a caller can act on, and none of them is a
// reason to crash anything.
func GenerateImage(ctx context.Context, gen ImageGen, parsed GenerateImageArgs) (string, bool) {
	// AN EMPTY PROMPT IS REFUSED BEFORE ANYTHING IS PAID FOR, the way the video
	// and music doors refuse theirs (tools_video.go, tools_music.go). A provider
	// asked to draw nothing still bills the call, and the answer it sends back
	// reads as its own fault rather than the caller's.
	//
	// THE GUARD LIVES HERE AND NOT ON THE BELT, so the command line's picture
	// door refuses the same call the same way (cmd/codeaf's image.go calls this
	// function too). It was here until the web pair and the picture hand were
	// made plain functions a command line could share: the block around it moved
	// and the check did not come with it, which left the one paid door of the
	// three with no argument check at all.
	prompt := strings.TrimSpace(parsed.Prompt)
	if prompt == "" {
		return rejectToolArguments(ctx, "Invalid arguments: prompt is required"), true
	}
	// THE COUNT IS CHECKED BEFORE ANYTHING IS PAID FOR, like the prompt: a request
	// for eleven pictures is a typo the caller can fix, not a charge.
	requested, refusal := imageCount(parsed.N)
	if refusal != "" {
		return rejectToolArguments(ctx, "Invalid arguments: "+refusal), true
	}
	// The call's own choice, resolved before anything is paid for, so a word
	// that matches nothing costs nothing. From here down `model` is the model
	// that actually draws, wherever it is named — the request, the failure
	// strings, the accounting, the result line.
	model, refusal := mediaPickWith(gen.Pick, modalityImage, parsed.Model, gen.DefaultModel)
	if refusal != "" {
		return rejectToolArguments(ctx, "Invalid arguments: "+refusal), true
	}
	// The references are read BEFORE the request is sent, so a picture that
	// cannot be read costs nothing: a refusal here is a typo the model can
	// fix, and paying for a generation that was going to be wrong about its
	// own inputs helps nobody.
	references, refusal := mediaReferencesFrom(gen.Workspace, parsed.ReferencePaths)
	if refusal != "" {
		return rejectToolArguments(ctx, "Invalid arguments: reference "+refusal), true
	}

	// THE ATTEMPT IS RECORDED BEFORE ANYTHING IS PAID FOR, and one that cannot
	// be recorded is never sent: a paid request with no record of having been
	// made is the gap the record exists to close (render_attempt.go). From here
	// every road out writes the record's one ending; close is the backstop.
	requestCtx := ctx
	var attempt *renderAttempt
	if gen.attempt != nil {
		started, err := gen.attempt(ctx, model)
		if err != nil {
			return "Could not start the image: " + err.Error() + ". No request was sent.", true
		}
		attempt, requestCtx = started, started.ctx
		defer attempt.close()
	}
	end := func(patch renderPatch) {
		if attempt != nil {
			attempt.end(patch)
		}
	}
	stoppedText := "Image generation was stopped (" + model + "); nothing was saved. Only waiting stopped here — the provider was not told to cancel, so whether it charged is unknown."

	response, err := gen.Client.GenerateImage(requestCtx, provider.ImageRequest{
		Model: model, Prompt: prompt, N: requested, OutputFormat: "png",
		AspectRatio:     strings.TrimSpace(parsed.AspectRatio),
		Size:            strings.TrimSpace(parsed.Size),
		InputReferences: references,
	})
	// A provider may return a usage receipt alongside an error. Account the
	// actual receipt once, before any failure or unusable-output branch.
	if response != nil && gen.Account != nil {
		gen.Account(model, response.Usage)
	}
	// And the same receipt goes to the attempt's record the moment the call
	// returns, whatever became of it meanwhile: before an ending it rides into
	// the ending, after a Stop it is a receipt on the cancelled record.
	if response != nil && attempt != nil {
		attempt.charge(response.Usage)
	}
	if attempt != nil && attempt.stopped() {
		return stoppedText, true
	}
	if err != nil {
		text := "Image generation failed (" + model + "): " + err.Error()
		if requestCtx.Err() != nil {
			// The turn ended under the call: only waiting stopped.
			end(renderPatch{state: RenderCancelled})
		} else {
			end(renderPatch{state: RenderFailed, err: text})
		}
		if requested > 1 {
			// A provider that cannot give n pictures says so; the answer is the
			// caller's to act on, and asking again as several calls would spend
			// money nobody approved.
			text += fmt.Sprintf(" — asked for %d images in one request; nothing was retried", requested)
		}
		return text, true
	}
	if response == nil {
		end(renderPatch{state: RenderFailed, err: "the provider returned no image"})
		return "Image generation returned no image (" + model + ")", true
	}
	// THE BILL IS PAID ONCE PER PROVIDER RESPONSE, BEFORE ANYTHING CAN REFUSE IT.
	// A reply that charged and then carried no picture, a picture that will not
	// decode or one too large to keep is still a charge, and the figure the
	// provider reported is the only one there is to record (see
	// [ImageGen.Account]). It is never once per picture.
	if len(response.Data) == 0 {
		end(renderPatch{state: RenderFailed, err: "the provider returned no image"})
		return "Image generation returned no image (" + model + ")", true
	}
	// Only the pictures that were asked for are kept: a provider that returns
	// more than n is not obeyed, and the one-picture call keeps its old reading
	// of the first.
	items := response.Data
	if len(items) > requested {
		items = items[:requested]
	}
	if why := imageReplyRefusal(items); why != "" {
		end(renderPatch{state: RenderFailed, err: "the reply was more than can be kept: " + why})
		return fmt.Sprintf("Image generation returned more than can be kept (%s): %s, so none were saved. "+
			"No request was retried; any provider-reported charge was recorded. Ask for fewer images or a smaller size.", model, why), true
	}

	// THE POINT OF NO RETURN, atomic with a Stop: one accepted before this line
	// means nothing is written; one after it is told it came too late.
	if attempt != nil && !attempt.commit() {
		return stoppedText, true
	}

	numbered := requested > 1
	batch := newBatchID()
	var saved []string
	var failures []string
	for i, item := range items {
		path, data, saveErr := saveImageVariant(gen, parsed, numbered, i+1, item)
		// The encoded copy is dropped as soon as it is decoded, so a large reply
		// does not stay in memory beside the pictures already written.
		response.Data[i].Base64 = ""
		if saveErr != nil {
			if !numbered {
				// The record names no path and quotes no file-system error: a
				// render record is never a way to learn where files live.
				if errors.Is(saveErr, errUnreadableImage) {
					end(renderPatch{state: RenderFailed, committed: true, err: "the provider's image was unreadable"})
					return "Image generation returned an unreadable image (" + model + ")", true
				}
				end(renderPatch{state: RenderFailed, committed: true, err: "the image could not be saved"})
				return "Could not save the generated image: " + saveErr.Error(), true
			}
			if errors.Is(saveErr, errUnreadableImage) {
				failures = append(failures, fmt.Sprintf("Image %d was not saved: the provider's image was unreadable.", i+1))
			} else {
				failures = append(failures, fmt.Sprintf("Image %d was not saved: %s", i+1, saveErr.Error()))
			}
			continue
		}
		// The facts are the ones the road itself holds: the bytes it wrote, the
		// usage the provider reported, and the reference paths exactly as given.
		fact := GeneratedMedia(path, data, model).WithReferences(parsed.ReferencePaths).WithRequested(requested)
		if numbered {
			// One request, one total: the charge is the call's, never a picture's.
			fact = fact.WithBatch(batch, i+1, len(response.Data)).WithCallUsage(response.Usage)
		} else {
			fact = fact.WithUsage(response.Usage)
		}
		if gen.RecordMedia != nil {
			gen.RecordMedia(path, parsed.Prompt, fact)
		} else if gen.Record != nil {
			gen.Record(path, parsed.Prompt)
		}
		saved = append(saved, describeGeneratedImage(path, data, model))
	}
	// The record's ending carries no path: the files are the media rows this
	// call made, found by its run and call id, never by a path on the record.
	if len(saved) == 0 {
		end(renderPatch{state: RenderFailed, committed: true, err: "none of the images could be saved"})
	} else {
		end(renderPatch{state: RenderFinished, committed: true})
	}
	if !numbered {
		return saved[0], false
	}
	return describeImageBatch(model, requested, len(response.Data), saved, failures)
}

// describeImageBatch is the result of a request for several pictures: how many
// were asked for, how many the provider sent, how many are on disk, then one
// line per file and one per failure. It claims no more than happened, and it
// does not rank — the order is the provider's.
func describeImageBatch(model string, requested, returned int, saved, failures []string) (string, bool) {
	var out strings.Builder
	fmt.Fprintf(&out, "Saved %d of %d requested images on %s from one request", len(saved), requested, model)
	if returned != requested {
		fmt.Fprintf(&out, " (the provider returned %d)", returned)
	}
	out.WriteString(", in the provider's order and not ranked:")
	for _, line := range saved {
		out.WriteString("\n" + line)
	}
	for _, line := range failures {
		out.WriteString("\n" + line)
	}
	if returned > requested {
		fmt.Fprintf(&out, "\nThe provider returned %d images; only the %d asked for were kept.", returned, requested)
	}
	return out.String(), len(saved) == 0
}

func imageDestinationFrom(workspace, asked, prompt string, data []byte, declared, directory string) (string, error) {
	extension := provider.ImageExtension(data, declared)
	if trimmed := strings.TrimSpace(asked); trimmed != "" {
		if ext := filepath.Ext(trimmed); ext != "" && ext != "." {
			asked = strings.TrimSuffix(trimmed, ext) + extension
		}
	}
	return mediaDestinationFrom(workspace, asked, prompt, extension, directory)
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
//
// The path is WHOLE ([picturePathInResult]), because this line is what a
// terminal that cannot draw the picture shows in its place, and a path with a
// directory missing off the front is a path nobody can open.
func describeGeneratedImage(path string, data []byte, model string) string {
	shown := picturePathInResult(path)
	if config, format, err := stdimage.DecodeConfig(bytes.NewReader(data)); err == nil {
		return fmt.Sprintf("%s — %d×%d %s, %s, generated on %s",
			shown, config.Width, config.Height, format, mediaByteSize(len(data)), model)
	}
	return fmt.Sprintf("%s — %s, generated on %s", shown, mediaByteSize(len(data)), model)
}
