package jsrun

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// A CALL TURNED AWAY FOR ITS ARGUMENTS NEVER RAN, so it spends nothing of the
// call ceiling and is not reported as a call made.
func TestScriptRejectedArgumentsSpendNoCall(t *testing.T) {
	calls := &scriptCalls{}
	got := runScript(t, `
for (let i = 0; i < 5; i++) { try { await tools.bash("ls") } catch (e) {} }
return "done"`, calls, ScriptLimits{Calls: 3})
	if got.Stopped != "" || got.Failed != "" {
		t.Fatalf("five rejected calls stopped the script: stopped=%q failed=%q", got.Stopped, got.Failed)
	}
	if got.Calls != 0 || len(calls.seen) != 0 {
		t.Errorf("calls = %d (seen %v), want 0: none of them ran", got.Calls, calls.seen)
	}
}

// THE CALL CEILING IS NAMED AS WHAT IT IS. "Steps" is the task's own unit, and
// a script that says it ran out of steps reads as the task's budget ending.
func TestScriptCallCeilingIsNamedAsCalls(t *testing.T) {
	got := runScript(t, `while (true) { await tools.bash({command: "ls"}) }`, &scriptCalls{}, ScriptLimits{Calls: 2})
	if want := fmt.Sprintf("%d tool calls", 2); !strings.Contains(got.Stopped, want) || strings.Contains(got.Stopped, "step") {
		t.Errorf("stopped = %q; want it to name %q and not steps", got.Stopped, want)
	}
}

// A CALL STARTED WITHOUT AWAIT IS COUNTED. The script's own promise settles
// first, the call is cancelled, and the result says so rather than reading as
// if the work had been done.
func TestScriptCountsTheCallsItDidNotWaitFor(t *testing.T) {
	blocking := func(ctx context.Context, name string, _ map[string]any) (any, error) {
		if name == "bash" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return "ok", nil
	}
	got := RunScript(context.Background(), `tools.bash({command: "make gen"}); await tools.read_document({path: "a"}); return "started"`,
		[]string{"bash", "read_document"}, blocking, ScriptLimits{})
	if got.Failed != "" || got.Stopped != "" {
		t.Fatalf("failed=%q stopped=%q", got.Failed, got.Stopped)
	}
	if got.Unawaited != 1 {
		t.Errorf("unawaited = %d, want 1", got.Unawaited)
	}
	every := RunScript(context.Background(), `await tools.read_document({path: "a"}); return "done"`,
		[]string{"bash", "read_document"}, blocking, ScriptLimits{})
	if every.Unawaited != 0 {
		t.Errorf("a script that waited for everything reports %d unawaited", every.Unawaited)
	}
}

// A CALL CANCELLED BEFORE IT RAN IS NOT A CALL MADE. Fifty calls fired without
// await while four slots run: the ones still queued when the script ended
// never ran, and the count says so.
func TestScriptCountsOnlyTheCallsThatRan(t *testing.T) {
	blocking := func(ctx context.Context, _ string, _ map[string]any) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	got := RunScript(context.Background(), `for (let i = 0; i < 50; i++) tools.bash({command: "x"}); return "fired"`,
		[]string{"bash"}, blocking, ScriptLimits{Parallel: 4})
	if got.Calls > 4 {
		t.Errorf("calls = %d; at most 4 ran, the rest were cancelled waiting", got.Calls)
	}
	if got.Unawaited != 50 {
		t.Errorf("unawaited = %d, want 50", got.Unawaited)
	}
}
