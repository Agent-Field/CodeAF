package bare

import (
	"context"
	"sync/atomic"
)

// ArgumentRejection is a call-local fact recorded by a trusted parser or
// validator before the requested operation starts. Tool output is never read
// back to classify it. It owns no history, goroutine or process-wide map.
type ArgumentRejection struct{ rejected atomic.Bool }
type argumentRejectionKey struct{}

// WithArgumentRejection gives one dispatch its own result-fact owner. Contexts
// passed to nested operations share it; another tool dispatch gets a fresh one.
func WithArgumentRejection(ctx context.Context) (context.Context, *ArgumentRejection) {
	mark := &ArgumentRejection{}
	return context.WithValue(ctx, argumentRejectionKey{}, mark), mark
}

// MarkArgumentRejection is called only at a proven pre-operation validation
// refusal. A direct standalone Execute without an observing caller is safe.
func MarkArgumentRejection(ctx context.Context) {
	if ctx != nil {
		if mark, _ := ctx.Value(argumentRejectionKey{}).(*ArgumentRejection); mark != nil {
			mark.rejected.Store(true)
		}
	}
}

// RejectArguments preserves the handler's exact existing result text.
func RejectArguments(ctx context.Context, text string) string {
	MarkArgumentRejection(ctx)
	return text
}

// RejectedBeforeRun reads the fact after that call's handler has returned.
func (m *ArgumentRejection) RejectedBeforeRun() bool { return m != nil && m.rejected.Load() }
