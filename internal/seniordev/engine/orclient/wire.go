//go:build !windows

package orclient

// Chunk validation: the streaming chunk shape and the error-response shape,
// tried in that order.
//
// Three properties of the validation drive behaviour and are easy to lose:
//
//  1. `choices` is a REQUIRED array on the chunk shape. A payload carrying
//     only `usage` fails BOTH shapes and surfaces as a parse failure, i.e. an
//     `error` part and `finishReason = error`, not a silently ignored chunk.
//  2. Unknown fields survive validation. The one place that is observable is
//     the `error` field: an object carrying both `choices` and `error`
//     validates as a CHUNK, and the emitted error is the RAW object. When it
//     validates as the ERROR shape instead, `code`/`type`/`param` are filled
//     with null and reordered to shape order.
//  3. `finish_reason` is any string, not an enum, so arbitrary provider
//     strings reach `raw` and map to `other`.
//
// The `error` value of a PARSE-FAILURE part is an envelope
// (`{"name":…,"cause":…,"value":…}` / `{"name":…,"cause":…,"text":…}`) whose
// discriminating `name` is what a consumer keys on; the router classifiers
// substring-match on `message`.

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"
)

// Chunk is one parsed SSE payload.
type Chunk struct {
	// Success reports whether the payload validated.
	Success bool
	// ParseError is the serialised error object emitted when Success is false.
	ParseError json.RawMessage
	// Value is the validated chunk.
	Value *ChunkValue
}

// ChunkValue is the validated payload.
type ChunkValue struct {
	ID       *string
	Model    *string
	Provider *string
	Usage    *calc.OpenRouterUsage
	Choices  []Choice

	// ErrorField is set when the payload carries an `error` key, for BOTH
	// shapes.
	ErrorField json.RawMessage
}

// Choice is `value.choices[i]`.
type Choice struct {
	Delta        *Delta
	FinishReason *string
}

// Delta is `choices[i].delta`.
type Delta struct {
	Content          *string
	Reasoning        *string
	ReasoningDetails []ReasoningDetail
	HasReasoningDeta bool
	Images           []ImageResponse
	ToolCalls        []ToolCallDelta
	HasToolCalls     bool
	Annotations      []Annotation
	HasAnnotations   bool
}

// ImageResponse is one recognised `delta.images[]` entry.
type ImageResponse struct{ URL string }

// ToolCallDelta is one `delta.tool_calls[]` entry. Every field may be absent;
// a missing `type` on a FIRST delta is an InvalidResponseDataError.
type ToolCallDelta struct {
	Index       *float64
	ID          *string
	Type        *string
	HasFunction bool
	Name        *string
	Arguments   *string
	// Raw is the original entry, carried because InvalidResponseDataError
	// reports it as `data`.
	Raw json.RawMessage
}

// Annotation is one `delta.annotations[]` entry.
type Annotation struct {
	Type string
	// url_citation fields.
	URL        string
	Title      *string
	StartIndex *float64
	EndIndex   *float64
	Content    *string
	// Raw carries the normalized annotation object; the old-format
	// `file_annotation` is validated and then IGNORED ENTIRELY.
	Raw json.RawMessage
}

// ParseChunk parses and validates one SSE payload.
func ParseChunk(text string) Chunk {
	parsed, err := parseJSONValue([]byte(text))
	if err != nil {
		return Chunk{Success: false, ParseError: jsonParseErrorValue(text)}
	}
	if hasForbiddenPrototypeJSONValue(parsed) {
		return Chunk{Success: false, ParseError: jsonParseErrorValue(text)}
	}
	raw := json.RawMessage(text)

	value, chunkErr := validateChunk(raw)
	if chunkErr == nil {
		return Chunk{Success: true, Value: value}
	}
	value, errErr := validateErrorResponse(raw)
	if errErr == nil {
		return Chunk{Success: true, Value: value}
	}
	return Chunk{
		Success:    false,
		ParseError: typeValidationErrorValue(raw, chunkErr, errErr),
	}
}

