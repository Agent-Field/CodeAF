package orclient

// Request assembly — ENGINE-DESIGN §3.1.
//
// The body is
//
//	{...getArgs(options), ...restOpenrouterOptions,
//	 ...(cacheControl != null && !("cache_control" in rest) ? {cache_control} : {}),
//	 stream: true, stream_options: <strict ? {include_usage:true} : undefined>}
//
// (`internal/index.mjs:3718`, `:3725-3731`) and the ONLY thing that makes the
// key order non-obvious is that `getArgs` writes ~25 keys whose value is
// `this.settings.X` — all undefined for codeaf, all invisible to
// JSON.stringify, all still holding a slot. `usage` and `prompt_cache_key`
// arrive from `providerOptions.openrouter`; `usage` overwrites its reserved
// slot (so it lands between `messages` and `tools`), `prompt_cache_key` is new
// (so it lands after `tool_choice`). Verified against the running provider.
//
// **The namespace is an unvalidated whole-body override.** `doStream` does NOT
// zod-validate call-level `providerOptions.openrouter`
// (`OpenRouterProviderOptionsSchema` is used only for message-level options),
// so anything codeaf puts there silently overwrites the corresponding top-level
// field. The merged option bag is therefore applied LAST, with shallow-spread
// semantics.
//
// R11 RESOLVED: codeaf reaches the provider through `createOpenRouter`
// (`provider.ts:103`), whose `compatibility` defaults to `"compatible"`
// (`dist/index.mjs:5269`) — NOT the `"strict"` exported singleton. So
// `stream_options` is **absent** from every codeaf request. CompatibilityStrict
// is still implemented because the setting is a config knob, not a constant.

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
)

// Compatibility modes (`internal/index.mjs:3728`).
const (
	CompatibilityCompatible = "compatible"
	CompatibilityStrict     = "strict"
)

