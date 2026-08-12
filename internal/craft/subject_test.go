package craft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func spacexResearch() *Workflow {
	return &Workflow{
		Name:        "spacex-investment-research",
		Description: "A three-step deep dive on SpaceX as an investment, ending in a written report.",
		Params:      []Param{{Name: "angle", Default: "valuation"}},
		Steps: []Step{
			{ID: "gather", Brief: "Research the latest news, filings and coverage and settle what matters."},
			{ID: "analyse", Brief: "Work out what the findings mean for the {{angle}}.", Needs: []string{"gather"}},
			{ID: "write", Brief: "Write the deep-dive report, with the evidence under each finding.", Needs: []string{"analyse"}},
		},
	}
}

// The reported defect, at the level it happened. A request to deep-dive a
// city's AI events matched a workflow distilled from investment research on one
// company, because the two are the same SHAPE of work: three steps, research,
// a report at the end. Nothing in the request is about what that workflow is
// about, and that is what the gate reads.
func TestAWorkflowBoundToASubjectIsNotReachedForByAnotherSubject(t *testing.T) {
	workflow := spacexResearch()
	if OnSubject(workflow, "do a deep dive on the AI events happening in Toronto this month and write it up") {
		t.Fatal("the spacex workflow answered a request about Toronto AI events")
	}
	// The requests it IS for still reach it, and one of its subject words is
	// enough — nobody says their whole file name back to themselves.
	for _, request := range []string{
		"deep dive on SpaceX as an investment",
		"how is the spacex research looking — do the usual write-up",
		"another investment deep dive please, same as before",
	} {
		if !OnSubject(workflow, request) {
			t.Fatalf("its own request was refused: %q", request)
		}
	}
}

// Subject is the craft's claim about itself minus the words that describe a
// shape of work: "research" is what every deep dive does, "spacex" is what this
// one is about, and a param is a hole rather than a subject.
func TestSubjectIsTheClaimWithoutTheShapeWords(t *testing.T) {
	subject := strings.Join(Subject(spacexResearch()), " ")
	if !strings.Contains(subject, "spacex") || !strings.Contains(subject, "investment") {
		t.Fatalf("subject = %q, want the words it is about", subject)
	}
	if strings.Contains(subject, "research") || strings.Contains(subject, "report") ||
		strings.Contains(subject, "deep") {
		t.Fatalf("subject = %q, want no shape words", subject)
	}
	// A generic craft binds nothing: it is written to be pointed anywhere, and
	// the hole is where the subject arrives from.
	deck := &Workflow{
		Name: "presentation", Description: "a deck about {{topic}}",
		Params: []Param{{Name: "topic", Required: true}},
	}
	if bound := Subject(deck); len(bound) != 0 {
		t.Fatalf("a topic-shaped workflow bound itself to %v", bound)
	}
}

// An unbound craft keeps working, which is the other half of the bargain: the
// person asks for it in their own words and it answers, and only a request that
// says nothing it claims falls through to the planner.
func TestAnUnboundWorkflowStillAnswersItsOwnWords(t *testing.T) {
	deck := &Workflow{
		Name: "presentation", Description: "a deck about {{topic}}",
		Params: []Param{{Name: "topic", Required: true}},
		Steps:  []Step{{ID: "research", Brief: "Research {{topic}}."}},
	}
	for _, request := range []string{
		"make me a presentation about the Q3 numbers",
		"a deck on the Q3 numbers please",
	} {
		if !OnSubject(deck, request) {
			t.Fatalf("an unbound workflow refused %q", request)
		}
	}
	// Its step briefs are not its claim. "Research the pricing page" is the
	// vocabulary of every workflow's middle, and matching only there is exactly
	// the miss this gate exists for.
	if OnSubject(deck, "research the pricing page and tell me what changed") {
		t.Fatal("a deck workflow answered a request that only brushed one step")
	}
	notes := &Workflow{Name: "release-notes", Description: "write the notes for a release"}
	if !OnSubject(notes, "cut the release notes for v2") {
		t.Fatal("a generic repo workflow stopped answering its own request")
	}
}

// A craft whose name says nothing can still be bound by a name in its
// description, which is where a distiller usually puts the subject it learned
// on. A description written in Title Case says nothing about names, because a
// signal that fires on every word carries none.
func TestAProperNameInTheDescriptionBinds(t *testing.T) {
	generic := &Workflow{
		Name:        "deep-dive",
		Description: "An investment deep dive on SpaceX, three steps ending in a report.",
	}
	if !OnSubject(generic, "deep dive on spacex for me") {
		t.Fatal("the workflow refused the subject its description names")
	}
	if OnSubject(generic, "deep dive on the Toronto AI events this month") {
		t.Fatal("a description-bound workflow answered another subject")
	}
	titled := &Workflow{Name: "deep-dive", Description: "An Investment Deep Dive Ending In A Report"}
	if names := properNouns(titled.Description); len(names) != 0 {
		t.Fatalf("title case was read as %v proper names", names)
	}
}

// The gate is needed because the SCORER cannot answer this. Measured against
// the real matcher on a real repository, the Toronto request clears the
// retrieval floor on the spacex workflow — which is the whole reason
// recognition asks a second question.
func TestTheScorerAloneStillMatchesTheWrongSubject(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, WorkflowDir), 0o755); err != nil {
		t.Fatal(err)
	}
	file := `name: spacex-investment-research
description: A three-step deep dive on SpaceX as an investment, ending in a written report.
steps:
  - id: gather
    brief: Research the latest news, filings and coverage and settle what matters.
  - id: analyse
    brief: Work out what the findings mean, deep diving into each one.
    needs: [gather]
  - id: write
    brief: Write the deep-dive report, with the evidence under each finding.
    needs: [analyse]
`
	if err := os.WriteFile(filepath.Join(dir, WorkflowDir, "spacex-investment-research.yaml"),
		[]byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := &Repo{dir: dir}
	matches := repo.Match("do a deep dive on the AI events happening in Toronto this month and write it up", 1)
	if len(matches) == 0 || matches[0].Score < MatchFloor {
		t.Skip("the scorer no longer matches this shape at all; the gate is belt and braces")
	}
	t.Logf("the scorer ranks the wrong-subject request at %.2f", matches[0].Score)
	workflow, err := Parse([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	if OnSubject(workflow, "do a deep dive on the AI events happening in Toronto this month and write it up") {
		t.Fatalf("a %.2f-scoring shape match passed the subject gate", matches[0].Score)
	}
}