func hasForbiddenPrototypeJSONValue(value jsonValue) bool {
	switch value.Kind {
	case kindArray:
		for _, child := range value.Array {
			if hasForbiddenPrototypeJSONValue(child) {
				return true
			}
		}
	case kindObject:
		for _, member := range value.Object {
			if member.Key == "__proto__" {
				return true
			}
			if member.Key == "constructor" && member.Value.Kind == kindObject {
				for _, child := range member.Value.Object {
					if child.Key == "prototype" {
						return true
					}
				}
			}
			if hasForbiddenPrototypeJSONValue(member.Value) {
				return true
			}
		}
	}
	return false
}

func jsonParseErrorValue(text string) json.RawMessage {
	w := newObjectWriter()
	w.str("name", "JSONParseError")
	w.raw("cause", json.RawMessage("{}"))
	w.str("text", text)
	out, err := w.done()
	if err != nil {
		return json.RawMessage(`{"name":"JSONParseError"}`)
	}
	return out
}

func typeValidationErrorValue(raw json.RawMessage, chunkErr, errErr error) json.RawMessage {
	cause := newObjectWriter()
	cause.str("name", "ValidationError")
	cause.str("message", chunkErr.Error()+"\n"+errErr.Error())
	causeRaw, err := cause.done()
	if err != nil {
		causeRaw = json.RawMessage("{}")
	}
	w := newObjectWriter()
	w.str("name", "TypeValidationError")
	w.raw("cause", causeRaw)
	w.raw("value", raw)
	out, err := w.done()
	if err != nil {
		return json.RawMessage(`{"name":"TypeValidationError"}`)
	}
	return out
}

// ── the chunk shape ──────────────────────────────────────────────────────

func validateChunk(raw json.RawMessage) (*ChunkValue, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, errors.New(`expected object`)
	}
	out := &ChunkValue{}

	var err error
	if out.ID, err = optionalString(obj, "id"); err != nil {
		return nil, err
	}
	if out.Model, err = optionalString(obj, "model"); err != nil {
		return nil, err
	}
	if out.Provider, err = optionalString(obj, "provider"); err != nil {
		return nil, err
	}
	if out.Usage, err = validateUsage(obj["usage"]); err != nil {
		return nil, err
	}

	choicesRaw, present := obj["choices"]
	if !present || bytes.Equal(bytes.TrimSpace(choicesRaw), []byte("null")) {
		return nil, errors.New(`Invalid input: expected array, received undefined at "choices"`)
	}
	var choices []json.RawMessage
	if err := json.Unmarshal(choicesRaw, &choices); err != nil {
		return nil, errors.New(`Invalid input: expected array at "choices"`)
	}
	out.Choices = make([]Choice, 0, len(choices))
	for _, c := range choices {
		choice, err := validateChoice(c)
		if err != nil {
			return nil, err
		}
		out.Choices = append(out.Choices, choice)
	}
	if e, ok := obj["error"]; ok {
		out.ErrorField = e
	}
	return out, nil
}

func validateChoice(raw json.RawMessage) (Choice, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return Choice{}, errors.New(`Invalid input: expected object at "choices[]"`)
	}
	out := Choice{}
	if _, err := nullishNumber(obj, "index"); err != nil {
		return Choice{}, err
	}
	if logprobs, ok := obj["logprobs"]; ok && !isJSONNull(logprobs) {
		if err := validateLogprobs(logprobs); err != nil {
			return Choice{}, err
		}
	}
	fr, err := nullableOptionalString(obj, "finish_reason")
	if err != nil {
		return Choice{}, err
	}
	out.FinishReason = fr

	deltaRaw, present := obj["delta"]
	if !present || bytes.Equal(bytes.TrimSpace(deltaRaw), []byte("null")) {
		return out, nil
	}
	delta, err := validateDelta(deltaRaw)
	if err != nil {
		return Choice{}, err
	}
	out.Delta = delta
	return out, nil
}

