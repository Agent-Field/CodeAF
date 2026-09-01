package tool

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/orclient"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
)

// aforge-embed: D9 — this diverges from upstream registry.ts:339-393, which
// gated apply_patch to gpt-family models and hid edit/write for them. Here
// every coder gets edit, write, and apply_patch together regardless of model,
// so the benchmark's deepseek leaves are no longer limited to single-file
// edits. See internal/swepro/EMBEDDING.md (D9).
func TestRegistryModelFilteringParity(t *testing.T) {
	registry := New(t.TempDir())
	// Every model family — non-gpt, gpt, the gpt variants upstream excluded,
	// and the benchmark's deepseek — gets the same coder toolset, including
	// both single-file (edit/write) and multi-file (apply_patch) editors.
	// V7: Firecrawl makes websearch part of the ordinary keyless coder belt.
	want := []string{"bash", "read", "glob", "grep", "edit", "write", "webfetch", "websearch", "apply_patch"}
	for _, modelID := range []string{
		"anthropic/claude-opus-4-6",
		"openai/gpt-5.4",
		"openai/gpt-oss-120b",
		"openai/gpt-4.1",
		"deepseek/deepseek-v4-flash",
	} {
		names := definitionNames(registry.DefinitionsFor(FilterInput{
			ProviderID: "openrouter",
			ModelID:    modelID,
			AgentName:  "coder",
		}))
		if !reflect.DeepEqual(names, want) {
			t.Fatalf("%s names = %v, want %v", modelID, names, want)
		}
		if !containsName(names, "apply_patch") {
			t.Fatalf("%s missing apply_patch: %v", modelID, names)
		}
	}
}

func TestRegistrySpecialistIsolationParity(t *testing.T) {
	definitions := namedDefinitions(
		"bash",
		"read",
		"edit",
		"write",
		"apply_patch",
		"task",
		"plandb",
		"pr_diff",
		"review_proof",
		"search_code",
		"custom",
	)
	coder := definitionNames(FilterDefinitions(definitions, FilterInput{
		ModelID:   "claude",
		AgentName: "coder",
	}))
	if containsName(coder, "pr_diff") || containsName(coder, "review_proof") || containsName(coder, "search_code") {
		t.Fatalf("specialist tools leaked to coder: %v", coder)
	}
	reviewer := definitionNames(FilterDefinitions(definitions, FilterInput{
		ModelID:   "claude",
		AgentName: "review-prover",
	}))
	if want := []string{"read", "task", "pr_diff", "review_proof", "search_code", "custom"}; !reflect.DeepEqual(reviewer, want) {
		t.Fatalf("review tools = %v, want %v", reviewer, want)
	}
	architect := definitionNames(FilterDefinitions(definitions, FilterInput{
		ModelID:   "claude",
		AgentName: "arch-architect",
	}))
	if containsName(architect, "task") || containsName(architect, "plandb") {
		t.Fatalf("arch forbidden tools leaked: %v", architect)
	}
}

func TestRegistryFixtureParity(t *testing.T) {
	// V7: Deleting the provider gate also removes every frozen
	// webSearchEnabled fixture; the fixture file deliberately has no rows now.
	file, err := os.Open("testdata/registry-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var fixture editFixture
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Fn == "webSearchEnabled" {
			t.Fatalf("deleted webSearchEnabled gate still has fixture %q", fixture.Name)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("fixture count = %d, want 0 after deleting the gate", count)
	}
}

func TestToolDescriptionTextParity(t *testing.T) {
	cases := []struct {
		path string
		got  string
	}{
		{"read.txt", readDescription},
		{"write.txt", writeDescription},
		{"edit.txt", editDescription},
		{"glob.txt", globDescription},
		{"grep.txt", grepDescription},
		{"apply_patch.txt", applyPatchDescription},
		{"webfetch.txt", webFetchDescription},
		{"websearch.txt", webSearchDescriptionTemplate},
	}
	for _, test := range cases {
		want, err := os.ReadFile("/home/abir/swe-migration/swe-pro/src/tool/" + test.path)
		if err != nil {
			t.Skipf("frozen TS source unavailable: %v", err)
		}
		if test.got != string(want) {
			t.Errorf("%s differs from frozen TypeScript bytes", test.path)
		}
	}
}

func TestRegistryIDsInsertionOrder(t *testing.T) {
	registry := New(t.TempDir())
	want := []string{"bash", "read", "glob", "grep", "edit", "write", "task", "webfetch", "plandb", "websearch", "apply_patch"}
	if got := registry.IDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs = %v, want %v", got, want)
	}
}

func definitionNames(definitions []steploop.ToolDefinition) []string {
	out := make([]string, 0, len(definitions))
	for _, item := range definitions {
		out = append(out, item.Provider.Name)
	}
	return out
}

func namedDefinitions(names ...string) []steploop.ToolDefinition {
	out := make([]steploop.ToolDefinition, 0, len(names))
	for _, name := range names {
		out = append(out, steploop.ToolDefinition{Provider: orclient.Tool{
			Type:        "function",
			Name:        name,
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}})
	}
	return out
}

func containsName(names []string, name string) bool {
	for _, item := range names {
		if item == name {
			return true
		}
	}
	return false
}
