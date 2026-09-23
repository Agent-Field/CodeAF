package codexauth

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/trace"
)

var refreshLocks struct {
	sync.Mutex
	byPath map[string]*sync.Mutex
}

const (
	refreshExchangeTimeout = 30 * time.Second
	refreshLockTimeout     = 60 * time.Second
	refreshLockCadence     = 25 * time.Millisecond
)

var errRefreshAlreadyRunning = errors.New("another codeaf is refreshing the codex sign-in and has not finished")

// Client returns the refreshing and translating client for one profile.
func Client(profileDir string) *http.Client { return ClientWithOptions(profileDir, Options{}) }

// ClientWithOptions returns the same client with deterministic test edges.
func ClientWithOptions(profileDir string, options Options) *http.Client {
	transport := Translate(profileDir, options)
	timeout := time.Duration(0)
	if options.HTTPClient != nil {
		timeout = options.HTTPClient.Timeout
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

// Translate authenticates every backend request and translates the provider
// client's chat-completions path into the ChatGPT Responses API.
func Translate(profileDir string, options Options) http.RoundTripper {
	base := http.DefaultTransport
	if options.HTTPClient != nil && options.HTTPClient.Transport != nil {
		base = options.HTTPClient.Transport
	}
	session := strings.TrimSpace(options.SessionID)
	if session == "" {
		session = uuid.NewString()
	}
	return &transport{
		profileDir: profileDir, options: options, base: base,
		sessionID: session,
	}
}

type transport struct {
	profileDir string
	options    Options
	base       http.RoundTripper
	sessionID  string
}

func (t *transport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, errors.New("codex request is missing")
	}
	originalBody, err := readRequestBody(request)
	if err != nil {
		return nil, err
	}
	translatedBody := originalBody
	wantsStream := true
	isTurn := request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/chat/completions")
	isCatalogList := request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/models") && request.URL.Query().Get("client_version") == ""
	if isTurn {
		translatedBody, wantsStream, err = translateRequest(t.profileDir, originalBody)
		if err != nil {
			return nil, err
		}
	}
	tokens, err := t.fresh(request.Context(), false, "")
	if err != nil {
		return nil, err
	}
	send := func(tokens Tokens) (*http.Response, error) {
		out := request.Clone(request.Context())
		out.Header = request.Header.Clone()
		out.Header.Del("X-Title")
		out.Header.Del("HTTP-Referer")
		out.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
		out.Header.Set("chatgpt-account-id", tokens.AccountID)
		out.Header.Set("originator", Originator)
		out.Header.Set("User-Agent", provider.DirectUserAgent)
		if isTurn {
			out.Header.Set("OpenAI-Beta", "responses=experimental")
			out.Header.Set("Accept", "text/event-stream")
			out.URL = cloneURL(request.URL)
			backend, parseErr := url.Parse(t.options.backend() + "/responses")
			if parseErr != nil {
				return nil, parseErr
			}
			out.URL = backend
			out.Host = backend.Host
			out.Header.Set("session_id", sessionIDFor(translatedBody, request.Header, t.sessionID))
		} else if isCatalogList {
			out.Header.Set("Accept", "application/json")
			out.URL = cloneURL(request.URL)
			backend, parseErr := url.Parse(t.options.backend() + "/models?client_version=" + clientVersion)
			if parseErr != nil {
				return nil, parseErr
			}
			out.URL = backend
			out.Host = backend.Host
		}
		out.Body = io.NopCloser(bytes.NewReader(translatedBody))
		out.ContentLength = int64(len(translatedBody))
		return t.base.RoundTrip(out)
	}
	response, err := send(tokens)
	if err != nil {
		return nil, scrubTransportError(err)
	}
	if response.StatusCode == http.StatusUnauthorized {
		_ = response.Body.Close()
		refreshed, refreshErr := t.fresh(request.Context(), true, tokens.AccessToken)
		if refreshErr != nil {
			return nil, refreshErr
		}
		response, err = send(refreshed)
		if err != nil {
			return nil, scrubTransportError(err)
		}
	}
	response.Status = string(trace.Scrub([]byte(response.Status)))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response, err = scrubHTTPResponse(response)
		if err != nil {
			return nil, scrubTransportError(err)
		}
	}
	if isTurn && response.StatusCode >= 400 && codexQuotaStatus(response.StatusCode) {
		response = quotaResponse(response)
	}
	if isCatalogList && response.StatusCode >= 200 && response.StatusCode < 300 {
		return t.translateCatalogResponse(response)
	}
	if !isTurn || response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return scrubHTTPResponse(response)
		}
		return response, nil
	}
	return translateResponse(response, wantsStream)
}

