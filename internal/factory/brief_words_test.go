package factory

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// briefBannedForTest is the law's list, spelled here apart from the filter so
// a word dropped from the filter's table still fails this test.
var briefBannedForTest = regexp.MustCompile(`(?i)\b(auditors?|verdicts?|verified|unverified|verifications?|refuted)\b`)

// The words are replaced deterministically and keep the case they came in.
func TestBriefPlainWords(t *testing.T) {
	for in, want := range map[string]string{
		"What the verification found":    "What the checks found",
		"WHAT THE VERIFICATION FOUND":    "WHAT THE CHECKS FOUND",
		"Verification":                   "Check",
		"Verified by tests":              "Checked by tests",
		"Two verifications failed":       "Two checks failed",
		"The auditor's verdict":          "The reviewer's outcome",
		"Auditors refuted the claim":     "Reviewers disproved the claim",
		"left unverified":                "left unchecked",
		"Verify the retry path":          "Verify the retry path",
		"reverified and verificationist": "reverified and verificationist",
	} {
		if got := BriefPlainWords(in); got != want {
			t.Errorf("BriefPlainWords(%q) = %q, want %q", in, got, want)
		}
	}
}

// THE SEVENTH LIVE RUN'S HEADING NEVER REACHES A PERSON. A draft whose every
// heading and text is machinery vocabulary is kept in plain words, its
// verdict too, while a proof's command and a question's step stay exactly as
// written because a person or the engine reads them literally.
func TestCheckBriefDraftHeadingsArePlain(t *testing.T) {
	it := writerJob()
	d := BriefDraft{
		Verdict:         "Verified. 42 tests pass.",
		VerdictEvidence: evs("test", 2, "tests"),
		Blocks: []BriefBlock{
			{Kind: BlockBullets, Heading: "What the verification found", Bullets: []BriefClaim{
				{Text: "**Verified** the auditor's 2 findings", Evidence: evs("review", 1, "findings")},
			}},
			{Kind: BlockOpenQuestion, Heading: "Unverified", Questions: []BriefQuestion{{Text: "Was the verdict refuted?", Evidence: evs("proof", 1, "claims"), Action: &BriefAction{Label: "Ask for a verification", Step: "proof"}}}},
			{Kind: BlockYourCall, Heading: "Your verdict", Evidence: evs("proof", 1, "claims"), Options: []string{"Ship verified", "Hold"}},
		},
	}
	v, _, fault := CheckBriefDraft(it, draft(t, d))
	if fault != "" {
		t.Fatalf("fault %q", fault)
	}
	var headings []string
	for _, b := range v.Blocks {
		headings = append(headings, b.Heading)
	}
	if strings.Join(headings, " | ") != "What the checks found | Unchecked | Your outcome" {
		t.Fatalf("headings = %q", headings)
	}
	shown, _ := json.Marshal(struct {
		Verdict string
		Blocks  []BriefBlock
	}{v.Verdict, v.Blocks})
	// The key the wire spells is not a word a person reads; everything else is.
	text := strings.ReplaceAll(string(shown), `"Verdict"`, "")
	if m := briefBannedForTest.FindString(text); m != "" {
		t.Fatalf("a banned word %q reached the brief: %s", m, text)
	}
	if v.Blocks[1].Questions[0].Action.Step != "proof" {
		t.Fatalf("a step name was rewritten: %+v", v.Blocks[1].Questions[0].Action)
	}
}

// Both of the writer's prompts tell it the rule.
func TestBriefPromptsCarryThePlainWordsRule(t *testing.T) {
	for name, p := range map[string]string{"after work": BriefSystem(), "before work": BriefFirstReadSystem()} {
		if !strings.Contains(p, BriefPlainWordsRule) || strings.Contains(p, "%!") {
			t.Errorf("the %s prompt does not carry the rule:\n%s", name, p)
		}
	}
}
