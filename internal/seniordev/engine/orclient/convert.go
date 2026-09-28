//go:build !windows

package orclient

// The `messages` field of the request body.
//
// Everything about this conversion is byte-order-sensitive: the wire body
// feeds OpenRouter's prompt cache, and the assistant
// `tool_calls[].function.arguments` string feeds Anthropic signature
// validation through DeterministicStringify. So every emitted object is an
// ordered Object, not a map, and every optional key is *absent* rather than
// null unless the wire wants an explicit null.
//
// `cache_control` is threaded through from provider options: it decides
// whether a single-text-part user message serialises as a bare string or as a
// one-element array.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

// ── getCacheControl ──────────────────────────────────────────────────────

// getCacheControl reads `openrouter.cacheControl ?? openrouter.cache_control ??
// anthropic.cacheControl ?? anthropic.cache_control` off a providerOptions bag.
func getCacheControl(providerOptions json.RawMessage) json.RawMessage {
	if len(providerOptions) == 0 {
		return nil
	}
	obj, err := ParseObject(providerOptions)
	if err != nil {
		return nil
	}
	for _, ns := range []string{Service, "anthropic"} {
		nsRaw, ok := obj.Get(ns)
		if !ok {
			continue
		}
		inner, err := ParseObject(nsRaw)
		if err != nil {
			continue
		}
		for _, key := range []string{"cacheControl", "cache_control"} {
			if v, ok := inner.Get(key); ok && rawTruthy(v) {
				return v
			}
		}
	}
	return nil
}

// ── the duplicate tracker ────────────────────────────────────────────────
//
// One tracker per convertToOpenRouterChatMessages CALL, shared across every
// assistant message in the prompt — so the same reasoning text appearing on two
// assistant turns is emitted once, on the first.

type reasoningDuplicateTracker struct{ seen map[string]bool }

func newReasoningDuplicateTracker() *reasoningDuplicateTracker {
	return &reasoningDuplicateTracker{seen: map[string]bool{}}
}

func (t *reasoningDuplicateTracker) upsert(d ReasoningDetail) bool {
	key, ok := canonicalReasoningKey(d)
	if !ok {
		return false
	}
	if t.seen[key] {
		return false
	}
	t.seen[key] = true
	return true
}

func canonicalReasoningKey(d ReasoningDetail) (string, bool) {
	switch d.Type {
	case ReasoningDetailSummary:
		return d.Summary, true
	case ReasoningDetailEncrypted:
		if id := rawString(d.ID); id != "" {
			return id, true
		}
		return d.Data, true
	case ReasoningDetailText:
		if text := rawString(d.Text); text != "" {
			return text, true
		}
		if sig := rawString(d.Signature); sig != "" {
			return sig, true
		}
		return "", false
	}
	return "", false
}

// ── ConvertToOpenRouterChatMessages ───────────────────────────────────────