func validateDelta(raw json.RawMessage) (*Delta, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, errors.New(`Invalid input: expected object at "delta"`)
	}
	out := &Delta{}

	if roleRaw, ok := obj["role"]; ok {
		var role string
		if err := json.Unmarshal(roleRaw, &role); err != nil || role != "assistant" {
			return nil, errors.New(`Invalid input: expected "assistant" at "delta.role"`)
		}
	}
	var err error
	if out.Content, err = nullishString(obj, "content"); err != nil {
		return nil, err
	}
	if out.Reasoning, err = nullishString(obj, "reasoning"); err != nil {
		return nil, err
	}

	if rd, ok := obj["reasoning_details"]; ok && !isJSONNull(rd) {
		var entries []json.RawMessage
		if err := json.Unmarshal(rd, &entries); err != nil {
			return nil, errors.New(`Invalid input: expected array at "delta.reasoning_details"`)
		}
		details := make([]ReasoningDetail, 0, len(entries))
		for _, e := range entries {
			d, ok := parseReasoningDetail(e)
			if !ok {
				continue // mapped to null, then filtered
			}
			details = append(details, d)
		}
		out.ReasoningDetails = details
		out.HasReasoningDeta = true
	}

	if imgs, ok := obj["images"]; ok && !isJSONNull(imgs) {
		var entries []json.RawMessage
		if err := json.Unmarshal(imgs, &entries); err != nil {
			return nil, errors.New(`Invalid input: expected array at "delta.images"`)
		}
		for _, e := range entries {
			img, ok := parseImageResponse(e)
			if !ok {
				continue // ImageResponseWithUnknownSchema → null → filtered
			}
			out.Images = append(out.Images, img)
		}
	}

	if tc, ok := obj["tool_calls"]; ok && !isJSONNull(tc) {
		var entries []json.RawMessage
		if err := json.Unmarshal(tc, &entries); err != nil {
			return nil, errors.New(`Invalid input: expected array at "delta.tool_calls"`)
		}
		out.HasToolCalls = true
		for _, e := range entries {
			d, err := validateToolCallDelta(e)
			if err != nil {
				return nil, err
			}
			out.ToolCalls = append(out.ToolCalls, d)
		}
	}

	if ann, ok := obj["annotations"]; ok && !isJSONNull(ann) {
		var entries []json.RawMessage
		if err := json.Unmarshal(ann, &entries); err != nil {
			return nil, errors.New(`Invalid input: expected array at "delta.annotations"`)
		}
		out.HasAnnotations = true
		for _, e := range entries {
			a, err := validateAnnotation(e)
			if err != nil {
				return nil, err
			}
			out.Annotations = append(out.Annotations, a)
		}
	}
	return out, nil
}

func validateToolCallDelta(raw json.RawMessage) (ToolCallDelta, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ToolCallDelta{}, errors.New(`Invalid input: expected object at "delta.tool_calls[]"`)
	}
	out := ToolCallDelta{Raw: raw}
	var err error
	if out.Index, err = nullishNumber(obj, "index"); err != nil {
		return ToolCallDelta{}, err
	}
	if out.ID, err = nullishString(obj, "id"); err != nil {
		return ToolCallDelta{}, err
	}
	if t, ok := obj["type"]; ok {
		var typ string
		if err := json.Unmarshal(t, &typ); err != nil || typ != "function" {
			return ToolCallDelta{}, errors.New(`Invalid input: expected "function" at "delta.tool_calls[].type"`)
		}
		out.Type = &typ
	}
	// `function` is REQUIRED on the delta entry.
	fnRaw, ok := obj["function"]
	if !ok {
		return ToolCallDelta{}, errors.New(`Invalid input: expected object, received undefined at "delta.tool_calls[].function"`)
	}
	if isJSONNull(fnRaw) {
		return ToolCallDelta{}, errors.New(`Invalid input: expected object at "delta.tool_calls[].function"`)
	}
	var fn map[string]json.RawMessage
	if err := json.Unmarshal(fnRaw, &fn); err != nil {
		return ToolCallDelta{}, errors.New(`Invalid input: expected object at "delta.tool_calls[].function"`)
	}
	out.HasFunction = true
	if out.Name, err = nullishString(fn, "name"); err != nil {
		return ToolCallDelta{}, err
	}
	if out.Arguments, err = nullishString(fn, "arguments"); err != nil {
		return ToolCallDelta{}, err
	}
	return out, nil
}

