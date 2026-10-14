package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// RecoverMachinery turns a reply whose tool call arrived as text back into a
// real tool call, so a leak costs a retry instead of the turn. Nothing here
// guesses: the tool must have been advertised, the arguments must parse, and
// the markup must be the reply rather than a quotation in it. Every refusal
// falls back to the cut that already existed.

// Minimum markup in a recovered span. Below it the markup is a quotation
// rather than the call; every real invocation is longer.
const recoverShapeFloor = 12

// A leak is the tail of the reply: a short lead-in is ordinary, a long tail
// resuming after the markup is somebody talking about tool calls.
const (
	recoverLeadAllowance = 160
	recoverLeadRatio     = 0.35
	recoverTailAllowance = 60
	recoverTailRatio     = 0.15
)

// What separates a leaked call from a quoted one. A reply explaining what a
// call looks like is also dense enough for MachineryLeak to convict, and
// differs only in having prose around the markup. The measured groups and
// the bias behind the bound:
// TestTheProseCeilingSeparatesCapturedLeaksFromQuotations.
const recoverProseCeiling = 0.45

// The fullwidth spelling folds to ASCII one character at a time, so every offset
// taken from the folded text still indexes the original.
var recoverFold = strings.NewReplacer(
	"＜", "<", // ＜
	"｜", "|", // ｜
	"＞", ">", // ＞
	"▁", "_", // ▁
	"／", "/", // ／
	"（", "(", // （
	"）", ")", // ）
)

var (
	// The wrapper only: the tag's own < stays, because an earlier fold that ate
	// it left a bare `tool_calls>` that no pattern could ever match again.
	recoverDSMLToken = regexp.MustCompile(`(?i)\|\s*/?\s*DSML\s*\|`)



	// The one spelling that matters: the invocation. The `[^>]*` before the close
	// is load-bearing — a provider that adds an attribute after the name
	// (`string="true"`) otherwise defeats the match, and the call it carries is
	// silently unrecoverable.
	recoverInvokeTag = regexp.MustCompile(`<invoke\s+name\s*=\s*"?([A-Za-z_][\w.:\-]*)"?\s*[^>]*>`)
	// A reply that opens a code fence is SHOWING code on purpose — the same
	// conservatism [MachineryLeak] keeps for itself.
	recoverFence = regexp.MustCompile("(?s)```.*?```")

	recoverParamArguments = regexp.MustCompile(`(?s)<parameter\s+name\s*=\s*"arguments"[^>]*>(.*?)(?:</parameter>|$)`)
	recoverParamAny       = regexp.MustCompile(`(?s)<parameter\s+name\s*=\s*"([^"]+)"[^>]*>(.*?)(?:</parameter>|$)`)
)

// Envelopes that wrap the calls and carry no meaning of their own.
var recoverEnvelopes = []string{
	"<|tool_calls|>", "</|tool_calls|>", "<|/tool_calls|>", "<|tool_sep|>", "<|tool|>",
	"<tool_calls>", "</tool_calls>", "<function_calls>", "</function_calls>",
}

// Why a recovery stopped. Exported through [ExplainMachinery] so a blind spot
// stops being invisible: the reason codes are part of the contract.
const (
	recoverOK           = "ok"
	recoverNoText       = "no_text"
	recoverNoTags       = "no_tags"
	recoverFenced       = "fenced"
	recoverUnadvertised = "unadvertised"
	recoverBadArguments = "bad_arguments"
	recoverShape        = "shape"
	// recoverProse: the markup was well-formed but the reply around it was
	// mostly WORDS — a model explaining a call rather than making one. Its own
	// code because it is the refusal a reader has to understand, and the one
	// that says the two shapes are closer than they look.
	recoverProse = "prose"
)

// RecoveredCall is one call read back out of a reply's text.
type RecoveredCall struct {
	Name      string
	Arguments string // compact JSON, the shape a ToolCall carries
}

