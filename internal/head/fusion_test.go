package head

import (
	"context"
	"strings"
	"testing"
)

// The working method comes back with the goal that needs one.
//
// For a task-scale ask the contract used to be a second structuring call,
// serialized between compile and dispatch on the critical path of every
// single-worker job — the shape of work the product handles most. The compile
// call has already read the whole ask; it writes the method in the same reply,
// and the separate pass becomes the fallback rather than the path.
func TestTheCompileCallWritesTheMethodItCompiles(t *testing.T) {
	for _, required := range []string{
		`"contract":""`,
		`For "task" scale only, also write "contract"`,
		"what done means, what will be run or checked as evidence before handover",
	} {
		if !strings.Contains(compilerSystemPrompt, required) {
			t.Errorf("the compiler prompt lost its method rule: %q", required)
		}
	}
	client := &fakeClient{responses: []string{
		`{"goal":"Fix the failing interval test.","title":"Interval overlap fix","assumptions":[],"scale":"task",` +
			`"contract":"Read the failing test first. Done means the suite passes and the fix names the line."}`,
	}}
	brief, err := NewCompiler(client).Compile(context.Background(), "fix the failing test", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(brief.Contract, "Done means the suite passes") {
		t.Fatalf("the compiled method was dropped: %q", brief.Contract)
	}
	if got := client.calls; got != 1 {
		t.Fatalf("the method cost %d calls beyond the compile, want 0", got-1)
	}

	// A compiler that says nothing about the method is not an error: the
	// separate contract pass still exists, and a method must never be able to
	// fail a job.
	client = &fakeClient{responses: []string{
		`{"goal":"Fix the failing interval test.","assumptions":[],"scale":"task"}`,
	}}
	brief, err = NewCompiler(client).Compile(context.Background(), "fix the failing test", "")
	if err != nil {
		t.Fatalf("a brief without a method was rejected: %v", err)
	}
	if brief.Contract != "" {
		t.Fatalf("a method appeared from nowhere: %q", brief.Contract)
	}
}

// The judgment boundary rides the scale rule: one artifact under judgment is
// one worker at any length, because what the judgment exists to catch lives in
// the cross-references a divided reading can never see. This is the compiler
// prompt's half of the same rule the planner carries — a benchmark cell fanned
// one narrow change under review into thirteen workers, and the deal-memo cell
// inverted a conclusion the same model got right when it read everything in
// one seat.
func TestJudgingOneArtifactIsOneWorkerAtAnyLength(t *testing.T) {
	for _, required := range []string{
		"One boundary overrides both of those questions",
		`is "task" at any length`,
		"never dividing one act of comprehension",
	} {
		if !strings.Contains(compilerSystemPrompt, required) {
			t.Errorf("the compiler prompt lost the judgment boundary: %q", required)
		}
	}
}

// The shapes models actually send for a bundle declaration, measured live: an
// array of wrapper objects killed the whole compile through a strict []string.
// A declaration field's worst legal outcome is no declaration.
func TestPartsSurviveTheShapesModelsActuallySend(t *testing.T) {
	for name, wire := range map[string]struct {
		raw  string
		want int
	}{
		"plain strings":   {`["fix the test","compute revenue"]`, 2},
		"wrapper objects": {`[{"part":"fix the test","id":"1"},{"text":"compute revenue"}]`, 2},
		"one bare string": {`"fix the test"`, 1},
		"garbage numbers": {`[1,2,3]`, 0},
		"an object":       {`{"parts":"nope"}`, 0},
	} {
		var parts PartList
		if err := parts.UnmarshalJSON([]byte(wire.raw)); err != nil {
			t.Errorf("%s: a parts declaration failed the parse: %v", name, err)
			continue
		}
		if len(parts) != wire.want {
			t.Errorf("%s: kept %d parts, want %d (%v)", name, len(parts), wire.want, parts)
		}
	}
}

// Two GAIA questions long enough to echo hit the compile's 1000-token ceiling
// to the digit and were lost whole — well-formed JSON, guillotined. The reply
// must have room for the ask it restates, and a truncated object earns one
// retry at double room before the question is forfeited.
func TestTheCompileReplyBreathesWithTheAskAndRetriesATruncation(t *testing.T) {
	short := compileReplyTokens("fix the failing test")
	long := compileReplyTokens(strings.Repeat("a question with many words in it ", 200))
	if short < 1000 || long <= short+1000 {
		t.Fatalf("the cap does not breathe: short=%d long=%d", short, long)
	}
	client := &fakeClient{responses: []string{
		`{"goal":"Fix the failing interval test, which is to say the whole of`, // cut mid-structure
		`{"goal":"Fix the failing interval test.","assumptions":[],"scale":"task"}`,
	}}
	brief, err := NewCompiler(client).Compile(context.Background(), "fix the failing test", "")
	if err != nil {
		t.Fatalf("a truncated first reply forfeited the question: %v", err)
	}
	if brief.Goal == "" || client.calls != 2 {
		t.Fatalf("retry did not rescue the compile: goal=%q calls=%d", brief.Goal, client.calls)
	}
}
