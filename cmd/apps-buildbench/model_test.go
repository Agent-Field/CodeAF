package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/provider"
)

type modelFunc func(context.Context, svc.ModelCall) (svc.ModelReply, error)

func (f modelFunc) Complete(ctx context.Context, call svc.ModelCall) (svc.ModelReply, error) {
	return f(ctx, call)
}

func TestCompleterKeepsReplyAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &modelCompleter{model: modelFunc(func(got context.Context, call svc.ModelCall) (svc.ModelReply, error) {
		if got != ctx || call.Model != "test/model" || len(call.Messages) != 1 || call.Messages[0] != (svc.ModelMessage{Role: "user", Text: "build an app"}) {
			t.Fatalf("wrong Builder call: context=%v call=%+v", got, call)
		}
		return svc.ModelReply{Text: "reply", Cost: 0.01}, got.Err()
	})}
	out, err := c.complete(ctx, "test/model", "build an app")
	if !errors.Is(err, context.Canceled) || out.Text != "reply" || out.Cost != 0.01 || out.Elapsed <= 0 {
		t.Fatalf("reply or cancellation lost: %+v, %v", out, err)
	}
}

func TestCompleterMissingKeyExplainsSetup(t *testing.T) {
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	_, err := newCompleter().complete(context.Background(), defaultModel, "build an app")
	if !errors.Is(err, provider.ErrNoAPIKey) {
		t.Fatalf("missing key must fail before a request: %v", err)
	}
	if !strings.Contains(err.Error(), "OPENROUTER_API_KEY") || !strings.Contains(err.Error(), "codeaf profile") {
		t.Fatalf("missing setup guidance: %v", err)
	}
}
