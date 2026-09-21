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
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/provider"
)

var refreshLocks struct {
	sync.Mutex
	byPath map[string]*sync.Mutex
}

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
		return nil, err
	}
	if response.StatusCode == http.StatusUnauthorized {
		_ = response.Body.Close()
		refreshed, refreshErr := t.fresh(request.Context(), true, tokens.AccessToken)
		if refreshErr != nil {
			return nil, refreshErr
		}
		response, err = send(refreshed)
		if err != nil {
			return nil, err
		}
	}
	if isTurn && response.StatusCode >= 400 && codexQuotaStatus(response.StatusCode) {
		response = quotaResponse(response)
	}
	if isCatalogList && response.StatusCode >= 200 && response.StatusCode < 300 {
		return t.translateCatalogResponse(response)
	}
	if !isTurn || response.StatusCode < 200 || response.StatusCode >= 300 {
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
	lower := strings.ToLower(string(raw))
	matched := false
	for _, phrase := range []string{"usage_limit_reached", "usage_not_included", "rate_limit_exceeded", "usage limit"} {
		matched = matched || strings.Contains(lower, phrase)
	}
	if !matched {
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
	mutex.Lock()
	defer mutex.Unlock()
	tokens, err := Load(t.profileDir)
	if err != nil {
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
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.options.issuer()+tokenPath, strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return Tokens{}, err
	}
	defer response.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maxExchangeBody))
	if readErr != nil {
		return Tokens{}, readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		tokens.AccessToken = ""
		tokens.ExpiresAt = time.Time{}
		_ = Save(t.profileDir, tokens)
		return Tokens{}, ErrSignInExpired
	}
	var answer struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if json.Unmarshal(raw, &answer) != nil || strings.TrimSpace(answer.AccessToken) == "" {
		return Tokens{}, ErrSignInExpired
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
	if claims, claimErr := claimsFrom(answer.AccessToken); claimErr == nil && claims.Exp != 0 {
		tokens.ExpiresAt = time.Unix(claims.Exp, 0)
	}
	tokens.LastRefresh = now.UTC()
	if err := Save(t.profileDir, tokens); err != nil {
		return Tokens{}, err
	}
	return Load(t.profileDir)
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
			if err == nil {
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
	body, _ := json.Marshal(map[string]any{
		"id": state.id, "object": "chat.completion", "created": state.created, "model": state.model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": state.finish}}, "usage": state.usage,
	})
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
	}
	return scanner.Err()
}

func mapEvent(event map[string]any, state *mappedStream) []map[string]any {
	kind, _ := event["type"].(string)
	chunk := func(delta map[string]any, finish any, usage map[string]any) map[string]any {
		return map[string]any{
			"id": state.id, "object": "chat.completion.chunk", "created": state.created, "model": state.model,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}, "usage": usage,
		}
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
		state.finish = "length"
		return nil
	case "response.completed":
		response, _ := event["response"].(map[string]any)
		if state.id == "" {
			state.id, _ = response["id"].(string)
			state.model, _ = response["model"].(string)
		}
		if state.finish == "" {
			if state.sawTool {
				state.finish = "tool_calls"
			} else {
				state.finish = "stop"
			}
		}
		state.usage = mappedUsage(response["usage"])
		return []map[string]any{chunk(map[string]any{}, state.finish, state.usage)}
	case "response.failed", "error":
		errorValue, _ := event["error"].(map[string]any)
		if errorValue == nil {
			response, _ := event["response"].(map[string]any)
			errorValue, _ = response["error"].(map[string]any)
		}
		if errorValue == nil {
			errorValue = map[string]any{"message": "codex did not finish the response", "type": "upstream_error", "code": 502}
		}
		if _, ok := errorValue["code"].(float64); !ok {
			errorValue["code"] = 502
		}
		return []map[string]any{{"error": errorValue}}
	}
	return nil
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
	usage, _ := value.(map[string]any)
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
