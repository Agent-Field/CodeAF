package praf

// The App the review runs on: pr-af's three verbs answered by codeaf, on the
// agent sessions internal/agentsession runs over the run's model API.
//
// THE SESSIONS ARE THE ONES sec RUNS ON, BECAUSE THEY ARE THE SAME THING.
// pr-af's reviewers were a coding-agent binary per call, and so were sec-af's
// agents; both became the same small read-only loop on the model API (four
// tools, an answer checked against its schema, a second chance when it does
// not parse, the run's 402 and 429 heard once for every call), told only what
// work it is part of.

import (
	"context"
	"strings"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/agentsession"
	secappx "github.com/Agent-Field/codeaf/internal/agentsession/appx"
	"github.com/Agent-Field/codeaf/internal/praf/appx"
)

// sessionApp is what a review needs of the sessions: sec's App, narrowed so a
// test can stand in for it.
type sessionApp interface {
	Harness(ctx context.Context, prompt string, schema map[string]any, dest any, opts secappx.HarnessOptions) (*secappx.HarnessResult, error)
	AI(ctx context.Context, prompt string, opts ...ai.Option) (*ai.Response, error)
	Spent() (cost float64, sessions, calls int)
}

var _ sessionApp = (*agentsession.App)(nil)

// app is the review's appx.App over the sessions, keeping count of every call
// and how many failed. The pipeline turns a failed reviewer into "no
// findings" rather than failing, so a run whose every reviewer failed would
// otherwise end as a clean review of nothing; the run reads these counts to
// tell the two apart.
type app struct {
	sessions sessionApp

	mu    sync.Mutex
	calls callStats
}

// callStats counts a run's model calls and their failures: all of them, and
// the reviewers' agent sessions alone.
type callStats struct {
	Total, Failed               int
	SessionTotal, SessionFailed int
	FirstErr                    string
}

var _ appx.App = (*app)(nil)

func (a *app) stats() callStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

func (a *app) record(session bool, failure string) {
	a.mu.Lock()
	a.calls.Total++
	if session {
		a.calls.SessionTotal++
	}
	if failure != "" {
		a.calls.Failed++
		if session {
			a.calls.SessionFailed++
		}
		if a.calls.FirstErr == "" {
			a.calls.FirstErr = strings.TrimSpace(failure)
		}
	}
	a.mu.Unlock()
}

// Harness runs one reviewer as an agent session over the checkout.
func (a *app) Harness(ctx context.Context, prompt string, schema map[string]any, dest any, opts appx.HarnessOptions) (*appx.HarnessResult, error) {
	result, err := a.sessions.Harness(ctx, prompt, schema, dest, secappx.HarnessOptions{
		Cwd: opts.Cwd, ProjectDir: opts.Cwd, Label: opts.Label,
	})
	switch {
	case err != nil:
		a.record(true, err.Error())
		return nil, err
	case result == nil:
		a.record(true, "the session gave no result")
		return nil, nil
	case result.IsError:
		a.record(true, result.ErrorMessage)
	default:
		a.record(true, "")
	}
	return &appx.HarnessResult{
		Result: result.Result, Parsed: result.Parsed, IsError: result.IsError,
		ErrorMessage: result.ErrorMessage, NumTurns: result.NumTurns,
		DurationMS: result.DurationMS, CostUSD: result.CostUSD,
	}, nil
}

// AI runs one single structured call: the intake, coverage, polish and merge
// gates.
func (a *app) AI(ctx context.Context, prompt string, opts ...ai.Option) (*ai.Response, error) {
	response, err := a.sessions.AI(ctx, prompt, opts...)
	failure := ""
	if err != nil {
		failure = err.Error()
	}
	a.record(false, failure)
	return response, err
}

// Note is dropped: the pipeline's notes were progress lines for the control
// plane's page, and codeaf's page is drawn from the run's stages and steps.
func (a *app) Note(context.Context, string, ...string) {}