// translateCatalogResponse turns the account backend's model list into the
// OpenAI-shaped data array the shared catalog reads. The same pass refreshes
// the reasoning-level cache used by later Responses requests.
func (t *transport) translateCatalogResponse(response *http.Response) (*http.Response, error) {
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	raw = trace.Scrub(raw)
	var answer struct {
		Models []struct {
			Slug          string `json:"slug"`
			DisplayName   string `json:"display_name"`
			Visibility    string `json:"visibility"`
			ContextWindow int    `json:"context_window"`
			Levels        []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, err
	}
	data := make([]map[string]any, 0, len(answer.Models))
	remembered := make([]Model, 0, len(answer.Models))
	for _, row := range answer.Models {
		id := strings.TrimSpace(row.Slug)
		if row.Visibility != "list" || id == "" {
			continue
		}
		data = append(data, map[string]any{
			"id": id, "name": strings.TrimSpace(row.DisplayName), "context_length": row.ContextWindow,
		})
		model := Model{ID: id}
		for _, level := range row.Levels {
			if effort := strings.TrimSpace(level.Effort); effort != "" {
				model.ReasoningLevels = append(model.ReasoningLevels, effort)
			}
		}
		remembered = append(remembered, model)
	}
	if len(data) == 0 {
		return nil, errors.New("codex model list carried no visible models")
	}
	if err := saveModels(t.profileDir, remembered); err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	response.Header.Set("Content-Type", "application/json")
	return response, nil
}

func codexQuotaStatus(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusNotFound || status == http.StatusTooManyRequests
}

func quotaResponse(response *http.Response) *http.Response {
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	_ = response.Body.Close()
	if err != nil {
		response.Body = io.NopCloser(bytes.NewReader(raw))
		return response
	}
	if !quotaPayload(raw) {
		response.Body = io.NopCloser(bytes.NewReader(raw))
		response.ContentLength = int64(len(raw))
		return response
	}
	// THE REWRITE CHANGES THE STATUS AS WELL AS THE WORDS. A 402 is the one
	// status internal/paymentrefusal already reads as "this account cannot
	// pay" on every service, so the session ends the turn the way it ends a
	// spent Z.ai window, and no vendor-wide word list has to learn the
	// backend's spellings — a generic "rate_limit_exceeded" would otherwise
	// turn every other vendor's passing rate limit into a terminal refusal.
	body, _ := json.Marshal(map[string]any{"error": map[string]any{"message": QuotaWords, "code": response.StatusCode}})
	response.StatusCode = http.StatusPaymentRequired
	response.Status = "402 Payment Required"
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	response.Header.Set("Content-Type", "application/json")
	return response
}

func quotaPayload(raw []byte) bool {
	lower := strings.ToLower(string(raw))
	for _, phrase := range []string{"usage_limit_reached", "usage_not_included", "rate_limit_exceeded", "usage limit"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// scrubHTTPResponse removes every credential already registered by Load before
// a response can reach the provider client, its call log, or a session journal.
// It is used on bounded control and error bodies; successful turn streams are
// scrubbed event by event in [mapResponseEvents].
func scrubHTTPResponse(response *http.Response) (*http.Response, error) {
	if response == nil || response.Body == nil {
		return response, nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	raw = trace.Scrub(raw)
	response.Body = io.NopCloser(bytes.NewReader(raw))
	response.ContentLength = int64(len(raw))
	response.Status = string(trace.Scrub([]byte(response.Status)))
	return response, nil
}

type scrubbedTransportError struct {
	err  error
	said string
}

func (e *scrubbedTransportError) Error() string { return e.said }
func (e *scrubbedTransportError) Unwrap() error { return e.err }

func scrubTransportError(err error) error {
	if err == nil {
		return nil
	}
	said := string(trace.Scrub([]byte(err.Error())))
	if said == err.Error() {
		return err
	}
	return &scrubbedTransportError{err: err, said: said}
}

func readRequestBody(request *http.Request) ([]byte, error) {
	if request.Body == nil {
		return nil, nil
	}
	raw, err := io.ReadAll(request.Body)
	_ = request.Body.Close()
	if err != nil {
		return nil, err
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	return raw, nil
}

func cloneURL(value *url.URL) *url.URL {
	copy := *value
	return &copy
}

func sessionIDFor(body []byte, headers http.Header, fallback string) string {
	var request map[string]any
	if json.Unmarshal(body, &request) == nil {
		if key, _ := request["prompt_cache_key"].(string); strings.TrimSpace(key) != "" {
			return key
		}
	}
	if key := strings.TrimSpace(headers.Get("X-Session-Affinity")); key != "" {
		return key
	}
	return fallback
}

func lockFor(path string) *sync.Mutex {
	refreshLocks.Lock()
	defer refreshLocks.Unlock()
	if refreshLocks.byPath == nil {
		refreshLocks.byPath = make(map[string]*sync.Mutex)
	}
	if refreshLocks.byPath[path] == nil {
		refreshLocks.byPath[path] = &sync.Mutex{}
	}
	return refreshLocks.byPath[path]
}

func (t *transport) fresh(ctx context.Context, force bool, rejected string) (Tokens, error) {
	mutex := lockFor(Path(t.profileDir))
	lockCtx, cancelLock := context.WithTimeout(ctx, refreshLockTimeout)
	defer cancelLock()
	if err := lockRefreshMutex(lockCtx, mutex); err != nil {
		return Tokens{}, err
	}
	defer mutex.Unlock()
	lock, err := lockTokenFile(lockCtx, t.profileDir)
	if err != nil {
		return Tokens{}, err
	}
	defer func() {
		_ = filelock.Unlock(lock)
		_ = lock.Close()
	}()
	tokens, err := Load(t.profileDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Tokens{}, ErrSignInExpired
		}
		return Tokens{}, err
	}
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return Tokens{}, ErrSignInExpired
	}
	if force && rejected != "" && tokens.AccessToken != rejected {
		return tokens, nil
	}
	now := t.options.now()
	if !force && (tokens.ExpiresAt.IsZero() || tokens.ExpiresAt.After(now.Add(5*time.Minute))) {
		return tokens, nil
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tokens.RefreshToken},
		"client_id":     {ClientID},
	}
	refreshCtx, cancelRefresh := context.WithTimeout(ctx, refreshExchangeTimeout)
	defer cancelRefresh()
	request, err := http.NewRequestWithContext(refreshCtx, http.MethodPost, t.options.issuer()+tokenPath, strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return Tokens{}, scrubTransportError(err)
	}
	defer response.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maxExchangeBody))
	if readErr != nil {
		return Tokens{}, readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnauthorized {
			tokens.AccessToken = ""
			tokens.ExpiresAt = time.Time{}
			if err := Save(t.profileDir, tokens); err != nil {
				return Tokens{}, err
			}
			return Tokens{}, ErrSignInExpired
		}
		status := strings.TrimSpace(string(trace.Scrub([]byte(response.Status))))
		return Tokens{}, fmt.Errorf("refresh codex sign-in: issuer answered %s", status)
	}
	var answer struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if json.Unmarshal(raw, &answer) != nil || strings.TrimSpace(answer.AccessToken) == "" {
		return Tokens{}, errors.New("refresh codex sign-in: issuer returned an unreadable answer")
	}
	tokens.AccessToken = answer.AccessToken
	if strings.TrimSpace(answer.RefreshToken) != "" {
		tokens.RefreshToken = answer.RefreshToken
	}
	if strings.TrimSpace(answer.IDToken) != "" {
		tokens.IDToken = answer.IDToken
		if claims, claimErr := claimsFrom(answer.IDToken); claimErr == nil {
			tokens.AccountID, tokens.Email, tokens.Plan = claims.Auth.AccountID, claims.Email, claims.Auth.Plan
		}
	}
	tokens.ExpiresAt = time.Time{}
	if claims, claimErr := claimsFrom(answer.AccessToken); claimErr == nil && claims.Exp != 0 {
		tokens.ExpiresAt = time.Unix(claims.Exp, 0)
	}
	tokens.LastRefresh = now.UTC()
	if err := Save(t.profileDir, tokens); err != nil {
		return Tokens{}, err
	}
	return Load(t.profileDir)
}

