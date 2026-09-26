//go:build !windows

package evidenceharvest

import (
	"strings"
	"testing"
)

func TestBackslashPathsAreNotHarvested(t *testing.T) {
	got := HarvestEvidence([]string{
		`opened C:\repo\src\a.ts`,
		`changed C:\repo\src\a.ts`,
	})
	if got != nil {
		t.Fatalf("Windows-only path unexpectedly harvested: %q", *got)
	}
}

func TestFileSectionIsKeptEvenOverBudget(t *testing.T) {
	got := HarvestEvidence([]string{
		"src/really-long-name.ts",
		"src/really-long-name.ts",
	}, 1)
	if got == nil || len(*got) <= 1 {
		t.Fatalf("expected over-budget structural file block, got %v", got)
	}
}

func TestSelectEvidencePromptEndsWithTheCorpus(t *testing.T) {
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

func TestHarvestEvidenceKeepsCurrentSuiteAbortAheadOfOldProbeNoise(t *testing.T) {
	got := HarvestEvidence([]string{
		"Error: obsolete scratch probe failed",
		"error[E0063]: missing fields in Config\nerror: could not compile `mycrate`",
	})
	if got == nil || !strings.Contains(*got, "error: could not compile `mycrate`") {
		t.Fatalf("current compiler failure was not preserved: %v", got)
	}
	if strings.Index(*got, "could not compile") > strings.Index(*got, "obsolete scratch") {
		t.Fatalf("old equal-strength noise outranked the current failure:\n%s", *got)
	}
}

func TestHarvestEvidenceKeepsLineOrderInsideCurrentFailure(t *testing.T) {
	got := HarvestEvidence([]string{
		"Error: stale probe",
		"Exception during run: loader abort\nTransform failed with 1 error",
	})
	if got == nil || strings.Index(*got, "Exception during run") >
		strings.Index(*got, "Transform failed with 1 error") {
		t.Fatalf("current failure lines were reversed:\n%v", got)
	}
}

func TestSemanticEvidenceJudgeSeesTheRecentEndOfALargeTranscript(t *testing.T) {
	const latest = "LATEST failure: error: could not compile mycrate"
	var prompt string
	SelectEvidence([]string{
		"STALE-BEGIN " + strings.Repeat("x", maxCorpusChars), latest,
	}, struct{}{}, &SelectEvidenceOptions{Judge: EvidenceJudgeFunc(
		func(got string, _ any) EvidenceJudgment {
			prompt = got
			return EvidenceJudgment{Source: SourceLLM}
		},
	)})
	if !strings.Contains(prompt, latest) || strings.Contains(prompt, "STALE-BEGIN") {
		t.Fatalf("judge did not receive the recent transcript tail")
	}
}