func validateAnnotation(raw json.RawMessage) (Annotation, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return Annotation{}, errors.New(`Invalid input: expected object at "delta.annotations[]"`)
	}
	typeRaw, ok := obj["type"]
	if !ok {
		return Annotation{}, errors.New(`Invalid input: expected a discriminated annotation`)
	}
	var typ string
	if err := json.Unmarshal(typeRaw, &typ); err != nil {
		return Annotation{}, errors.New(`Invalid input: expected string at "delta.annotations[].type"`)
	}
	switch typ {
	case "url_citation":
		inner, ok := obj["url_citation"]
		if !ok {
			return Annotation{}, errors.New(`Invalid input: expected object at "url_citation"`)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(inner, &fields); err != nil {
			return Annotation{}, errors.New(`Invalid input: expected object at "url_citation"`)
		}
		url, ok := requiredString(fields, "url")
		if !ok {
			return Annotation{}, errors.New(`Invalid input: expected string at "url_citation.url"`)
		}
		a := Annotation{Type: typ, URL: url, Raw: raw}
		var err error
		if a.Title, err = optionalString(fields, "title"); err != nil {
			return Annotation{}, err
		}
		if a.StartIndex, err = optionalNumber(fields, "start_index"); err != nil {
			return Annotation{}, err
		}
		if a.EndIndex, err = optionalNumber(fields, "end_index"); err != nil {
			return Annotation{}, err
		}
		if a.Content, err = optionalString(fields, "content"); err != nil {
			return Annotation{}, err
		}
		normalizedInner, err := normalizePassthroughObject(inner,
			[]string{"url", "title", "start_index", "end_index", "content"}, nil)
		if err != nil {
			return Annotation{}, err
		}
		normalizedOuter, err := normalizePassthroughObject(raw,
			[]string{"type", "url_citation"},
			map[string]json.RawMessage{"url_citation": normalizedInner})
		if err != nil {
			return Annotation{}, err
		}
		a.Raw = normalizedOuter
		return a, nil
	case "file_annotation":
		inner, ok := obj["file_annotation"]
		if !ok || isJSONNull(inner) {
			return Annotation{}, errors.New(`Invalid input: expected object at "file_annotation"`)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(inner, &fields); err != nil {
			return Annotation{}, errors.New(`Invalid input: expected object at "file_annotation"`)
		}
		if _, ok := requiredString(fields, "file_id"); !ok {
			return Annotation{}, errors.New(`Invalid input: expected string at "file_annotation.file_id"`)
		}
		if _, err := optionalString(fields, "quote"); err != nil {
			return Annotation{}, err
		}
		normalizedInner, err := normalizePassthroughObject(inner,
			[]string{"file_id", "quote"}, nil)
		if err != nil {
			return Annotation{}, err
		}
		normalizedOuter, err := normalizePassthroughObject(raw,
			[]string{"type", "file_annotation"},
			map[string]json.RawMessage{"file_annotation": normalizedInner})
		if err != nil {
			return Annotation{}, err
		}
		return Annotation{Type: typ, Raw: normalizedOuter}, nil
	case "file":
		inner, ok := obj["file"]
		if !ok || isJSONNull(inner) {
			return Annotation{}, errors.New(`Invalid input: expected object at "file"`)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(inner, &fields); err != nil {
			return Annotation{}, errors.New(`Invalid input: expected object at "file"`)
		}
		if _, ok := requiredString(fields, "hash"); !ok {
			return Annotation{}, errors.New(`Invalid input: expected string at "file.hash"`)
		}
		if _, ok := requiredString(fields, "name"); !ok {
			return Annotation{}, errors.New(`Invalid input: expected string at "file.name"`)
		}
		overrides := map[string]json.RawMessage{}
		if content, present := fields["content"]; present {
			normalized, err := normalizeFileAnnotationContent(content)
			if err != nil {
				return Annotation{}, err
			}
			overrides["content"] = normalized
		}
		normalizedInner, err := normalizePassthroughObject(inner,
			[]string{"hash", "name", "content"}, overrides)
		if err != nil {
			return Annotation{}, err
		}
		normalizedOuter, err := normalizePassthroughObject(raw,
			[]string{"type", "file"},
			map[string]json.RawMessage{"file": normalizedInner})
		if err != nil {
			return Annotation{}, err
		}
		return Annotation{Type: typ, Raw: normalizedOuter}, nil
	}
	// The annotation union has NO unknown fallback, so an unrecognised entry
	// fails the whole chunk.
	return Annotation{}, errors.New(`Invalid input: unrecognised annotation type ` + typ)
}

func parseImageResponse(raw json.RawMessage) (ImageResponse, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ImageResponse{}, false
	}
	typ, ok := requiredString(obj, "type")
	if !ok || typ != "image_url" {
		return ImageResponse{}, false
	}
	inner, ok := obj["image_url"]
	if !ok {
		return ImageResponse{}, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(inner, &fields); err != nil {
		return ImageResponse{}, false
	}
	url, ok := requiredString(fields, "url")
	if !ok {
		return ImageResponse{}, false
	}
	return ImageResponse{URL: url}, true
}

// validateUsage checks the `usage` object: prompt_tokens,
// completion_tokens and total_tokens are all REQUIRED numbers, so a partial
// usage object fails the whole chunk.
func validateUsage(raw json.RawMessage) (*calc.OpenRouterUsage, error) {
	if raw == nil || isJSONNull(raw) {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, errors.New(`Invalid input: expected object at "usage"`)
	}
	for _, required := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
		v, ok := obj[required]
		if !ok {
			return nil, errors.New(`Invalid input: expected number, received undefined at "usage.` + required + `"`)
		}
		if !isJSONNumber(v) {
			return nil, errors.New(`Invalid input: expected number at "usage.` + required + `"`)
		}
	}
	overrides := map[string]json.RawMessage{}
	if d, ok := obj["prompt_tokens_details"]; ok && !isJSONNull(d) {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(d, &fields); err != nil {
			return nil, errors.New(`Invalid input: expected object at "usage.prompt_tokens_details"`)
		}
		cached, ok := fields["cached_tokens"]
		if !ok {
			return nil, errors.New(`Invalid input: expected number, received undefined at "usage.prompt_tokens_details.cached_tokens"`)
		}
		if !isJSONNumber(cached) {
			return nil, errors.New(`Invalid input: expected number at "usage.prompt_tokens_details.cached_tokens"`)
		}
		if cacheWrite, present := fields["cache_write_tokens"]; present &&
			!isJSONNull(cacheWrite) && !isJSONNumber(cacheWrite) {
			return nil, errors.New(`Invalid input: expected number at "usage.prompt_tokens_details.cache_write_tokens"`)
		}
		normalized, err := normalizePassthroughObject(d,
			[]string{"cached_tokens", "cache_write_tokens"}, nil)
		if err != nil {
			return nil, err
		}
		overrides["prompt_tokens_details"] = normalized
	}
	if d, ok := obj["completion_tokens_details"]; ok && !isJSONNull(d) {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(d, &fields); err != nil {
			return nil, errors.New(`Invalid input: expected object at "usage.completion_tokens_details"`)
		}
		reasoning, ok := fields["reasoning_tokens"]
		if !ok {
			return nil, errors.New(`Invalid input: expected number, received undefined at "usage.completion_tokens_details.reasoning_tokens"`)
		}
		if !isJSONNumber(reasoning) {
			return nil, errors.New(`Invalid input: expected number at "usage.completion_tokens_details.reasoning_tokens"`)
		}
		normalized, err := normalizePassthroughObject(d,
			[]string{"reasoning_tokens"}, nil)
		if err != nil {
			return nil, err
		}
		overrides["completion_tokens_details"] = normalized
	}
	if cost, present := obj["cost"]; present && !isJSONNumber(cost) {
		return nil, errors.New(`Invalid input: expected number at "usage.cost"`)
	}
	if details, present := obj["cost_details"]; present && !isJSONNull(details) {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(details, &fields); err != nil {
			return nil, errors.New(`Invalid input: expected object at "usage.cost_details"`)
		}
		if upstream, ok := fields["upstream_inference_cost"]; ok &&
			!isJSONNull(upstream) && !isJSONNumber(upstream) {
			return nil, errors.New(`Invalid input: expected number at "usage.cost_details.upstream_inference_cost"`)
		}
		normalized, err := normalizePassthroughObject(details,
			[]string{"upstream_inference_cost"}, nil)
		if err != nil {
			return nil, err
		}
		overrides["cost_details"] = normalized
	}
	normalized, err := normalizePassthroughObject(raw, []string{
		"prompt_tokens",
		"prompt_tokens_details",
		"completion_tokens",
		"completion_tokens_details",
		"total_tokens",
		"cost",
		"cost_details",
	}, overrides)
	if err != nil {
		return nil, err
	}
	var usage calc.OpenRouterUsage
	if err := json.Unmarshal(normalized, &usage); err != nil {
		return nil, errors.New(`Invalid input: expected object at "usage"`)
	}
	return &usage, nil
}

// ── the error shape ──────────────────────────────────────────────────────

// validateErrorResponse checks the error-response shape: `error.message` is
// required, and `code`/`type`/`param` default to explicit nulls in the
// normalized object, in SHAPE order, with extra keys after.
func validateErrorResponse(raw json.RawMessage) (*ChunkValue, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, errors.New(`expected object`)
	}
	errRaw, ok := obj["error"]
	if !ok {
		return nil, errors.New(`Invalid input: expected object, received undefined at "error"`)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(errRaw, &fields); err != nil {
		return nil, errors.New(`Invalid input: expected object at "error"`)
	}
	if _, ok := requiredString(fields, "message"); !ok {
		return nil, errors.New(`Invalid input: expected string at "error.message"`)
	}
	if code, present := fields["code"]; present && !isJSONNull(code) &&
		!isJSONString(code) && !isJSONNumber(code) {
		return nil, errors.New(`Invalid input: expected string or number at "error.code"`)
	}
	if typ, present := fields["type"]; present && !isJSONNull(typ) && !isJSONString(typ) {
		return nil, errors.New(`Invalid input: expected string at "error.type"`)
	}

	ordered, err := ParseObject(errRaw)
	if err != nil {
		return nil, err
	}
	normalized := NewObject()
	for _, key := range []string{"code", "message", "type", "param"} {
		if v, ok := ordered.Get(key); ok {
			if err := normalized.Set(key, v); err != nil {
				return nil, err
			}
			continue
		}
		if key == "message" {
			continue
		}
		normalized.set(key, jsonValue{Kind: kindNull})
	}
	for _, key := range ordered.Keys() {
		if normalized.Has(key) {
			continue
		}
		v, _ := ordered.Get(key)
		if err := normalized.Set(key, v); err != nil {
			return nil, err
		}
	}
	encoded, err := normalized.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return &ChunkValue{ErrorField: encoded}, nil
}

// ── small field helpers ──────────────────────────────────────────────────

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// optionalString reads an optional string: absent is fine, null is NOT.
func optionalString(obj map[string]json.RawMessage, key string) (*string, error) {
	raw, ok := obj[key]
	if !ok {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, errors.New(`Invalid input: expected string at "` + key + `"`)
	}
	return &s, nil
}

// nullishString reads a string that may be absent or null; both give nil.
func nullishString(obj map[string]json.RawMessage, key string) (*string, error) {
	raw, ok := obj[key]
	if !ok || isJSONNull(raw) {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, errors.New(`Invalid input: expected string at "` + key + `"`)
	}
	return &s, nil
}

// nullableOptionalString is nullishString under the name the wire field
// declares.
func nullableOptionalString(obj map[string]json.RawMessage, key string) (*string, error) {
	return nullishString(obj, key)
}

func nullishNumber(obj map[string]json.RawMessage, key string) (*float64, error) {
	raw, ok := obj[key]
	if !ok || isJSONNull(raw) {
		return nil, nil
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, errors.New(`Invalid input: expected number at "` + key + `"`)
	}
	return &n, nil
}

func optionalNumber(obj map[string]json.RawMessage, key string) (*float64, error) {
	raw, ok := obj[key]
	if !ok {
		return nil, nil
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, errors.New(`Invalid input: expected number at "` + key + `"`)
	}
	return &n, nil
}

// validateLogprobs validates choice.logprobs, which nothing reads afterwards:
// a malformed block still turns the whole SSE chunk into an error part.
func validateLogprobs(raw json.RawMessage) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return errors.New(`Invalid input: expected object at "choices[].logprobs"`)
	}
	content, ok := obj["content"]
	if !ok {
		return errors.New(`Invalid input: expected array at "choices[].logprobs.content"`)
	}
	// content may be an explicit null.
	if isJSONNull(content) {
		return nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(content, &entries); err != nil {
		return errors.New(`Invalid input: expected array at "choices[].logprobs.content"`)
	}
	for _, entry := range entries {
		var token map[string]json.RawMessage
		if err := json.Unmarshal(entry, &token); err != nil {
			return errors.New(`Invalid input: expected object at "choices[].logprobs.content[]"`)
		}
		if _, ok := requiredString(token, "token"); !ok {
			return errors.New(`Invalid input: expected string at "choices[].logprobs.content[].token"`)
		}
		if value, ok := token["logprob"]; !ok || !isJSONNumber(value) {
			return errors.New(`Invalid input: expected number at "choices[].logprobs.content[].logprob"`)
		}
		top, ok := token["top_logprobs"]
		if !ok || isJSONNull(top) {
			return errors.New(`Invalid input: expected array at "choices[].logprobs.content[].top_logprobs"`)
		}
		var alternatives []json.RawMessage
		if err := json.Unmarshal(top, &alternatives); err != nil {
			return errors.New(`Invalid input: expected array at "choices[].logprobs.content[].top_logprobs"`)
		}
		for _, alternative := range alternatives {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(alternative, &fields); err != nil {
				return errors.New(`Invalid input: expected object at "choices[].logprobs.content[].top_logprobs[]"`)
			}
			if _, ok := requiredString(fields, "token"); !ok {
				return errors.New(`Invalid input: expected string at "choices[].logprobs.content[].top_logprobs[].token"`)
			}
			if value, ok := fields["logprob"]; !ok || !isJSONNumber(value) {
				return errors.New(`Invalid input: expected number at "choices[].logprobs.content[].top_logprobs[].logprob"`)
			}
		}
	}
	return nil
}

