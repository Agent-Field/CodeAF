package session

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// The kinds a turn's ending error is sorted into for a surface that reads it
// across a wire ([ClassifyTurnError]).
const (
	// TurnErrorProvider is the model's service refusing after the engine's
	// retry ladder gave up on it; the status and words are the service's own.
	TurnErrorProvider = "provider"
	// TurnErrorBudget is the conversation's spend rail reached ([ErrSpendRail]).
	TurnErrorBudget = "budget"
	// TurnErrorStopped is a turn that ended because it was cancelled.
	TurnErrorStopped = "stopped"
	// TurnErrorOffline is a turn that never reached the model's service at
	// all: the connection was refused, the name did not resolve, or the dial
	// could not leave this computer. It is its own kind because a person reads
	// it as "paused until the network is back", never as the work failing.
	TurnErrorOffline = "offline"
	// TurnErrorOther is anything else.
	TurnErrorOther = "other"
)

// TurnOfflineWords is what an offline turn says, in a person's words. The
// transport's own sentence ("after 0 retries: after 8 attempts: … connection
// refused") is machinery and contradicts itself, so it is never what is shown.
const TurnOfflineWords = "this computer can't reach the model right now"

// ClassifyTurnError sorts the error an EventError carries. For a provider
// refusal it answers the HTTP status and the provider's own words: the
// upstream's raw answer when the router passed one on, its message otherwise.
func ClassifyTurnError(err error) (kind string, status int, words string) {
	if err == nil {
		return "", 0, ""
	}
	switch {
	case errors.Is(err, ErrSpendRail):
		return TurnErrorBudget, 0, ""
	case errors.Is(err, context.Canceled):
		return TurnErrorStopped, 0, ""
	}
	var refused *provider.APIError
	if errors.As(err, &refused) {
		words = strings.TrimSpace(refused.Raw)
		if words == "" {
			words = strings.TrimSpace(refused.Message)
		}
		return TurnErrorProvider, refused.Status, words
	}
	if turnUnreachable(err) {
		return TurnErrorOffline, 0, TurnOfflineWords
	}
	return TurnErrorOther, 0, ""
}

// turnUnreachable says the request never left this computer: a refused or
// unreachable dial, or a name that would not resolve. A reset or a timeout
// mid-answer is NOT offline — the service was reached and the words for that
// stay the transport's own.
func turnUnreachable(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETDOWN) {
		return true
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return true
	}
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial" && !op.Timeout()
}
