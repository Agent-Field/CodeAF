package baked

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()

	var fixtures []fixture
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var item fixture
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		fixtures = append(fixtures, item)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return fixtures
}

func decodeStringArgs(t *testing.T, raw string) []string {
	t.Helper()
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatalf("decode args_json %q: %v", raw, err)
	}
	return args
}

func callFixture(t *testing.T, item fixture) any {
	t.Helper()
	args := decodeStringArgs(t, item.ArgsJSON)
	switch item.Fn {
	case "roster":
		return Roster()
	case "entryAgent":
		return EntryAgent
	case "loadBearingAgents":
		return LoadBearingAgents()
	case "listBakedAgents":
		return ListBakedAgents()
	case "tierAssignments":
		return TierAssignments()
	case "categoryTierAssignments":
		return CategoryTierAssignments()
	case "specialistModes":
		return SpecialistModes()
	case "allExclusiveToolIDs":
		return AllExclusiveToolIDs().Values()
	case "getBakedAgent":
		if len(args) != 1 {
			t.Fatalf("getBakedAgent wants 1 arg, got %d", len(args))
		}
		if markdown, ok := GetBakedAgentMarkdown(args[0]); ok {
			return markdown
		}
		return nil
	case "tierFor":
		if len(args) < 1 || len(args) > 2 {
			t.Fatalf("tierFor wants 1-2 args, got %d", len(args))
		}
		if len(args) == 2 {
			return TierFor(args[0], Tier(args[1]))
		}
		return TierFor(args[0])
	case "modeForAgent":
		if len(args) != 1 {
			t.Fatalf("modeForAgent wants 1 arg, got %d", len(args))
		}
		if mode, ok := ModeForAgent(args[0]); ok {
			return mode
		}
		return nil
	case "entryAgentForMode":
		if len(args) != 1 {
			t.Fatalf("entryAgentForMode wants 1 arg, got %d", len(args))
		}
		if agent, ok := EntryAgentForMode(args[0]); ok {
			return agent
		}
		return nil
	default:
		t.Fatalf("unknown fixture function %q", item.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 90 {
		t.Fatalf("expected at least 90 fixtures, got %d", len(fixtures))
	}
	for _, item := range fixtures {
		t.Run(item.Fn+"/"+item.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callFixture(t, item))
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(got) != item.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", item.ArgsJSON, got, item.OutJSON)
			}
		})
	}
}

func TestFixtureCoverage(t *testing.T) {
	seen := map[string]bool{}
	for _, item := range loadFixtures(t) {
		seen[item.Fn] = true
	}
	for _, function := range []string{
		"roster",
		"entryAgent",
		"loadBearingAgents",
		"listBakedAgents",
		"tierAssignments",
		"categoryTierAssignments",
		"specialistModes",
		"allExclusiveToolIDs",
		"getBakedAgent",
		"tierFor",
		"modeForAgent",
		"entryAgentForMode",
	} {
		if !seen[function] {
			t.Errorf("no fixture cases for %s", function)
		}
	}
}

func TestExclusiveToolFilterMembership(t *testing.T) {
	exclusive := AllExclusiveToolIDs()
	if !exclusive.Has("review_proof") || !exclusive.Has("search_code") {
		t.Fatal("review-only tools are not excluded from the coder surface")
	}
	for _, standard := range []string{"read", "write", "edit", "bash", "task"} {
		if exclusive.Has(standard) {
			t.Errorf("standard tool %q marked exclusive", standard)
		}
	}
}

func TestCoderOnlyRosterSeam(t *testing.T) {
	for _, name := range []string{"review-prover", "review-synthesizer", "arch-architect", "arch-critic"} {
		if markdown, ok := GetBakedAgent(name); ok {
			t.Errorf("specialist agent %q leaked into coder roster: %q", name, markdown)
		}
	}
}

func TestBakedAgentPromptSeparatesFrontmatterMetadata(t *testing.T) {
	// Validation contract 1: no baked YAML frontmatter, including the
	// machine-specific designer permission list, reaches a request payload.
	prompt, ok := GetBakedAgent("designer")
	if !ok {
		t.Fatal("missing designer")
	}
	if strings.HasPrefix(prompt, "---") || strings.Contains(prompt, "/Users/santoshkumarradha/") {
		t.Fatalf("designer frontmatter leaked into prompt: %.200q", prompt)
	}
	metadata, ok := GetBakedAgentMetadata("designer")
	if !ok || metadata["permission"] == nil || metadata["mode"] != "subagent" {
		t.Fatalf("designer metadata = %#v", metadata)
	}
	if prompt := PromptContent("---\npermission:\n  secret: allow\n"); prompt != "" {
		t.Fatalf("malformed frontmatter failed open: %q", prompt)
	}
}
