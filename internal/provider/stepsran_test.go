package provider

import (
	"context"
	"testing"
)

// Steps a model's own program already ran ride the stream as one event each
// chunk, in order with its words, and never become calls to run.
func TestStepsAlreadyRanArriveAsTheirOwnEventAndAreNotCallsToRun(t *testing.T) {
	steps := `[{"id":"claude_code_t1","name":"bash","arguments":{"command":"ls"},"output":"a.go","took_ms":3}]`
	client := streamClientForTest(t, sseHandler(
		`{"id":"one","choices":[{"index":0,"delta":{"role":"assistant","reasoning":"▸ Bash ls\n"}}]}`,
		`{"id":"one","choices":[{"index":0,"delta":{"codeaf_steps":`+steps+`}}]}`,
		`{"id":"one","choices":[{"index":0,"delta":{"content":"There is one file."},"finish_reason":"stop"}]}`,
	))
	var ran []string
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		if event.Kind == StreamStepsRan {
			ran = append(ran, event.Delta)
		}
	})
	response, err := client.CompleteWithMessages(ctx, userMessages("what is here"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != steps {
		t.Fatalf("steps events = %q", ran)
	}
	if calls := response.ToolCalls(); len(calls) != 0 {
		t.Fatalf("a step already run came back as a call to run: %+v", calls)
	}
	if response.Text() != "There is one file." {
		t.Fatalf("answer = %q", response.Text())
	}
}
