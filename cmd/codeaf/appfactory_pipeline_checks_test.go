package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func checkAdd(name, after, command string) appwire.FactoryPipelineEdit {
	return appwire.FactoryPipelineEdit{Op: appwire.PipelineEditAddStep, Pipeline: "issue", Name: name, After: str(after), Kind: str("check"), Prompt: str(command), Until: str("green")}
}

// A CHECK IS ADDED ONLY FOR A FILE ITS FACTORY ATTESTED: `kind` is a runtime
// key, so an unattested base is refused before anything is read, and an
// attested one previews the check with its command.
func TestAPipelineCheckIsPreviewedOnlyForAnAttestedFile(t *testing.T) {
	zero := 0
	w := newGuardWire(t)
	if !strings.Contains(w.hello, appwire.FeatureFactoryPipelineEditChecks) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryPipelineEditChecks, w.hello)
	}
	if firstRuntimeEdit([]appwire.FactoryPipelineEdit{checkAdd("unit", "plan", "go test ./...")}) != 0 {
		t.Fatal("adding a check is a runtime edit")
	}
	_, err := w.previewAt(prBaseSHA, editTestFile, checkAdd("unit", "plan", "go test ./..."))
	wantRefusalDetail(t, err, "bad_op", &zero)
	w.attest(t, editTestFile)
	got, err := w.previewAt(prBaseSHA, editTestFile, checkAdd("unit", "plan", "go test ./..."))
	if err != nil || !got.Changed || got.Structure == nil || strings.Join(got.Structure.Added, ",") != "unit" {
		t.Fatalf("%+v %v", got, err)
	}
	if !strings.Contains(got.Text, "kind: check") || !strings.Contains(got.Text, "go test ./...") {
		t.Fatal(got.Text)
	}
}
