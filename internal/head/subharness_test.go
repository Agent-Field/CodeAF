package head

import (
	"context"
	"os"
	"strings"
	"testing"
)

const compilerReply = `{"goal":"do the thing","title":"the thing","scale":"task","contract":"",` +
	`"parts":[],"builds_on":[],"assumptions":["did it now"],"question":"","question_options":[],` +
	`"trial_of":0,"subharness":"swe"}`

// The compiler's system message is the pre-subharness prompt, byte for byte,
// from a golden captured at the commit before this feature — with a menu
// installed, with an empty menu, and with no menu at all. That is stronger than
// the additive law it started as: the menu carries measured counters that move
// within a session, and the system message is the one string in the whole
// compile that could be identical from job to job, so nothing measured is
// allowed into it whatever it is measuring.
func TestBaselineCompilerPromptIsByteIdentical(t *testing.T) {
	golden, err := os.ReadFile("testdata/compiler_prompt_baseline.golden")
	if err != nil {
		t.Fatal(err)
	}
	if compilerSystemPrompt != string(golden) {
		t.Fatal("the compiler prompt drifted from its pre-subharness bytes")
	}

	for name, compiler := range map[string]*Compiler{
		"no menu installed": NewCompiler(&fakeClient{responses: []string{compilerReply}}),
		"menu is empty": NewCompiler(&fakeClient{responses: []string{compilerReply}}).
			WithSubharnessMenu(func() string { return "" }, func(string) bool { return false }),
		"menu carries measured counters": NewCompiler(&fakeClient{responses: []string{compilerReply}}).
			WithSubharnessMenu(func() string {
				return "Subharnesses.\n\n- swe — software engineering taken whole\n" +
					"  measured here so far: median 40000 tokens, 12 turns over 31 runs; " +
					"90% succeeded; avg cost $0.0412\n"
			}, func(name string) bool { return name == "swe" }),
	} {
		client := compiler.client.(*fakeClient)
		brief, err := compiler.Compile(context.Background(), "do the thing", "no jobs yet")
		if err != nil {
			t.Fatalf("%s: compile: %v", name, err)
		}
		if client.systemPrompt() != string(golden) {
			t.Fatalf("%s: the system message is not the baseline prompt", name)
		}
		if name == "menu carries measured counters" {
			// Dropped from the system message means moved, not lost.
			if !strings.Contains(client.userPrompt(), "avg cost $0.0412") {
				t.Fatalf("%s: the measured menu reached neither message:\n%s", name, client.userPrompt())
			}
			continue
		}
		// The model volunteered a worker nobody offered it. It is dropped.
		if brief.Subharness != "" {
			t.Fatalf("%s: subharness = %q, want it dropped", name, brief.Subharness)
		}
	}
}

// The menu reads as law and is positioned as churn, because position is by
// volatility and never by semantic category: its measured lines move with every
// leaf a specialist finishes. So it rides at the very end of the user message,
// under the instruction, where the bytes above it were already going to differ
// from one compile to the next.
func TestMeasuredMenuRidesTheEndOfTheUserMessageNotTheSystemPrompt(t *testing.T) {
	client := &fakeClient{responses: []string{compilerReply}}
	compiler := NewCompiler(client).WithSubharnessMenu(
		func() string { return "Subharnesses.\n\n- swe — software engineering taken whole" },
		func(name string) bool { return name == "swe" },
	)
	brief, err := compiler.Compile(context.Background(), "fix the failing tests", "no jobs yet")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if system := client.systemPrompt(); system != compilerSystemPrompt {
		t.Fatal("the menu is back in the system message, which every compile shares")
	}
	user := client.userPrompt()
	for _, want := range []string{"swe — software engineering taken whole", `"subharness":"<name>"`} {
		if !strings.Contains(user, want) {
			t.Fatalf("the user message is missing %q:\n%s", want, user)
		}
	}
	if menu := strings.Index(user, "Subharnesses."); menu < strings.Index(user, "fix the failing tests") {
		t.Fatalf("the menu sits above the instruction it should be trailing:\n%s", user)
	}
	if brief.Subharness != "swe" {
		t.Fatalf("subharness = %q, want swe", brief.Subharness)
	}
}

// Mis-selection degrades to the baseline. A compile is the cheapest call in the
// job and the only one whose loss forfeits everything after it, so a name that
// reaches nothing costs the job its specialist and not the job.
func TestUnknownSubharnessIsDroppedRatherThanRefused(t *testing.T) {
	client := &fakeClient{responses: []string{compilerReply}}
	compiler := NewCompiler(client).WithSubharnessMenu(
		func() string { return "Subharnesses.\n\n- reviewer — reading one change whole" },
		func(name string) bool { return name == "reviewer" },
	)
	brief, err := compiler.Compile(context.Background(), "fix the failing tests", "no jobs yet")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if brief.Subharness != "" {
		t.Fatalf("subharness = %q, want it dropped", brief.Subharness)
	}
	if strings.TrimSpace(brief.Goal) == "" {
		t.Fatal("the brief itself was lost with the bad name")
	}
}
