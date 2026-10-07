// Package appx declares the agent-capability seam every reasoner and phase of
// a carried program depends on: sec's first, which it was written for. The original Python code received the SDK
// `Agent` (or the `AgentRouter` proxying to it) and called `.harness(...)`,
// `.ai(...)`, `.note(...)` and `.call(...)` on it; the Go code receives an App.
//
// Inside codeaf there is no control plane and no external coding-agent CLI, so
// the seam no longer names any SDK agent type. The harness options and result
// are declared here, carrying only the fields the program actually sets and
// reads, and the codeaf side supplies the implementation: Harness runs
// codeaf's own agent loop, AI goes through codeaf's model API, and Call is
// answered in process by the reasoner registry (internal/secaf/audit).
package appx

import (
	"context"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// HarnessOptions is what one harness run is told about where it works.
//
// Cwd is the directory the agent runs in — for most agents a scratch
// directory the agent writes its structured output into — and ProjectDir is
// the repository the run reads, which the agent reads. Those two are the only
// options any agent in the program sets: the model, turn budget and provider were
// process-wide configuration of the external CLI this seam used to drive, and
// are now whatever the codeaf implementation of Harness decides.
type HarnessOptions struct {
	Cwd        string
	ProjectDir string
	// Label is the agent the run is for, in the words its errors already use
	// ("Hunt location scanner", "DataFlowTracer"). It changes nothing about
	// the run; codeaf names the session by it on the program's page.
	Label string
}

// HarnessResult is the outcome of one harness run, mirroring the fields a
// program reads off it.
//
// A run that HAPPENED and failed is reported with IsError and ErrorMessage and
// a nil Go error; a Go error from Harness means the run could not be made at
// all. The callers treat the two differently (harnessx.Extract, the gate
// retry loop), so an implementation must keep that split.
//
// Parsed is the dest pointer the caller handed to Harness once it holds a
// schema-valid object, and nil when the run produced nothing that validated;
// the callers then fall back to parsing Result, the agent's raw final text.
//
// CostUSD is a pointer because "no cost reported" is not "free": the cost
// trackers skip a nil and add a reported zero, so an implementation that cannot
// price a run leaves it nil rather than writing 0.
type HarnessResult struct {
	Result       string
	Parsed       any
	IsError      bool
	ErrorMessage string
	NumTurns     int
	DurationMS   int64
	CostUSD      *float64
}

// Harnesser is the `app.harness(...)` seam.
//
// schema is the JSON schema of the object the run must produce (nil for a
// free-text run), and dest is a pointer the implementation decodes that object
// into before returning it as HarnessResult.Parsed.
type Harnesser interface {
	Harness(ctx context.Context, prompt string, schema map[string]any, dest any, opts HarnessOptions) (*HarnessResult, error)
}

// AIer is the `app.ai(...)` seam. The option list and the response keep the
// SDK `ai` package's shapes, which codeaf already speaks everywhere it talks to
// a model.
type AIer interface {
	AI(ctx context.Context, prompt string, opts ...ai.Option) (*ai.Response, error)
}

// Noter is the `app.note(msg, tags=[...])` seam: a progress line for whoever
// is watching the program, never something the algorithm reads back.
type Noter interface {
	Note(ctx context.Context, message string, tags ...string)
}

// Caller is the `app.call(f"{NODE_ID}.x", **kwargs)` seam — the reasoner
// invocation that used to travel through the control plane as a tracked child
// execution. It returns the target reasoner's result map, already decoded the
// way a JSON hop decodes it, and an error on any failure.
type Caller interface {
	Call(ctx context.Context, target string, input map[string]any) (map[string]any, error)
}

// App is the union the reasoners, phases and orchestrator are written
// against.
type App interface {
	Harnesser
	AIer
	Noter
	Caller
}
