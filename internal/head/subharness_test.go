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

// A compiler with no menu is the compiler as it was: the system message it
// sends is the pre-subharness prompt, byte for byte, from a golden captured at
// the commit before this feature. A menu with one entry is no menu, so a
// process with only the default worker never mentions the field at all.
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
	} {
		client := compiler.client.(*fakeClient)
		brief, err := compiler.Compile(context.Background(), "do the thing", "no jobs yet")
		if err != nil {
			t.Fatalf("%s: compile: %v", name, err)
		}
		if client.systemPrompt() != string(golden) {
			t.Fatalf("%s: the system message is not the baseline prompt", name)
		}
		// The model volunteered a worker nobody offered it. It is dropped.
		if brief.Subharness != "" {
			t.Fatalf("%s: subharness = %q, want it dropped", name, brief.Subharness)
		}
	}
}

func TestMenuReachesTheCompileAndTheChoiceRidesTheBrief(t *testing.T) {
	client := &fakeClient{responses: []string{compilerReply}}
	compiler := NewCompiler(client).WithSubharnessMenu(
		func() string { return "Subharnesses.\n\n- swe — software engineering taken whole" },
		func(name string) bool { return name == "swe" },
	)
	brief, err := compiler.Compile(context.Background(), "fix the failing tests", "no jobs yet")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	system := client.systemPrompt()
	if !strings.HasPrefix(system, compilerSystemPrompt) {
		t.Fatal("the menu replaced the prompt instead of being added to it")
	}
	for _, want := range []string{"swe — software engineering taken whole", `"subharness":"<name>"`} {
		if !strings.Contains(system, want) {
			t.Fatalf("the system message is missing %q", want)
		}
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
