package main

// THE TRANSPORT, deliberately the smallest one that tells the truth.
//
// internal/provider is aforge's own OpenRouter path and it is the right thing
// for a chat turn: attribution, the refusal ladder, the velocity ledger, the
// stream. None of that is what this rig is measuring. What this rig measures is
// whether a MODEL can architect a sub-harness from a plain goal, so the
// transport wants to be a flat, auditable request/response with a visible token
// bill and no belt of its own — a wrapper whose bugs would be indistinguishable
// from the model's.
//
// TODO-consolidate: when internal/subharness grows a model-backed Env of its
// own (exec_model.go, ModelExec), this file and execmodel.go are what it should
// be made of, and this rig should call it instead.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

const openrouterURL = "https://openrouter.ai/api/v1/chat/completions"

// message is one turn on the wire. Content is a plain string rather than the
// parts array because nothing here sends an image, and ToolCalls/ToolCallID are
// what make a tool round-trip legal: an assistant turn that asked for a tool and
// a tool turn that answers it must both be in the conversation or the next
// request is rejected.
type message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// toolDef is one tool as the wire declares it.
type toolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type chatRequest struct {
	Model      string    `json:"model"`
	Messages   []message `json:"messages"`
	MaxTokens  int       `json:"max_tokens,omitempty"`
	Tools      []toolDef `json:"tools,omitempty"`
	ToolChoice string    `json:"tool_choice,omitempty"`
	// Usage asks the endpoint to price the call it just served. It is not
	// optional here: the adaptive run meters a fuel tank in DOLLARS, and a tank
	// fed zeroes is a tank that never empties.
	Usage *usageAsk `json:"usage,omitempty"`
}

type usageAsk struct {
	Include bool `json:"include"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   string     `json:"content"`
			Reasoning string     `json:"reasoning"`
			ToolCalls []toolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		Cost             float64 `json:"cost"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// chatClient is one model at one endpoint, with the bill kept as it goes. The
// bill is a field and not a return value because every stage of this rig adds to
// the same one, and a run's cost is the number the report needs.
type chatClient struct {
	key   string
	model string
	http  *http.Client
	// url is the endpoint, a field only so a test can stand a server in front
	// of this client. Nothing configures it: a rig that talked to somewhere
	// else would be measuring somewhere else.
	url string

	// The ledger, behind a lock because the ADAPTIVE RUN is the first caller
	// here with several nodes in flight at once. The design stages are still
	// serial and pay nothing for it.
	mu     sync.Mutex
	calls  int
	tokens int
	cost   float64
}

func newChatClient(key, model string) *chatClient {
	return &chatClient{
		key:   key,
		model: model,
		url:   openrouterURL,
		// Long, because a designer turn on this model spends a thousand-odd
		// reasoning tokens before it writes the first brace.
		http: &http.Client{Timeout: 6 * time.Minute},
	}
}

// reply is one completion, condensed to what a caller here reads. Cost and
// Tokens are THIS call's, not the running total: a run with several nodes in
// flight cannot price one of them by reading the ledger before and after.
type reply struct {
	Text      string
	Reasoning string
	ToolCalls []toolCall
	Finish    string
	Cost      float64
	Tokens    int
}

// complete makes one request, retrying the failures that are worth retrying: a
// rate limit, a 5xx, a transport hiccup. Everything else comes back as itself —
// a 400 is a bug in the body and another identical body will not fix it.
func (c *chatClient) complete(ctx context.Context, req chatRequest) (reply, error) {
	req.Model = c.model
	req.Usage = &usageAsk{Include: true}
	body, err := json.Marshal(req)
	if err != nil {
		return reply{}, err
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return reply{}, ctx.Err()
			case <-time.After(time.Duration(attempt*attempt) * 2 * time.Second):
			}
		}
		out, retry, err := c.once(ctx, body)
		if err == nil {
			return out, nil
		}
		last = err
		if !retry {
			return reply{}, err
		}
	}
	return reply{}, fmt.Errorf("after 3 attempts: %w", last)
}

func (c *chatClient) once(ctx context.Context, body []byte) (reply, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return reply{}, false, err
	}
	request.Header.Set("Authorization", "Bearer "+c.key)
	request.Header.Set("Content-Type", "application/json")
	// The app, from the one place that spells it. This rig used to set a title
	// of its own and no referer at all, which bought it nothing — a title
	// without a referer creates no app page, so the rig's tokens were
	// attributed to nobody — while leaving a second app name in the tree that
	// would have RENAMED the real app page the day anything paired the two.
	provider.ApplyAttribution(request.Header)

	response, err := c.http.Do(request)
	if err != nil {
		return reply{}, true, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return reply{}, true, err
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return reply{}, true, fmt.Errorf("openrouter %s: %s", response.Status, clip(string(payload), 400))
	}
	if response.StatusCode != http.StatusOK {
		return reply{}, false, fmt.Errorf("openrouter %s: %s", response.Status, clip(string(payload), 600))
	}
	var decoded chatResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return reply{}, false, fmt.Errorf("decode: %w: %s", err, clip(string(payload), 400))
	}
	if decoded.Error != nil {
		return reply{}, false, fmt.Errorf("openrouter: %s", decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 {
		return reply{}, true, errors.New("openrouter: a response with no choices")
	}
	tokens := decoded.Usage.PromptTokens + decoded.Usage.CompletionTokens
	c.mu.Lock()
	c.calls++
	c.tokens += tokens
	c.cost += decoded.Usage.Cost
	c.mu.Unlock()

	choice := decoded.Choices[0]
	out := reply{
		Text:      strings.TrimSpace(choice.Message.Content),
		Reasoning: strings.TrimSpace(choice.Message.Reasoning),
		ToolCalls: choice.Message.ToolCalls,
		Finish:    choice.FinishReason,
		Cost:      decoded.Usage.Cost,
		Tokens:    tokens,
	}
	// A REASONING MODEL THAT RAN OUT OF ROOM ANSWERS WITH NOTHING. The content
	// is null and the whole completion went into the thinking, which reads
	// identically to a refusal unless it is named — so it is named here, once,
	// where the retry above can still act on it.
	if out.Text == "" && len(out.ToolCalls) == 0 {
		if choice.FinishReason == "length" {
			return reply{}, true, fmt.Errorf("the model spent its whole budget thinking (%d completion tokens, finish=length)", decoded.Usage.CompletionTokens)
		}
		return reply{}, true, fmt.Errorf("an empty answer (finish=%s)", choice.FinishReason)
	}
	return out, false, nil
}

// ask is the one-shot form: a system prompt, a user prompt, an answer.
func (c *chatClient) ask(ctx context.Context, system, user string, maxTokens int) (string, error) {
	out, err := c.complete(ctx, chatRequest{
		Messages: []message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		MaxTokens: maxTokens,
	})
	if err != nil {
		return "", err
	}
	return out.Text, nil
}

// spent is the ledger, read atomically.
func (c *chatClient) spent() (calls, tokens int, cost float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.tokens, c.cost
}

func (c *chatClient) bill() string {
	calls, tokens, cost := c.spent()
	return fmt.Sprintf("%d calls · %d tokens · $%.4f", calls, tokens, cost)
}

func clip(text string, at int) string {
	text = strings.TrimSpace(text)
	if len(text) <= at {
		return text
	}
	return text[:at] + "…"
}
