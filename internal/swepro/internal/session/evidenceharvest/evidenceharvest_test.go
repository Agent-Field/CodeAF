package evidenceharvest

import (
	"strings"
	"testing"
)

func TestKeptWindowsPathBlindSpot(t *testing.T) {
	got := HarvestEvidence([]string{
		`opened C:\repo\src\a.ts`,
		`changed C:\repo\src\a.ts`,
	})
	if got != nil {
		t.Fatalf("Windows-only path unexpectedly harvested: %q", *got)
	}
}

func TestKeptFileSectionCanExceedBudget(t *testing.T) {
	got := HarvestEvidence([]string{
		"src/really-long-name.ts",
		"src/really-long-name.ts",
	}, 1)
	if got == nil || len(*got) <= 1 {
		t.Fatalf("expected over-budget structural file block, got %v", got)
	}
}

func TestSelectEvidenceBuildsByteExactPrompt(t *testing.T) {
	var prompt string
	opts := &SelectEvidenceOptions{
		Judge: EvidenceJudgeFunc(func(got string, _ any) EvidenceJudgment {
			prompt = got
			return EvidenceJudgment{Source: SourceLLM}
		}),
	}
	result := SelectEvidence([]string{"alpha", "beta"}, struct{}{}, opts)
	if result.Source != SourceLLM || result.Text != nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !strings.HasSuffix(prompt, "--- TRANSCRIPT REGION ---\nalpha\n---\nbeta") {
		t.Fatalf("prompt corpus mismatch:\n%s", prompt)
	}
}
