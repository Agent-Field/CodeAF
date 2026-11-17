package pipeline

import (
	"strings"
	"testing"
)

// A REMOVED LIBRARY PIPELINE TAKES ITS OWN STEPS WITH IT, so the same name can
// be added again later with other words. The live desk walk removed quick-fix
// and then could not add run-vet-and with new words: the definition stayed
// behind, and the add was refused as a clash with a step nobody could see.
func TestRemovePipelineDropsTheStepsOnlyItUsed(t *testing.T) {
	ed, err := LibraryEdited("", []EditOp{
		addPipeline("quick-fix", "write", "proof"),
		add("quick-fix", "run-vet-and", "write", "run go vet and the tests"),
	})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := LibraryEdited(ed.Text, []EditOp{removePipeline("quick-fix")})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gone.Text, "run-vet-and") {
		t.Fatalf("the removed pipeline's own step stayed behind:\n%s", gone.Text)
	}
	again, err := LibraryEdited(gone.Text, []EditOp{
		addPipeline("quick-fix", "write", "proof"),
		add("quick-fix", "run-vet-and", "write", "run only the vet"),
	})
	if err != nil {
		t.Fatalf("a later add of the same name was refused: %v", err)
	}
	if lib := mustLibrary(t, again.Text); lib.Steps["run-vet-and"].Prompt != "run only the vet" {
		t.Fatalf("%+v", lib.Steps["run-vet-and"])
	}
}

// A STEP ANOTHER PIPELINE LISTS STAYS, and so does a shipped step's own words:
// only what the removed pipeline alone used goes.
func TestRemovePipelineKeepsStepsAnotherPipelineOrTheFileUses(t *testing.T) {
	ed, err := LibraryEdited("", []EditOp{
		addPipeline("quick-fix", "write", "proof"),
		add("quick-fix", "shared", "write", "check the shared thing"),
		add("quick-fix", "only-here", "shared", "check only here"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ed, err = LibraryEdited(ed.Text, []EditOp{addPipeline("careful", "write", "shared", "proof")})
	if err != nil {
		t.Fatal(err)
	}
	ed, err = LibraryEdited(ed.Text, []EditOp{{Op: OpSetStep, Name: "write", Prompt: s("write it carefully")}})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := LibraryEdited(ed.Text, []EditOp{removePipeline("quick-fix")})
	if err != nil {
		t.Fatal(err)
	}
	lib := mustLibrary(t, gone.Text)
	if lib.Steps["shared"].Prompt != "check the shared thing" {
		t.Fatalf("a step careful lists lost its definition:\n%s", gone.Text)
	}
	if lib.Steps["write"].Prompt != "write it carefully" {
		t.Fatalf("a shipped step's own words were dropped:\n%s", gone.Text)
	}
	if strings.Contains(gone.Text, "only-here") {
		t.Fatalf("a step only quick-fix used stayed behind:\n%s", gone.Text)
	}

	// In a repository file the fixed list counts as a use too.
	base := strings.Replace(editBase, "fixed: [approve, proof]\n", "fixed: [approve, proof, lonely]\n", 1)
	base = strings.Replace(base, "  ci: [bisect, fix, test, proof, approve]\n", "  ci: [bisect, fix, test, proof, approve]\n  quick: [write, lonely, proof]\n", 1)
	base = strings.Replace(base, "steps:\n", "steps:\n  lonely:\n    prompt: stay put\n", 1)
	back := reread(t, mustApply(t, base, removePipeline("quick")).Text)
	if back.Steps["lonely"].Prompt != "stay put" {
		t.Fatalf("a step the fixed list names lost its definition: %+v", back.Steps["lonely"])
	}
}
