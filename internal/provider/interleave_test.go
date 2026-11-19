package provider

import (
	"context"
	"strings"
	"testing"
)

// REASONING THAT ARRIVES BETWEEN TWO RUNS OF ANSWER NEVER JOINS THE ANSWER.
//
// qloop run10's flyer reply put a model's self-check in front of the person
// ("Lounge: last message already there is unverified. Let me only cite what I
// saw. Links: default. Done.Fixed. …"), and the run-together "Done.Fixed." is
// the shape two channels concatenated would leave. The router's accounting for
// that call (142 completion tokens, 4 of them reasoning, the 4 the engine
// recorded as reasoning) says the endpoint sent the self-check as content, so
// the engine was not at fault. This pins why it could not have been: an
// endpoint that interleaves its channels — answer, then working, then answer
// again, which interleaved-thinking models are allowed to do — gets exactly its
// content back as the reply, through the real client and the real decoder, and
// every word of the working on the reasoning channel and nowhere else.
func TestReasoningBetweenTwoRunsOfAnswerNeverJoinsTheAnswer(t *testing.T) {
	for _, field := range []string{"reasoning", "reasoning_content", "reasoning_text"} {
		t.Run(field, func(t *testing.T) {
			client := streamClientForTest(t, sseHandler(
				`{"id":"one","choices":[{"index":0,"delta":{"role":"assistant","`+field+`":"Confirmed. "}}]}`,
				`{"id":"one","choices":[{"index":0,"delta":{"content":"Fixed — the date "}}]}`,
				`{"id":"one","choices":[{"index":0,"delta":{"content":"now reads 14 March."}}]}`,
				`{"id":"one","choices":[{"index":0,"delta":{"`+field+`":"Links: default. "}}]}`,
				`{"id":"one","choices":[{"index":0,"delta":{"`+field+`":"Done."}}]}`,
				`{"id":"one","choices":[{"index":0,"delta":{"content":" The PDF is re-exported."},"finish_reason":"stop"}]}`,
			))

			var reasoning, answer strings.Builder
			ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
				switch event.Kind {
				case StreamReasoning:
					reasoning.WriteString(event.Delta)
				case StreamDelta:
					answer.WriteString(event.Delta)
				}
			})
			response, err := client.CompleteWithMessages(WithProseAnswer(ctx), userMessages("fix the flyer date"))
			if err != nil {
				t.Fatal(err)
			}

			const wantAnswer = "Fixed — the date now reads 14 March. The PDF is re-exported."
			if got := response.Text(); got != wantAnswer {
				t.Fatalf("the reply kept %q, want only the content channel %q", got, wantAnswer)
			}
			if got := answer.String(); got != wantAnswer {
				t.Fatalf("the surface drew %q as the answer, want %q", got, wantAnswer)
			}
			if got := reasoning.String(); got != "Confirmed. Links: default. Done." {
				t.Fatalf("the working drawn was %q, want every reasoning delta and nothing else", got)
			}
		})
	}
}