// lockRefreshMutex gives callers in this process the same cancellation and
// patience as the file lock below. A plain sync.Mutex would strand an already
// cancelled turn behind the network request that currently owns the refresh.
func lockRefreshMutex(ctx context.Context, mutex *sync.Mutex) error {
	ticker := time.NewTicker(refreshLockCadence)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return errRefreshAlreadyRunning
		}
		if mutex.TryLock() {
			if ctx.Err() == nil {
				return nil
			}
			mutex.Unlock()
			return errRefreshAlreadyRunning
		}
		select {
		case <-ctx.Done():
			return errRefreshAlreadyRunning
		case <-ticker.C:
		}
	}
}

// lockTokenFile extends the in-process refresh mutex across codeaf processes.
// The sidecar is never removed: the operating system owns the live lock, and a
// process exit releases it without a stale-file protocol. The token file is
// re-read only after this returns, so a waiter sees whichever refresh won. A
// non-blocking attempt keeps cancellation observable while another process is
// inside its issuer round trip.
func lockTokenFile(ctx context.Context, profileDir string) (*os.File, error) {
	path := Path(profileDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("refresh codex sign-in: make profile directory: %w", err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("refresh codex sign-in: open token lock: %w", err)
	}
	if err := lock.Chmod(0o600); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("refresh codex sign-in: protect token lock: %w", err)
	}
	ticker := time.NewTicker(refreshLockCadence)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			_ = lock.Close()
			return nil, errRefreshAlreadyRunning
		}
		err := filelock.Lock(lock, true, true)
		if err == nil {
			if ctx.Err() == nil {
				return lock, nil
			}
			_ = filelock.Unlock(lock)
			_ = lock.Close()
			return nil, errRefreshAlreadyRunning
		}
		if !filelock.IsBusy(err) {
			_ = lock.Close()
			return nil, fmt.Errorf("refresh codex sign-in: lock tokens: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = lock.Close()
			return nil, errRefreshAlreadyRunning
		case <-ticker.C:
		}
	}
}

