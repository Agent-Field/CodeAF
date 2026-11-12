package appwire

import (
	"context"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/session"
)

const FeatureReplySuggestion = "agents.suggest_reply"

type ReplySuggestionParams struct {
	Agent   string `json:"agent"`
	Context string `json:"context"`
}

type ReplySuggestion struct {
	Text string `json:"text"`
}

type ReplySuggester interface {
	SuggestReply(context.Context, ReplySuggestionParams) (ReplySuggestion, error)
}

func withReplySuggestionFeature(h Hello, e Engine) Hello {
	if _, ok := e.(ReplySuggester); ok {
		return withFeature(h, FeatureReplySuggestion)
	}
	return h
}

func init() {
	methods[FeatureReplySuggestion] = typed(func(e Engine, p ReplySuggestionParams) (*ReplySuggestion, error) {
		door, ok := e.(ReplySuggester)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "reply suggestions are unavailable")
		}
		if p.Agent == "" || len(p.Context) == 0 || len(p.Context) > session.ReplySuggestionContextBytes || !utf8.ValidString(p.Context) {
			return nil, Errorf(CodeBadParams, "a conversation and bounded reply context are required")
		}
		ctx, cancel := context.WithTimeout(context.Background(), session.ReplySuggestionWait)
		defer cancel()
		reply, err := door.SuggestReply(ctx, p)
		return &reply, err
	})
}
