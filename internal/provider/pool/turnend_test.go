package pool

import (
	"context"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// TurnEnd is the one line every structuring caller needs at its posting seam.
// It has to be total: a nil response, an empty choices list and a reason nobody
// has seen before all have to answer without panicking and without inventing a
// mark for a turn that finished.
func TestTurnEndIsTotalOverWhatAProviderCanReturn(t *testing.T) {
	streamed := provider.WithStreamObserver(context.Background(), func(provider.StreamEvent) {})
	plain := context.Background()

	respond := func(reason string) *ai.Response {
		return &ai.Response{Choices: []ai.Choice{{FinishReason: reason}}}
	}

	cases := []struct {
		name     string
		ctx      context.Context
		response *ai.Response
		want     *store.EndedPart
	}{
		{"capped", plain, respond("length"), &store.EndedPart{How: store.EndLength, FinishReason: "length"}},
		{"finished", plain, respond("stop"), nil},
		{"tool call", streamed, respond("tool_calls"), nil},
		{"dropped stream", streamed, respond(""), &store.EndedPart{How: store.EndStreamDrop}},
		{"terse endpoint", plain, respond(""), nil},
		{"no choices", streamed, &ai.Response{}, &store.EndedPart{How: store.EndStreamDrop}},
		{"nil response", plain, nil, nil},
	}
	for _, testCase := range cases {
		got := TurnEnd(testCase.ctx, testCase.response)
		switch {
		case testCase.want == nil && got != nil:
			t.Fatalf("%s: invented %+v", testCase.name, got)
		case testCase.want != nil && got == nil:
			t.Fatalf("%s: dropped the ending, want %+v", testCase.name, testCase.want)
		case testCase.want != nil && *got != *testCase.want:
			t.Fatalf("%s: got %+v, want %+v", testCase.name, got, testCase.want)
		}
	}
}
