package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// gatesEditEngine is a structure editor with the gates marker.
type gatesEditEngine struct{ structureEditEngine }

func (*gatesEditEngine) PipelineGateEdits() {}

// gatesApproversEditEngine announces gates and approvers, like appBackend.
type gatesApproversEditEngine struct{ approversEditEngine }

func (*gatesApproversEditEngine) PipelineGateEdits() {}

const (
	wireAddGate    = `{"op":"add_step","pipeline":"issue","name":"signoff","after":"test","kind":"gate","prompt":"read the auth change"}`
	wireAddGateWho = `{"op":"add_step","pipeline":"issue","name":"signoff","after":"test","kind":"gate","who":"@rel"}`
)

func previewWith(t *testing.T, e Engine, edit string) error {
	t.Helper()
	_, err := methods["factory.pipeline_preview"].call(e, json.RawMessage(editPreviewOf(`"pipeline: {}\n"`, "["+edit+"]")))
	return err
}

func TestAGateOnAddStepNeedsTheGatesFeature(t *testing.T) {
	features := func(e Engine) string { return strings.Join(withFactoryPipelineEditFeature(Hello{}, e).Features, ",") }
	if got := features(&structureEditEngine{}); strings.Contains(got, FeatureFactoryPipelineEditGates) {
		t.Fatalf("an editor without the marker announced %v", got)
	}
	if got := features(&gatesEditEngine{}); !strings.Contains(got, FeatureFactoryPipelineEditGates) {
		t.Fatalf("a gates editor announced %v", got)
	}
	old := &checksEditEngine{}
	if err := previewWith(t, old, wireAddGate); wireCode(t, err) != CodeBadParams || len(old.previews) != 0 {
		t.Fatalf("a gate reached an engine without the feature: %v", err)
	}
	eng := &gatesEditEngine{}
	if err := previewWith(t, eng, wireAddGate); err != nil {
		t.Fatal(err)
	}
	if k := PipelineEditOps(eng.previews[0].Edits)[0].Kind; k == nil || *k != "gate" {
		t.Fatalf("%+v", eng.previews[0].Edits[0])
	}
	// A gates editor alone adds no check.
	if err := previewWith(t, eng, wireAddCheck); wireCode(t, err) != CodeBadParams || len(eng.previews) != 1 {
		t.Fatalf("a check reached a gates-only engine: %v", err)
	}
}

// WHO ON AN ADDED GATE IS THE APPROVERS FEATURE'S TOO: an engine that does not
// name approvers never receives one.
func TestWhoOnAnAddedGateNeedsTheApproversFeature(t *testing.T) {
	gatesOnly := &gatesEditEngine{}
	if err := previewWith(t, gatesOnly, wireAddGateWho); wireCode(t, err) != CodeBadParams || len(gatesOnly.previews) != 0 {
		t.Fatalf("who reached an engine without approvers: %v", err)
	}
	approversOnly := &approversEditEngine{}
	if err := previewWith(t, approversOnly, wireAddGateWho); wireCode(t, err) != CodeBadParams || len(approversOnly.previews) != 0 {
		t.Fatalf("a gate reached an engine without gates: %v", err)
	}
	eng := &gatesApproversEditEngine{}
	if err := previewWith(t, eng, wireAddGateWho); err != nil {
		t.Fatal(err)
	}
	if who := PipelineEditOps(eng.previews[0].Edits)[0].Who; who == nil || *who != "@rel" {
		t.Fatalf("%+v", eng.previews[0].Edits[0])
	}
	for _, bad := range []string{
		`{"op":"add_step","pipeline":"issue","name":"signoff","after":"test","kind":"gate","who":"everyone"}`,
		`{"op":"add_step","pipeline":"issue","name":"lint","after":"test","prompt":"x","who":"@rel"}`,
		`{"op":"add_step","pipeline":"issue","name":"signoff","after":"test","kind":"gate","until":"they agree"}`,
	} {
		if err := previewWith(t, eng, bad); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if len(eng.previews) != 1 {
		t.Fatalf("%d previews", len(eng.previews))
	}
}
