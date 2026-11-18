package jsrun

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A SCRIPTED CALLER AND NOTHING ELSE, for fake_test.go's reason: these tests
// are about what the script host does with answers, not where answers come from.
type scriptCalls struct {
	answers map[string]any
	fail    map[string]error
	seen    []string
}

func (s *scriptCalls) call(_ context.Context, name string, args map[string]any) (any, error) {
	s.seen = append(s.seen, name)
	if err := s.fail[name]; err != nil {
		return nil, err
	}
	if answer, ok := s.answers[name]; ok {
		return answer, nil
	}
	return name + " answered", nil
}

func runScript(t *testing.T, source string, calls *scriptCalls, limits ScriptLimits) ScriptResult {
	t.Helper()
	return RunScript(context.Background(), source, []string{"bash", "read_document", "web-search"}, calls.call, limits)
}

func TestScriptReturnsTextAndCallResults(t *testing.T) {
	calls := &scriptCalls{answers: map[string]any{
		"bash": map[string]any{"output": "a\nb\n", "exit_code": 1},
	}}
	got := runScript(t, `
const run = await tools.bash({command: "grep x"});
text("exit " + run.exit_code);
console.log("lines", run.output.trim().split("\n").length);
return {doc: await tools.read_document({path: "a.pdf"})};
`, calls, ScriptLimits{})
	if got.Failed != "" || got.Stopped != "" {
		t.Fatalf("the script did not finish: failed=%q stopped=%q", got.Failed, got.Stopped)
	}
	for _, want := range []string{"exit 1", `{"doc":"read_document answered"}`, "lines 2"} {
		if !strings.Contains(got.Output, want) {
			t.Errorf("output is missing %q:\n%s", want, got.Output)
		}
	}
	if got.Calls != 2 {
		t.Errorf("calls = %d, want 2", got.Calls)
	}
}

func TestScriptReachesANameThatIsNotAnIdentifier(t *testing.T) {
	calls := &scriptCalls{}
	got := runScript(t, `return [await tools.web_search({q: "x"}), await tools["web-search"]({q: "y"})];`, calls, ScriptLimits{})
	if got.Failed != "" {
		t.Fatalf("failed: %s", got.Failed)
	}
	if len(calls.seen) != 2 || calls.seen[0] != "web-search" || calls.seen[1] != "web-search" {
		t.Errorf("calls reached %v, want web-search twice", calls.seen)
	}
}

func TestScriptRefusedCallRejectsAndCanBeCaught(t *testing.T) {
	calls := &scriptCalls{fail: map[string]error{"bash": errors.New("denied by the policy: bash")}}
	got := runScript(t, `
const settled = await Promise.allSettled([tools.bash({command: "rm -rf x"}), tools.read_document({path: "a"})]);
try { await tools.bash({command: "again"}) } catch (e) { text("caught: " + e.message) }
return settled.map(s => s.status);
`, calls, ScriptLimits{})
	if got.Failed != "" {
		t.Fatalf("a caught refusal failed the script: %s", got.Failed)
	}
	for _, want := range []string{"caught: denied by the policy: bash", `["rejected","fulfilled"]`} {
		if !strings.Contains(got.Output, want) {
			t.Errorf("output is missing %q:\n%s", want, got.Output)
		}
	}
}

func TestScriptUncaughtErrorFailsWithPartialOutput(t *testing.T) {
	calls := &scriptCalls{fail: map[string]error{"bash": errors.New("Command timed out after 5 seconds")}}
	got := runScript(t, `text("before"); await tools.bash({command: "sleep 9"}); text("after");`, calls, ScriptLimits{})
	if !strings.Contains(got.Failed, "Command timed out after 5 seconds") {
		t.Errorf("failed = %q, want the tool's own text", got.Failed)
	}
	if !strings.Contains(got.Output, "before") || strings.Contains(got.Output, "after") {
		t.Errorf("output = %q, want only what came before the failure", got.Output)
	}
}

func TestScriptCompileErrorFailsWithoutCalling(t *testing.T) {
	calls := &scriptCalls{}
	got := runScript(t, `await tools.bash({command: "x"}) +++ ;`, calls, ScriptLimits{})
	if got.Failed == "" || len(calls.seen) != 0 {
		t.Errorf("failed = %q, calls = %v; want a compile failure and no calls", got.Failed, calls.seen)
	}
}

