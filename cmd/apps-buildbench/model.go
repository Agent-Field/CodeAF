package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// completion is one model answer with what it cost and how long it took.
type completion struct {
	Text    string
	Cost    float64
	Elapsed time.Duration
}

type completer interface {
	complete(ctx context.Context, model, prompt string) (completion, error)
}

type modelCompleter struct{ model svc.Model }

// newCompleter uses the shipped Builder's model door so key resolution,
// reasoning policy, retries and billing stay the same as the product's.
func newCompleter() *modelCompleter {
	return &modelCompleter{model: builder.NewModel(svc.Config{}, nil)}
}

func (c *modelCompleter) complete(ctx context.Context, model, prompt string) (completion, error) {
	start := time.Now()
	reply, err := c.model.Complete(ctx, svc.ModelCall{
		Model: model, Messages: []svc.ModelMessage{{Role: "user", Text: prompt}},
	})
	out := completion{Text: reply.Text, Cost: reply.Cost, Elapsed: time.Since(start)}
	if errors.Is(err, provider.ErrNoAPIKey) {
		return out, fmt.Errorf("no model API key: set OPENROUTER_API_KEY or save a key in your codeaf profile: %w", err)
	}
	return out, err
}
