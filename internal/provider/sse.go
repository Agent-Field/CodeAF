package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// sseDecoder reads the provider's event stream one message at a time.
//
// The SDK ships a decoder and this one exists because that one is quadratic in
// the length of a single message: it keeps an undelivered []byte, and every
// call converts the whole of it to a string to look for the blank line and
// converts the remainder back. On ordinary token deltas nobody could measure
// it. On one multi-megabyte message — a reasoning block delivered whole — the
// buffer is re-copied twice per 8 KB read, which is about a gigabyte of memcpy
// to deliver four megabytes. This reads through a bufio.Reader instead, so the
// bytes of a message are copied exactly once, and the SDK is left alone.
//
// It is a *replacement*, not an improvement: the framing below is the SDK's,
// quirks included, because a stream that decoded differently here would change
// answers. Messages are separated by "\n\n" and by nothing else — a CRLF
// stream has no separator at all as far as this is concerned, exactly as
// before — only a message beginning "data: " is looked at, "[DONE]" ends the
// stream with io.EOF, and anything that will not parse as JSON is skipped.
type sseDecoder struct {
	reader  *bufio.Reader
	message []byte
	// done records the protocol's explicit [DONE] marker, which shares the
	// decoder's io.EOF return with an underlying connection close.
	done bool
	// alive, when set, is called once per SSE comment line — the ": OPENROUTER
	// PROCESSING" keepalives a router sends while an upstream assembles its
	// answer. Comments never become chunks (parseSSEMessage drops them), so
	// without this seam the one reader who cares that the endpoint is still
	// speaking — the stall watch — would never hear it (streamguard.go says
	// what it buys). It is called from the read loop, so it must be cheap.
	alive func()
	// chunk and wire are the decode targets, held here rather than declared per
	// event: a streamed answer is thousands of events, and the address of a
	// local one escapes into json.Unmarshal every single time.
	//
	// BOTH ARE ZEROED BEFORE EVERY DECODE, and that is not tidiness. Unmarshal
	// leaves a field the event did not mention exactly as it found it, so a
	// target carried forward unzeroed would give one event the previous one's
	// role, id or finish reason. Zeroing also nils every slice in them before
	// the decode, which is what keeps the chunk handed back from aliasing the
	// next one's.
	chunk ai.StreamChunk
	wire  streamChunk
}

// sseReadBuffer is the read size. Larger than the SDK's 8 KB because the read
// is now the only copy: a bigger buffer is fewer syscalls and no more memory
// traffic.
const sseReadBuffer = 64 << 10

func newSSEDecoder(reader io.Reader) *sseDecoder {
	return &sseDecoder{reader: bufio.NewReaderSize(reader, sseReadBuffer)}
}

// Decode returns the next chunk, io.EOF at the end of the stream, and whatever
// the underlying reader failed with otherwise.
func (d *sseDecoder) Decode() (ai.StreamChunk, error) {
	for {
		payload, err := d.next()
		if err != nil {
			return ai.StreamChunk{}, err
		}
		d.chunk = ai.StreamChunk{}
		if json.Unmarshal(payload, &d.chunk) != nil {
			continue
		}
		return d.chunk, nil
	}
}

// DecodeChunk is the same stream read into the wire's own shape rather than the
// SDK's. Everything the SDK's type has no field for — a tool call being spelled
// out fragment by fragment, a reasoning token — is dropped by Decode and kept
// here, which is the whole difference between the two.
func (d *sseDecoder) DecodeChunk() (streamChunk, error) {
	for {
		payload, err := d.next()
		if err != nil {
			return streamChunk{}, err
		}
		d.wire = streamChunk{}
		if json.Unmarshal(payload, &d.wire) != nil {
			continue
		}
		return d.wire, nil
	}
}

