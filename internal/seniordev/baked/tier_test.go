//go:build !windows

package baked

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestTierForMapsEachAgentToItsPool(t *testing.T) {
	for _, test := range []struct {
		agent string
		want  Tier
	}{
		{"coder", TierHigh},
		{"compaction", TierLow},
		{"", TierHigh},
		{"some-agent-that-does-not-exist", TierHigh},
	} {
		if got := TierFor(test.agent); got != test.want {
			t.Errorf("TierFor(%q) = %q, want %q", test.agent, got, test.want)
		}
	}
}

func TestShippedCoderDocumentLeavesTheTierToTheTable(t *testing.T) {
	// The override exists for an operator; the shipped document must not use
	// it, or the table stops describing what the binary does.
	metadata, ok := GetBakedAgentMetadata("coder")
	if !ok {
		t.Fatal("the coder document is missing")
	}
	if value, present := metadata["tier"]; present {
		t.Fatalf("coder.md sets tier: %v", value)
	}
}

func TestFrontmatterTierOverridesTheTable(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  Tier
	}{
		{"frontier", "tier: frontier\n", TierFrontier},
		{"low", "tier: low\n", TierLow},
		{"case and space are forgiven", "tier: \"  Frontier \"\n", TierFrontier},
		{"an unknown value keeps the table's answer", "tier: platinum\n", TierHigh},
		{"a non-string keeps the table's answer", "tier: 3\n", TierHigh},
		{"no key at all keeps the table's answer", "", TierHigh},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Parsed the same way the embedded documents are, so the test
			// covers the frontmatter path and not just the lookup.
			_, frontmatter, err := parseAgentMarkdown(
				"---\nmodel: inherit\n" + test.value + "---\n\nbody\n",
			)
			if err != nil {
				t.Fatal(err)
			}
			metadata := map[string]any{}
			if err := yaml.Unmarshal([]byte(frontmatter), &metadata); err != nil {
				t.Fatal(err)
			}
			if got := tierFrom(metadata, "coder"); got != test.want {
				t.Fatalf("tier = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFrontmatterTierOverridesTheCompactionDefaultToo(t *testing.T) {
	if got := tierFrom(map[string]any{"tier": "high"}, "compaction"); got != TierHigh {
		t.Fatalf("tier = %q, want %q", got, TierHigh)
	}
	if got := tierFrom(nil, "compaction"); got != TierLow {
		t.Fatalf("tier = %q, want %q", got, TierLow)
	}
}
