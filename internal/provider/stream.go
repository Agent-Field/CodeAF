package provider

import "context"

// StreamEventKind names one boundary in an observed provider stream. The
// observer is opt-in through context, so every existing completion remains
// byte-for-byte non-streaming unless its caller is an interactive surface.
type StreamEventKind int

const (
	StreamStarted StreamEventKind = iota
	StreamDelta
	// StreamThinking says the model is producing reasoning rather than answer.
	// It carries no text: reasoning tokens are the model's own working and are
	// never shown, so the only thing that leaves the provider is that the wait
	// has a reason. It is raised once per run of reasoning, not per token.
	StreamThinking
	StreamFinished
	StreamFailed
	// The TOOL ACTIVITY boundaries. They are not produced by the adapter — no
	// endpoint reports them — but by whoever is RUNNING the tool loop above it,
	// through [Emit]. They ride this vocabulary rather than a second channel
	// because the person is watching one turn: a read the head performed and a
	// word it streamed are the same turn happening, and two feeds would have to
	// be re-interleaved by every surface that drew them.
	//
	// StreamToolBegin carries a person-readable gloss of the call in Delta —
	// "searching for «navctx»", never the raw arguments. The two ends carry a
	// short result hint, which is very often empty.
	//
	// THE OUTCOME IS A KIND AND NOT A FIELD, so the event struct stays three
	// strings wide and every bridge between vocabularies stays a copy.
	StreamToolBegin
	StreamToolEnd
	StreamToolFailed
	// StreamReasoning carries one chunk of the model's reasoning TEXT in Delta.
	//
	// It is the companion of StreamThinking and not its replacement: thinking
	// says a run of reasoning has begun and is raised once, this is raised per
	// delta and carries the words. Both are emitted, in that order, because a
	// surface that only draws "thinking…" must keep working unchanged and a
	// surface that wants the text must not have to infer where the run started.
	//
	// The wire spells it two ways — OpenRouter's "reasoning" and the
	// "reasoning_content" the DeepSeek-family endpoints send — and sse.go reads
	// both into one field, so what leaves here is one vocabulary regardless.
	StreamReasoning
	// StreamToolCallReady says ONE tool call has finished streaming, before the
	// response it belongs to has. Delta is that call JSON-marshaled — an
	// ai.ToolCall object, id and function and all, not a gloss — because the
	// consumer is code deciding whether to start work, not a person reading a
	// line.
	//
	// It is raised when the stream opens a LATER call (the one before it can
	// receive no more fragments) and, for whatever is still open, once the
	// stream ends cleanly. A call is only announced when its name is known and
	// its arguments parse as JSON: a fragment boundary misread would otherwise
	// hand a consumer a truncated instruction, and "not yet" is always a safe
	// answer here — the response's own ToolCalls() remains the authority.
	//
	// NOTHING IS PROMISED ABOUT WHO RUNS IT. This is an early sighting, not a
	// dispatch: see the safety law in session/loop.go for which calls may act on
	// one and which must wait for the response.
	StreamToolCallReady
	// StreamToolCallForming says ONE tool call is still ARRIVING: the model has
	// begun spelling it out and has not finished. Index, ID and Tool name the
	// call as far as the wire has said them — the index is always known, the id
	// and the name arrive on the first fragment or shortly after — and Delta
	// carries the call's ACCUMULATED ARGUMENTS TEXT so far, raw and partial.
	//
	// IT IS RAW ON PURPOSE. The arguments of a half-sent call are not JSON yet,
	// so nothing here may be unmarshaled and nothing downstream may treat this as
	// an instruction. It is the answer to "what is it doing right now" for the
	// seconds between the model starting a long write and the call being whole —
	// seconds a surface with only StreamToolCallReady has to draw as silence.
	//
	// It is raised PER FRAGMENT, which is per token for the endpoints that stream
	// arguments a token at a time. That rate is deliberate and the consumer's
	// problem: this vocabulary reports the wire, and whoever is drawing decides
	// how often a person needs to see it (session/toolhint.go throttles it).
	//
	// EVERY CALL THAT FORMS IS LATER READY, in that order, unless the stream dies
	// mid-call — the same condition under which StreamToolCallReady says nothing
	// either. A non-streaming endpoint raises none of these at all.
	StreamToolCallForming
	// StreamNotice carries one line ABOUT the call rather than from it, in Delta.
	//
	// It is the only kind the adapter itself raises that is not the model
	// speaking, and it exists for exactly one thing today: the endpoint-refusal
	// chain saying which attempt it is on and what it just took off the request
	// (endpoints.go). A retry that reshapes a person's request has to be visible
	// or it is an adapter answering a different question from the one it was
	// asked — and the only channel that reaches the room in order is this one.
	//
	// It is raised BEFORE the stream opens, so a surface may receive notices on a
	// turn that goes on to produce no StreamStarted at all.
	StreamNotice
)