// ConvertToOpenRouterChatMessages converts the prompt into wire messages.
// The returned slice is what lands in the body's `messages` field.
func ConvertToOpenRouterChatMessages(prompt []msgmodel.ModelMessage) ([]*Object, error) {
	messages := []*Object{}
	tracker := newReasoningDuplicateTracker()

	// Images lifted out of tool results. The chat-completions schema types a
	// tool message's content as text and specifies `image_url` for user
	// messages, so the image travels in a user message instead. It cannot go
	// out immediately: every tool message of an assistant turn has to stay
	// contiguous and directly follow that turn, so the images buffer across a
	// whole run of tool messages and flush as ONE user message when the run
	// ends -- at the next non-tool message, or at the end of the prompt.
	var pendingImages []msgmodel.ToolOutputContentMedia
	flushImages := func() error {
		if len(pendingImages) == 0 {
			return nil
		}
		out, err := toolImageUserMessage(pendingImages)
		if err != nil {
			return err
		}
		pendingImages = nil
		messages = append(messages, out)
		return nil
	}

	for _, msg := range prompt {
		if msg.Role != "tool" {
			if err := flushImages(); err != nil {
				return nil, err
			}
		}
		switch msg.Role {
		case "system":
			cacheControl := getCacheControl(msg.ProviderOptions)
			text := NewObject()
			text.SetString("type", "text")
			content, _ := msg.Content.(string)
			text.SetString("text", content)
			if cacheControl != nil {
				if err := text.Set("cache_control", cacheControl); err != nil {
					return nil, err
				}
			}
			out := NewObject()
			out.SetString("role", "system")
			out.set("content", jsonValue{Kind: kindArray, Array: []jsonValue{text.value()}})
			messages = append(messages, out)

		case "user":
			out, err := convertUserMessage(msg)
			if err != nil {
				return nil, err
			}
			messages = append(messages, out)

		case "assistant":
			out, err := convertAssistantMessage(msg, tracker)
			if err != nil {
				return nil, err
			}
			messages = append(messages, out)

		case "tool":
			parts, _ := msg.Content.([]any)
			for _, part := range parts {
				tr, ok := part.(msgmodel.ToolResultContent)
				if !ok {
					// Anything that is not a tool result is skipped.
					continue
				}
				content, images, err := getToolResultContent(tr)
				if err != nil {
					return nil, err
				}
				pendingImages = append(pendingImages, images...)
				out := NewObject()
				out.SetString("role", "tool")
				out.SetString("tool_call_id", tr.ToolCallID)
				out.set("content", content)
				out.SetString("name", tr.ToolName)
				cc := getCacheControl(msg.ProviderOptions)
				if cc == nil {
					cc = getCacheControl(tr.ProviderOptions)
				}
				if cc != nil {
					if err := out.Set("cache_control", cc); err != nil {
						return nil, err
					}
				}
				messages = append(messages, out)
			}

		default:
			// Any other role is dropped entirely.
		}
	}
	if err := flushImages(); err != nil {
		return nil, err
	}
	return messages, nil
}

