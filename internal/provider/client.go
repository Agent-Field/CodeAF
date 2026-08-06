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

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Config configures the adapter. It is deliberately the same shape the
// AgentField SDK client takes, plus the two resolvers that let the adapter
// decide a request's economics without ever performing I/O on the hot path.
type Config struct {
	APIKey      string
	BaseURL     string
	Model       string
	Temperature float64
	MaxTokens   int
	Timeout     time.Duration
	SiteURL     string
	SiteName    string

	// SupportsParameter answers "does this model accept this request field?"
	// from data already in memory. It must not block or perform I/O; an unknown
	// answer is reported by returning known=false, never by waiting.
	SupportsParameter func(model, parameter string) (bool, bool)

	// HTTPClient is optional and exists for deterministic tests.
	HTTPClient *http.Client
}

// Client is Aforge's model adapter. It satisfies the harness's LoopClient and
// TextStreamer interfaces structurally, so nothing above it knows a wire
// format, and it owns the only outbound provider path in the process.
type Client struct {
	config Config
	http   *http.Client
	// base is the pinned AgentField client, retained for the surfaces Aforge
	// does not drive itself. It never sees a request the adapter has shaped.
	base *ai.Client
}

// NewClient builds the adapter. It performs no network request.
func NewClient(config Config) (*Client, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("provider API key is required")
	}
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, errors.New("provider base URL is required")
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, errors.New("provider model is required")
	}
	if config.Timeout < 0 {
		return nil, errors.New("provider timeout must not be negative")
	}
	base, err := ai.NewClient(&ai.Config{
		APIKey:      config.APIKey,
		BaseURL:     config.BaseURL,
		Model:       config.Model,
		Temperature: config.Temperature,
		MaxTokens:   config.MaxTokens,
		Timeout:     config.Timeout,
		SiteURL:     config.SiteURL,
		SiteName:    config.SiteName,
	})
	if err != nil {
		return nil, err
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: config.Timeout}
	}
	return &Client{config: config, http: httpClient, base: base}, nil
}

// Model reports the adapter's default model slug.
func (c *Client) Model() string { return c.config.Model }

// OwnsToolLoop tells the harness that this client's transport is harness-owned,
// so the bounded safe loop — horizon compaction, stall detection, the tool
// membrane — drives it rather than a provider-side loop.
func (c *Client) OwnsToolLoop() bool { return true }

// ExecuteToolCallLoop exists only to satisfy the harness's LoopClient
// interface. Aforge always drives its own loop against this adapter, so the
// delegation is a contract detail rather than a live path.
func (c *Client) ExecuteToolCallLoop(
	ctx context.Context,
	messages []ai.Message,
	tools []ai.ToolDefinition,
	config ai.ToolCallConfig,
	call ai.CallFunc,
	options ...ai.Option,
) (*ai.Response, *ai.ToolCallTrace, error) {
	return c.base.ExecuteToolCallLoop(ctx, messages, tools, config, call, options...)
}

type callKnobs struct {
	cacheKey string
	effort   effortRequest
}

func knobsFrom(ctx context.Context) callKnobs {
	return callKnobs{cacheKey: CacheKeyFrom(ctx), effort: effortFrom(ctx)}
}

func (c *Client) newRequest(messages []ai.Message, options []ai.Option) (*ai.Request, error) {
	request := &ai.Request{
		Messages: messages,
		Model:    c.config.Model,
	}
	// Temperature is always sent: an operator who configured zero wants zero,
	// not whatever the endpoint happens to default to.
	temperature := c.config.Temperature
	request.Temperature = &temperature
	if c.config.MaxTokens > 0 {
		maxTokens := c.config.MaxTokens
		request.MaxTokens = &maxTokens
	}
	for _, option := range options {
		if err := option(request); err != nil {
			return nil, fmt.Errorf("apply option: %w", err)
		}
	}
	return request, nil
}

// CompleteWithMessages performs one completion. Interactive callers may attach
// a stream observer while retaining the accumulated response contract.
func (c *Client) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	if observer := streamObserverFrom(ctx); observer != nil {
		return c.completeWithMessagesStreaming(ctx, observer, messages, options...)
	}
	request, err := c.newRequest(messages, options)
	if err != nil {
		return nil, err
	}
	body, err := c.encodeRequest(request, knobsFrom(ctx))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	httpResponse, err := c.send(ctx, request, body, false)
	if err != nil {
		return nil, err
	}
	defer httpResponse.Body.Close()

	payload, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if httpResponse.StatusCode >= 400 {
		return nil, apiError(httpResponse.StatusCode, payload)
	}
	var response ai.Response
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &response, nil
}

