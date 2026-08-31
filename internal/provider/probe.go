package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE PROBE: THE TRANSPORT HALF ───────────────────────────────────────────
//
// `internal/lane`'s prober decides WHETHER a probe is worth buying — a person
// has started typing, nobody has bought one in the last twenty seconds, and
// somebody is actually waiting. This file is the request it buys: one token, to
// exactly one named lane, with the first-token wait timed and nothing else kept.
//
// IT IS THE LEANEST REQUEST THIS ADAPTER MAKES, ON PURPOSE. It does not go
// through the shaping chain, the endpoint-refusal ladder, the retry backoff or
// the call log, because none of those are measuring what a probe measures: what
// the wait to this lane, from this machine, is RIGHT NOW. A probe that was
// retried would time the retry; a probe that was reshaped would time a
// different request. So the body is written here, in full, and it is the only
// request in this package that is.
//
// The prompt is fixed and about ten tokens long. Fixed because two probes that
// timed different prompts would not be comparable, and short because prefill is
// the one part of a first-token wait that is nobody's endpoint's fault.
//
// WHAT IT COSTS: one prompt of ten tokens and one output token per lane, which
// is about two hundredths of a cent for a pair. What it buys: a measurement
// seconds old instead of a public aggregate half an hour old, and a warm
// connection, so that the real request's first token is not paying for a TLS
// handshake this one already paid.

// probePrompt is the ten tokens every probe sends. It asks for one word so that
// a lane which ignores `max_tokens: 1` still stops almost at once.
const probePrompt = "Reply with the single word ok and nothing else at all."

// probeCeiling is the longest a probe may take before it is abandoned. A lane
// that has not said anything in this long has failed the only question a probe
// asks, and waiting longer costs a goroutine and a connection for an answer
// nobody will use.
const probeCeiling = 10 * time.Second

// probeLane sends one probe to one lane and reports the wait before its first
// token.
//
// An error is a real answer and is returned as one — a lane that refuses, or a
// path that will not carry — but it carries no duration, because what a refusal
// times is the refusal.
func (c *Client) probeLane(ctx context.Context, model, lane string) (time.Duration, error) {
	model, lane = strings.TrimSpace(model), strings.TrimSpace(lane)
	if model == "" || lane == "" {
		return 0, errors.New("probe: a model and a lane are both required")
	}
	body := probeBody(model, lane)
	ctx, stop := context.WithTimeout(ctx, probeCeiling)
	defer stop()
	httpRequest, err := c.newHTTPRequest(ctx, &ai.Request{Model: model}, body, true)
	if err != nil {
		return 0, err
	}
	began := c.clock()
	response, err := c.streamClient().Do(httpRequest)
	if err != nil {
		return 0, fmt.Errorf("probe: %w", err)
	}
	defer func() {
		// The body is drained a little and closed, so the connection goes back
		// to the pool warm — which is half of what the probe was for.
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxErrorPeek))
		_ = response.Body.Close()
	}()
	if response.StatusCode >= 400 {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorPeek))
		return 0, apiError(response.StatusCode, payload)
	}
	if !isEventStream(response.Header.Get("Content-Type")) {
		// A gateway that answered in one piece answered after generating the
		// whole thing, so the wait to here is the answer, not the first token.
		// It is honest to report it and dishonest to call it a first token.
		return 0, errors.New("probe: the endpoint did not stream")
	}
	decoder := newSSEDecoder(response.Body)
	for {
		chunk, err := decoder.DecodeChunk()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, errors.New("probe: the stream ended before a token")
			}
			return 0, fmt.Errorf("probe: %w", err)
		}
		if refusal := streamRefusal(chunk.Error); refusal != nil {
			return 0, refusal
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" || choice.Delta.thinking() || choice.FinishReason != nil {
				return c.clock().Sub(began), nil
			}
		}
	}
}

// streamClient is the client streamed requests ride, falling back to the
// ordinary one for a Client assembled without a separate stream transport.
func (c *Client) streamClient() *http.Client {
	if c.stream != nil {
		return c.stream
	}
	if c.http != nil {
		return c.http
	}
	return http.DefaultClient
}

// InstallLaneProber wires this client's transport into the registry's prober,
// and reports whether it did.
//
// waiting is the caller's own answer to "is anybody waiting on this model right
// now" — the λ of the design, which only a surface can know. Nil is "always",
// which is the right reading for a headless run that probes at all.
//
// IT REFUSES A CLIENT THAT IS NOT TALKING TO A ROUTER. `provider.only` is a
// router's dialect and a probe without it measures whatever endpoint happened
// to answer, which is a number worse than none.
func InstallLaneProber(c *Client, waiting lanes.ProbeGate) bool {
	if c == nil || !c.isOpenRouter() {
		return false
	}
	gate := func(model string) bool {
		if c.routing() == RoutingOff {
			// A session that asked not to be steered is not measured either,
			// and it is certainly not billed for a measurement.
			return false
		}
		if sharedLimiter.pacing(c.clock()) {
			// The pool is already backing off a rate limit. A probe now is one
			// more request into a queue that is the reason the last one was
			// refused.
			return false
		}
		return waiting == nil || waiting(model)
	}
	lanes.Default().SetProber(lanes.NewProber(lanes.ProberConfig{
		Send: c.probeLane,
		Gate: gate,
		Now:  c.clock,
	}))
	return true
}

// pacing reports whether the shared pool is backing off right now: a slot has
// been cut and not yet healed, or a named wait is still running.
//
// It lives here rather than in limiter.go because it exists for exactly one
// caller — the probe's gate — and it is a read of state that file already
// keeps.
func (l *adaptiveLimiter) pacing(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.capacity < limiterCeiling || now.Before(l.cutUntil)
}

// probeBody is the exact bytes a probe sends, and the one place the shape of a
// probe is stated.
//
// ONLY, AND NO FALLBACK. A probe the router was free to serve from somewhere
// else would be a measurement of a lane nobody asked about, written into the
// belief of the lane that was asked.
func probeBody(model, lane string) []byte {
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"stream":     true,
		"max_tokens": 1,
		"messages": []map[string]any{
			{"role": "user", "content": probePrompt},
		},
		"provider": map[string]any{
			"only":            []string{lane},
			"allow_fallbacks": false,
		},
	})
	return bytes.TrimSpace(body)
}
