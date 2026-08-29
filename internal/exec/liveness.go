package exec

import "context"

// A worker is alive because of what it is DOING, and the journal is the wrong
// place to ask.
//
// THE DEFECT THIS ANSWERS. The claim reaper decides a node is held by nobody
// from the marks a worker leaves in the store: a billed model call, a flushed
// batch of recorded turns. Both are written when something FINISHES, and the
// transcript's flush is batched at sixty-four entries on purpose — one write per
// tool result would put several hundred rows under a node and turn the database
// into a log file (see store/transcript.go). So a leaf that spends twenty
// minutes inside three long shell commands writes nothing at all in that time,
// and a reaper reading only the journal cannot tell it from a corpse.
//
// The fix is not a flush timer. A timer would be a second clock answering a
// question the first clock is already wrong about, and it would spend a durable
// write on every leaf in the system to rescue the rare one that is quiet. The
// fact that was missing is simply that A CALL IS IN FLIGHT — the worker asked
// the model, or started a command, and has not been answered yet — and that
// fact is known in this process, for free, by the code that is waiting.
//
// So a worker says so, through the context, exactly as it already says where its
// transcript goes (WithTranscript above). The scheduler that dispatched the leaf
// is the listener, because the reaper is in the same process as the worker it
// would be reaping; nobody listening is the ordinary case for a unit test or a
// bench harness and costs one type assertion.
//
// IT IS A SPAN AND NOT A PING. "A tool call was issued" would keep a claim alive
// for one instant and then go quiet again for the whole seven minutes the
// command actually runs, which is the case this exists for. What is reported is
// the beginning and the end, and everything between them is a worker that is
// demonstrably waiting on something.

// LivenessMark is told that a call is starting and returns the function that
// says it finished. An implementation must be safe for concurrent use: a batch
// of tools runs in parallel, so several spans are open at once.
type LivenessMark func() func()

type livenessContextKey struct{}

// WithLiveness arms one attempt's liveness reporting. Like the transcript sink,
// it belongs to the attempt rather than to the worker.
func WithLiveness(ctx context.Context, mark LivenessMark) context.Context {
	if mark == nil {
		return ctx
	}
	return context.WithValue(ctx, livenessContextKey{}, mark)
}

// Working opens a span: something this worker is waiting on has started, and the
// returned function says it is over. Calling the returned function more than
// once is safe, and so is ignoring the whole mechanism — with nobody listening
// both halves are no-ops.
//
// It is deliberately shaped so the call site reads as a defer:
//
//	defer Working(ctx)()
func Working(ctx context.Context) func() {
	mark, _ := ctx.Value(livenessContextKey{}).(LivenessMark)
	if mark == nil {
		return func() {}
	}
	if done := mark(); done != nil {
		return done
	}
	return func() {}
}