func translateRequest(profileDir string, raw []byte) ([]byte, bool, error) {
	var source map[string]any
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, false, fmt.Errorf("translate codex request: %w", err)
	}
	wantsStream, _ := source["stream"].(bool)
	model, _ := source["model"].(string)
	instructions := make([]string, 0, 1)
	input := make([]any, 0)
	messages, _ := source["messages"].([]any)
	for _, rawMessage := range messages {
		message, _ := rawMessage.(map[string]any)
		role, _ := message["role"].(string)
		if role == "system" {
			if text := contentText(message["content"]); strings.TrimSpace(text) != "" {
				instructions = append(instructions, text)
			}
			continue
		}
		if role == "assistant" {
			for _, detail := range reasoningDetails(message["reasoning_details"]) {
				input = append(input, detail)
			}
			if text := contentText(message["content"]); text != "" {
				input = append(input, map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text}}})
			}
			if calls, ok := message["tool_calls"].([]any); ok {
				for _, rawCall := range calls {
					call, _ := rawCall.(map[string]any)
					function, _ := call["function"].(map[string]any)
					input = append(input, map[string]any{"type": "function_call", "call_id": call["id"], "name": function["name"], "arguments": function["arguments"]})
				}
			}
			continue
		}
		if role == "tool" {
			input = append(input, map[string]any{"type": "function_call_output", "call_id": message["tool_call_id"], "output": contentText(message["content"])})
			continue
		}
		if role == "user" {
			input = append(input, map[string]any{"type": "message", "role": "user", "content": inputContent(message["content"])})
		}
	}
	if len(instructions) == 0 {
		instructions = append(instructions, "You are codeaf, a coding agent.")
	}
	target := map[string]any{
		"model": model, "instructions": strings.Join(instructions, "\n\n"), "input": input,
		"store": false, "stream": true, "include": []string{"reasoning.encrypted_content"},
		"text": map[string]any{"verbosity": "medium"},
	}
	if text, ok := source["text"].(map[string]any); ok {
		target["text"] = text
	}
	if key, ok := source["prompt_cache_key"]; ok {
		target["prompt_cache_key"] = key
	}
	if parallel, ok := source["parallel_tool_calls"]; ok {
		target["parallel_tool_calls"] = parallel
	}
	if maximum, ok := source["max_completion_tokens"]; ok {
		target["max_output_tokens"] = maximum
	} else if maximum, ok := source["max_tokens"]; ok {
		target["max_output_tokens"] = maximum
	}
	if tools, ok := source["tools"].([]any); ok {
		flat := make([]any, 0, len(tools))
		for _, rawTool := range tools {
			tool, _ := rawTool.(map[string]any)
			function, _ := tool["function"].(map[string]any)
			flat = append(flat, map[string]any{"type": "function", "name": function["name"], "description": function["description"], "parameters": function["parameters"], "strict": false})
		}
		target["tools"] = flat
	}
	if choice, ok := source["tool_choice"]; ok {
		if named, yes := choice.(map[string]any); yes {
			function, _ := named["function"].(map[string]any)
			target["tool_choice"] = map[string]any{"type": "function", "name": function["name"]}
		} else {
			target["tool_choice"] = choice
		}
	}
	effort := ""
	if reasoning, ok := source["reasoning"].(map[string]any); ok {
		effort, _ = reasoning["effort"].(string)
	}
	if effort == "" {
		effort, _ = source["reasoning_effort"].(string)
	}
	if effort = clampEffort(profileDir, model, effort); effort != "" {
		target["reasoning"] = map[string]any{"effort": effort, "summary": "auto"}
	}
	encoded, err := json.Marshal(target)
	return encoded, wantsStream, err
}