// next returns the JSON payload of the next data: message. The bytes belong to
// the decoder and are valid until the call after this one, which is why both
// decoders above unmarshal before asking for another.
func (d *sseDecoder) next() ([]byte, error) {
	for {
		line, err := d.reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// One SSE line may be the whole message and the whole message may
			// be megabytes. A piece that does not fit the read buffer carries
			// no '\n' at all, so it is stitched onto the message and cannot be
			// mistaken for the blank line below.
			d.message = append(d.message, line...)
			continue
		}
		if len(line) > 0 {
			// A line opening with a colon is an SSE comment — the keepalive
			// vocabulary, and the only non-answer this layer reports. It is
			// noticed HERE, per line rather than per message, because a router
			// under a slow upstream sends comments without terminating a
			// message for minutes at a time, and a hook on message completion
			// would stay silent exactly when it matters.
			if line[0] == ':' && d.alive != nil {
				d.alive()
			}
			// The separator is "\n\n": this line opens with a newline and the
			// message so far ended with one. Everything before that first
			// newline is the message; the rest of the buffer is the next one.
			if line[0] == '\n' && len(d.message) > 0 && d.message[len(d.message)-1] == '\n' {
				payload, delivered, done := parseSSEMessage(d.message[:len(d.message)-1])
				d.message = d.message[:0]
				switch {
				case done:
					d.done = true
					return nil, io.EOF
				case delivered:
					return payload, nil
				}
				continue
			}
			d.message = append(d.message, line...)
		}
		if err != nil {
			// Per the io.Reader contract a read may return bytes alongside its
			// error, and the bytes are consumed above before the error is
			// surfaced here. A partial trailing message is dropped, which is
			// what a message with no separator has always been.
			return nil, err
		}
	}
}

// parseSSEMessage is the SDK's message handling, byte for byte: delivered says
// the payload is one to hand back, done says the stream said [DONE]. A payload
// that will not parse as JSON is still delivered here and skipped by the caller,
// which is where the SDK skips it too.
func parseSSEMessage(message []byte) (payload []byte, delivered, done bool) {
	const prefix = "data: "
	if !bytes.HasPrefix(message, []byte(prefix)) {
		// A comment, a keepalive, an event: line, a multi-line message — none
		// of them are chunks and none of them stop the stream.
		return nil, false, false
	}
	payload = bytes.TrimSpace(message[len(prefix):])
	if bytes.Equal(payload, []byte("[DONE]")) {
		return nil, false, true
	}
	return payload, true, false
}

// streamChunk is the streamed response as the wire actually spells it. The
// SDK's StreamChunk carries a delta of role and content and nothing else, so a
// tool call arriving in fragments and a reasoning token are invisible to
// anything decoding through it: every streamed completion returned zero tool
// calls, which made tool-calling dead on the streamed path and turned the head's
// control belt into a round trip that could not succeed.
//
// The fields below are the OpenAI streaming shape plus the names reasoning
// travels under. They leave as StreamReasoning events with their wire spelling
// intact because a later tool step must replay model working as continuation
// metadata, never as answer content.
type streamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	// Provider is who actually served this stream, which a router says and an
	// endpoint does not. It is read once per stream into the velocity ledger
	// (velocity.go): a model fanned over many endpoints is answered at very
	// different speeds, and the name is the only thing that makes a measurement
	// attributable to anything.
	Provider string         `json:"provider,omitempty"`
	Choices  []streamChoice `json:"choices"`
	// Usage is the SDK's own block plus the one figure its type has no field
	// for; see [usageWire]. The stream's usage arrives in a terminal frame, so
	// this is decoded once per stream rather than once per delta.
	Usage *usageWire `json:"usage,omitempty"`
	// Error is a REFUSAL DELIVERED INSIDE A 200, which is how a router reports
	// an upstream that broke after the headers were already sent. It is raw
	// because it is the same object an error response carries and it is decoded
	// by the same function ([streamRefusal] → [apiError]), so a mid-stream
	// refusal and an HTTP one become the same value.
	//
	// ── THE MEASURED FAILURE ────────────────────────────────────────────────
	//
	// SWE-Marathon run s2, 22:45 UTC: three streams in fifteen seconds decoded
	// with no field here at all, so each one ended with no content, no tool
	// call, no usage and no error — and the turn loop wrote an EMPTY ASSISTANT
	// MESSAGE for each, which the remains-reader then read as a turn that had
	// stopped short and re-opened, three times, at mark-reader prices. A FAILED
	// CALL MUST NEVER LOOK LIKE AN EMPTY ANSWER.
	Error json.RawMessage `json:"error,omitempty"`
}

