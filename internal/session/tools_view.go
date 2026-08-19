package session

// view_image: the model's own eyes on a file, through the looking slot.
//
// Everything else on this belt reads TEXT. A picture reached the conversation
// one way only — a person attached it — which left the model unable to check
// the screenshot it just told a person to take, unable to look at the diagram it
// just generated, and unable to say anything about a photograph sitting in the
// workspace beyond its size in bytes. A task node, which has no person in front
// of it at all, could never look at anything.
//
// So looking becomes a verb. It is ONE SHOT AND ONE ANSWER, exactly as the
// vision fallback is (image.go): the looking model is sent the picture and the
// question and NOTHING ELSE — no system prompt, no transcript, no tools — and
// what comes back is the tool's result. The bytes go to that model and are never
// recorded, which is the same law the fallback keeps and for the same reason: a
// data URL in this transcript is a data URL re-sent on every step of every turn
// afterwards, and the model calling this tool is usually the one that cannot
// read it anyway.
//
// IT RESOLVES THROUGH THE SAME SLOT AS EVERYTHING ELSE. The seer is
// [Agent.visionSeer] — the settings sheet's looking slot, then the role pin,
// then the catalog — so "can I see" has one answer whether it is asked by an
// attached picture, by read_document's image rung, or by this tool
// (docs/MULTIMODAL.md, Decision 8).
//
// AND A BELT WITH NO EYES HAS NO VERB. When nothing resolves, the tool is left
// off entirely rather than added and made to refuse every time it is called —
// the design law tools_search.go and tools_image.go both state, and the reason
// it matters here is that a model told it can look at pictures plans around
// looking at them for the rest of the conversation.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// viewImageDefaultQuestion is what is asked of a picture when the caller asked
// nothing. It is internal/exec/media.go's, word for word, because a model that
// looks at a screenshot from a task node and one that looks at it from a
// conversation should get the same kind of answer — and because "describe this
// image" alone comes back as a caption where the caller wanted the text on the
// error dialog.
const viewImageDefaultQuestion = "Describe this image precisely: subject, composition, any text verbatim, and anything that looks wrong or malformed."

// viewLookWindow is how long ONE look may take before the tool answers without
// it. IT IS THE WHOLE REASON THIS TOOL CANNOT HANG: a call that starts always
// ends journaled, and the completion below was the one path out of this
// function that could return neither a result nor an error — the turn's context
// carries no deadline of its own, so a provider that accepted the request and
// then went quiet left the row running for the rest of the session (the surface
// draws a journaled call with no result as still-running, internal/tui3's
// room.go, and it is right to).
//
// It is [providerTimeout] rather than a second number: the look is one
// non-streamed completion on this session's client, which is exactly what that
// constant already bounds.
//
// It is a var only so the tests can run the clock out in a millisecond, the way
// connect.go's own waits are. NOTHING IN A BUILD WRITES IT.
var viewLookWindow = providerTimeout

// The description TEACHES, by Decision 8: capability is not something a model
// should discover by failing. It names the uses that are otherwise invisible —
// the screenshot, the rendered page, the picture this session made itself — the
// question that focuses the look, and the fact that the answer comes from
// another model and says so.
const viewImageDescription = "Look at an image file and get an answer about it, without anyone attaching it. This is how you check a screenshot, examine a page or chart you rendered to a picture, read a photograph of a document, or look at an image you generated a moment ago to see whether it came out right. Ask a question to point the look where you need it — \"what does the error dialog say?\", \"is the legend cut off?\", \"how many rows are in this table?\" — or leave it out for a full description. Reads png, jpeg, webp and gif up to 10MB. The picture is looked at once, by the model this session uses for looking, and the answer names it; the image itself is not added to this conversation, so ask everything you need about a picture in one call."

const viewImageSchemaJSON = `{"type":"object","properties":{"path":{"type":"string","description":"The image to look at, relative to the workspace or absolute"},"question":{"type":"string","description":"What you need to know about the picture (default: a full description — subject, composition, any text verbatim, and anything malformed). A specific question gets a specific answer."}},"required":["path"],"additionalProperties":false}`

// viewTools is the looking half of the belt: one tool, and only when a model can
// actually look.
//
// STUB(media/belt): tools.go appends this to [Agent.belt], beside the other
// conditional families. The name is the contract between the two lanes.
//
// The seer is resolved HERE, at belt time, because presence is the decision it
// makes — and again at call time, because the person can change the looking slot
// mid-conversation and the resolver is the truth at the moment of use. The
// belt-time answer is what a call falls back to when the resolver has since gone
// quiet: a tool the model has been promised should look with the model it was
// promised rather than refuse.
func (a *Agent) viewTools() []bare.Tool {
	seer := a.visionSeer()
	if seer == "" {
		return nil
	}
	return []bare.Tool{a.viewImageTool(seer)}
}