// StreamEvent carries provider text as it arrives. Delta is populated only
// for StreamDelta; the terminal events deliberately carry no provider error
// text because the normal completion return remains the error authority.
// Session names the room this call's turn belongs to (empty in the one
// caller — the belt/router tests — that streams without ever setting one).
// Today there is exactly one room, so every event's Session is the same
// value; keyed events are the prerequisite, not a multi-room consumer.
type StreamEvent struct {
	Kind    StreamEventKind
	Delta   string
	Session string

	// Index, ID and Tool name the tool call a StreamToolCallForming event is
	// about, and are zero on every other kind. They are fields rather than a
	// JSON payload in Delta — the shape StreamToolCallReady uses — because a
	// forming event is raised per fragment and a marshal per token is work the
	// read loop does not have the budget for.
	//
	// ID and Tool are empty until the wire has said them: an endpoint sends the
	// id and the name on the first fragment of a call, but "sends them first" is
	// a convention rather than a guarantee, and a consumer that assumed it would
	// key its rows on "".
	Index int
	ID    string
	Tool  string
}

// StreamObserver receives provider deltas synchronously and in order.
type StreamObserver func(StreamEvent)

type streamObserverContextKey struct{}
type streamSessionContextKey struct{}

// WithStreamObserver asks the adapter to stream this completion while still
// returning the ordinary accumulated response to its existing caller.
func WithStreamObserver(ctx context.Context, observer StreamObserver) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, streamObserverContextKey{}, observer)
}

// WithoutStream takes the observer back off a context, for a call made inside a
// surface's own context that is not the surface's conversation.
//
// The observer is installed once, on the process's serving context, so anything
// that borrows that context to ask a model something inherits a live typewriter
// pointed at the transcript — and a call whose answer is a LABEL rather than a
// reply would type its label into the room as if somebody were saying it. The
// head's room-naming clerk is the first such caller (head/scribe.go); a
// background summarizer would be the second.
//
// A typed nil is stored rather than the key being removed, because a context
// value cannot be unset — and the reader below already treats a nil observer as
// "do not stream", which is exactly what this means.
func WithoutStream(ctx context.Context) context.Context {
	if streamObserverFrom(ctx) == nil {
		return ctx
	}
	return context.WithValue(ctx, streamObserverContextKey{}, StreamObserver(nil))
}

func streamObserverFrom(ctx context.Context) StreamObserver {
	observer, _ := ctx.Value(streamObserverContextKey{}).(StreamObserver)
	return observer
}

// WithStreamSession stamps the room a turn is answering for. The caller that
// owns the turn (the head, one per turn) sets this on the turn's context
// before making the provider call; every StreamEvent that call emits carries
// it, so a consumer fed by more than one room can tell them apart.
func WithStreamSession(ctx context.Context, session string) context.Context {
	return context.WithValue(ctx, streamSessionContextKey{}, session)
}

func streamSessionFrom(ctx context.Context) string {
	session, _ := ctx.Value(streamSessionContextKey{}).(string)
	return session
}

// Emit hands one event to whatever observer is listening on this context,
// stamped with the room the turn is answering for.
//
// It is the door for the boundaries no adapter can report: a TOOL CALL is
// something the caller above the client does, and until this existed the only
// way to tell a surface about one was to invent a second channel beside the
// token feed. Everything the surface needs to interleave the two — order, and
// the room key — is already the property of this one.
//
// Nothing listening is the ordinary case (every headless run), and it costs one
// context lookup. The observer contract is unchanged and still synchronous: a
// caller emitting from inside a read loop is paying for it in that loop.
func Emit(ctx context.Context, kind StreamEventKind, delta string) {
	EmitEvent(ctx, StreamEvent{Kind: kind, Delta: delta})
}

// EmitEvent is [Emit] for a kind that carries more than a string — today only
// StreamToolCallForming, whose call index, id and name are fields. The Session
// is stamped here from the context, so a caller never sets it and cannot set it
// to the wrong room.
func EmitEvent(ctx context.Context, event StreamEvent) {
	if ctx == nil {
		return
	}
	observer := streamObserverFrom(ctx)
	if observer == nil {
		return
	}
	event.Session = streamSessionFrom(ctx)
	observer(event)
}