// streamRefusal turns an in-band error object into the same [APIError] an HTTP
// refusal produces, or nil when the field carried nothing to report.
//
// The status is the router's own `code` when it sent one that is an HTTP status,
// and 502 otherwise: a stream that broke after its headers landed is an upstream
// failing mid-answer, which is what a bad gateway means, and inventing a 200
// here would make the refusal look like a success to every classifier above.
func streamRefusal(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var decoded struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	if strings.TrimSpace(decoded.Message) == "" && decoded.Code == 0 {
		return nil
	}
	status := decoded.Code
	if status < 400 || status > 599 {
		status = http.StatusBadGateway
	}
	// Re-wrapped rather than re-decoded field by field, so the metadata this
	// object carries — `provider_name`, `raw` — reaches [apiError] by the one
	// path that knows how to read it.
	return apiError(status, append(append([]byte(`{"error":`), raw...), '}'))
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type streamDelta struct {
	Role             string          `json:"role,omitempty"`
	Content          string          `json:"content,omitempty"`
	ToolCalls        []toolCallDelta `json:"tool_calls,omitempty"`
	Reasoning        string          `json:"reasoning,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ReasoningText    string          `json:"reasoning_text,omitempty"`
	ReasoningDetails json.RawMessage `json:"reasoning_details,omitempty"`
}

// thinking reports that this delta carried thought rather than answer.
func (d streamDelta) thinking() bool {
	return d.Reasoning != "" || d.ReasoningContent != "" || d.ReasoningText != "" || len(d.ReasoningDetails) > 0
}

// callText is the tool-call arguments this delta carried, every fragment's in
// order. It is the part of a call being assembled that is the model's work —
// the id and the name are labels on it — and it is ANSWER, not thought: see the
// read loop's reading of it in client.go.
func (d streamDelta) callText() string {
	if len(d.ToolCalls) == 1 {
		return d.ToolCalls[0].Function.Arguments
	}
	var text strings.Builder
	for _, fragment := range d.ToolCalls {
		text.WriteString(fragment.Function.Arguments)
	}
	return text.String()
}

// written is how many bytes of the model's writing this delta carried: answer,
// thought and call arguments alike. It is what the stream wall counts
// ([stallWatch.progress]).
func (d streamDelta) written() int {
	bytes := len(d.Content) + len(d.Reasoning) + len(d.ReasoningContent) + len(d.ReasoningText)
	for _, fragment := range d.ToolCalls {
		bytes += len(fragment.Function.Arguments)
	}
	return bytes
}

// reasoningEvents keeps the field signature attached to each piece. Providers
// use one spelling consistently; retaining all three here also makes an odd
// mixed stream lossless instead of silently choosing one.
func (d streamDelta) reasoningEvents() ([3]StreamEvent, int) {
	var events [3]StreamEvent
	count := 0
	for _, item := range []struct{ field, text string }{
		{"reasoning", d.Reasoning},
		{"reasoning_content", d.ReasoningContent},
		{"reasoning_text", d.ReasoningText},
	} {
		if item.text != "" {
			events[count] = StreamEvent{Kind: StreamReasoning, Delta: item.text, ReasoningField: item.field}
			count++
		}
	}
	if len(d.ReasoningDetails) > 0 {
		if count == 0 {
			count = 1
			events[0] = StreamEvent{Kind: StreamReasoning}
		}
		events[count-1].ReasoningDetails = append(json.RawMessage(nil), d.ReasoningDetails...)
	}
	return events, count
}

// toolCallDelta is one fragment of one tool call. Index is a pointer because
// its absence and its zero are different claims: an endpoint that omits it is
// spelling out one call at a time, and reading that as "call 0" would fuse a
// second call onto the first.
type toolCallDelta struct {
	Index    *int              `json:"index"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function toolFunctionDelta `json:"function"`
}

type toolFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// toolCallAccumulator reassembles the fragments into whole calls. Providers
// differ in what they repeat — some resend the id and the name on every
// fragment, some send them once — so the rule is the same for every field: the
// first non-empty value wins, and arguments always append.
//
// It also reports CALL BOUNDARIES as they pass, which is the whole basis of the
// early sighting: see [toolCallAccumulator.complete].
type toolCallAccumulator struct {
	order []int
	calls map[int]*ai.ToolCall
	args  map[int]*strings.Builder
	// reported is the set of indexes already handed back as complete, so a call
	// is announced exactly once no matter how the boundaries fall.
	reported map[int]bool
	// open is the index the last fragment landed on. It is what
	// [toolCallAccumulator.current] reads: the fragment loop needs to say WHICH
	// call just grew, and the index add resolved is not a thing the caller can
	// work out for itself.
	open     int
	openSeen bool
}

// formingCall is the state of one call while it is still arriving: what the wire
// has said of its identity, and its arguments text so far — raw and, until the
// call is whole, not JSON.
type formingCall struct {
	Index int
	ID    string
	Name  string
	Args  string
}

// current reports the call the most recent fragment grew, or false if there is
// nothing to say about it.
//
// A call ALREADY ANNOUNCED is nothing to say about: an endpoint that sent a
// trailing fragment for a call this decoder had closed would otherwise put a
// "still arriving" after that call's StreamToolCallReady, and the one promise
// this vocabulary makes about order is that it never does that.
func (a *toolCallAccumulator) current() (formingCall, bool) {
	if !a.openSeen || a.reported[a.open] {
		return formingCall{}, false
	}
	call, known := a.calls[a.open]
	if !known {
		return formingCall{}, false
	}
	forming := formingCall{Index: a.open, ID: call.ID, Name: call.Function.Name}
	if builder, ok := a.args[a.open]; ok {
		forming.Args = builder.String()
	}
	return forming, true
}

// add folds one fragment in and reports the call the fragment ENDED, if any: a
// fragment that opens an index never seen before means the call that was open
// until now can receive nothing more.
//
// The last call of a message has no such successor and is reported by
// [toolCallAccumulator.flush] instead, at the clean end of the stream.
func (a *toolCallAccumulator) add(fragment toolCallDelta) (ai.ToolCall, bool) {
	index := 0
	switch {
	case fragment.Index != nil:
		index = *fragment.Index
	case len(a.order) > 0 && fragment.ID == "":
		// A continuation of the call already open.
		index = a.order[len(a.order)-1]
	default:
		index = len(a.order)
	}
	if a.calls == nil {
		a.calls, a.args = make(map[int]*ai.ToolCall, 2), make(map[int]*strings.Builder, 2)
		a.reported = make(map[int]bool, 2)
	}
	call, known := a.calls[index]
	var completed ai.ToolCall
	ready := false
	if !known && len(a.order) > 0 {
		// A NEW index opens here, so the call that was open is whole. Only the
		// open one is closed — an endpoint that jumped backwards would leave the
		// others to flush, which is later but never wrong.
		completed, ready = a.complete(a.order[len(a.order)-1])
	}
	if !known {
		call = &ai.ToolCall{Type: "function"}
		a.calls[index], a.args[index] = call, &strings.Builder{}
		a.order = append(a.order, index)
	}
	if fragment.ID != "" {
		call.ID = fragment.ID
	}
	if fragment.Type != "" {
		call.Type = fragment.Type
	}
	if fragment.Function.Name != "" {
		call.Function.Name = fragment.Function.Name
	}
	a.args[index].WriteString(fragment.Function.Arguments)
	a.open, a.openSeen = index, true
	return completed, ready
}

// complete returns one index's call as a whole instruction, and says whether it
// is one worth announcing.
//
// IT IS DELIBERATELY CONSERVATIVE. A call with no name is not an instruction —
// assembled() drops it for the same reason — and arguments that do not parse as
// JSON are a boundary this decoder read wrongly, not something to act on. Both
// answer "no", and the response's own assembly, which happens after the last
// byte, remains the authority for what actually ran.
func (a *toolCallAccumulator) complete(index int) (ai.ToolCall, bool) {
	if a.reported[index] {
		return ai.ToolCall{}, false
	}
	call, known := a.calls[index]
	if !known || call.Function.Name == "" {
		return ai.ToolCall{}, false
	}
	arguments := a.args[index].String()
	if !wholeArguments(arguments) {
		return ai.ToolCall{}, false
	}
	a.reported[index] = true
	whole := *call
	whole.Function.Arguments = arguments
	return whole, true
}

// flush reports every call not yet announced, in the order the stream opened
// them. It runs once, at the clean end of a message: ordinarily it returns the
// single call that was still open, and after an out-of-order endpoint it returns
// whatever add left behind.
func (a *toolCallAccumulator) flush() []ai.ToolCall {
	var ready []ai.ToolCall
	for _, index := range a.order {
		if call, ok := a.complete(index); ok {
			ready = append(ready, call)
		}
	}
	return ready
}

// wholeArguments reports whether an arguments string is a complete JSON value.
// Empty counts: a no-argument tool is spelled that way by some endpoints, and
// the belt reads it the same as "{}".
func wholeArguments(arguments string) bool {
	trimmed := strings.TrimSpace(arguments)
	return trimmed == "" || json.Valid([]byte(trimmed))
}

// assembled returns the whole calls, in the order the stream opened them. A call
// with no name is dropped: the harness would only fail it, and a half-delivered
// fragment is not an instruction.
func (a *toolCallAccumulator) assembled() []ai.ToolCall {
	if len(a.order) == 0 {
		return nil
	}
	assembled := make([]ai.ToolCall, 0, len(a.order))
	for _, index := range a.order {
		call := a.calls[index]
		if call.Function.Name == "" {
			continue
		}
		call.Function.Arguments = a.args[index].String()
		assembled = append(assembled, *call)
	}
	if len(assembled) == 0 {
		return nil
	}
	return assembled
}
