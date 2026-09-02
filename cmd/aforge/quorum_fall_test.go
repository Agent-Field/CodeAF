package main

import (
	"context"
	"io"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// fallingVerifierClient falls over on its first call and answers ACCEPT on
// every one after it. Two verifiers run at once, so this is exactly one
// verifier that fell beside one that worked.
type fallingVerifierClient struct {
	calls atomic.Int64
}

func (c *fallingVerifierClient) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	if c.calls.Add(1) == 1 {
		panic("the verifier fell over inside the provider")
	}
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant",
		Content: []ai.ContentPart{{Type: "text", Text: "ACCEPT"}}}}}}, nil
}

func (c *fallingVerifierClient) Model() string { return "deepseek/deepseek-v4-flash" }

// A verifier that falls used to take the whole gate with it. Both verdicts were
// sent from the happy paths inside the goroutine, so a panic — absorbed by
// guard.Go, which is what stops it killing the surface — ended the goroutine
// with nothing on the channel, and the two reads below it waited for a verdict
// that was never coming. The gate did not fail; it stopped, holding the job and
// the person watching it.
//
// The verdict leaves from a defer now, so a fall arrives at the reader as a
// result: a no, because a check that did not finish never saw the deliverable.
func TestAQuorumVerifierThatFallsSaysNoRatherThanHanging(t *testing.T) {
	// guard.Note writes the fault and its stack through the standard logger,
	// which in a test is the test's own output.
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	clients := newMessageClientPool(config.Config{})
	clients.Adopt("deepseek/deepseek-v4-flash", &fallingVerifierClient{})

	type outcome struct {
		accepts int
		rejects string
	}
	done := make(chan outcome, 1)
	go func() {
		accepts, rejects := quorumVerify(context.Background(), config.Config{}, clients,
			"job", "write the report", "here is the report")
		done <- outcome{accepts, rejects}
	}()

	select {
	case got := <-done:
		if got.accepts != 1 {
			t.Fatalf("accepts = %d, want 1: the verifier that fell must not be counted as one that agreed", got.accepts)
		}
		if !strings.Contains(got.rejects, "did not finish") {
			t.Fatalf("rejects = %q, want the reason to say the check did not finish", got.rejects)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("quorumVerify never returned: a verifier that fell sent no verdict and the gate is waiting for one")
	}
}