// completeWithMessagesStreaming preserves the completion interface while
// exposing each text delta to an interactive observer. The accumulated
// response is the same shape callers already parse after the stream closes.
func (c *Client) completeWithMessagesStreaming(
	ctx context.Context,
	observer StreamObserver,
	messages []ai.Message,
	options ...ai.Option,
) (*ai.Response, error) {
	request, err := c.newRequest(messages, append(append([]ai.Option(nil), options...), ai.WithStream()))
	if err != nil {
		return nil, err
	}
	request.Stream = true
	body, err := c.encodeRequest(request, knobsFrom(ctx))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	httpResponse, err := c.send(ctx, request, body, true)
	if err != nil {
		return nil, err
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode >= 400 {
		payload, _ := io.ReadAll(httpResponse.Body)
		return nil, apiError(httpResponse.StatusCode, payload)
	}

	observer(StreamEvent{Kind: StreamStarted})
	finished := false
	defer func() {
		if !finished {
			observer(StreamEvent{Kind: StreamFailed})
		}
	}()

	response := &ai.Response{Model: request.Model}
	var content strings.Builder
	finishReason := ""
	decoder := ai.NewSSEDecoder(httpResponse.Body)
	for {
		chunk, decodeErr := decoder.Decode()
		if decodeErr != nil {
			if errors.Is(decodeErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode stream: %w", decodeErr)
		}
		if response.ID == "" {
			response.ID = chunk.ID
			response.Object = chunk.Object
			response.Created = chunk.Created
		}
		if chunk.Model != "" {
			response.Model = chunk.Model
		}
		if chunk.Usage != nil {
			response.Usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				continue
			}
			if choice.Delta.Content != "" {
				content.WriteString(choice.Delta.Content)
				observer(StreamEvent{Kind: StreamDelta, Delta: choice.Delta.Content})
			}
			if choice.FinishReason != nil {
				finishReason = *choice.FinishReason
			}
		}
	}
	response.Choices = []ai.Choice{{
		Index: 0,
		Message: ai.Message{
			Role:    "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: content.String()}},
		},
		FinishReason: finishReason,
	}}
	finished = true
	observer(StreamEvent{Kind: StreamFinished})
	return response, nil
}

// StreamComplete performs one streaming completion over a single user prompt.
// It mirrors the SDK's channel contract exactly so the harness's stream pump is
// unchanged.
func (c *Client) StreamComplete(ctx context.Context, prompt string, options ...ai.Option) (<-chan ai.StreamChunk, <-chan error) {
	chunks := make(chan ai.StreamChunk)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		messages := []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: prompt}}}}
		request, err := c.newRequest(messages, append(append([]ai.Option(nil), options...), ai.WithStream()))
		if err != nil {
			errs <- err
			return
		}
		request.Stream = true
		body, err := c.encodeRequest(request, knobsFrom(ctx))
		if err != nil {
			errs <- fmt.Errorf("marshal request: %w", err)
			return
		}
		// Retrying happens entirely before the first byte of the stream is
		// handed over, so a reconnect can never duplicate delivered chunks.
		httpResponse, err := c.send(ctx, request, body, true)
		if err != nil {
			errs <- err
			return
		}
		defer httpResponse.Body.Close()
		if httpResponse.StatusCode >= 400 {
			payload, _ := io.ReadAll(httpResponse.Body)
			errs <- apiError(httpResponse.StatusCode, payload)
			return
		}

		decoder := ai.NewSSEDecoder(httpResponse.Body)
		for {
			chunk, err := decoder.Decode()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					errs <- fmt.Errorf("decode stream: %w", err)
				}
				return
			}
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			case chunks <- chunk:
			}
		}
	}()

	return chunks, errs
}

func (c *Client) newHTTPRequest(ctx context.Context, request *ai.Request, body []byte, stream bool) (*http.Request, error) {
	endpoint := strings.TrimSuffix(strings.TrimSpace(c.config.BaseURL), "/") + "/chat/completions"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	apiKey := c.config.APIKey
	if override := strings.TrimSpace(request.APIKeyOverride); override != "" {
		apiKey = override
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
	if stream {
		httpRequest.Header.Set("Accept", "text/event-stream")
	}
	if c.isOpenRouter() {
		if c.config.SiteURL != "" {
			httpRequest.Header.Set("HTTP-Referer", c.config.SiteURL)
		}
		if c.config.SiteName != "" {
			httpRequest.Header.Set("X-OpenRouter-Title", c.config.SiteName)
			httpRequest.Header.Set("X-Title", c.config.SiteName)
		}
	}
	// The header half of cache affinity. Routers that ignore the body field
	// still honour a session header, and a router that honours neither is
	// unharmed by an extra one.
	if key := CacheKeyFrom(ctx); key != "" {
		httpRequest.Header.Set("X-Session-Affinity", key)
	}
	return httpRequest, nil
}

func (c *Client) isOpenRouter() bool {
	return strings.Contains(strings.ToLower(c.config.BaseURL), "openrouter.ai") ||
		strings.HasPrefix(strings.ToLower(c.config.Model), "openrouter/")
}

// adaptiveCompletionTimeout accounts for reasoning and output tokens being
// generated serially. It scales at one second per 64 requested tokens, keeps
// the old five-minute timeout as its floor, and caps wedged calls at 15 minutes.
func adaptiveCompletionTimeout(maxTokens int, configuredFloor time.Duration) time.Duration {
	const (
		floor   = 5 * time.Minute
		ceiling = 15 * time.Minute
	)
	if configuredFloor < floor {
		configuredFloor = floor
	}
	scaled := time.Duration(maxTokens/64) * time.Second
	if scaled < configuredFloor {
		scaled = configuredFloor
	}
	if scaled > ceiling {
		return ceiling
	}
	return scaled
}

// apiError keeps the SDK's exact error phrasing. The harness's provider-error
// taxonomy recovers a status code from that text, so changing the wording here
// would silently disable rate-limit and transient-failure retries.
func apiError(status int, payload []byte) error {
	var decoded ai.ErrorResponse
	if err := json.Unmarshal(payload, &decoded); err == nil && strings.TrimSpace(decoded.Error.Message) != "" {
		return fmt.Errorf("API error (%d): %s", status, decoded.Error.Message)
	}
	return fmt.Errorf("API error (%d): %s", status, string(payload))
}
