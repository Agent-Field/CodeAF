package provider

import (
	"context"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Every completion this package returns already knows how it ended: the
// non-streamed path decodes finish_reason straight off the wire, and the
// streamed assembly in completeWithMessagesStreaming copies the last one it saw
// onto the same field. Every consumer then threw it away by calling
// response.Text(). The two functions below are what a consumer needs to stop
// throwing it away, and nothing more — no state, no locks, no goroutines. The
// reason rides the value that is already being returned.

// FinishReason reports the provider's own word for how a completion ended, or
// "" when it never said. It reads the first choice because that is the only one
// the adapter assembles: the streaming path builds exactly one, and single
// responses are requested with n=1.
func FinishReason(response *ai.Response) string {
	if response == nil || len(response.Choices) == 0 {
		return ""
	}
	return response.Choices[0].FinishReason
}

// EmptyAtCeiling is the one answer shape that says more room would change the
// result: no text, a "length" finish, and the whole ceiling spent. It is
// deliberately narrower than "blank" — a refusal or a cut stream also has no
// text, but neither of those is cured by a larger max_tokens. It is the
// signature of a thinking pass that ate the answer, and it is defined once so
// the adapter's recovery, the reflex's, and the bill that journals a paid call
// all call the same thing empty.
func EmptyAtCeiling(response *ai.Response, ceiling int) bool {
	if response == nil || strings.TrimSpace(response.Text()) != "" ||
		!strings.EqualFold(strings.TrimSpace(FinishReason(response)), "length") {
		return false
	}
	return response.Usage == nil || response.Usage.CompletionTokens >= ceiling
}

// Streaming reports whether a call made on this context is served over the
// event stream. It is what tells an empty finish reason apart: no terminal
// frame on a stream is a dropped connection, while a single response that
// omitted the field is just an endpoint being terse.
func Streaming(ctx context.Context) bool {
	return streamObserverFrom(ctx) != nil
}
