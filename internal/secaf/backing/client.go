// Package backing is what sec-af's agents run on inside codeaf: the
// implementation of [appx.App] that the audit is handed in place of the
// AgentField node it was written for.
//
// sec-af was a node on a control plane. Its plain model calls went to a router
// with a key it held, its agent sessions were a coding-agent binary it spawned
// once per call, and every call between its own reasoners was an HTTP round
// trip. Here all three are codeaf's: a model call goes to the run's model API
// (internal/provider/modelapi), the only road to a model a program codeaf
// carries has; an agent session is a small read-only loop in this process
// ([Session]) whose every turn is one call on that same road; and a call
// between reasoners stays in the process (internal/secaf/audit's local calls).
//
// IT CHANGES NOTHING IN THE FOLDER IT READS. The audit promises the person
// their repository back exactly as it was, so the tools an agent session is
// given read and search and never write, and there is no shell to write with.
package backing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

// Client is the run's model API as sec-af's calls reach it: one base URL, one
// token, and the ceiling's refusal remembered once it has been heard.
type Client struct {
	base  string
	token string
	http  *http.Client
	// stopped is set by the first refusal that ends the run — the dollar
	// ceiling, a key the service refused — so the dozens of calls still queued
	// behind it learn the same answer without asking a server that already
	// gave it.
	stopped atomic.Pointer[error]
	// pause is how long a retry waits, per attempt; a test shortens it.
	pause func(attempt int) time.Duration
}

// NewClient opens the run's model API. It refuses an API with no address or no
// token, because a program with neither has no road to a model at all.
func NewClient(api delegate.ModelAPI) (*Client, error) {
	if strings.TrimSpace(api.BaseURL) == "" || strings.TrimSpace(api.Token) == "" {
		return nil, errors.New("sec-af runs only inside codeaf, which serves its models; this process was handed no model API")
	}
	return &Client{
		base: api.BaseURL, token: api.Token,
		// NO CLIENT TIMEOUT: a thinking model can be quiet for minutes, and the
		// call's own context (the session's wall, the run's stop) is what ends
		// it.
		http:  &http.Client{},
		pause: func(attempt int) time.Duration { return time.Duration(2<<attempt) * time.Second },
	}, nil
}

// Request is one model call: who is asking (the thread its turns are kept
// under on the task's page), what, and in what shape the answer must come.
type Request struct {
	Model    string
	Thread   string
	Messages []ai.Message
	Tools    []ai.ToolDefinition
	// ResponseFormat asks for a JSON answer of one schema; nil is free text.
	ResponseFormat *ai.ResponseFormat
}

// wireRequest is the body on the wire: the chat-completions shape the model
// API reads, with the thread as its prompt_cache_key.
type wireRequest struct {
	Model          string              `json:"model,omitempty"`
	Messages       []ai.Message        `json:"messages"`
	Tools          []ai.ToolDefinition `json:"tools,omitempty"`
	ToolChoice     string              `json:"tool_choice,omitempty"`
	ResponseFormat *ai.ResponseFormat  `json:"response_format,omitempty"`
	PromptCacheKey string              `json:"prompt_cache_key,omitempty"`
}

// ErrCeiling is the run's dollar ceiling, reached: the model API made no call.
// It ends the audit's model work everywhere at once, and what the audit found
// before it is still reported.
var ErrCeiling = errors.New("the run's dollar ceiling is reached, so codeaf made no call")

// RefusedError is an answer that ends the run's model work: the ceiling (402)
// or a service that refused the key (401, 403). Nothing that follows it can
// succeed, so nothing is retried and the client answers it to every call after.
//
// WHICH OF THE TWO IT IS IS DECIDED ONCE, where the answer arrives, and kept
// as a fact: nothing downstream reads the status to decide again
// (internal/taxonomy's law).
type RefusedError struct {
	// Ceiling is the run's dollar ceiling; otherwise the key was refused.
	Ceiling bool
	Code    int
	Message string
}