// RecoverMachinery puts the tool calls hiding as text in a reply that
// MachineryLeak would have cut onto response as real calls, returning
// recoverOK. Any other reason leaves response untouched, and the caller
// cuts exactly as it did before this existed.
//
// Mutates response: a recovered reply reads finish_reason tool_calls with
// the markup dropped from content, so the turn dispatches the call
// instead of showing the grammar it was written in.
func RecoverMachinery(request *ai.Request, response *ai.Response) ([]RecoveredCall, string) {
	if request == nil || response == nil || len(request.Tools) == 0 {
		return nil, recoverNoText
	}
	permitted := make(map[string]bool, len(request.Tools))
	for _, tool := range request.Tools {
		if name := strings.TrimSpace(tool.Function.Name); name != "" {
			permitted[name] = true
		}
	}
	calls, reason := ExplainMachinery(responseText(response), permitted)
	if reason != recoverOK {
		return nil, reason
	}
	out := make([]ai.ToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, ai.ToolCall{
			ID:       recoveredCallID(call.Name, call.Arguments),
			Type:     "function",
			Function: ai.ToolCallFunction{Name: call.Name, Arguments: call.Arguments},
		})
	}
	if len(out) == 0 {
		return nil, recoverNoTags
	}
	if len(response.Choices) > 0 {
		// Only choice zero: it is the choice every guard in this file reads, and a
		// second choice carrying its own content is not the reply that leaked.
		response.Choices[0].Message.ToolCalls = out
		response.Choices[0].Message.Content = nil
		if finish := strings.TrimSpace(response.Choices[0].FinishReason); finish == "" || finish == "stop" {
			response.Choices[0].FinishReason = "tool_calls"
		}
	}
	return calls, recoverOK
}

// ExplainMachinery is RecoverMachinery without the mutation: the calls
// found, and the reason. permitted is the request's advertised names; an
// empty set skips the name check.
func ExplainMachinery(text string, permitted map[string]bool) ([]RecoveredCall, string) {
	if strings.TrimSpace(text) == "" {
		return nil, recoverNoText
	}
	hay := recoverDSMLToken.ReplaceAllString(recoverFold.Replace(text), "")
	for _, envelope := range recoverEnvelopes {
		hay = strings.ReplaceAll(hay, envelope, "")
	}

	starts := recoverInvokeTag.FindAllStringSubmatchIndex(hay, -1)
	if len(starts) == 0 {
		return nil, recoverNoTags
	}
	// EVERY invocation inside a fence is showing code on purpose.
	if fenced := recoverFence.FindStringIndex(hay); fenced != nil {
		allFenced := true
		for _, m := range starts {
			if m[0] < fenced[0] || m[1] > fenced[1] {
				allFenced = false
				break
			}
		}
		if allFenced {
			return nil, recoverFenced
		}
	}

	invocations := recoverInvocations{starts: starts, hay: hay}
	var (
		calls []RecoveredCall
		spans [][2]int // rune offsets, for the shape test
	)
	for i := range starts {
		name := hay[starts[i][2]:starts[i][3]]
		if len(permitted) > 0 && !permitted[name] {
			return nil, recoverUnadvertised
		}
		limit := len(hay)
		if i+1 < len(starts) {
			limit = starts[i+1][0]
		}
		call, end, ok := invocations.read(i, name, limit)
		if !ok {
			return nil, recoverBadArguments
		}
		calls = append(calls, call)
		spans = append(spans, [2]int{runeOffset(hay, starts[i][0]), runeOffset(hay, end)})
	}
	switch recoveredShapeReason(hay, spans) {
	case recoverOK:
		// The markup carries the reply, which is what recovery needs.
	case recoverProse:
		return nil, recoverProse
	default:
		return nil, recoverShape
	}
	return calls, recoverOK
}

// recoverInvocations holds the shared state of a pass over the invocations so
// the loop above reads as the policy it is, rather than as index arithmetic.
type recoverInvocations struct {
	starts [][]int
	hay    string
}

// read returns the call for invocation i (whose tag is starts[i][1] past the
// tag) and the byte offset it ends at.
func (r recoverInvocations) read(i int, name string, limit int) (RecoveredCall, int, bool) {
	tagEnd := r.starts[i][1]
	segment := r.hay[tagEnd:limit]

	closeAt := strings.Index(segment, "</invoke>")
	closed := closeAt != -1
	body := segment
	if closed {
		body = segment[:closeAt]
	}

	args, consumed, ok := recoveredArguments(body, closed)
	if !ok {
		return RecoveredCall{}, 0, false
	}
	end := tagEnd + len(body)
	if closed {
		end = tagEnd + closeAt + len("</invoke>")
	} else if consumed > 0 {
		end = tagEnd + consumed
	}
	return RecoveredCall{Name: name, Arguments: args}, end, true
}

