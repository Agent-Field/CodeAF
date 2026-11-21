package jsrun

import (
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
