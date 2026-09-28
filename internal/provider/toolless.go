package provider

import (
	"context"
	"fmt"
	"sync"
)

// ── A MODEL THAT TAKES NO TOOLS IS NOT SENT ANY ─────────────────────────────
//
// The catalog publishes which request fields a model accepts, and a row that
// lists its parameters without "tools" has said the model takes no tool calls.
// Sending the belt anyway bought, on every turn of a 2026-09-28 conversation
// with microsoft/phi-4: a refused first attempt, a `Retry 1/1: removed tools`
// line nobody could read the reason in, and a request-size check that counted
// ~9k tokens of definitions the model was never going to receive — so the
// conversation was refused as too long at 15.6k when the request that would
// have gone out was 6k, and summarized to make room it already had.
//
// So the definitions are left off before the body is encoded, the size check
// measures what is really sent, and the person is told once, in words about
// what the model can do rather than about request fields. A row that lists no
// parameters has said nothing, and the tools go out as before: the refusal
// ladder is still there for a model the catalog does not know.

// toollessMemo is which models this client has already said it is sending no
// tools to, so the notice is said once per model rather than on every turn.
type toollessMemo struct {
	said sync.Map
}

// publishesNoTools says the catalog knows this model and says it takes no
// tool calls. Unknown is false.
func (c *Client) publishesNoTools(model string) bool {
	if c.config.SupportsParameter == nil {
		return false
	}
	supported, known := c.config.SupportsParameter(model, "tools")
	return known && !supported
}

// leaveOffTools marks this call's body to go without tool definitions when the
// model takes none, and tells the person the first time.
func (c *Client) leaveOffTools(ctx context.Context, model string, knobs callKnobs, carriesTools bool) callKnobs {
	if !carriesTools || knobs.relaxed.has(relaxTools) || !c.publishesNoTools(model) {
		return knobs
	}
	knobs.relaxed |= relaxTools
	if _, said := c.toolless.said.LoadOrStore(normalizeModel(model), true); !said {
		// NEWS ABOUT THE MODEL, NOT NARRATION ABOUT ONE REQUEST: it rides the
		// row-news channel a surface keeps out of the folded work block,
		// because a person who never opens that block still has to learn why
		// the model will not touch their files.
		Emit(ctx, StreamRowNews, toollessNotice(model))
	}
	return knobs
}

// toollessNotice is the one line a person reads about it.
func toollessNotice(model string) string {
	return fmt.Sprintf("%s can't use tools, so it answers without them — it cannot read, search or change files", model)
}

// encodeFailure is what a body that could not be built says. A refusal the
// size check made is already a sentence about the request and travels as it
// is; "marshal request:" in front of it read, on the person's screen, as
// though codeaf had failed to write JSON.
func encodeFailure(err error) error {
	if _, refused := RefusalFrom(err); refused {
		return err
	}
	return fmt.Errorf("marshal request: %w", err)
}
