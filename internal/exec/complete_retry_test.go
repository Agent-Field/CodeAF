package exec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// retryRecordingCompleter plays back one error per call and records the
// retry-avoid list each call was handed, so a test can assert what the retry
// asked the router to avoid.
type retryRecordingCompleter struct {
	errors []error
	asked  [][]string
}

func (s *retryRecordingCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	s.asked = append(s.asked, provider.RetryAvoidFrom(ctx))
	index := len(s.asked) - 1
	if index < len(s.errors) && s.errors[index] != nil {
		return nil, s.errors[index]
	}
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "done"}}}, FinishReason: "stop"}},
		Usage:   &ai.Usage{PromptTokens: 10, CompletionTokens: 5},
	}, nil
}

func upstreamError(status int, providerName string) error {
	return &provider.APIError{Status: status, Message: "upstream broke", Provider: providerName}
}

// complete is the node call under test, with the fields the loop itself needs
// and nothing else.
func (l *Linear) completeCall(ctx context.Context) (*ai.Response, error) {
	return l.complete(ctx, []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "work"}}}}, nil)
}

func TestARetryAfterANamedUpstreamFailureAvoidsEveryLaneThatFailed(t *testing.T) {
	client := &retryRecordingCompleter{errors: []error{
		upstreamError(502, "Alpha"),
		upstreamError(502, "Beta"),
		upstreamError(502, "Alpha"),
	}}
	linear := &Linear{client: client}
	_, err := linear.completeCall(context.Background())
	if err == nil {
		t.Fatal("every attempt failed, so the call was meant to fail")
	}
	want := [][]string{nil, {"Alpha"}, {"Alpha", "Beta"}}
	if len(client.asked) != len(want) {
		t.Fatalf("attempts = %d, want %d", len(client.asked), len(want))
	}
	for attempt, lanes := range want {
		if got := client.asked[attempt]; len(got) != len(lanes) {
			t.Fatalf("attempt %d avoided %v, want %v", attempt+1, got, lanes)
		}
		for index, lane := range lanes {
			if client.asked[attempt][index] != lane {
				t.Fatalf("attempt %d avoided %v, want %v", attempt+1, client.asked[attempt], lanes)
			}
		}
	}
	// The sentence names what was tried; the failure underneath stays whole.
	if !strings.Contains(err.Error(), "(providers tried: Alpha and Beta)") {
		t.Fatalf("final error = %q, want the tried lanes named", err.Error())
	}
	var relayed *provider.APIError
	if !errors.As(err, &relayed) || relayed.Provider != "Alpha" {
		t.Fatalf("final error = %v, want the last failure still reachable under it", err)
	}
}

func TestARetryAfterACutStreamAvoidsTheLaneTheStreamNamed(t *testing.T) {
	client := &retryRecordingCompleter{errors: []error{
		&provider.StreamCut{Reason: provider.CutOverrun, Provider: "Alpha", Waited: 4 * 60 * 1e9},
		nil,
	}}
	linear := &Linear{client: client}
	if _, err := linear.completeCall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(client.asked) != 2 {
		t.Fatalf("attempts = %d, want the retry to have landed", len(client.asked))
	}
	if lanes := client.asked[1]; len(lanes) != 1 || lanes[0] != "Alpha" {
		t.Fatalf("retry avoided %v, want the lane the cut named", lanes)
	}
}

func TestARetryAfterAFailureThatNamesNobodyAvoidsNothing(t *testing.T) {
	client := &retryRecordingCompleter{errors: []error{
		errors.New("connection reset by peer"),
		errors.New("connection reset by peer"),
		errors.New("connection reset by peer"),
	}}
	linear := &Linear{client: client}
	_, err := linear.completeCall(context.Background())
	if err == nil {
		t.Fatal("every attempt failed, so the call was meant to fail")
	}
	for attempt, lanes := range client.asked {
		if len(lanes) != 0 {
			t.Fatalf("attempt %d avoided %v, want a failure that names nobody to avoid nothing", attempt+1, lanes)
		}
	}
	// The sentence is the one it has always been: nothing learned, nothing said.
	want := fmt.Sprintf("after %d node call attempts: connection reset by peer", nodeCallAttempts)
	if err.Error() != want {
		t.Fatalf("final error = %q, want %q", err.Error(), want)
	}
}

func TestAnUpstream4xxNamesNoLaneToAvoid(t *testing.T) {
	// A 4xx relayed from the upstream is not the retry's evidence: the brief
	// for avoidance is an upstream fault, and a 4xx goes back untouched.
	client := &retryRecordingCompleter{errors: []error{
		upstreamError(400, "Alpha"),
		upstreamError(400, "Beta"),
		upstreamError(400, "Alpha"),
	}}
	linear := &Linear{client: client}
	_, err := linear.completeCall(context.Background())
	if err == nil {
		t.Fatal("every attempt failed, so the call was meant to fail")
	}
	for attempt, lanes := range client.asked {
		if len(lanes) != 0 {
			t.Fatalf("attempt %d avoided %v, want a 4xx to name no lane", attempt+1, lanes)
		}
	}
	if strings.Contains(err.Error(), "providers tried") {
		t.Fatalf("final error = %q, want no lanes named when none were learned", err.Error())
	}
}

func TestTheRetryAvoidListDoesNotOutliveTheCallThatBuiltIt(t *testing.T) {
	client := &retryRecordingCompleter{errors: []error{
		upstreamError(502, "Alpha"),
		upstreamError(502, "Alpha"),
		upstreamError(502, "Alpha"),
		nil,
	}}
	linear := &Linear{client: client}
	if _, err := linear.completeCall(context.Background()); err == nil {
		t.Fatal("every attempt of the first call failed, so it was meant to fail")
	}
	if _, err := linear.completeCall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(client.asked) != 4 {
		t.Fatalf("calls = %d, want three failed attempts and one fresh call", len(client.asked))
	}
	if lanes := client.asked[3]; len(lanes) != 0 {
		t.Fatalf("the next call avoided %v, want the list to have ended with its call", lanes)
	}
}
