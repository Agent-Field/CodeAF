package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdimage "image"
	"time"
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
const generateImageDescription = "Generate an image from a text prompt and save it. Returns the path it was written to and the picture's dimensions — never the image itself, which stays on disk: this conversation carries the path, and the file is what you and the user both refer to afterwards. Give reference_paths to work from pictures instead of from words alone — one reference edits or restyles it, several combine them — and remember that the path this tool just returned is itself a valid reference, so you can pass your own last render back in and iterate on it until the diagram, the character or the layout is right. To change one region and keep the rest, add mask_path: a picture the size of the first reference, white where it may change and black where it must stay; the result says whether this model took it as a real mask or only as a guide. Set n (1 to 10) to get that many variants of one prompt; you choose among them yourself. A model that draws fewer pictures per request than n (every Gemini image model draws one) is asked in several requests at once, each billed on its own, and the result says so. A first render is a draft: look at the file before you build on it or hand it over, judge it against what was asked, and iterate when it fell short. Give a path to choose the name and the folder; leave it out and the image is saved under a timestamped name derived from the prompt, and the result says where it went."

const generateImageSchemaJSON = `{"type":"object","properties":{` +
	`"prompt":{"type":"string","description":"What to draw, as a full description: subject, composition, style, lighting. The whole prompt reaches the image model, so detail is worth writing — every dimension the prompt leaves open, the model fills with its statistical average, and that average is what generic AI imagery looks like. Decide the medium, the light, the palette and the mood yourself and write them down; specificity is the difference between the picture you meant and a generic one. A prompt assembled from the genre's usual clichés lands on that same average by choice — escape a genre by naming a real medium (a print process, a photographic setup, a drafting tradition), not by recoloring it. And specify positively: image models barely read negation, so describe the surface, material and light that ARE there until the default has no room. With reference_paths it is the instruction — what to change about the pictures you gave."},` +
	`"reference_paths":{"type":"array","items":{"type":"string"},"description":"Pictures to work from, as paths in the workspace (png, jpeg, webp or gif, each up to 10MB). One is an edit or a restyle of it; several are combined. A path this tool returned earlier is a valid reference, which is how you iterate on your own output."},` +
	`"mask_path":{"type":"string","description":"A mask for the first of reference_paths, as a path in the workspace: the same size as that picture, white where it may change, black where it must stay. Needs reference_paths. A model that takes masks edits only the white area; any other model is sent the mask as a guide picture beside the source, the change may reach past it, and the result says which happened."},` +
	`"aspect_ratio":{"type":"string","description":"Shape of the frame as width:height (for example 16:9, 1:1, or the shape of the picture you are editing). A shape this model does not draw is drawn at the nearest one it does and cropped back, and the result says so; leave it out for the model's own default."},` +
	`"size":{"type":"string","description":"Exact pixel size as WIDTHxHEIGHT (for example 1024x1024). Without aspect_ratio, a size whose shape this model does not draw is drawn at the nearest shape it does and cropped and scaled back to this size, and a width:height here is read as aspect_ratio; leave it out for the model's own default."},` +
	`"n":{"type":"integer","minimum":1,"maximum":10,"default":1,"description":"How many pictures to make from this one prompt, for a contact sheet of variants. Default 1. Each is saved as its own file. A model that returns fewer per request is asked in several requests at once, each billed on its own; if some of them fail, the pictures that came back are kept and the failures are listed."},` +
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
	// N is how many pictures the CALL asks for, 1 to [ImageVariantsMax]; zero
	// means one. A model that draws fewer per request is asked several times
	// (tools_image_split.go).
	N int `json:"n"`
	// MaskPath is a mask for the first of ReferencePaths (tools_image_mask.go).
	MaskPath string `json:"mask_path"`
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
	// TakesMask says whether a model takes a real mask. Nil, or false for the
	// model that draws, sends a mask_path as a guide picture instead
	// (tools_image_mask.go) — the honest default when nobody can vouch.
	TakesMask func(model string) bool
	// AspectRatios answers the frame shapes a model publishes, nil when it
	// published none. A shape the model does not take is sent as its nearest
	// published one and the result cropped back (tools_image_aspect.go); nil,
	// or a nil answer, passes every shape through untouched.
	AspectRatios func(model string) []string
	// MaxOutputs answers how many pictures one request to a model may return,
	// zero when the model published no maximum. A call for more than that is
	// sent as several requests at once (tools_image_split.go); nil, or zero,
	// sends one request and splits it only when the provider refuses the count.
	MaxOutputs func(model string) int
	// attempt writes the call's render record before the provider is asked
	// (render_attempt.go). A conversation's belt sets it; the command line's
	// door and a saved harness's belt have no ledger by construction, so they
	// leave it nil and record nothing ([Agent.imageAttemptDoor]).
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
				TakesMask:    a.config.MediaMasks,
				AspectRatios: a.config.MediaAspects,
				MaxOutputs:   a.config.MediaOutputs,
				Account:      a.accountImageRung,
				attempt:      a.imageAttemptDoor(),
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
	// The mask is read and measured with the references, for their reason.
	mask, refusal := readImageMask(gen.Workspace, parsed.MaskPath, parsed.ReferencePaths)
	if refusal != "" {
		return rejectToolArguments(ctx, "Invalid arguments: "+refusal), true
	}
	// The shape is fitted to what this model draws before anything is paid
	// for, so a shape it does not take is not a refused call.
	var published []string
	if gen.AspectRatios != nil {
		published = gen.AspectRatios(model)
	}
	aspect, size := fitShape(parsed.AspectRatio, parsed.Size, published)
	request := provider.ImageRequest{
		Model: model, Prompt: prompt, N: requested, OutputFormat: "png",
		AspectRatio:     aspect.sent,
		Size:            size,
		InputReferences: references,
	}
	maskNote := ""
	if mask != nil {
		maskNote = mask.note(mask.apply(&request, gen.TakesMask != nil && gen.TakesMask(model)))
	}

	// THE ATTEMPT IS RECORDED BEFORE ANYTHING IS PAID FOR, and one that cannot
	// be recorded is never sent: a paid request with no record of having been
	// made is the gap the record exists to close (render_attempt.go). From here
	// every road out writes the record's one ending; close is the backstop.
	//
	// A call whose turn has already ended starts nothing: there is no request
	// to record, and the answer is the one the provider's client would give.
	if err := ctx.Err(); err != nil {
		return "Image generation failed (" + model + "): " + err.Error(), true
	}
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
	stoppedText := "Image generation was stopped (" + model + "); nothing was saved. Only waiting stopped here — the provider was not told to cancel, so whether it charged is unknown."
	// After the provider answered, what it reported charging is already on the
	// record, so the stop says that rather than "unknown".
	stoppedAfterReply := "Image generation was stopped (" + model + ") after the provider answered; nothing was saved, and whatever it reported charging is recorded."

	// THE REQUESTS ARE SIZED TO WHAT THIS MODEL DRAWS PER CALL (tools_image_split.go).
	// A model whose catalog row publishes a smaller maximum than n is asked in
	// several requests at once; one that publishes nothing is asked once, and
	// asked again as single pictures only when that one request was refused
	// about the count and nothing else.
	maxOutputs := 0
	if gen.MaxOutputs != nil {
		maxOutputs = gen.MaxOutputs(model)
	}
	sizes := imageRequestSizes(requested, maxOutputs)
	split := len(sizes) > 1
	replies := sendImageRequests(requestCtx, gen.Client, request, sizes)
	retried := false
	if !split && requested > 1 && replies[0].response == nil && imageCountRefused(replies[0].err) &&
		requestCtx.Err() == nil && (attempt == nil || !attempt.stopped()) {
		sizes, split, retried = imageSingles(requested), true, true
		replies = sendImageRequests(requestCtx, gen.Client, request, sizes)
	}
	// A provider may return a usage receipt alongside an error. Account every
	// actual receipt once, each as its own request, before any failure or
	// unusable-output branch.
	answered := false
	for _, reply := range replies {
		if reply.response == nil {
			continue
		}
		answered = true
		if gen.Account != nil {
			gen.Account(model, reply.response.Usage)
		}
	}
	// And the call's one total goes to the attempt's record the moment the
	// requests return, whatever became of it meanwhile: before an ending it rides
	// into the ending, after a Stop it is a receipt on the cancelled record.
	total := sumImageUsage(replies)
	if answered && attempt != nil {
		attempt.charge(total)
	}
	if attempt != nil && attempt.stopped() {
		if answered {
			return stoppedAfterReply, true
		}
		return stoppedText, true
	}
	return finishImageCall(gen, parsed, model, requested, aspect, maskNote, attempt, requestCtx, sizes, replies, retried, total)
}