func contentText(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	var words strings.Builder
	if parts, ok := value.([]any); ok {
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			kind, _ := part["type"].(string)
			if kind == "text" || kind == "input_text" || kind == "output_text" {
				text, _ := part["text"].(string)
				words.WriteString(text)
			}
		}
	}
	return words.String()
}

func inputContent(value any) []any {
	if text, ok := value.(string); ok {
		return []any{map[string]any{"type": "input_text", "text": text}}
	}
	out := make([]any, 0)
	if parts, ok := value.([]any); ok {
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			switch part["type"] {
			case "text", "input_text":
				out = append(out, map[string]any{"type": "input_text", "text": part["text"]})
			case "image_url", "input_image":
				image := part["image_url"]
				if object, ok := image.(map[string]any); ok {
					image = object["url"]
				}
				out = append(out, map[string]any{"type": "input_image", "image_url": image})
			}
		}
	}
	return out
}

func reasoningDetails(value any) []any {
	details, _ := value.([]any)
	out := make([]any, 0, len(details))
	for _, rawDetail := range details {
		detail, _ := rawDetail.(map[string]any)
		if detail["format"] != "openai-responses-v1" || detail["type"] != "reasoning.encrypted" {
			continue
		}
		out = append(out, map[string]any{"type": "reasoning", "id": detail["id"], "encrypted_content": detail["data"], "summary": []any{}})
	}
	return out
}

type mappedStream struct {
	id, model string
	created   int64
	content   strings.Builder
	reasoning strings.Builder
	tools     []map[string]any
	toolAt    map[int]int
	details   []any
	usage     map[string]any
	finish    string
	sawTool   bool
	terminal  bool
	failure   *mappedFailure
}

type mappedFailure struct {
	status int
	value  map[string]any
}

