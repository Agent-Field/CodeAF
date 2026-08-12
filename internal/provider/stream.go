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
