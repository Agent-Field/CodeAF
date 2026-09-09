package provider

import (
	"context"
	"net/http"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// terminalResponseError reads the terminal marker before either transport
// teaches the cache and speed ledgers. An HTTP 200 only accepted the request;
// finish_reason:error says its generation failed, even without an error object.
// Like streamRefusal, it normalizes an in-band failure without a status to 502
// so the existing bounded recovery policy can handle it as an upstream fault.
func (c *Client) terminalResponseError(ctx context.Context, request *ai.Request, knobs callKnobs, response *ai.Response, served string) error {
	if response == nil || len(response.Choices) == 0 ||
		!strings.EqualFold(strings.TrimSpace(response.Choices[0].FinishReason), "error") {
		return nil
	}
	err := &APIError{
		Status:   http.StatusBadGateway,
		Message:  "provider ended the response with finish_reason=error",
		Provider: served,
	}
	c.notePrefsFromAnswer(ctx, served)
	c.refuseUpstream(request, knobs, err)
	c.noteLaneOutcome(c.modelFor(request), served, "error", false)
	c.releaseEndpoint(ctx, c.modelFor(request))
	noteServed(ctx, served, "")
	return err
}
