package session

// toolask.go — THE ONE DOOR A HAND ON THE BELT ASKS A MODEL THROUGH.
//
// Three hands on this belt answer by asking another model: `view_image` looks at
// a picture, and `read` senses an image, a recording or a video through the
// ladder in tools_sense.go. Each of them was written on its own, and each of
// them got the same four things wrong in its own way — which is the definition
// of a shape that wants one mechanism rather than three copies.
//
// WHAT THE FOUR ARE, each measured rather than supposed (the model-call census
// of 2026-09-10 and the reading taken on 2026-09-11):
//
//  1. THE CALL WAS ANONYMOUS. A row in calls.jsonl is named by its tag, and a
//     tag is either set by the caller or derived from a routing slot the
//     planning packages open — and a tool opens none. So `view_image`'s twenty-
//     three calls yesterday landed with `tag:null`, and the census could not
//     attribute 2,830 finished calls in ten days. Everything through this door
//     is tagged `tool:<name>`, and carries the node when it is a task's, which
//     is the same pair of facts loop.go writes for a turn.
//  2. NOBODY WAS TOLD IT WAS HAPPENING. A look is seven seconds at the median
//     and twenty at the tail, and for all of it the surface drew a tool row with
//     a name on it and nothing else. Every ask says what it is doing, in words —
//     `looking at shot.png`, `listening to memo.m4a` — for as long as it lasts
//     ([Agent.tellPhase] keeps saying it).
//  3. THE ANSWER LOOKED UNBOUNDED IN LENGTH, and the answer to that is NOT a
//     ceiling on this request. See [toolAskSendsNoOutputCeiling], which is the
//     one piece of reasoning in this file that ends in "so we do nothing".
//  4. THE WAIT WAS A SECOND BUDGET. `view_image` carried its own ten-minute
//     window, spelled as [providerTimeout], beside the deadline the dispatcher
//     already builds from the call's role (internal/provider's dispatch.go,
//     docs/design/recovery/DESIGN.md §4 — ONE deadline, derived from the role's
//     patience column). The window here is that same derivation, held at this
//     door so that a call which never reaches the dispatcher still ends.
//
// AND THE ROLE IS DECLARED, NOT DESCRIBED. What a hand's question IS — somebody
// is waiting on it, its text is never drawn, it never streams, it is one shot —
// is a row in internal/lane's table ([lane.RoleTool]), and every number this
// file needs is read off that row. There is no duration written down here.

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// toolAskRole is what a hand asking a model IS. Every number this file needs
// comes out of this role's row in internal/lane's table, so a change of mind
// about how patient one of these is reaches every tool without anybody editing
// a tool.

// toolAskWindow is the longest ONE ask may take before the tool answers without
// it, and it is [lane.Role.GiveUp] for the role above rather than a number typed
// here: the recovery design replaced eleven hand-written budgets with that one
// derivation, and a tool that kept its own would be the twelfth.
//
// IT IS WHAT MAKES THESE TOOLS UNABLE TO HANG. A call that starts always ends
// journaled, and a completion was the one path out of these functions that could
// return neither a result nor an error — the turn's context carries no deadline
// of its own, so a provider that accepted the request and then went quiet left
// the row running for the rest of the session (the surface draws a journaled
// call with no result as still-running, internal/tui3's room.go, and it is right
// to).
//
// It is a var only so the tests can run the clock out in a millisecond, the way
// connect.go's own waits are. NOTHING IN A BUILD WRITES IT.
var toolAskWindow = lane.RoleTool.GiveUp()

// toolAsk is one hand's question to one model.
//
// It is a struct rather than eight arguments because every field is a decision a
// caller has to make deliberately, and a positional list of eight is a list
// somebody eventually gets out of order.
type toolAsk struct {
	// tool is the hand's REGISTERED NAME — `read`, `view_image` — and it is what
	// the model-call log is tagged with. It is the registered name and not a
	// prettier one so that a row in the log and a row on the screen name the
	// same thing.
	tool string
	// doing is what the person reads while this lasts, in a person's words and
	// naming the file: `looking at shot.png`. Empty draws no phase, which is for
	// a caller that is already drawing one.
	doing string
	// model is who is being asked, and prompt and part are the question and the
	// thing it is about.
	model  string
	prompt string
	part   ai.ContentPart
}

