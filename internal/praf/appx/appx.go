// Package appx declares the agent-capability seam the review's reasoners,
// gates and orchestrator are written against. pr-af received the AgentField
// SDK's `Agent` and called `.Harness(...)`, `.AI(...)` and `.Note(...)` on it;
// here the codeaf side answers those verbs (internal/praf's app.go, over the
// agent sessions internal/agentsession runs), so the seam names no SDK agent
// or harness type.
package appx

import (
	"context"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// HarnessOptions is what one agent session is told about where it works.
//
// Cwd is the checkout under review, which the session reads; an empty Cwd is
// a call that needs no file, and codeaf's session still reads only the run's
// checkout. Label names the session on the review's page ("review
// dimension", "evidence"); it changes nothing about the run.
type HarnessOptions struct {
	Cwd   string
	Label string
}

// HarnessResult is the outcome of one agent session, carrying the fields the
// review reads off it.
//
// A session that HAPPENED and failed is reported with IsError and
// ErrorMessage and a nil Go error; a Go error means the session could not be
// made at all. Parsed is the dest pointer once it holds a schema-valid
// object, nil when nothing validated. CostUSD is a pointer because "no cost
// reported" is not "free".
type HarnessResult struct {
	Result       string
	Parsed       any
	IsError      bool
	ErrorMessage string
	NumTurns     int
	DurationMS   int64
	CostUSD      *float64
}

// Harnesser is the agent-session seam.
type Harnesser interface {
	Harness(ctx context.Context, prompt string, schema map[string]any, dest any, opts HarnessOptions) (*HarnessResult, error)
}

// AIer is the single structured call seam. The options and the response keep
// the SDK `ai` package's shapes, which codeaf speaks everywhere it talks to a
// model.
type AIer interface {
	AI(ctx context.Context, prompt string, opts ...ai.Option) (*ai.Response, error)
}

// Noter is a progress line for whoever is watching the review, never
// something the pipeline reads back.
type Noter interface {
	Note(ctx context.Context, message string, tags ...string)
}

// App is the union the orchestrator is written against.
type App interface {
	Harnesser
	AIer
	Noter
}
