package revision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
)

// The name a person writes is the address they mean. A name carrying a
// directory is satisfied at that place and nowhere else — the case a leaf lost
// when it wrote a correct memo inside a directory wearing its output hint's
// name — and a bare name is satisfied wherever the file landed, because someone
// who wrote "report.md" said nothing about which folder.
func TestAProducedFileIsMatchedAtTheAddressTheRequestUsed(t *testing.T) {
	root := t.TempDir()
	write := func(relative string) string {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("body"), 0o600); err != nil {
			t.Fatal(err)
		}
		return full
	}
	memo := write("docs/decision-memo.md")
	elsewhere := write("scratch/07-write-docs-decision-memo-md.md/decision-memo.md")
	directory := filepath.Dir(elsewhere)

	for name, want := range map[string]bool{
		"docs/decision-memo.md":   true,
		"./docs/decision-memo.md": true,
		"decision-memo.md":        true,
		"docs/other.md":           false,
	} {
		if _, ok := ProducedFile(name, []string{memo}); ok != want {
			t.Errorf("ProducedFile(%q) = %v, want %v", name, ok, want)
		}
	}
	// The incident itself: the content landed at the wrong address, so the
	// address the person named was not produced.
	if at, ok := ProducedFile("docs/decision-memo.md", []string{elsewhere}); ok {
		t.Errorf("a file at the wrong address counted as produced, at %s", at)
	}
	// And a directory wearing the name is not a written file.
	if _, ok := ProducedFile(filepath.Base(directory), []string{directory}); ok {
		t.Error("a directory counted as a produced file")
	}
	// Nor is a path nothing is at.
	if _, ok := ProducedFile("gone.md", []string{filepath.Join(root, "gone.md")}); ok {
		t.Error("a recorded path with nothing at it counted as produced")
	}
}

// NamedFiles reads filenames out of prose and nothing else out of it, because
// the whole point is that a gap quoting "e.g." or "1.5x" must not be mistaken
// for one quoting a deliverable.
func TestNamedFilesReadsFilenamesAndNotProse(t *testing.T) {
	got := NamedFiles("write ./docs/decision-memo.md and report.md — e.g. within 1.5x of the vs. baseline, docs/decision-memo.md again")
	want := []string{"docs/decision-memo.md", "report.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("NamedFiles = %v, want %v", got, want)
	}
	if names := NamedFiles("summarise the findings and state a recommendation"); len(names) != 0 {
		t.Fatalf("prose named files: %v", names)
	}
}

// The refusal is narrow on purpose: it closes a gap the run has already
// answered on disk and refuses to touch anything else, because a gap about
// what is INSIDE a file quotes the substance rather than the name.
func TestAGapTheRunAlreadyClosedBuysNothing(t *testing.T) {
	root := t.TempDir()
	produced := filepath.Join(root, "stochastic_fit_plots.png")
	if err := os.WriteFile(produced, []byte("a rendered plot"), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence := Evidence{Artifacts: []string{produced}, Observed: true}

	if refusal := AdmitGapArtifact([]string{"save the plots to stochastic_fit_plots.png"}, evidence); refusal == "" {
		t.Fatal("a gap quoting a produced file still bought work")
	}
	if refusal := AdmitGapArtifact([]string{"the fit must stay inside the 5-95% band"}, evidence); refusal != "" {
		t.Fatalf("a gap about substance was refused: %q", refusal)
	}
	if refusal := AdmitGapArtifact([]string{"write summary.md"}, evidence); refusal != "" {
		t.Fatalf("a gap naming a file nothing produced was refused: %q", refusal)
	}
	// Every name it quotes has to be there. One missing file is a real gap
	// however many of its siblings landed.
	if refusal := AdmitGapArtifact([]string{"stochastic_fit_plots.png and summary.md"}, evidence); refusal != "" {
		t.Fatalf("a partially produced quote was refused: %q", refusal)
	}
}

// The record the gate reads settles each named file itself, and states the
// criterion the plan set before the work started. Both directions are in the
// same block so the judge cannot read one without the other.
func TestTheEvidenceBlockSettlesTheNamedFilesAndCarriesTheCriterion(t *testing.T) {
	root := t.TempDir()
	produced := filepath.Join(root, "report.md")
	if err := os.WriteFile(produced, []byte("twelve"), 0o600); err != nil {
		t.Fatal(err)
	}
	block := Evidence{
		Artifacts: []string{produced},
		Named:     []string{"report.md", "docs/appendix.md"},
		Observed:  true,
	}.block(ctxbudget.Budget{})
	for _, want := range []string{
		"report.md — produced, at " + produced,
		"docs/appendix.md — nothing of that name is among what was left behind",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("the record omits %q:\n%s", want, block)
		}
	}
	// Named files with nothing produced at all is still a record, not silence:
	// an ask that named a file and a run that left nothing is the loudest
	// reading there is, and it used to render as the empty string.
	bare := Evidence{Named: []string{"report.md"}, Observed: true}.block(ctxbudget.Budget{})
	if !strings.Contains(bare, "report.md — nothing of that name") {
		t.Fatalf("an unproduced named file rendered as nothing:\n%s", bare)
	}
	// And an evidence with nothing in it at all still renders nothing, so a
	// caller holding no outcome cannot manufacture a record by omission.
	if got := (Evidence{}).block(ctxbudget.Budget{}); got != "" {
		t.Fatalf("an empty record rendered %q", got)
	}
}

// The gate could not previously tell a check this work broke from a check that
// was broken when the work arrived, and it convicted correct patches for the
// second (audit-notes §14.4.1). The block states the difference as fact.
func TestTheEvidenceBlockCarriesWhatWasAlreadyBroken(t *testing.T) {
	note := "`make all` exited 2, and every failing test it reports " +
		"(TestFailGenFishCompletionFile) was ALREADY failing at this commit " +
		"before the run touched the workspace."
	block := Evidence{Baseline: []string{note}, Observed: true}.block(ctxbudget.Budget{})
	if !strings.Contains(block, note) {
		t.Fatalf("the record dropped the baseline delta:\n%s", block)
	}
	if !strings.Contains(block, "not this work's doing and are not gaps") {
		t.Fatalf("the record does not tell the judge how to read it:\n%s", block)
	}
	// It is a record like the others: present, it is never the unexercised
	// sentence, and absent it manufactures nothing.
	if strings.Contains(block, UnexercisedRecord) {
		t.Fatalf("a baseline-only record read as an unexercised run:\n%s", block)
	}
	if got := (Evidence{Observed: true}).block(ctxbudget.Budget{}); got != UnexercisedRecord {
		t.Fatalf("an empty observed record = %q", got)
	}
}
