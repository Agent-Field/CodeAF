package session

import (
	"context"
	"errors"
	"strings"

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
	// TurnErrorOther is anything else.
	TurnErrorOther = "other"
)

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
	return TurnErrorOther, 0, ""
}
