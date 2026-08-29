package revision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// A named file that nothing produced is a gate failure naming the file
// verbatim, in the same shape a judged gap takes. The model judge buys nothing
// here, so MissingProduces is the whole verdict and ok reports that it fired.
func TestMissingProducesNamesAbsentFilesVerbatim(t *testing.T) {
	judgment, ok := MissingProduces(plan.Done{Produces: []string{"report.md", "summary.txt"}}, nil)
	if !ok {
		t.Fatal("a missing named file did not fire the mechanical gate")
	}
	if judgment.Pass || !judgment.Checked {
		t.Fatalf("judgment = %+v, want a checked fail", judgment)
	}
	if judgment.Gaps != "report.md, summary.txt" {
		t.Fatalf("gap = %q, want the two names verbatim joined", judgment.Gaps)
	}
	if judgment.Quote != judgment.Gaps {
		t.Fatalf("quote = %q, want it to carry the verbatim names so the repair path grounds them", judgment.Quote)
	}
	// ONE CITATION PER MISSING FILE, which is the whole shape of the fix: the
	// grounding rule asks whether a citation is the person's own words, and a
	// comma list of five paths is the person's own words about nothing.
	if len(judgment.Citations) != 2 ||
		judgment.Citations[0] != "report.md" || judgment.Citations[1] != "summary.txt" {
		t.Fatalf("citations = %q, want one per missing file", judgment.Citations)
	}
	// And the gap is marked as coming from the disk rather than from a judge,
	// because refusing it settles nothing about whether the work landed.
	if !judgment.Mechanical {
		t.Fatal("a gap about files on disk was not marked mechanical")
	}
}

// A judged gap is not mechanical, and the distinction has to survive the shape
// they share: the repair flow runs on both, and only one of them is a fact the
// exit code may not shrug off.
func TestOnlyTheDiskHalfIsMechanical(t *testing.T) {
	judged := Judgment{Gaps: "the benchmark numbers are missing", Quote: "the benchmark numbers",
		Citations: []string{"the benchmark numbers"}, Checked: true}
	if judged.Mechanical {
		t.Fatal("a model judge's gap claimed to be a fact about the filesystem")
	}
	// A judgement written before the list existed is still weighed on its line
	// rather than treated as citing nothing.
	if cited := (Judgment{Quote: "the benchmark numbers"}).Cited(); len(cited) != 1 ||
		cited[0] != "the benchmark numbers" {
		t.Fatalf("cited = %q, want the joined line read as the one citation it was", cited)
	}
	if cited := (Judgment{Quote: "   "}).Cited(); len(cited) != 0 {
		t.Fatalf("cited = %q, want a blank line to cite nothing", cited)
	}
}

// An abstract output — "the write-up", "the comparison table" — names no file,
// so the mechanical check is a no-op and the judge is reached as it always was.
func TestMissingProducesIgnoresAbstractOutputs(t *testing.T) {
	if _, ok := MissingProduces(plan.Done{Produces: []string{"the write-up", "the comparison table"}}, nil); ok {
		t.Fatal("an abstract output was treated as a file the gate may settle")
	}
}

// A criterion that names nothing at all is the legal, everywhere-default case,
// and it reaches the judge unchanged.
func TestMissingProducesNoProducesReachesTheJudge(t *testing.T) {
	if _, ok := MissingProduces(plan.Done{}, nil); ok {
		t.Fatal("an empty criterion fired the mechanical gate")
	}
}

// A present, non-empty named file passes the mechanical check and reaches the
// judge — the path is byte-identical to before the check existed.
func TestMissingProducesPassesWhenAllFilesPresent(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "report.md")
	if err := os.WriteFile(present, []byte("the weekly report"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := MissingProduces(plan.Done{Produces: []string{"report.md"}}, []string{present}); ok {
		t.Fatal("a present, non-empty named file was treated as missing")
	}
}

// A file left at zero bytes was not written; the gate counts it as missing so
// it cannot pass a deliverable that exists only as a name on disk.
func TestMissingProducesFailsOnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "report.md")
	if err := os.WriteFile(empty, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	judgment, ok := MissingProduces(plan.Done{Produces: []string{"report.md"}}, []string{empty})
	if !ok {
		t.Fatal("an empty named file did not fire the mechanical gate")
	}
	if !strings.Contains(judgment.Gaps, "report.md") {
		t.Fatalf("gap = %q, want it to name the empty file", judgment.Gaps)
	}
}

// Only the absent files are named; a sibling the worker did produce is not
// dragged into the gap, so the critique the repair round reads names what to
// make and nothing else.
func TestMissingProducesNamesOnlyTheMissing(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "summary.txt")
	if err := os.WriteFile(present, []byte("the summary"), 0o600); err != nil {
		t.Fatal(err)
	}
	judgment, ok := MissingProduces(plan.Done{Produces: []string{"report.md", "summary.txt"}}, []string{present})
	if !ok {
		t.Fatal("a missing file among present ones did not fire")
	}
	if strings.Contains(judgment.Gaps, "summary.txt") {
		t.Fatalf("gap named a present file: %q", judgment.Gaps)
	}
	if !strings.Contains(judgment.Gaps, "report.md") {
		t.Fatalf("gap omitted the missing file: %q", judgment.Gaps)
	}
}

// A produces entry that mentions a file inside a sentence is prose, not a
// name; the gate must not infer a deliverable name from prose.
func TestMissingProducesDoesNotInferFilesFromProse(t *testing.T) {
	if _, ok := MissingProduces(plan.Done{Produces: []string{"see report.md for the summary"}}, nil); ok {
		t.Fatal("a file name was inferred from prose rather than read off the structured field")
	}
}
