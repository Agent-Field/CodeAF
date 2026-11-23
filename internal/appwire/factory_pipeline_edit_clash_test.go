package appwire

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// A STEP THAT CLASHES IS NAMED ON THE WIRE. The library walk saw only "That
// value is out of bounds" for an add_step whose name the file already defined
// with other words; the person could not tell what to change.
func TestAClashingStepRefusalNamesTheStep(t *testing.T) {
	base := "pipeline:\n    quick:\n        - write\n        - lint-it\n        - proof\n    careful:\n        - write\n        - proof\nsteps:\n    lint-it:\n        prompt: run the linter\n"
	_, err := pipeline.LibraryEdited(base, []pipeline.EditOp{{Op: pipeline.OpAddStep, Pipeline: "careful", Name: "lint-it", After: ptr("write"), Prompt: ptr("run the linter twice")}})
	bad, ok := err.(*pipeline.EditError)
	if !ok {
		t.Fatalf("not refused as an edit: %v", err)
	}
	got := PipelineEditRefusal("f", "", bad).Message
	want := "There is already a step named lint-it with other words. Pick another name, or add it with the words it has. Nothing was written."
	if got != want {
		t.Fatalf("refusal says %q, want %q", got, want)
	}
	// Any other out-of-bounds value keeps the fixed words, with nothing echoed.
	other := PipelineEditRefusal("f", "", &pipeline.EditError{Code: pipeline.EditBadValue, Op: 0, Why: "anything"}).Message
	if !strings.HasPrefix(other, "That value is out of bounds.") {
		t.Fatalf("%q", other)
	}
}

func ptr(s string) *string { return &s }
