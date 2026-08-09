package tool

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/engine/orclient"
	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
)

func TestRegistryModelFilteringParity(t *testing.T) {
	registry := New(t.TempDir())
	nonGPT := definitionNames(registry.DefinitionsFor(FilterInput{
		ProviderID: "openrouter",
		ModelID:    "anthropic/claude-opus-4-6",
		AgentName:  "coder",
	}))
	if want := []string{"bash", "read", "glob", "grep", "edit", "write", "webfetch"}; !reflect.DeepEqual(nonGPT, want) {
		t.Fatalf("non-GPT = %v, want %v", nonGPT, want)
	}
	gpt := definitionNames(registry.DefinitionsFor(FilterInput{
		ProviderID: "openrouter",
		ModelID:    "openai/gpt-5.4",
		AgentName:  "coder",
	}))
	if want := []string{"bash", "read", "glob", "grep", "webfetch", "apply_patch"}; !reflect.DeepEqual(gpt, want) {
		t.Fatalf("GPT = %v, want %v", gpt, want)
	}
	for _, modelID := range []string{"openai/gpt-oss-120b", "openai/gpt-4.1"} {
		names := definitionNames(registry.DefinitionsFor(FilterInput{
			ProviderID: "openrouter",
			ModelID:    modelID,
			AgentName:  "coder",
		}))
		if !containsName(names, "edit") || containsName(names, "apply_patch") {
			t.Fatalf("%s names = %v", modelID, names)
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

func TestWebSearchEnabledParity(t *testing.T) {
	cases := []struct {
		provider string
		flags    WebSearchFlags
		want     bool
	}{
		{"codeaf", WebSearchFlags{}, true},
		{"openrouter", WebSearchFlags{}, false},
		{"openrouter", WebSearchFlags{Exa: true}, true},
		{"openrouter", WebSearchFlags{Parallel: true}, true},
	}
	for _, test := range cases {
		if got := WebSearchEnabled(test.provider, test.flags); got != test.want {
			t.Errorf("WebSearchEnabled(%q, %#v) = %v", test.provider, test.flags, got)
		}
	}
}

func TestRegistryFixtureParity(t *testing.T) {
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
		var args []json.RawMessage
		if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
			t.Fatal(err)
		}
		var provider string
		var flags struct {
			Exa      bool `json:"exa"`
			Parallel bool `json:"parallel"`
		}
		if err := json.Unmarshal(args[0], &provider); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &flags); err != nil {
			t.Fatal(err)
		}
		got := WebSearchEnabled(provider, WebSearchFlags{Exa: flags.Exa, Parallel: flags.Parallel})
		data, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != fixture.OutJSON {
			t.Errorf("%s: got %s want %s", fixture.Name, data, fixture.OutJSON)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("fixture count = %d", count)
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