func translateResponse(response *http.Response, wantsStream bool) (*http.Response, error) {
	if wantsStream {
		upstream := response.Body
		reader, writer := io.Pipe()
		response.Body = reader
		response.ContentLength = -1
		response.Header.Set("Content-Type", "text/event-stream")
		guard.Go("codexauth/stream", func() {
			defer upstream.Close()
			state := &mappedStream{}
			err := mapResponseEvents(upstream, state, func(chunk map[string]any) error {
				encoded, _ := json.Marshal(chunk)
				_, writeErr := fmt.Fprintf(writer, "data: %s\n\n", encoded)
				return writeErr
			})
			if err == nil && state.failure == nil {
				_, err = io.WriteString(writer, "data: [DONE]\n\n")
			}
			_ = writer.CloseWithError(err)
		})
		return response, nil
	}
	raw, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	state := &mappedStream{}
	if err := mapResponseEvents(bytes.NewReader(raw), state, nil); err != nil {
		return nil, err
	}
	if state.failure != nil {
		body, _ := json.Marshal(map[string]any{"error": state.failure.value})
		response.StatusCode = state.failure.status
		response.Status = fmt.Sprintf("%d %s", state.failure.status, http.StatusText(state.failure.status))
		response.Body = io.NopCloser(bytes.NewReader(body))
		response.ContentLength = int64(len(body))
		response.Header.Set("Content-Type", "application/json")
		return response, nil
	}
	message := map[string]any{"role": "assistant", "content": state.content.String()}
	if len(state.tools) > 0 {
		message["tool_calls"] = state.tools
	}
	if state.reasoning.Len() > 0 {
		message["reasoning"] = state.reasoning.String()
	}
	if len(state.details) > 0 {
		message["reasoning_details"] = state.details
	}
	completion := map[string]any{
		"id": state.id, "object": "chat.completion", "created": state.created, "model": state.model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": state.finish}},
	}
	if state.usage != nil {
		completion["usage"] = state.usage
	}
	body, _ := json.Marshal(completion)
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	response.Header.Set("Content-Type", "application/json")
	return response, nil
}

func mapResponseEvents(reader io.Reader, state *mappedStream, emit func(map[string]any) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		data = string(trace.Scrub([]byte(data)))
		var event map[string]any
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}
		for _, chunk := range mapEvent(event, state) {
			if emit != nil {
				if err := emit(chunk); err != nil {
					return err
				}
			}
		}
		if state.terminal {
			return nil
		}
	}
	return scanner.Err()
}

func mapEvent(event map[string]any, state *mappedStream) []map[string]any {
	kind, _ := event["type"].(string)
	chunk := func(delta map[string]any, finish any, usage map[string]any) map[string]any {
		value := map[string]any{
			"id": state.id, "object": "chat.completion.chunk", "created": state.created, "model": state.model,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		}
		if usage != nil {
			value["usage"] = usage
		}
		return value
	}
	switch kind {
	case "response.created":
		response, _ := event["response"].(map[string]any)
		state.id, _ = response["id"].(string)
		state.model, _ = response["model"].(string)
		state.created = integer(response["created_at"])
		return []map[string]any{chunk(map[string]any{"role": "assistant"}, nil, nil)}
	case "response.output_text.delta", "response.refusal.delta":
		text, _ := event["delta"].(string)
		state.content.WriteString(text)
		return []map[string]any{chunk(map[string]any{"content": text}, nil, nil)}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		text, _ := event["delta"].(string)
		state.reasoning.WriteString(text)
		return []map[string]any{chunk(map[string]any{"reasoning": text}, nil, nil)}
	case "response.output_item.added":
		item, _ := event["item"].(map[string]any)
		if item["type"] != "function_call" {
			return nil
		}
		outputIndex := int(integer(event["output_index"]))
		index := len(state.tools)
		call := map[string]any{"id": item["call_id"], "type": "function", "function": map[string]any{"name": item["name"], "arguments": ""}}
		state.sawTool = true
		state.tools = append(state.tools, call)
		if state.toolAt == nil {
			state.toolAt = make(map[int]int)
		}
		state.toolAt[outputIndex] = index
		return []map[string]any{chunk(map[string]any{"tool_calls": []any{map[string]any{"index": index, "id": item["call_id"], "type": "function", "function": map[string]any{"name": item["name"], "arguments": ""}}}}, nil, nil)}
	case "response.function_call_arguments.delta":
		outputIndex := int(integer(event["output_index"]))
		index, found := state.toolAt[outputIndex]
		if !found {
			return nil
		}
		text, _ := event["delta"].(string)
		if index >= 0 && index < len(state.tools) {
			function, _ := state.tools[index]["function"].(map[string]any)
			current, _ := function["arguments"].(string)
			function["arguments"] = current + text
		}
		return []map[string]any{chunk(map[string]any{"tool_calls": []any{map[string]any{"index": index, "function": map[string]any{"arguments": text}}}}, nil, nil)}
	case "response.output_item.done":
		item, _ := event["item"].(map[string]any)
		if item["type"] != "reasoning" {
			return nil
		}
		detail := map[string]any{"type": "reasoning.encrypted", "format": "openai-responses-v1", "id": item["id"], "data": item["encrypted_content"]}
		state.details = append(state.details, detail)
		return []map[string]any{chunk(map[string]any{"reasoning_details": []any{detail}}, nil, nil)}
	case "response.incomplete":
		response, _ := event["response"].(map[string]any)
		state.setIdentity(response)
		details, _ := response["incomplete_details"].(map[string]any)
		if details == nil {
			details, _ = event["incomplete_details"].(map[string]any)
		}
		reason, _ := details["reason"].(string)
		if strings.TrimSpace(reason) == "max_output_tokens" {
			state.finish = "length"
			state.usage = mappedUsage(response["usage"])
			state.terminal = true
			return []map[string]any{chunk(map[string]any{}, state.finish, state.usage)}
		}
		message := "codex did not finish the response"
		if reason = strings.TrimSpace(reason); reason != "" {
			message += " · " + reason
		}
		state.failure = classifyMappedFailure(map[string]any{
			"message": message, "type": "upstream_error", "code": http.StatusBadGateway,
		})
		state.terminal = true
		return []map[string]any{{"error": state.failure.value}}
	case "response.completed":
		response, _ := event["response"].(map[string]any)
		state.setIdentity(response)
		if state.finish == "" {
			if state.sawTool {
				state.finish = "tool_calls"
			} else {
				state.finish = "stop"
			}
		}
		state.usage = mappedUsage(response["usage"])
		state.terminal = true
		return []map[string]any{chunk(map[string]any{}, state.finish, state.usage)}
	case "response.failed", "error":
		errorValue, _ := event["error"].(map[string]any)
		if errorValue == nil {
			response, _ := event["response"].(map[string]any)
			errorValue, _ = response["error"].(map[string]any)
		}
		if errorValue == nil {
			errorValue = make(map[string]any)
			if message, _ := event["message"].(string); strings.TrimSpace(message) != "" {
				errorValue["message"] = message
			}
			if code, ok := event["code"]; ok {
				errorValue["code"] = code
			}
		}
		state.failure = classifyMappedFailure(errorValue)
		state.terminal = true
		return []map[string]any{{"error": state.failure.value}}
	}
	return nil
}

