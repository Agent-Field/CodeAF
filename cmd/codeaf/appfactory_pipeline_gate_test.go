package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func addGateEdit(who *string) appwire.FactoryPipelineEdit {
	return appwire.FactoryPipelineEdit{Op: "add_step", Pipeline: "issue", Name: "signoff", After: str("plan"),
		Kind: str("gate"), Prompt: str("read the plan"), Who: who}
}

func TestTheAppAnnouncesItAddsApprovals(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	if !strings.Contains(r.hello, appwire.FeatureFactoryPipelineEditGates) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryPipelineEditGates, r.hello)
	}
}

// A GATE IS A RUNTIME WORD: the app previews one only for a file its factory
// attested, and then writes the person and who they are.
func TestAnAddedApprovalIsPreviewedOnlyForAnAttestedFile(t *testing.T) {
	w := newGuardWire(t)
	_, err := w.previewAt(prBaseSHA, runtimeSourceFile, addGateEdit(nil))
	wantRefusal(t, err, rereadWords)
	w.attest(t, runtimeSourceFile)
	got, err := w.previewAt(prBaseSHA, runtimeSourceFile, addGateEdit(str("@Rel")))
	if err != nil || !got.Changed || got.Structure == nil || strings.Join(got.Structure.Added, ",") != "signoff" {
		t.Fatalf("%+v %v", got, err)
	}
	for _, word := range []string{"kind: gate", "who: '@rel'", "read the plan"} {
		if !strings.Contains(got.Text, word) {
			t.Fatalf("lost %q:\n%s", word, got.Text)
		}
	}
}
