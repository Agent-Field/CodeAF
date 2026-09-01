package head

import (
	"context"
	"os"
	"testing"
)

const compilerReply = `{"goal":"do the thing","title":"the thing","scale":"task","contract":"",` +
	`"parts":[],"builds_on":[],"assumptions":["did it now"],"question":"","question_options":[],` +
	`"trial_of":0}`

// The compiler's system message is pinned byte for byte against a golden. It is
// the one string in the whole compile that could be identical from job to job
// and from machine to machine, so nothing that moves — no counter, no measured
// figure, no list of anything this process happens to have installed — is
// allowed into it, whatever it is measuring.
func TestBaselineCompilerPromptIsByteIdentical(t *testing.T) {
	golden, err := os.ReadFile("testdata/compiler_prompt_baseline.golden")
	if err != nil {
		t.Fatal(err)
	}
	if compilerSystemPrompt != string(golden) {
		t.Fatal("the compiler prompt drifted from its pinned bytes")
	}

	client := &fakeClient{responses: []string{compilerReply}}
	compiler := NewCompiler(client)
	if _, err := compiler.Compile(context.Background(), "do the thing", "no jobs yet"); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if client.systemPrompt() != string(golden) {
		t.Fatal("the system message the compiler actually sent is not the pinned prompt")
	}
}