func (s *mappedStream) setIdentity(response map[string]any) {
	if s == nil || response == nil || s.id != "" {
		return
	}
	s.id, _ = response["id"].(string)
	s.model, _ = response["model"].(string)
	if created := integer(response["created_at"]); created != 0 {
		s.created = created
	}
}

func classifyMappedFailure(value map[string]any) *mappedFailure {
	encoded, _ := json.Marshal(value)
	if quotaPayload(encoded) {
		return &mappedFailure{status: http.StatusPaymentRequired, value: map[string]any{
			"message": QuotaWords, "type": "upstream_error", "code": http.StatusPaymentRequired,
		}}
	}
	message, _ := value["message"].(string)
	if message = strings.TrimSpace(message); message == "" {
		message = "codex did not finish the response"
	}
	kind, _ := value["type"].(string)
	if kind = strings.TrimSpace(kind); kind == "" || kind == "error" {
		kind = "upstream_error"
	}
	return &mappedFailure{status: http.StatusBadGateway, value: map[string]any{
		"message": message, "type": kind, "code": http.StatusBadGateway,
	}}
}

func integer(value any) int64 {
	switch number := value.(type) {
	case float64:
		return int64(number)
	case int64:
		return number
	case int:
		return int64(number)
	}
	return 0
}

func mappedUsage(value any) map[string]any {
	usage, ok := value.(map[string]any)
	if !ok || usage == nil {
		return nil
	}
	input := integer(usage["input_tokens"])
	output := integer(usage["output_tokens"])
	inputDetails, _ := usage["input_tokens_details"].(map[string]any)
	outputDetails, _ := usage["output_tokens_details"].(map[string]any)
	return map[string]any{
		"prompt_tokens": input, "completion_tokens": output, "total_tokens": input + output,
		"prompt_tokens_details":     map[string]any{"cached_tokens": integer(inputDetails["cached_tokens"])},
		"completion_tokens_details": map[string]any{"reasoning_tokens": integer(outputDetails["reasoning_tokens"])},
	}
}
