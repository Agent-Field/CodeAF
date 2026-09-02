package head

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// The instruction #427 was measured on, verbatim.
const constraintErrand = "Run the command 'go test ./internal/subharness/ -count=1' in this " +
	"workspace and report the final line it prints. Change no files."

// THE GATE MAY ONLY HOLD PEOPLE TO THEIR OWN WORDS. A constraint is the one
// field on this brief that ends in a mechanical refusal, so a rule the compiler
// invented is not a smaller mistake than a rule it missed: it is a delivery
// failed by arithmetic over a sentence nobody wrote.
func TestOnlyAConstraintThePersonWroteSurvivesTheCompile(t *testing.T) {
	brief := Brief{Constraints: []plan.Constraint{
		{Text: "Change no files.", Kind: plan.ConstraintNoWrites},
		// The shape a careful model reaches for and may never be given: a rule
		// that is good engineering and is nobody's instruction.
		{Text: "Do not commit anything to git.", Kind: plan.ConstraintOther},
		// And the same rule retyped with its whitespace tidied, which is the
		// person's own words and must survive.
		{Text: "report  the final line\nit prints", Kind: plan.ConstraintOther},
	}}
	keepStatedConstraints(&brief, constraintErrand)

	if len(brief.Constraints) != 2 {
		t.Fatalf("kept %d rules: %+v", len(brief.Constraints), brief.Constraints)
	}
	if brief.Constraints[0].Text != "Change no files." {
		t.Fatalf("the person's own rule did not survive: %+v", brief.Constraints[0])
	}
	for _, kept := range brief.Constraints {
		if strings.Contains(kept.Text, "commit") {
			t.Fatalf("a rule nobody stated was kept: %+v", kept)
		}
	}
	// A brief the model wrote no rules for is the ordinary brief, and it must
	// come out of here exactly as it went in.
	ordinary := Brief{Assumptions: []string{"the default branch is the one to read"}}
	keepStatedConstraints(&ordinary, constraintErrand)
	if ordinary.Constraints != nil || len(ordinary.Assumptions) != 1 {
		t.Fatalf("a brief with no rules was changed: %+v", ordinary)
	}
}

// A CONSTRAINT MUST NEVER LIVE ONLY IN ASSUMPTIONS. `aforge do` discards them —
// the one-shot surface's law is that the caller's sentence is the specification
// and the compiler's own decisions are not (resident.keepTheAskVerbatim) — so a
// rule duplicated into that field is a rule that surface silently drops.
func TestAKeptConstraintIsNotAlsoLeftInAssumptions(t *testing.T) {
	brief := Brief{
		Constraints: []plan.Constraint{{Text: "Change no files.", Kind: plan.ConstraintNoWrites}},
		Assumptions: []string{
			"Change no files.",
			"the workspace is the checkout in hand",
			"CHANGE NO FILES.",
		},
	}
	keepStatedConstraints(&brief, constraintErrand)

	if len(brief.Constraints) != 1 {
		t.Fatalf("the stated rule was lost: %+v", brief.Constraints)
	}
	if len(brief.Assumptions) != 1 || brief.Assumptions[0] != "the workspace is the checkout in hand" {
		t.Fatalf("the rule is still echoed as an assumption: %+v", brief.Assumptions)
	}
}

// A PROMPT CHANGE WITH NO MECHANICAL READER IS DECORATION. The field the model
// is asked for has to be the field the brief decodes and the guard above holds,
// so the shape line is pinned here beside them.
func TestTheCompilerAsksForTheRulesItWillBeHeldTo(t *testing.T) {
	for _, want := range []string{
		`"constraints":[{"text":"...","kind":"no_writes|paths_only|other","paths":["..."]}]`,
		"may or may not DO",
		"Never invent one",
	} {
		if !strings.Contains(compilerSystemPrompt, want) {
			t.Fatalf("the compiler prompt does not ask for %q", want)
		}
	}
	for _, kind := range []string{plan.ConstraintNoWrites, plan.ConstraintPathsOnly, plan.ConstraintOther} {
		if !strings.Contains(compilerSystemPrompt, kind) {
			t.Fatalf("the prompt names no %q reading, so nothing can return one", kind)
		}
	}
}