// Tool is one entry of `options.tools` — the AI SDK's
// `LanguageModelV3FunctionTool`. `InputSchema` is already through
// `ProviderTransform.schema`.
type Tool struct {
	Type            string          `json:"type"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	InputSchema     json.RawMessage `json:"inputSchema"`
	ProviderOptions json.RawMessage `json:"providerOptions,omitempty"`
}

// ToolChoice is `options.toolChoice`. codeaf sends `{"type":"required"}` only
// for the json_schema output format (`prompt.ts:1824`); otherwise nil.
type ToolChoice struct {
	Type     string `json:"type"`
	ToolName string `json:"toolName,omitempty"`
}

// RequestParams is the `LanguageModelV3CallOptions` slice this client uses.
// Field order mirrors the TS destructuring at `internal/index.mjs:3431-3444`.
type RequestParams struct {
	// ModelID is the routed `Provider.Model.id` — the full `<vendor>/<name>`
	// string, since `api.id === model.id` for every OpenRouter pool entry.
	ModelID string

	Prompt          []msgmodel.ModelMessage
	MaxOutputTokens *float64
	Temperature     *float64
	TopP            *float64
	TopK            *float64
	Tools           []Tool
	ToolChoice      *ToolChoice

	// OpenRouterOptions is `providerOptions.openrouter` — the merged option bag
	// produced by `mergeOptions(mergeOptions(mergeOptions(base, model.options),
	// agent.options), variant)` and wrapped by
	// `ProviderTransform.providerOptions`. `cacheControl` is split out of it by
	// doStream before the spread.
	OpenRouterOptions *Object

	// Compatibility selects whether `stream_options` is emitted.
	Compatibility string
}

// BuildRequestBody returns the exact bytes POSTed to /chat/completions.
func BuildRequestBody(p RequestParams) ([]byte, error) {
	args, err := getArgs(p)
	if err != nil {
		return nil, err
	}

	// `const {cacheControl, ...rest} = providerOptions.openrouter ?? {}`
	rest := NewObject()
	var cacheControl json.RawMessage
	if p.OpenRouterOptions != nil {
		for _, m := range p.OpenRouterOptions.enumeratedMembers() {
			if m.Key == "cacheControl" {
				raw, err := marshalJSONValue(m.Value)
				if err != nil {
					return nil, err
				}
				cacheControl = raw
				continue
			}
			rest.set(m.Key, m.Value)
		}
	}

	body := args.Clone()
	for _, m := range rest.enumeratedMembers() {
		body.set(m.Key, m.Value)
	}
	if cacheControl != nil && !rest.Has("cache_control") {
		if err := body.Set("cache_control", cacheControl); err != nil {
			return nil, err
		}
	}

	body.SetBool("stream", true)
	if p.Compatibility == CompatibilityStrict {
		streamOptions := NewObject()
		streamOptions.SetBool("include_usage", true)
		body.SetObject("stream_options", streamOptions)
	} else {
		body.SetUndefined("stream_options")
	}
	return body.MarshalJSON()
}

// getArgs is `OpenRouterChatLanguageModel.getArgs` (`:3431-3517`). Every key of
// the `baseArgs` object literal is written, in literal order, with the ones
// sourced from `this.settings` reserved as undefined — codeaf constructs the
// model with `settings = {}` and `config.extraBody` unset.
func getArgs(p RequestParams) (*Object, error) {
	base := NewObject()
	base.SetString("model", p.ModelID)
	base.SetUndefined("models")
	base.SetUndefined("logit_bias")
	base.SetUndefined("logprobs")
	base.SetUndefined("top_logprobs")
	base.SetUndefined("user")
	base.SetUndefined("parallel_tool_calls")
	base.SetNumberPtr("max_tokens", p.MaxOutputTokens)
	base.SetNumberPtr("temperature", p.Temperature)
	base.SetNumberPtr("top_p", p.TopP)
	base.SetUndefined("frequency_penalty")
	base.SetUndefined("presence_penalty")
	base.SetUndefined("seed")
	base.SetUndefined("stop")
	base.SetUndefined("response_format")
	base.SetNumberPtr("top_k", p.TopK)

	messages, err := ConvertToOpenRouterChatMessages(p.Prompt)
	if err != nil {
		return nil, err
	}
	base.SetArray("messages", messages)

	base.SetUndefined("include_reasoning")
	base.SetUndefined("reasoning")
	base.SetUndefined("usage")
	base.SetUndefined("plugins")
	base.SetUndefined("web_search_options")
	base.SetUndefined("provider")
	base.SetUndefined("debug")
	base.SetUndefined("cache_control")

	if len(p.Tools) == 0 {
		// `if (tools && tools.length > 0)` — no tools means no `tools` key and
		// no `tool_choice` key at all, not even reserved ones.
		return base, nil
	}

	mapped := make([]*Object, 0, len(p.Tools))
	for _, tool := range p.Tools {
		if tool.Type != "function" {
			// `tool.type === "provider"` goes through mapProviderTool; codeaf
			// never registers a provider tool.
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
		} else {
			fn.SetUndefined("parameters")
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
	if p.ToolChoice == nil {
		base.SetUndefined("tool_choice")
		return base, nil
	}
	choice, err := chatCompletionToolChoice(*p.ToolChoice)
	if err != nil {
		return nil, err
	}
	base.set("tool_choice", choice)
	return base, nil
}

// chatCompletionToolChoice is `getChatCompletionToolChoice` (`:3185-3204`).
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

// InvalidArgumentError mirrors the SDK's error of the same name. Message text
// is the behavioural contract.
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

// HeaderInputs are the three contributors ENGINE-DESIGN §3.1 lists, in the
// order they are combined.
type HeaderInputs struct {
	// Provider is `createOpenRouter`'s `getHeaders()` BEFORE its own
	// user-agent suffix: Authorization, X-OpenRouter-Title (appName),
	// HTTP-Referer (appUrl), then codeaf's `options.headers`
	// (`provider.ts:427-440`: HTTP-Referer + X-Title).
	Provider []HeaderPair
	// ProviderUserAgentSuffix is `ai-sdk/openrouter/<VERSION>`.
	ProviderUserAgentSuffix string
	// Call is `options.headers` from `llm.ts:457-479`.
	Call []HeaderPair
	// UtilsUserAgentSuffix / RuntimeUserAgentSuffix are what `postToApi`
	// appends: `ai-sdk/provider-utils/4.0.23` and `runtime/<...>`.
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
//	provider   = withUserAgentSuffix(providerHeaders, "ai-sdk/openrouter/2.8.1")
//	combined   = {...provider, ...call}                    // case-SENSITIVE spread
//	withType   = {"Content-Type": "application/json", ...combined}
//	final      = withUserAgentSuffix(withType, "ai-sdk/provider-utils/…", "runtime/…")
//
// `withUserAgentSuffix` lowercases every name, drops null values, joins the
// non-empty user-agent parts with a space, and returns
// `Object.fromEntries(new Headers(...).entries())` — and `Headers.entries()` is
// specified to iterate in ASCENDING name order, which is why the result is
// sorted rather than insertion-ordered.
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
