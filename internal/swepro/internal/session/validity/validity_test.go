package validity

import (
	"strings"
	"testing"
)

func TestOneLineUsesJavaScriptWhitespace(t *testing.T) {
	verdict := ValidityVerdict{
		Status:                 StatusInvalid,
		Confidence:             ConfidenceHigh,
		Evidence:               "\u2028 first\u3000second \ufeff",
		RecommendedDeliverable: "none",
	}
	got := ApplyValidityPolicy(verdict, PhaseIntake)
	if !strings.HasSuffix(got.Note, "first second") {
		t.Fatalf("JavaScript whitespace was not collapsed: %q", got.Note)
	}
}

// TestStalenessPromptWithoutCopiesIsUnchanged: the copy-provenance section is
// additive. With nothing copied the prompt must stay byte-identical to the TS
// original, which the fixture suite pins for the no-copy path.
func TestStalenessPromptWithoutCopiesIsUnchanged(t *testing.T) {
	input := StalenessPromptInput{
		TaskText: "fix the thing", ContractCommand: "./t.sh", ContractOutput: "PASS",
	}
	if strings.Contains(BuildStalenessPrompt(input), "copied into the base checkout") {
		t.Fatal("no-copy prompt must not carry the provenance section")
	}
}

// TestStalenessPromptReportsCopyProvenance: the judge is told which files the
// harness copied into the base checkout and whether each existed at base. A
// file that did not exist at base is what lets it recognize "passed because the
// deliverable was copied in" rather than ruling a live issue stale.
func TestStalenessPromptReportsCopyProvenance(t *testing.T) {
	prompt := BuildStalenessPrompt(StalenessPromptInput{
		TaskText:        "create hello.txt containing hello engine",
		ContractCommand: "./test-hello.sh", ContractOutput: "PASS",
		CopiedFiles: []CopiedFile{
			{Path: "test-hello.sh", ExistedAtBase: false},
			{Path: "conftest.py", ExistedAtBase: true},
		},
	})
	for _, want := range []string{
		"## Files the harness copied into the base checkout",
		"`test-hello.sh` — DID NOT EXIST at base",
		"`conftest.py` — already existed at base",
		"handed its own answer",
		"belongs in `asserted_paths` rather than `paths`",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

// TestStalenessPromptOmitsContaminationWarningWhenAllCopiesExistedAtBase: the
// warning is reserved for the case that can actually mislead the judge.
func TestStalenessPromptOmitsContaminationWarningWhenAllCopiesExistedAtBase(t *testing.T) {
	prompt := BuildStalenessPrompt(StalenessPromptInput{
		TaskText: "fix", ContractCommand: "./t.sh", ContractOutput: "PASS",
		CopiedFiles: []CopiedFile{{Path: "t.sh", ExistedAtBase: true}},
	})
	if !strings.Contains(prompt, "`t.sh` — already existed at base") {
		t.Fatal("prompt must still list the copy")
	}
	if strings.Contains(prompt, "handed its own answer") {
		t.Fatal("no contamination warning when every copy existed at base")
	}
}