func (e *RefusedError) Error() string {
	if e.Ceiling {
		return ErrCeiling.Error() + ": " + e.Message
	}
	return fmt.Sprintf("the model service refused the run (%d): %s", e.Code, e.Message)
}

// Is makes the ceiling match [ErrCeiling].
func (e *RefusedError) Is(target error) bool { return target == ErrCeiling && e.Ceiling }

// retries is how many times a call that failed on the way is sent again. The
// model API already walks codeaf's own ladder of services for every call, so
// this covers only what that cannot — a connection dropped on this machine, a
// moment the API was busy — and stays small.
const retries = 2

// Complete sends one call and answers the model's reply.
func (c *Client) Complete(ctx context.Context, request Request) (*ai.Response, error) {
	if stopped := c.stopped.Load(); stopped != nil {
		return nil, *stopped
	}
	wire := wireRequest{
		Model: request.Model, Messages: request.Messages, Tools: request.Tools,
		ResponseFormat: request.ResponseFormat, PromptCacheKey: request.Thread,
	}
	if len(wire.Tools) > 0 {
		wire.ToolChoice = "auto"
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("encode the call: %w", err)
	}
	var last error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.pause(attempt)):
			}
		}
		response, err, again := c.send(ctx, body)
		if err == nil {
			return response, nil
		}
		var refused *RefusedError
		if errors.As(err, &refused) {
			c.stopped.CompareAndSwap(nil, &err)
			return nil, err
		}
		if !again || ctx.Err() != nil {
			return nil, err
		}
		last = err
	}
	return nil, last
}

// send is one attempt: the reply, or the error and whether another attempt
// could answer differently.
func (c *Client) send(ctx context.Context, body []byte) (*ai.Response, error, bool) {
	call, err := http.NewRequestWithContext(ctx, http.MethodPost, modelapi.ChatURL(c.base), bytes.NewReader(body))
	if err != nil {
		return nil, err, false
	}
	call.Header.Set("Content-Type", "application/json")
	call.Header.Set("Authorization", "Bearer "+c.token)
	answer, err := c.http.Do(call)
	if err != nil {
		return nil, fmt.Errorf("reach the model API: %w", err), true
	}
	defer answer.Body.Close()
	raw, err := io.ReadAll(answer.Body)
	if err != nil {
		return nil, fmt.Errorf("read the model's reply: %w", err), true
	}
	if answer.StatusCode >= 400 {
		message := apiMessage(raw)
		switch answer.StatusCode {
		case http.StatusPaymentRequired:
			return nil, &RefusedError{Ceiling: true, Code: answer.StatusCode, Message: message}, false
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, &RefusedError{Code: answer.StatusCode, Message: message}, false
		case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusInternalServerError:
			return nil, fmt.Errorf("the model API answered %d: %s", answer.StatusCode, message), true
		}
		return nil, fmt.Errorf("the model API answered %d: %s", answer.StatusCode, message), false
	}
	var response ai.Response
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("the model's reply does not parse: %w", err), true
	}
	if len(response.Choices) == 0 {
		return nil, errors.New("the model's reply has no choices"), true
	}
	return &response, nil, false
}

// apiMessage is the one sentence an error body carries, or the body itself.
func apiMessage(raw []byte) string {
	var shaped ai.ErrorResponse
	if json.Unmarshal(raw, &shaped) == nil && strings.TrimSpace(shaped.Error.Message) != "" {
		return strings.TrimSpace(shaped.Error.Message)
	}
	text := strings.TrimSpace(string(raw))
	if len(text) > 400 {
		text = text[:400] + "…"
	}
	return text
}

// costOf is a reply's metered price, zero when it carries none.
func costOf(response *ai.Response) float64 {
	if response == nil || response.Usage == nil || response.Usage.Cost == nil {
		return 0
	}
	return *response.Usage.Cost
}