// finishImageCall is everything after the provider answered: the failure
// strings, the bounds, the point of no return, the crop, the files and their
// facts, and the result. It reads one reply exactly as the road always did, and
// several replies (a split call, tools_image_split.go) as one batch whose
// requests each answered for themselves.
func finishImageCall(gen ImageGen, parsed GenerateImageArgs, model string, requested int, aspect aspectFit, maskNote string,
	attempt *renderAttempt, requestCtx context.Context, sizes []int, replies []imageReply, retried bool, total *ai.Usage) (string, bool) {
	// end reports false only when a Stop already wrote the ending.
	end := func(patch renderPatch) bool {
		return attempt == nil || attempt.end(patch)
	}
	stoppedText := "Image generation was stopped (" + model + "); nothing was saved. Only waiting stopped here — the provider was not told to cancel, so whether it charged is unknown."
	stoppedAfterReply := "Image generation was stopped (" + model + ") after the provider answered; nothing was saved, and whatever it reported charging is recorded."
	split := len(sizes) > 1

	// The pictures, in request order, each remembering the request it came
	// from, and one sentence per request that brought nothing back.
	var items []provider.GeneratedImage
	// origins and positions say which request each picture came from and where
	// it stood in that reply (1-based, so a picture that cannot be saved leaves
	// a gap); held says how many pictures each reply carried.
	var origins, positions []int
	held := make([]int, len(replies))
	var unanswered []string
	var firstErr error
	returned := 0
	for r, reply := range replies {
		switch {
		case reply.err != nil:
			if firstErr == nil {
				firstErr = reply.err
			}
			unanswered = append(unanswered, fmt.Sprintf("One request for %s was not made: %s", pictureCount(sizes[r]), reply.err.Error()))
			continue
		case reply.response == nil || len(reply.response.Data) == 0:
			unanswered = append(unanswered, fmt.Sprintf("One request for %s returned no image.", pictureCount(sizes[r])))
			continue
		}
		returned += len(reply.response.Data)
		held[r] = len(reply.response.Data)
		// Only the pictures that were asked for are kept: a provider that
		// returns more than n is not obeyed, and the one-picture call keeps its
		// old reading of the first.
		data := reply.response.Data
		if len(data) > sizes[r] {
			data = data[:sizes[r]]
		}
		for j, item := range data {
			items = append(items, item)
			origins = append(origins, r)
			positions = append(positions, j+1)
		}
		// The encoded copies now live in items alone, so a large reply does not
		// stay in memory twice while the pictures are written.
		reply.response.Data = nil
	}

	if len(items) == 0 {
		if firstErr != nil {
			text := "Image generation failed (" + model + "): " + firstErr.Error()
			patch := renderPatch{state: RenderFailed, err: text}
			if requestCtx.Err() != nil {
				// The turn ended under the call: only waiting stopped.
				patch = renderPatch{state: RenderCancelled}
			}
			if !end(patch) {
				// A Stop landed after the provider returned and before the ending.
				return stoppedText, true
			}
			switch {
			case split:
				text += fmt.Sprintf(" — asked for %d images as %d separate requests; none of them returned a picture", requested, len(sizes))
			case requested > 1:
				// A refusal that was not about the count is the caller's to act
				// on; asking again for a reason nobody can name would spend money
				// nobody approved.
				text += fmt.Sprintf(" — asked for %d images in one request; the refusal was not about the count, so nothing was retried", requested)
			}
			return text, true
		}
		end(renderPatch{state: RenderFailed, err: "the provider returned no image"})
		return "Image generation returned no image (" + model + ")", true
	}
	// THE BILL WAS PAID ONCE PER PROVIDER RESPONSE, BEFORE ANYTHING COULD REFUSE
	// IT (the caller accounted every reply). A reply that charged and then
	// carried no picture, a picture that will not decode or one too large to keep
	// is still a charge, and the figure the provider reported is the only one
	// there is to record (see [ImageGen.Account]). It is never once per picture.
	if why := imageReplyRefusal(items); why != "" {
		end(renderPatch{state: RenderFailed, err: "the reply was more than can be kept: " + why})
		return fmt.Sprintf("Image generation returned more than can be kept (%s): %s, so none were saved. "+
			"No request was retried; any provider-reported charge was recorded. Ask for fewer images or a smaller size.", model, why), true
	}

	// THE POINT OF NO RETURN, atomic with a Stop: one accepted before this line
	// means nothing is written; one after it is told it came too late.
	if attempt != nil && !attempt.commit() {
		return stoppedAfterReply, true
	}

	// A shape the model was sent in place of the asked one is cropped back
	// before anything is written, so the file on disk has the asked shape.
	targetW, targetH := aspect.editTarget(gen.Workspace, parsed.ReferencePaths)
	if note := aspect.fitItems(items, model, targetW, targetH); note != "" {
		maskNote = joinNotes(note, maskNote)
	}

	numbered := requested > 1
	// One batch id per provider REQUEST, because that is what a batch names
	// ([MediaFact.Batch]): files asked for together by one call share its tool
	// id, and files from one reply share a batch.
	batches := make([]string, len(sizes))
	for r := range batches {
		batches[r] = newBatchID()
	}
	var saved []string
	var failures []string
	for i, item := range items {
		path, data, saveErr := saveImageVariant(gen, parsed, numbered, i+1, item)
		// The encoded copy is dropped as soon as it is decoded, so a large reply
		// does not stay in memory beside the pictures already written.
		items[i].Base64 = ""
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
		r := origins[i]
		usage := replies[r].response.Usage
		fact := GeneratedMedia(path, data, model).WithReferences(parsed.ReferencePaths).WithRequested(requested)
		switch {
		case !numbered:
			fact = fact.WithUsage(usage)
		case sizes[r] > 1:
			// One request, one total: the charge is the request's, never a picture's.
			fact = fact.WithBatch(batches[r], positions[i], held[r]).WithCallUsage(usage)
		default:
			// A request for one picture: its charge IS this picture's.
			fact = fact.WithBatch(batches[r], positions[i], held[r]).WithUsage(usage)
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
		return withMaskNote(saved[0], maskNote), false
	}
	text, failed := describeImageBatch(model, requested, returned, len(sizes), saved, append(unanswered, failures...))
	if split {
		text += "\n" + describeImageSplit(model, len(sizes), retried, total)
	}
	if !failed {
		text = withMaskNote(text, maskNote)
	}
	return text, failed
}

// pictureCount is "one picture" or "3 pictures".
func pictureCount(n int) string {
	if n == 1 {
		return "one picture"
	}
	return fmt.Sprintf("%d pictures", n)
}

// joinNotes puts two closing lines together, either of which may be empty.
func joinNotes(first, second string) string {
	switch {
	case first == "":
		return second
	case second == "":
		return first
	}
	return first + "\n" + second
}

// withMaskNote closes a result with the mask's line, when there was a mask.
// The path stays the first thing on the first line, which the command line's
// door and every caller that reads the path back rely on.
func withMaskNote(text, note string) string {
	if note == "" {
		return text
	}
	return text + "\n" + note
}

// describeImageBatch is the result of a call for several pictures: how many
// were asked for, in how many requests, how many the providers sent, how many are on disk, then one
// line per file and one per failure. It claims no more than happened, and it
// does not rank — the order is the provider's.
func describeImageBatch(model string, requested, returned, requests int, saved, failures []string) (string, bool) {
	var out strings.Builder
	if requests > 1 {
		fmt.Fprintf(&out, "Saved %d of %d requested images on %s from %d requests", len(saved), requested, model, requests)
	} else {
		fmt.Fprintf(&out, "Saved %d of %d requested images on %s from one request", len(saved), requested, model)
	}
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