// toolImageUserMessage carries the images named in a run of tool results,
// introduced by a line that ties them back to those results. Each image part
// is built by the user-message part converter, so it is the part the provider
// would see for the same file attached by hand.
func toolImageUserMessage(images []msgmodel.ToolOutputContentMedia) (*Object, error) {
	parts := make([]jsonValue, 0, len(images)+1)
	lead := NewObject()
	lead.SetString("type", "text")
	lead.SetString("text", toolImageUserMessageText)
	parts = append(parts, lead.value())
	for _, image := range images {
		part, err := convertUserPart(msgmodel.FileContent{
			Type:      "file",
			MediaType: image.MediaType,
			Data:      image.Data,
		}, nil)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	out := NewObject()
	out.SetString("role", "user")
	out.set("content", jsonValue{Kind: kindArray, Array: parts})
	return out, nil
}

// toolImageUserMessageText introduces the relocated images.
const toolImageUserMessageText = "The images below are attachments from the preceding tool results, " +
	"in the order those tools returned them."

// contentParts is the only reader of ModelMessage.Content for user and
// assistant messages. It accepts the two shapes the runtime legitimately
// produces -- a []any of content parts (what ConvertToModelMessages builds) and
// a bare string (a hand-written prompt) -- and REFUSES everything else.
//
// A silent fallback here would turn any other shape into an empty message: a
// typed []TextContent slice, or an empty trailing user turn, would go out as
// `"content": []` and the model would see no transcript at all. An
// unconvertible message is a programming error and must fail here, at the
// wire.
func contentParts(msg msgmodel.ModelMessage) ([]any, error) {
	switch content := msg.Content.(type) {
	case []any:
		if len(content) == 0 {
			return nil, fmt.Errorf(
				"orclient: %s message has no content parts", msg.Role,
			)
		}
		return content, nil
	case string:
		if content == "" {
			return nil, fmt.Errorf("orclient: %s message has empty text content", msg.Role)
		}
		return []any{msgmodel.TextContent{Type: "text", Text: content}}, nil
	case nil:
		return nil, fmt.Errorf("orclient: %s message has nil content", msg.Role)
	default:
		return nil, fmt.Errorf(
			"orclient: %s message content must be a string or []any of content parts, got %T (build it with msgmodel.UserText)",
			msg.Role, msg.Content,
		)
	}
}

func convertUserMessage(msg msgmodel.ModelMessage) (*Object, error) {
	parts, err := contentParts(msg)
	if err != nil {
		return nil, err
	}

	// Single text part → bare string content, unless a cache_control applies.
	if len(parts) == 1 {
		if text, ok := parts[0].(msgmodel.TextContent); ok {
			cc := getCacheControl(msg.ProviderOptions)
			if cc == nil {
				cc = getCacheControl(text.ProviderOptions)
			}
			out := NewObject()
			out.SetString("role", "user")
			if cc != nil {
				part := NewObject()
				part.SetString("type", "text")
				part.SetString("text", text.Text)
				if err := part.Set("cache_control", cc); err != nil {
					return nil, err
				}
				out.set("content", jsonValue{Kind: kindArray, Array: []jsonValue{part.value()}})
			} else {
				out.SetString("content", text.Text)
			}
			return out, nil
		}
	}

	messageCacheControl := getCacheControl(msg.ProviderOptions)
	lastTextPartIndex := -1
	for i := len(parts) - 1; i >= 0; i-- {
		if _, ok := parts[i].(msgmodel.TextContent); ok {
			lastTextPartIndex = i
			break
		}
	}

	contentParts := make([]jsonValue, 0, len(parts))
	for index, part := range parts {
		var partProviderOptions json.RawMessage
		isText := false
		switch p := part.(type) {
		case msgmodel.TextContent:
			partProviderOptions = p.ProviderOptions
			isText = true
		case msgmodel.FileContent:
			partProviderOptions = p.ProviderOptions
		}
		partCacheControl := getCacheControl(partProviderOptions)
		cacheControl := partCacheControl
		if isText && partCacheControl == nil && index == lastTextPartIndex {
			cacheControl = messageCacheControl
		}

		converted, err := convertUserPart(part, cacheControl)
		if err != nil {
			return nil, err
		}
		contentParts = append(contentParts, converted)
	}

	out := NewObject()
	out.SetString("role", "user")
	out.set("content", jsonValue{Kind: kindArray, Array: contentParts})
	return out, nil
}

func convertUserPart(part any, cacheControl json.RawMessage) (jsonValue, error) {
	withCC := func(o *Object) (jsonValue, error) {
		if cacheControl != nil {
			if err := o.Set("cache_control", cacheControl); err != nil {
				return jsonValue{}, err
			}
		}
		return o.value(), nil
	}

	switch p := part.(type) {
	case msgmodel.TextContent:
		o := NewObject()
		o.SetString("type", "text")
		o.SetString("text", p.Text)
		return withCC(o)

	case msgmodel.FileContent:
		switch {
		case strings.HasPrefix(p.MediaType, "image/"):
			o := NewObject()
			o.SetString("type", "image_url")
			inner := NewObject()
			inner.SetString("url", buildFileDataURL(p.Data, p.MediaType, "image/jpeg"))
			o.SetObject("image_url", inner)
			return withCC(o)
		case strings.HasPrefix(p.MediaType, "video/"):
			o := NewObject()
			o.SetString("type", "video_url")
			inner := NewObject()
			inner.SetString("url", buildFileDataURL(p.Data, p.MediaType, "video/mp4"))
			o.SetObject("video_url", inner)
			return withCC(o)
		case strings.HasPrefix(p.MediaType, "audio/"):
			audio, err := inputAudioData(p)
			if err != nil {
				return jsonValue{}, err
			}
			o := NewObject()
			o.SetString("type", "input_audio")
			o.SetObject("input_audio", audio)
			return withCC(o)
		}
		fileName := ""
		if opts, err := ParseObject(p.ProviderOptions); err == nil {
			if nsRaw, ok := opts.Get(Service); ok {
				if ns, err := ParseObject(nsRaw); err == nil {
					if v, ok := ns.Get("filename"); ok {
						fileName = textOf(rawJSONValue(v))
					}
				}
			}
		}
		if fileName == "" && len(p.Filename) > 0 {
			fileName = textOf(rawJSONValue(p.Filename))
		}
		fileData := buildFileDataURL(p.Data, p.MediaType, "application/pdf")
		o := NewObject()
		o.SetString("type", "file")
		inner := NewObject()
		inner.SetString("filename", fileName)
		inner.SetString("file_data", fileData)
		o.SetObject("file", inner)
		if isHTTPURL(fileData) {
			// The http(s) branch returns WITHOUT cache_control.
			return o.value(), nil
		}
		return withCC(o)
	}

	// Anything else becomes an empty text part.
	o := NewObject()
	o.SetString("type", "text")
	o.SetString("text", "")
	return withCC(o)
}

func convertAssistantMessage(msg msgmodel.ModelMessage, tracker *reasoningDuplicateTracker) (*Object, error) {
	parts, err := contentParts(msg)
	if err != nil {
		return nil, err
	}

	var text, reasoning strings.Builder
	toolCalls := []jsonValue{}
	for _, part := range parts {
		switch p := part.(type) {
		case msgmodel.TextContent:
			text.WriteString(p.Text)
		case msgmodel.ToolCallContent:
			args, err := DeterministicStringify(p.Input)
			if err != nil {
				if errors.Is(err, errUndefinedStringify) {
					// An absent input means no `arguments` key on the wire
					// object.
					args = nil
				} else {
					return nil, errors.New("orclient: tool call " + p.ToolCallID + ": " + err.Error())
				}
			}
			call := NewObject()
			call.SetString("id", p.ToolCallID)
			call.SetString("type", "function")
			fn := NewObject()
			fn.SetString("name", p.ToolName)
			if args != nil {
				fn.SetString("arguments", string(args))
			}
			call.SetObject("function", fn)
			toolCalls = append(toolCalls, call.value())
		case msgmodel.ReasoningContent:
			reasoning.WriteString(p.Text)
		case msgmodel.FileContent:
			// File parts are not sent on assistant messages.
		}
	}

	messageDetails, messageDetailsPresent := openrouterReasoningDetails(msg.ProviderOptions)
	annotations, _ := openrouterAnnotations(msg.ProviderOptions)

	candidate := messageDetails
	haveCandidate := messageDetailsPresent
	if !haveCandidate {
		candidate, haveCandidate = findFirstReasoningDetails(parts)
	}

	var finalDetails []ReasoningDetail
	haveFinal := false
	if haveCandidate {
		valid := make([]ReasoningDetail, 0, len(candidate))
		for _, d := range candidate {
			if d.Type != ReasoningDetailText {
				valid = append(valid, d)
				continue
			}
			format := rawString(d.Format)
			if format == "" {
				format = DefaultReasoningFormat
			}
			if format != "anthropic-claude-v1" && format != "google-gemini-v1" {
				valid = append(valid, d)
				continue
			}
			if rawTruthy(d.Signature) {
				valid = append(valid, d)
			}
		}
		unique := make([]ReasoningDetail, 0, len(valid))
		for _, d := range valid {
			if tracker.upsert(d) {
				unique = append(unique, d)
			}
		}
		finalDetails = unique
		haveFinal = true
	}

	out := NewObject()
	out.SetString("role", "assistant")
	out.SetString("content", text.String())
	if len(toolCalls) > 0 {
		out.set("tool_calls", jsonValue{Kind: kindArray, Array: toolCalls})
	}
	// Reasoning text is sent only alongside non-empty reasoning details.
	if reasoning.Len() > 0 && haveFinal && len(finalDetails) > 0 {
		out.SetString("reasoning", reasoning.String())
	}
	if haveFinal {
		encoded, err := json.Marshal(finalDetails)
		if err != nil {
			return nil, err
		}
		if err := out.Set("reasoning_details", encoded); err != nil {
			return nil, err
		}
	}
	if annotations != nil {
		if err := out.Set("annotations", annotations); err != nil {
			return nil, err
		}
	}
	if cc := getCacheControl(msg.ProviderOptions); cc != nil {
		if err := out.Set("cache_control", cc); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// findFirstReasoningDetails looks at tool-call parts first, then reasoning
// parts, taking the first non-empty array found.
func findFirstReasoningDetails(parts []any) ([]ReasoningDetail, bool) {
	for _, part := range parts {
		p, ok := part.(msgmodel.ToolCallContent)
		if !ok {
			continue
		}
		if details, present := openrouterReasoningDetails(p.ProviderOptions); present && len(details) > 0 {
			return details, true
		}
	}
	for _, part := range parts {
		p, ok := part.(msgmodel.ReasoningContent)
		if !ok {
			continue
		}
		if details, present := openrouterReasoningDetails(p.ProviderOptions); present && len(details) > 0 {
			return details, true
		}
	}
	return nil, false
}

func openrouterReasoningDetails(providerOptions json.RawMessage) ([]ReasoningDetail, bool) {
	raw, ok := openrouterNamespaceField(providerOptions, "reasoning_details")
	if !ok {
		return nil, false
	}
	details := ParseReasoningDetails(raw)
	if details == nil {
		// Not an array at all: treated as absent.
		return nil, false
	}
	return details, true
}

func openrouterAnnotations(providerOptions json.RawMessage) (json.RawMessage, bool) {
	return openrouterNamespaceField(providerOptions, "annotations")
}

func openrouterNamespaceField(providerOptions json.RawMessage, field string) (json.RawMessage, bool) {
	if len(providerOptions) == 0 {
		return nil, false
	}
	obj, err := ParseObject(providerOptions)
	if err != nil {
		return nil, false
	}
	nsRaw, ok := obj.Get(Service)
	if !ok {
		return nil, false
	}
	ns, err := ParseObject(nsRaw)
	if err != nil {
		return nil, false
	}
	v, ok := ns.Get(field)
	if !ok {
		return nil, false
	}
	return v, true
}

// ── getToolResultContent ─────────────────────────────────────────────────

// getToolResultContent returns the tool message's content and the images that
// content only names: the caller sends those on in a user message.
func getToolResultContent(tr msgmodel.ToolResultContent) (jsonValue, []msgmodel.ToolOutputContentMedia, error) {
	switch tr.Output.Type {
	case "text", "error-text":
		s, _ := tr.Output.Value.(string)
		return stringValue(s), nil, nil
	case "json", "error-json":
		encoded, err := json.Marshal(tr.Output.Value)
		if err != nil {
			return jsonValue{}, nil, err
		}
		return stringValue(string(encoded)), nil, nil
	case "content":
		items, _ := tr.Output.Value.([]any)
		out := make([]jsonValue, 0, len(items))
		var images []msgmodel.ToolOutputContentMedia
		for _, item := range items {
			mapped, image, err := mapToolResultContentPart(item)
			if err != nil {
				return jsonValue{}, nil, err
			}
			out = append(out, mapped)
			if image != nil {
				images = append(images, *image)
			}
		}
		return jsonValue{Kind: kindArray, Array: out}, images, nil
	case "execution-denied":
		reason, ok := tr.Output.Value.(string)
		if !ok || reason == "" {
			reason = "Tool execution denied"
		}
		return stringValue(reason), nil, nil
	}
	// An unknown output type serialises as null content.
	return jsonValue{Kind: kindNull}, nil, nil
}

// mapToolResultContentPart handles the two element shapes toModelOutput can
// produce: a text part and a `media` part. Anything else is stringified whole.
// An image is returned alongside its note so the caller can send it where the
// wire accepts an image; every other part returns a nil image.
func mapToolResultContentPart(item any) (jsonValue, *msgmodel.ToolOutputContentMedia, error) {
	switch p := item.(type) {
	case msgmodel.ToolOutputContentText:
		o := NewObject()
		o.SetString("type", "text")
		o.SetString("text", p.Text)
		return o.value(), nil, nil
	case msgmodel.ToolOutputContentMedia:
		mediaType := p.MediaType
		if mediaType == "" {
			mediaType = "unknown"
		}
		// A tool message's content is text on this schema, so an image is
		// only announced here and sent as an `image_url` part on the user
		// message that follows the tool run.
		if strings.HasPrefix(p.MediaType, "image/") {
			o := NewObject()
			o.SetString("type", "text")
			o.SetString("text", "[attachment: "+mediaType+" (sent as an image in the next user message)]")
			return o.value(), &p, nil
		}
		// Any other media -- a PDF, say -- has no inline shape the model
		// can read at all, and its base64 payload would cost a fortune in
		// tokens for nothing. Name the attachment instead of sending it.
		o := NewObject()
		o.SetString("type", "text")
		o.SetString("text", "[attachment: "+mediaType+" (content not sent)]")
		return o.value(), nil, nil
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		return jsonValue{}, nil, err
	}
	o := NewObject()
	o.SetString("type", "text")
	o.SetString("text", string(encoded))
	return o.value(), nil, nil
}

// ── file url helpers ─────────────────────────────────────────────────────

func buildFileDataURL(data, mediaType, defaultMediaType string) string {
	if isHTTPURL(data) {
		return data
	}
	if strings.HasPrefix(data, "data:") {
		return data
	}
	mt := mediaType
	if mt == "" {
		mt = defaultMediaType
	}
	return "data:" + mt + ";base64," + data
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// mimeToAudioFormat maps an audio mime subtype to the wire format name.
var mimeToAudioFormat = map[string]string{
	"mpeg":   "mp3",
	"mp3":    "mp3",
	"x-wav":  "wav",
	"wave":   "wav",
	"wav":    "wav",
	"ogg":    "ogg",
	"x-flac": "flac",
	"flac":   "flac",
	"aac":    "aac",
	"x-m4a":  "m4a",
	"m4a":    "m4a",
	"mp4":    "m4a",
	"webm":   "webm",
	"opus":   "opus",
	"pcm":    "pcm16",
	"pcm16":  "pcm16",
	"L16":    "pcm16",
}

func inputAudioData(p msgmodel.FileContent) (*Object, error) {
	fileData := buildFileDataURL(p.Data, p.MediaType, "audio/mpeg")
	data := base64FromDataURL(fileData)
	mediaType := p.MediaType
	if mediaType == "" {
		mediaType = "audio/mpeg"
	}
	rawFormat := strings.Replace(mediaType, "audio/", "", 1)
	format, ok := mimeToAudioFormat[rawFormat]
	if !ok {
		return nil, fmt.Errorf("Unsupported audio format: %q", mediaType)
	}
	o := NewObject()
	o.SetString("data", data)
	o.SetString("format", format)
	return o, nil
}

// base64FromDataURL extracts the payload of a `data:<type>;base64,<payload>`
// URL, falling back to the input when it does not match.
func base64FromDataURL(dataURL string) string {
	if !strings.HasPrefix(dataURL, "data:") {
		return dataURL
	}
	rest := dataURL[len("data:"):]
	semi := strings.IndexByte(rest, ';')
	if semi < 0 {
		return dataURL
	}
	if !strings.HasPrefix(rest[semi:], ";base64,") {
		return dataURL
	}
	payload := rest[semi+len(";base64,"):]
	if payload == "" {
		return dataURL
	}
	// `[^;]*` forbids a `;` before the `;base64,`.
	if strings.ContainsRune(rest[:semi], ';') {
		return dataURL
	}
	return payload
}