func normalizeFileAnnotationContent(raw json.RawMessage) (json.RawMessage, error) {
	if isJSONNull(raw) {
		return nil, errors.New(`Invalid input: expected array at "file.content"`)
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, errors.New(`Invalid input: expected array at "file.content"`)
	}
	values := make([]jsonValue, 0, len(entries))
	for _, entry := range entries {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(entry, &fields); err != nil {
			return nil, errors.New(`Invalid input: expected object at "file.content[]"`)
		}
		if _, ok := requiredString(fields, "type"); !ok {
			return nil, errors.New(`Invalid input: expected string at "file.content[].type"`)
		}
		if _, err := optionalString(fields, "text"); err != nil {
			return nil, err
		}
		normalized, err := normalizePassthroughObject(entry, []string{"type", "text"}, nil)
		if err != nil {
			return nil, err
		}
		value, err := parseJSONValue(normalized)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return marshalJSONValue(jsonValue{Kind: kindArray, Array: values})
}

// normalizePassthroughObject re-emits an object with the declared shape keys
// first, in shape order, followed by the remaining keys in source order.
// Overrides contain already-normalized nested objects.
func normalizePassthroughObject(
	raw json.RawMessage,
	shape []string,
	overrides map[string]json.RawMessage,
) (json.RawMessage, error) {
	source, err := ParseObject(raw)
	if err != nil {
		return nil, err
	}
	out := NewObject()
	for _, key := range shape {
		value, present := source.Get(key)
		if !present {
			continue
		}
		if override, ok := overrides[key]; ok {
			value = override
		}
		if err := out.Set(key, value); err != nil {
			return nil, err
		}
	}
	for _, key := range source.Keys() {
		if out.Has(key) {
			continue
		}
		value, _ := source.Get(key)
		if err := out.Set(key, value); err != nil {
			return nil, err
		}
	}
	return out.MarshalJSON()
}

func isJSONNumber(raw json.RawMessage) bool {
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	number, ok := token.(json.Number)
	if !ok {
		return false
	}
	var n float64
	return json.Unmarshal([]byte(number.String()), &n) == nil
}

func isJSONString(raw json.RawMessage) bool {
	var value string
	return json.Unmarshal(raw, &value) == nil
}

// getMediaType reads the media type of a data URL, else the default.
func getMediaType(dataURL, defaultMediaType string) string {
	if !strings.HasPrefix(dataURL, "data:") {
		return defaultMediaType
	}
	rest := dataURL[len("data:"):]
	end := strings.IndexByte(rest, ';')
	if end < 0 {
		end = len(rest)
	}
	if end == 0 {
		return defaultMediaType
	}
	return rest[:end]
}

// InvalidResponseDataError is returned by the tool-call accumulator for a
// malformed first delta. It tears the whole stream down; it is not an `error`
// part.
type InvalidResponseDataError struct {
	Message string
	Data    json.RawMessage
}

func (e *InvalidResponseDataError) Error() string { return e.Message }

func newInvalidResponseDataError(message string, data any) *InvalidResponseDataError {
	encoded, err := json.Marshal(data)
	if err != nil {
		encoded = nil
	}
	return &InvalidResponseDataError{Message: message, Data: encoded}
}
