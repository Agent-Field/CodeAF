package provider

import "context"

// StreamEventKind names one boundary in an observed provider stream. The
// observer is opt-in through context, so every existing completion remains
// byte-for-byte non-streaming unless its caller is an interactive surface.
type StreamEventKind int

const (
	StreamStarted StreamEventKind = iota
	StreamDelta
	StreamFinished
	StreamFailed
)

// StreamEvent carries provider text as it arrives. Delta is populated only
// for StreamDelta; the terminal events deliberately carry no provider error
// text because the normal completion return remains the error authority.
type StreamEvent struct {
	Kind  StreamEventKind
	Delta string
}

// StreamObserver receives provider deltas synchronously and in order.
type StreamObserver func(StreamEvent)

type streamObserverContextKey struct{}

// WithStreamObserver asks the adapter to stream this completion while still
// returning the ordinary accumulated response to its existing caller.
func WithStreamObserver(ctx context.Context, observer StreamObserver) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, streamObserverContextKey{}, observer)
}

func streamObserverFrom(ctx context.Context) StreamObserver {
	observer, _ := ctx.Value(streamObserverContextKey{}).(StreamObserver)
	return observer
}
