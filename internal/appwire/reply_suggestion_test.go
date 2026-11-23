package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type replyEngine struct {
	sendEngine
	calls int
}

func (e *replyEngine) SuggestReply(ctx context.Context, p ReplySuggestionParams) (ReplySuggestion, error) {
	e.calls++
	if _, ok := ctx.Deadline(); !ok {
		panic("missing suggestion deadline")
	}
	return ReplySuggestion{Text: "Explain the second option."}, nil
}
func TestReplySuggestionDispatchIsOptionalAndBounded(t *testing.T) {
	call := methods[FeatureReplySuggestion]
	if _, err := call.call(&sendEngine{}, json.RawMessage(`{"agent":"a","context":"x"}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatal(err)
	}
	e := &replyEngine{}
	if h := withReplySuggestionFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureReplySuggestion {
		t.Fatal(h)
	}
	for _, p := range []string{`{}`, `{"agent":"a","context":""}`, `{"agent":"a","context":1}`, `{"agent":"a","context":"` + strings.Repeat("x", 12289) + `"}`} {
		if _, err := call.call(e, json.RawMessage(p)); codeOf(err) != CodeBadParams {
			t.Fatal("accepted invalid reply context", err)
		}
	}
	if _, err := call.call(e, json.RawMessage(`{"agent":"a","context":"The plan has two options."}`)); err != nil || e.calls != 1 {
		t.Fatal(err, e.calls)
	}
}
