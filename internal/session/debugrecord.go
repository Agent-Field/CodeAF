package session

import (
	"context"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/trace"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The engine's end of the debug record (internal/trace says why it exists and
// what it costs when nobody asked for one).
//
// THE MODEL CALLS ARE RECORDED BY THE ADAPTER and the tool calls are recorded
// here, because those are the two doors every one of them passes through. A
// person reading a run's folder afterwards is reconstructing an alternation —
// the model asked for this, the machine answered that — and half of it written
// by one lane and half never written at all is the folder that sent somebody
// back to guessing.

// recordToolCall writes one tool call to the debug record. It is a no-op on
// every run nobody switched the record on for: [trace.For] answers nil in two
// atomic loads, which is what lets this sit at the tool chokepoint at all.
//
// A REFUSAL IS NOT A FAILURE, and the record says which it was. A tool a door
// refused never ran — the world never saw the call — and a record in which that
// reads like the tool failing is a record that sends somebody debugging the
// tool instead of the gate. The refuser's own name rides on the result from the
// one place every veto passes through (hooks.go's preAction).
func recordToolCall(ctx context.Context, call ai.ToolCall, result toolResult, started time.Time, took time.Duration) {
	recorder := trace.For(ctx)
	if recorder == nil {
		return
	}
	recorder.Tool(ctx, trace.ToolEvent{
		CallID:   call.ID,
		Name:     call.Function.Name,
		Args:     call.Function.Arguments,
		Result:   result.text,
		Started:  started,
		Duration: took,
		Status:   toolStatus(result),
		Refuser:  result.refusedBy,
		Reason:   refusalReason(result),
	})
}

// toolStatus is the record's own three-way reading of how a call ended, and the
// three words are internal/trace's: "ok", "failed", "refused".
func toolStatus(result toolResult) string {
	switch {
	case result.refusedBy != "":
		return "refused"
	case result.isError:
		return "failed"
	default:
		return "ok"
	}
}

// refusalReason is the refuser's own sentence, which for a veto is the text the
// model was answered with — a door that refuses says why in words the model can
// act on (hooks.go's preActionHook), so there is no second sentence to keep. It
// is empty on everything that is not a refusal, because the result text is
// already on the record's own field and a record that said the same thing twice
// would double the size of the folder for nothing.
func refusalReason(result toolResult) string {
	if result.refusedBy == "" {
		return ""
	}
	return result.text
}

// recordEffort writes the rung this call will ask for. It is the third kind
// of choice the record names (internal/trace's Decision), and it is written
// at the stamp rather than inside the resolver because the resolver has no
// context and the stamp is where the rung actually goes on the wire.
//
// ABSENCE IS NOT A CHOICE. A call that left effort off the request — the
// emptiness law, [effort.None] — writes nothing, because a record that said the
// model was asked to think at "" would be a record of a knob nobody turned.
func recordEffort(ctx context.Context, model string, rung effort.Rung) {
	if rung == effort.None {
		return
	}
	recorder := trace.For(ctx)
	if recorder == nil {
		return
	}
	recorder.Decision(ctx, trace.Decision{
		Kind:    "effort",
		Subject: model,
		Choice:  rung.String(),
	})
}