// askModel sends one question and returns the answer.
//
// NOTHING OF THIS CONVERSATION GOES WITH IT: no system prompt, no transcript, no
// tools. The model is being asked what is in a file, not being made a second
// agent with a second memory of this session — and the file's bytes never enter
// the transcript, so the chat model does not carry a megabyte of base64 on every
// step of every turn after this one.
//
// The usage folds into the SESSION total and never the turn's
// ([Agent.addAuxiliaryUsage]), the same treatment the title, the compaction
// summary and read_document's rungs get, and for the same reason: the person
// pays for it, but no turn of theirs ran on that model.
func (a *Agent) askModel(ctx context.Context, question toolAsk) (string, error) {
	if doing := strings.TrimSpace(question.doing); doing != "" {
		a.tellPhase(provider.PhaseRunning, doing, time.Now())
		defer a.endPhase()
	}

	// UNSTREAMED, for the reason the title, the guardian and the compaction
	// summary use it (internal/provider's stream.go): this answer is a TOOL
	// RESULT and not the room's reply, so it must not be typed into the
	// transcript in the chat model's voice — and it is also what puts the call on
	// the client bounded in total rather than on the stream client, which by
	// design carries no total deadline at all (internal/provider's transport.go).
	asked := provider.WithRole(provider.WithoutStream(ctx), lane.RoleTool)
	if a.config.taskID != 0 {
		asked = provider.WithCallNode(asked, strconv.FormatUint(a.config.taskID, 10))
	}
	asked, stop := context.WithTimeout(asked, toolAskWindow)
	defer stop()

	// NO OUTPUT CEILING TRAVELS WITH IT ([toolAskSendsNoOutputCeiling]).
	// THE TOOL IS THE PURPOSE, carried to the door rather than stamped here, so
	// that this package has one place that spells a tag (clientdoor.go).
	response, err := a.completeWithModel(asked, callPurpose(toolCallTag(question.tool)), []ai.Message{{
		Role:    "user",
		Content: []ai.ContentPart{{Type: "text", Text: question.prompt}, question.part},
	}}, question.model)
	// Accounted BEFORE the answer is judged: the ask was paid for whether or not
	// it said anything useful.
	if response != nil {
		a.addAuxiliaryUsage(response, question.model, 1)
	}
	if err != nil {
		// The window running out is its own answer, and it is told apart from the
		// person's interrupt by the CALLER's context still being alive: a model
		// that stopped answering is something the asking model can act on, while
		// "context deadline exceeded" is a Go sentence about nothing it can see.
		if asked.Err() != nil && ctx.Err() == nil {
			return "", errToolAskRanOut
		}
		return "", err
	}
	answer := ""
	if response != nil {
		answer = strings.TrimSpace(response.Text())
	}
	if answer == "" {
		return "", errToolAskSaidNothing
	}
	return answer, nil
}

// The two ways an ask comes to nothing that the caller words for itself, because
// the sentence a model reads names the FILE and the model that was asked, and
// this door knows neither in the caller's own vocabulary.
const (
	errToolAskRanOut      = errStr("did not answer in time")
	errToolAskSaidNothing = errStr("returned no answer")
)

// toolCallTag is how a hand's call is named in the model-call log. One prefix,
// spelled once, so every tool-made call sorts together and none of them can be
// mistaken for a turn's own.
func toolCallTag(tool string) string {
	if tool = strings.TrimSpace(tool); tool == "" {
		return "tool"
	}
	return "tool:" + tool
}

// toolAskSendsNoOutputCeiling is why this door does NOT put a `max_tokens` on a
// hand's request, written down because "bound the answer's length" is the
// obvious next thing to reach for and it is wrong here.
//
// THE ONLY PRINCIPLED CEILING IS THE ONE THE BELT ALREADY IMPOSES, and it does
// not bind. Every one of these answers is paged through pi's law to
// [Agent.resultCaps] — 50KB on a frontier window, which is about twelve and a
// half thousand tokens — while the measured answers run 588 to 1,256. A ceiling
// an order of magnitude above the distribution buys nothing.
//
// AND SENDING IT WOULD COST THE SERVING SET. `max_tokens` is not only a cap on
// the reply: internal/lane's gate rules a machine OUT when its published
// `MaxOut` is below what the request asked for (frontier.go's `capable`). A
// twelve-and-a-half-thousand-token ask would quietly strike every vision machine
// that publishes less than that, on a tool whose whole problem was being slow —
// so the ceiling would thin the set that makes it fast in exchange for cutting a
// paragraph nobody was going to read.
//
// A NUMBER FROM THE MEASUREMENT WOULD BE A MAGIC NUMBER. "1,500 tokens, because
// that is yesterday's ninety-fifth percentile" is a constant nobody can derive
// tomorrow, and it would cut exactly the long transcript this ladder exists to
// produce. What actually bounds the wait is the ROLE ([toolAskWindow]) and what
// makes it visible is the phase; the length is the lane layer's business,
// because the lane layer is the only place that knows what each machine can do.
const toolAskSendsNoOutputCeiling = "the belt's own cap does not bind, and sending it would thin the serving set"
