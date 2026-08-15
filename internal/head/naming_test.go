package head

import (
	"context"
	"strings"
	"testing"
)

// The job's name comes back with the goal that needs naming.
//
// It used to be its own model round-trip — measured at 222 to 330 prompt
// tokens for a five-token answer, in front of every job, before any leaf could
// start — and the call that already reads the whole ask was sitting right
// beside it returning a JSON object. Nothing downstream waits on the label, so
// the only thing that round-trip ever bought was ~0.6 s of dead critical path.
func TestTheCompileCallNamesTheJobItCompiles(t *testing.T) {
	for _, required := range []string{
		`"title":"..."`,
		"recognise it at a glance among unrelated jobs",
		"It is a NAME, not a summary",
	} {
		if !strings.Contains(compilerSystemPrompt, required) {
			t.Errorf("the compiler prompt lost its naming rule: %q", required)
		}
	}
	client := &fakeClient{responses: []string{
		`{"goal":"Fix the failing interval test.","title":"Interval overlap fix.","assumptions":[],"scale":"task"}`,
	}}
	brief, err := NewCompiler(client).Compile(context.Background(), "fix the failing test", "")
	if err != nil {
		t.Fatal(err)
	}
	// Trailing punctuation is stripped because a rail shows the name beside
	// other names, not as a sentence.
	if brief.Title != "Interval overlap fix" {
		t.Fatalf("compiled title = %q", brief.Title)
	}
	if got := client.calls; got != 1 {
		t.Fatalf("naming cost %d calls beyond the compile, want 0", got-1)
	}
}

// A compiler that says nothing about the name is not an error: the caller
// still has the separate naming pass and, failing that, the brief's first
// line. Titling is a nicety and must never be able to fail a job.
func TestAnUnnamedBriefIsStillAValidBrief(t *testing.T) {
	client := &fakeClient{responses: []string{
		`{"goal":"Fix the failing interval test.","assumptions":[],"scale":"task"}`,
	}}
	brief, err := NewCompiler(client).Compile(context.Background(), "fix the failing test", "")
	if err != nil {
		t.Fatalf("a brief without a title was rejected: %v", err)
	}
	if brief.Title != "" {
		t.Fatalf("a title appeared from nowhere: %q", brief.Title)
	}
	for name, raw := range map[string]string{
		"quoted":            `"Interval overlap fix"`,
		"trailing period":   "Interval overlap fix.",
		"a whole paragraph": "Interval overlap fix\nand then some explanation",
	} {
		if got := normalizeTitle(raw); got != "Interval overlap fix" {
			t.Errorf("%s normalized to %q", name, got)
		}
	}
}
