//go:build !windows

package orclient

// Request assembly: the chat-completions body and the header set.
//
// The body is the client's own fields (model, sampling, messages, tools),
// then the merged provider option bag from config applied on top as a
// shallow spread, then the streaming flags. The option bag is an unvalidated
// whole-body override: anything config puts there replaces the matching
// top-level field. stream_options is emitted only in strict compatibility
// mode; senior-dev runs in compatible mode.

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

// Compatibility modes.
const (
	CompatibilityCompatible = "compatible"
	CompatibilityStrict     = "strict"
)

// Tool is one registered tool as the request sees it. InputSchema is the JSON
// Schema the provider receives.
type Tool struct {
	Type            string          `json:"type"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	InputSchema     json.RawMessage `json:"inputSchema"`
	ProviderOptions json.RawMessage `json:"providerOptions,omitempty"`
}

// ToolChoice constrains which tool the model may call. senior-dev sends
// `{"type":"required"}` only for a json_schema output format; otherwise nil.
type ToolChoice struct {
	Type     string `json:"type"`
	ToolName string `json:"toolName,omitempty"`
}

// RequestParams are the inputs to one request.
type RequestParams struct {
	// ModelID is the full `<vendor>/<name>` OpenRouter model id.
	ModelID string

	Prompt          []msgmodel.ModelMessage
	MaxOutputTokens *float64

	// Sampling parameters. A nil field is omitted from the body, so the
	// serving provider's default applies; the caller decides what to set.
	Temperature       *float64
	TopP              *float64
	TopK              *float64
	MinP              *float64
	Seed              *float64
	FrequencyPenalty  *float64
	PresencePenalty   *float64
	RepetitionPenalty *float64

	Tools      []Tool
	ToolChoice *ToolChoice

	// OpenRouterOptions is the merged provider option bag from config (base,
	// model, agent and variant options, in that order). `cacheControl` is
	// split out of it before the spread.
	OpenRouterOptions *Object

	// Compatibility selects whether `stream_options` is emitted.
	Compatibility string
}

// BuildRequestBody returns the bytes POSTed to /chat/completions: the model and
// sampling parameters, the converted messages, the tool definitions, then any
// provider options from config (which may override a base field), then the
// streaming flags.
func BuildRequestBody(p RequestParams) ([]byte, error) {
	body, err := baseArgs(p)
	if err != nil {
		return nil, err
	}

	// Provider options are applied last with shallow-spread semantics. A
	// cacheControl entry is renamed to the wire field cache_control unless the
	// options already carry one.
	var cacheControl json.RawMessage
	if p.OpenRouterOptions != nil {
		for _, m := range p.OpenRouterOptions.members {
			if m.Key == "cacheControl" {
				raw, err := marshalJSONValue(m.Value)
				if err != nil {
					return nil, err
				}
				cacheControl = raw
				continue
			}
			body.set(m.Key, m.Value)
		}
	}
	if cacheControl != nil && (p.OpenRouterOptions == nil || !p.OpenRouterOptions.Has("cache_control")) {
		if err := body.Set("cache_control", cacheControl); err != nil {
			return nil, err
		}
	}

	body.SetBool("stream", true)
	if p.Compatibility == CompatibilityStrict {
		streamOptions := NewObject()
		streamOptions.SetBool("include_usage", true)
		body.SetObject("stream_options", streamOptions)
	}
	return body.MarshalJSON()
}

// baseArgs writes the request fields this client sets itself. A nil sampling
// parameter is omitted so the serving provider's default applies.
func baseArgs(p RequestParams) (*Object, error) {
	base := NewObject()
	base.SetString("model", p.ModelID)
	base.SetNumberPtr("max_tokens", p.MaxOutputTokens)
	base.SetNumberPtr("temperature", p.Temperature)
	base.SetNumberPtr("top_p", p.TopP)
	base.SetNumberPtr("frequency_penalty", p.FrequencyPenalty)
	base.SetNumberPtr("presence_penalty", p.PresencePenalty)
	base.SetNumberPtr("seed", p.Seed)
	base.SetNumberPtr("top_k", p.TopK)
	base.SetNumberPtr("min_p", p.MinP)
	base.SetNumberPtr("repetition_penalty", p.RepetitionPenalty)

	messages, err := ConvertToOpenRouterChatMessages(p.Prompt)
	if err != nil {
		return nil, err
	}
	base.SetArray("messages", messages)

	if len(p.Tools) == 0 {
		return base, nil
	}
	mapped := make([]*Object, 0, len(p.Tools))
	for _, tool := range p.Tools {
		if tool.Type != "function" {
			continue
		}
		entry := NewObject()
		entry.SetString("type", "function")
		fn := NewObject()
		fn.SetString("name", tool.Name)
		fn.SetString("description", tool.Description)
		if len(tool.InputSchema) > 0 {
			if err := fn.Set("parameters", tool.InputSchema); err != nil {
				return nil, err
			}
		}
		entry.SetObject("function", fn)
		if eager, ok := openrouterNamespaceField(tool.ProviderOptions, "eager_input_streaming"); ok && !bytes.Equal(eager, []byte("null")) {
			if err := entry.Set("eager_input_streaming", eager); err != nil {
				return nil, err
			}
		}
		mapped = append(mapped, entry)
	}
	base.SetArray("tools", mapped)
	if p.ToolChoice != nil {
		choice, err := chatCompletionToolChoice(*p.ToolChoice)
		if err != nil {
			return nil, err
		}
		base.set("tool_choice", choice)
	}
	return base, nil
}

// chatCompletionToolChoice maps a ToolChoice to the wire `tool_choice` value.
func chatCompletionToolChoice(tc ToolChoice) (jsonValue, error) {
	switch tc.Type {
	case "auto", "none", "required":
		return stringValue(tc.Type), nil
	case "tool":
		o := NewObject()
		o.SetString("type", "function")
		fn := NewObject()
		fn.SetString("name", tc.ToolName)
		o.SetObject("function", fn)
		return o.value(), nil
	}
	rendered := NewObject()
	rendered.SetString("type", tc.Type)
	if tc.ToolName != "" {
		rendered.SetString("toolName", tc.ToolName)
	}
	encoded, _ := rendered.MarshalJSON()
	return jsonValue{}, &InvalidArgumentError{
		Argument: "toolChoice",
		Message:  "Invalid tool choice type: " + string(encoded),
	}
}

// InvalidArgumentError reports an unusable request parameter.
type InvalidArgumentError struct {
	Argument string
	Message  string
}

func (e *InvalidArgumentError) Error() string { return e.Message }

// ── headers ───────────────────────────────────────────────────────────────

// HeaderPair is one final header, lowercase-named.
type HeaderPair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HeaderInputs are the header contributors, in the order they are combined.
type HeaderInputs struct {
	// Provider is the provider-level header set before its user-agent
	// suffix: Authorization, X-OpenRouter-Title, HTTP-Referer, then the
	// configured provider headers (HTTP-Referer + X-Title).
	Provider []HeaderPair
	// ProviderUserAgentSuffix is appended to the user agent first.
	ProviderUserAgentSuffix string
	// Call is the per-call header set.
	Call []HeaderPair
	// UtilsUserAgentSuffix / RuntimeUserAgentSuffix are appended to the user
	// agent last.
	UtilsUserAgentSuffix   string
	RuntimeUserAgentSuffix string
}

// BuildHeaders reproduces the whole merge, which is worth doing as one function
// because the precedence is genuinely surprising: `X-Title` and
// `X-OpenRouter-Title` are BOTH on the wire, `HTTP-Referer` is set three times
// with the call-level value winning, and `User-Agent`/`user-agent` collide only
// after normalizeHeaders lowercases them.
//
// The pipeline is:
//
//	provider   = withUserAgentSuffix(providerHeaders, providerSuffix)
//	combined   = {...provider, ...call}                    // case-SENSITIVE spread
//	withType   = {"Content-Type": "application/json", ...combined}
//	final      = withUserAgentSuffix(withType, utilsSuffix, runtimeSuffix)
//
// withUserAgentSuffix lowercases every name, joins the non-empty user-agent
// parts with a space, and returns the pairs sorted by name.
func BuildHeaders(in HeaderInputs) []HeaderPair {
	provider := withUserAgentSuffix(in.Provider, in.ProviderUserAgentSuffix)

	combined := append([]HeaderPair{}, provider...)
	combined = spreadHeaders(combined, in.Call)

	withType := append([]HeaderPair{{Name: "Content-Type", Value: "application/json"}}, nil...)
	withType = spreadHeaders(withType, combined)

	return withUserAgentSuffix(withType, in.UtilsUserAgentSuffix, in.RuntimeUserAgentSuffix)
}

// spreadHeaders is the `{...a, ...b}` object spread: case-SENSITIVE, later
// wins, new keys appended.
func spreadHeaders(target, source []HeaderPair) []HeaderPair {
	out := append([]HeaderPair{}, target...)
	for _, s := range source {
		replaced := false
		for i := range out {
			if out[i].Name == s.Name {
				out[i].Value = s.Value
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, s)
		}
	}
	return out
}

func withUserAgentSuffix(headers []HeaderPair, suffixes ...string) []HeaderPair {
	normalized := []HeaderPair{}
	for _, h := range headers {
		lower := strings.ToLower(h.Name)
		replaced := false
		for i := range normalized {
			if normalized[i].Name == lower {
				normalized[i].Value = h.Value
				replaced = true
				break
			}
		}
		if !replaced {
			normalized = append(normalized, HeaderPair{Name: lower, Value: h.Value})
		}
	}

	current := ""
	for _, h := range normalized {
		if h.Name == "user-agent" {
			current = h.Value
			break
		}
	}
	parts := make([]string, 0, len(suffixes)+1)
	if current != "" {
		parts = append(parts, current)
	}
	for _, s := range suffixes {
		if s != "" {
			parts = append(parts, s)
		}
	}
	ua := strings.Join(parts, " ")

	set := false
	for i := range normalized {
		if normalized[i].Name == "user-agent" {
			normalized[i].Value = ua
			set = true
			break
		}
	}
	if !set {
		normalized = append(normalized, HeaderPair{Name: "user-agent", Value: ua})
	}

	sort.SliceStable(normalized, func(i, j int) bool {
		return normalized[i].Name < normalized[j].Name
	})
	return normalized
}