func TestScriptCallCeilingCannotBeCaught(t *testing.T) {
	calls := &scriptCalls{}
	got := runScript(t, `
for (let i = 0; i < 10; i++) { try { await tools.bash({command: "echo " + i}) } catch (e) {} }
text("escaped the ceiling");
`, calls, ScriptLimits{Calls: 3})
	if !strings.Contains(got.Stopped, "3") || strings.Contains(got.Output, "escaped") {
		t.Errorf("stopped = %q, output = %q; want an uncatchable stop at 3 calls", got.Stopped, got.Output)
	}
	if len(calls.seen) != 3 {
		t.Errorf("calls reached %d, want 3", len(calls.seen))
	}
}

func TestScriptWallDeadlineStopsALoopThatCallsNothing(t *testing.T) {
	got := runScript(t, `while (true) {}`, &scriptCalls{}, ScriptLimits{Wall: 50 * time.Millisecond})
	if !strings.Contains(got.Stopped, "ran out of time") {
		t.Errorf("stopped = %q, want the deadline", got.Stopped)
	}
}

func TestScriptCancelledContextStopsIt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := &scriptCalls{}
	caller := func(c context.Context, name string, args map[string]any) (any, error) {
		cancel()
		return calls.call(c, name, args)
	}
	got := RunScript(ctx, `await tools.bash({}); while (true) {}`, []string{"bash"}, caller, ScriptLimits{})
	if !strings.Contains(got.Stopped, "stopped before it finished") {
		t.Errorf("stopped = %q, want the cancellation", got.Stopped)
	}
}

func TestScriptOutputCeilingStopsIt(t *testing.T) {
	got := runScript(t, `for (;;) text("0123456789")`, &scriptCalls{}, ScriptLimits{Output: 100})
	if !strings.Contains(got.Stopped, "100 bytes") {
		t.Errorf("stopped = %q, want the output ceiling", got.Stopped)
	}
	if len(got.Output) > 100 {
		t.Errorf("output kept %d bytes past a 100-byte ceiling", len(got.Output))
	}
}

func TestScriptWaitingOnNothingFailsAtOnce(t *testing.T) {
	got := runScript(t, `await new Promise(() => {});`, &scriptCalls{}, ScriptLimits{Wall: time.Minute})
	if !strings.Contains(got.Failed, "can never finish") {
		t.Errorf("failed = %q, want the never-settles failure", got.Failed)
	}
}

func TestScriptNonObjectArgumentsReject(t *testing.T) {
	calls := &scriptCalls{}
	got := runScript(t, `await tools.bash("ls")`, calls, ScriptLimits{})
	if !strings.Contains(got.Failed, "one object of arguments") || len(calls.seen) != 0 {
		t.Errorf("failed = %q, calls = %v; want a refusal before any call", got.Failed, calls.seen)
	}
}

// THE CONCURRENCY TESTS USE BARRIERS, NOT CLOCKS: a caller waits until enough
// calls are inside at once, so a host that ran them one at a time fails by its
// own deadline rather than by a timing guess.