func (a *Agent) viewImageTool(known string) bare.Tool {
	return bare.Tool{
		Name:        "view_image",
		Description: viewImageDescription,
		Schema:      json.RawMessage(viewImageSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Path     string `json:"path"`
				Question string `json:"question"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			path := strings.TrimSpace(parsed.Path)
			if path == "" {
				return "Invalid arguments: path is required", true, nil
			}
			return a.viewImage(ctx, path, strings.TrimSpace(parsed.Question), known)
		},
	}
}

// viewImage is the tool's body. Every answer below is a TOOL ERROR and never a
// Go error, by tools_search.go's rule: a missing file, a picture nobody's
// endpoint reads, a model having a bad minute are all things the MODEL can act
// on — look somewhere else, ask the person, say so out loud — and none of them
// is a reason to end the turn.
func (a *Agent) viewImage(ctx context.Context, path, question, known string) (string, bool, error) {
	seer := a.visionSeer()
	if seer == "" {
		seer = known
	}

	absolute := resolveInWorkspace(path, a.config.Workspace)
	// EVERY REFUSAL BELOW NAMES THE FILE WHOLE ([picturePathInResult]), not as
	// the caller happened to spell it. A relative path echoed back says which
	// string was passed and not which file was opened, and this line is the only
	// thing a terminal that cannot draw the picture has to go on.
	shown := picturePathInResult(absolute)

	// The type and the size are image.go's own checks, called rather than
	// copied: a picture accepted at this door and refused at the attachment door
	// would be two answers to one question about the same file.
	image := Image{Path: absolute}
	mediaType, err := imageMediaType(image)
	if err != nil {
		return toolReason(err), true, nil
	}
	data, err := imageBytes(image)
	if err != nil {
		return toolReason(err), true, nil
	}

	if question == "" {
		question = viewImageDefaultQuestion
	}
	// The look is bounded here and streamed nowhere ([viewLookWindow]).
	// WithoutStream for the reason the title, the guardian and the compaction
	// summary use it (internal/provider's stream.go): this answer is a TOOL
	// RESULT and not the room's reply, so it must not be typed into the
	// transcript in the chat model's voice — and it is also what puts the call
	// on the client bounded in total rather than on the stream client, which by
	// design carries no total deadline at all (internal/provider's
	// transport.go).
	look, stopLooking := context.WithTimeout(provider.WithoutStream(ctx), viewLookWindow)
	defer stopLooking()
	response, err := a.client.CompleteWithMessages(look, []ai.Message{{
		Role: "user",
		Content: []ai.ContentPart{
			{Type: "text", Text: question},
			{Type: "image_url", ImageURL: &ai.ImageURLData{
				URL: "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data),
			}},
		},
	}}, ai.WithModel(seer))
	// Accounted BEFORE the answer is judged, and folded into the SESSION total
	// rather than the turn's ([Agent.addAuxiliaryUsage]): the look was paid for
	// whether or not it said anything useful, and no turn of the person's ran on
	// that model — the same treatment the title, the compaction summary and a
	// generated picture get.
	if response != nil {
		a.addAuxiliaryUsage(response, seer, 1)
	}
	if err != nil {
		// The window running out is its own answer, and it is told apart from
		// the person's interrupt by the caller's context still being alive: a
		// model that stopped answering is something this model can act on, while
		// "context deadline exceeded" is a Go sentence about nothing it can see.
		if look.Err() != nil && ctx.Err() == nil {
			return seer + " did not answer about " + shown + " within " + taskSpanWord(viewLookWindow) + " — try again, or ask about a smaller picture", true, nil
		}
		// The cause reaches the model verbatim, bounded to a line: "try
		// something else" is only actionable when the refusal names what went
		// wrong, and an API error can carry a whole HTML page.
		return seer + " could not look at " + shown + ": " + oneLineReason(err.Error()), true, nil
	}
	answer := ""
	if response != nil {
		answer = strings.TrimSpace(response.Text())
	}
	if answer == "" {
		return seer + " returned no answer for " + shown, true, nil
	}
	// "seen by <model>" is the same sentence internal/exec's own view_image
	// returns, and it is said out loud for image.go's reason: a second model
	// answering in the first one's voice, silently, would be the surface lying
	// about who looked.
	return "seen by " + seer + ": " + answer, false, nil
}

// toolReason is one of image.go's refusals as a TOOL result. The sentences are
// written for a person reading an error from the surface, so they open
// "session: "; a model reading a tool result is being told about a file, not
// about a Go package.
func toolReason(err error) string {
	return strings.TrimPrefix(err.Error(), "session: ")
}