// recoveredArguments is the arguments for one invocation, as compact JSON, and
// how much of body they consume. ok=false means "do not guess", and the caller
// refuses the whole repair.
func recoveredArguments(body string, closed bool) (string, int, bool) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		// A call with no arguments is a real call, not a parse failure — but only
		// when the invocation actually closed. An unterminated one is a truncated
		// reply and guessing {} for it would dispatch a command nobody wrote.
		if closed {
			return "{}", 0, true
		}
		return "", 0, false
	}

	if m := recoverParamArguments.FindStringSubmatchIndex(body); m != nil {
		raw := body[m[2]:m[3]]
		obj, _, ok := recoveredJSONObject(raw)
		if !ok {
			return "", 0, false
		}
		if at := strings.Index(body, "</parameter>"); at >= 0 {
			return obj, at + len("</parameter>"), true
		}
		return obj, len(body), true
	}

	if pairs := recoverParamAny.FindAllStringSubmatchIndex(body, -1); len(pairs) > 0 {
		// A named-parameter form. Values are read as JSON when they are JSON and
		// kept as text when they are not, because "false" and 17 arrive both ways
		// and a tool that wanted the string is owed the string.
		fields := make([]string, 0, len(pairs))
		for _, m := range pairs {
			key := body[m[2]:m[3]]
			raw := strings.TrimSpace(body[m[4]:m[5]])
			var value any
			if json.Unmarshal([]byte(raw), &value) != nil {
				value = raw
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return "", 0, false
			}
			fields = append(fields, `"`+key+`":`+string(encoded))
		}
		out := "{" + strings.Join(fields, ",") + "}"
		last := strings.LastIndex(body, "</parameter>")
		if last < 0 {
			return out, len(body), true
		}
		return out, last + len("</parameter>"), true
	}

	if strings.HasPrefix(trimmed, "{") {
		obj, span, ok := recoveredJSONObject(trimmed)
		if !ok {
			return "", 0, false
		}
		return obj, len(body) - len(trimmed) + span, true
	}

	return "", 0, false
}

// recoveredJSONObject is the leading {...} object and how far it reaches in
// runes. It scans for the matching brace rather than matching a pattern so a
// nested object inside a value is not mistaken for the end.
func recoveredJSONObject(text string) (string, int, bool) {
	runes := []rune(text)
	start := -1
	for i, r := range runes {
		if r == '{' {
			start = i
			break
		}
	}
	if start < 0 {
		return "", 0, false
	}
	depth := 0
	inString, escaped := false, false
	for i := start; i < len(runes); i++ {
		r := runes[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case r == '\\':
				escaped = true
			case r == '"':
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				span := string(runes[start : i+1])
				var probe map[string]any
				if json.Unmarshal([]byte(span), &probe) != nil || probe == nil {
					return "", 0, false
				}
				return span, i + 1, true
			}
		}
	}
	// Unterminated: a reply cut mid-call is not a call.
	return "", 0, false
}

// Whether the markup is the reply's substance or a quotation inside it:
// recoverOK, recoverProse when the reply is mostly words, recoverShape
// otherwise. This is the distinction structural matching cannot settle.
func recoveredShapeReason(hay string, spans [][2]int) string {
	if len(spans) == 0 {
		return recoverShape
	}
	covered, lead, last := 0, spans[0][0], 0
	for _, span := range spans {
		covered += span[1] - span[0]
		if span[0] < lead {
			lead = span[0]
		}
		if span[1] > last {
			last = span[1]
		}
	}
	size := utf8.RuneCountInString(hay)
	if size < 1 {
		size = 1
	}
	tail := size - last
	if covered < recoverShapeFloor {
		return recoverShape
	}
	// THE DISCRIMINATOR, checked before the allowances below because it is the
	// one that survives them: a reply quoting an invocation satisfies both.
	if float64(lead+tail)/float64(size) > recoverProseCeiling {
		return recoverProse
	}
	leadOK := lead <= maxInt(recoverLeadAllowance, int(recoverLeadRatio*float64(size)))
	tailOK := tail <= maxInt(recoverTailAllowance, int(recoverTailRatio*float64(size)))
	if leadOK && tailOK {
		return recoverOK
	}
	return recoverShape
}

// recoveredCallID is a stable id, so a repair repeated over an unchanged reply
// leaves the prompt prefix byte-identical and the cache still hits. A random id
// would change the prefix on every turn for the same work.
func recoveredCallID(name, arguments string) string {
	digest := sha256.Sum256([]byte(name + "\x00" + arguments))
	return "call_" + hex.EncodeToString(digest[:])[:24]
}

// runeOffset converts a byte offset into the rune offset the shape test compares
// in, so a reply with non-ASCII prose in its lead-in is not misjudged.
func runeOffset(s string, byteOffset int) int {
	if byteOffset <= 0 {
		return 0
	}
	if byteOffset > len(s) {
		return utf8.RuneCountInString(s)
	}
	return utf8.RuneCountInString(s[:byteOffset])
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}