func TestScriptCallsRunAtTheSameTimeUpToTheCap(t *testing.T) {
	var inside, most atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	caller := func(ctx context.Context, name string, _ map[string]any) (any, error) {
		now := inside.Add(1)
		defer inside.Add(-1)
		for {
			seen := most.Load()
			if now <= seen || most.CompareAndSwap(seen, now) {
				break
			}
		}
		if now == 3 {
			once.Do(func() { close(release) })
		}
		select {
		case <-release:
			return name, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	got := RunScript(context.Background(), `
const answers = await Promise.all([1, 2, 3, 4, 5, 6].map(() => tools.bash({})));
return answers.length;
`, []string{"bash"}, caller, ScriptLimits{Parallel: 3, Wall: 5 * time.Second})
	if got.Failed != "" || got.Stopped != "" || got.Output != "6" {
		t.Fatalf("the calls did not run together: output=%q failed=%q stopped=%q", got.Output, got.Failed, got.Stopped)
	}
	if most.Load() != 3 {
		t.Errorf("at most %d calls ran at once, want the cap of 3", most.Load())
	}
}

func TestScriptStopsWaitingAtItsDeadlineAndCancelsItsCalls(t *testing.T) {
	var started, returned atomic.Int32
	caller := func(ctx context.Context, _ string, _ map[string]any) (any, error) {
		started.Add(1)
		defer returned.Add(1)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	got := RunScript(context.Background(), `await Promise.all([tools.bash({}), tools.bash({})])`,
		[]string{"bash"}, caller, ScriptLimits{Wall: 50 * time.Millisecond})
	if !strings.Contains(got.Stopped, "ran out of time") {
		t.Errorf("stopped = %q, want the deadline", got.Stopped)
	}
	if started.Load() != 2 || returned.Load() != 2 {
		t.Errorf("started %d, returned %d by the time the script ended; every call must end first", started.Load(), returned.Load())
	}
}

func TestScriptCancelledWhileWaitingCancelsItsCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var returned atomic.Int32
	caller := func(c context.Context, _ string, _ map[string]any) (any, error) {
		defer returned.Add(1)
		cancel()
		<-c.Done()
		return nil, c.Err()
	}
	got := RunScript(ctx, `await tools.bash({})`, []string{"bash"}, caller, ScriptLimits{Wall: time.Minute})
	if !strings.Contains(got.Stopped, "stopped before it finished") || returned.Load() != 1 {
		t.Errorf("stopped = %q, returned = %d; want the cancellation and the call ended", got.Stopped, returned.Load())
	}
}

func TestScriptCeilingInAContinuationStopsIt(t *testing.T) {
	calls := &scriptCalls{}
	got := runScript(t, `
await tools.bash({});
await tools.bash({});
for (;;) {}
`, calls, ScriptLimits{Wall: 50 * time.Millisecond})
	if !strings.Contains(got.Stopped, "ran out of time") || len(calls.seen) != 2 {
		t.Errorf("stopped = %q after %d calls; want the deadline after 2", got.Stopped, len(calls.seen))
	}
}

// A RETURN VALUE PAST THE OUTPUT CEILING STOPS THE SCRIPT, and says so. It is
// kept after the interpreter has finished, where an interrupt reaches nothing,
// so the reason is recorded rather than raised.
func TestScriptReturnPastTheOutputCeilingSaysSo(t *testing.T) {
	got := runScript(t, `return "x".repeat(200)`, &scriptCalls{}, ScriptLimits{Output: 100})
	if !strings.Contains(got.Stopped, "100 bytes") {
		t.Errorf("stopped = %q, failed = %q; want the output ceiling named", got.Stopped, got.Failed)
	}
}

// A NAME IS NEVER TAKEN BY ANOTHER TOOL'S ALIAS, whichever is bound first.
func TestScriptAliasNeverReplacesAToolsOwnName(t *testing.T) {
	for _, names := range [][]string{{"foo_bar", "foo-bar"}, {"foo-bar", "foo_bar"}} {
		var seen []string
		caller := func(_ context.Context, name string, _ map[string]any) (any, error) {
			seen = append(seen, name)
			return name, nil
		}
		got := RunScript(context.Background(), `return await tools.foo_bar({})`, names, caller, ScriptLimits{})
		if len(seen) != 1 || seen[0] != "foo_bar" {
			t.Errorf("names %v: tools.foo_bar reached %v (output %q)", names, seen, got.Output)
		}
	}
}

// A CALL STILL WAITING FOR A SLOT WHEN THE SCRIPT ENDS NEVER STARTS. There is
// one slot and every call holds it until the script's context ends, so at most
// one call can pass the slot before the ending; any other gets the slot only
// after it, and must not start. Counting entries, not their timing, keeps this
// exact.
func TestScriptQueuedCallsDoNotStartAfterItEnds(t *testing.T) {
	for i := 0; i < 50; i++ {
		var entered atomic.Int32
		caller := func(ctx context.Context, _ string, _ map[string]any) (any, error) {
			entered.Add(1)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		RunScript(context.Background(), `tools.bash({}); tools.bash({}); tools.bash({}); return 1`,
			[]string{"bash"}, caller, ScriptLimits{Parallel: 1})
		if n := entered.Load(); n > 1 {
			t.Fatalf("run %d: %d calls started; the queued ones began after the script ended", i, n)
		}
	}
}
