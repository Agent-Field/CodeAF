package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// twoPipelines lists a second pipeline, so a word for one of them is a map
// and not the whole file's.
var twoPipelines = strings.Replace(editTestFile, "  issue: [plan, approve, write, proof]\n", "  issue: [plan, approve, write, proof]\n  docs: [write, proof]\n", 1)

// THE SOURCE SAYS EACH PIPELINE'S ADAPT WORD when the file writes one per
// pipeline, and {} when it writes one word or none. A map is a key an older
// factory drops, so only a factory that attests the file is believed.
func TestPipelineSourceCarriesEachPipelinesAdapt(t *testing.T) {
	file := twoPipelines + "adapt:\n  issue: fixed\n"
	if _, err := runtimeSource(t, runtimeReply(t, editSourceFor(prRepo, file), nil)); err == nil || err.Error() != errPipelineSourceRuntimeUnattested.Error() {
		t.Fatalf("an unattested per-pipeline adapt was read: %v", err)
	}
	w := newGuardWire(t)
	if !strings.Contains(w.hello, appwire.FeatureFactoryPipelineEditAdaptKind) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryPipelineEditAdaptKind, w.hello)
	}
	got := w.attest(t, file)
	if got.AdaptKinds == nil || !reflect.DeepEqual(*got.AdaptKinds, map[string]string{"issue": "fixed"}) || *got.Adapt != "" {
		t.Fatalf("%+v", got)
	}
	if got = w.attest(t, twoPipelines); got.AdaptKinds == nil || len(*got.AdaptKinds) != 0 {
		t.Fatalf("%+v", got.AdaptKinds)
	}
}

// ONE PIPELINE'S WORD IS A MAP an older factory would drop, so it is
// previewed only for a file its factory attested.
func TestAnAdaptForOnePipelineIsPreviewedOnlyForAnAttestedFile(t *testing.T) {
	zero := 0
	fixed := "fixed"
	edit := appwire.FactoryPipelineEdit{Op: appwire.PipelineEditSetAdapt, Pipeline: "issue", Adapt: &fixed}
	file := twoPipelines
	w := newGuardWire(t)
	_, err := w.previewAt(prBaseSHA, file, edit)
	wantRefusalDetail(t, err, "bad_op", &zero)
	w.attest(t, file)
	got, err := w.previewAt(prBaseSHA, file, edit)
	if err != nil || !got.Changed {
		t.Fatalf("%+v %v", got, err)
	}
	back, err := pipeline.Parse(got.Text)
	if err != nil || !reflect.DeepEqual(back.AdaptKinds, map[factory.Kind]factory.AdaptMode{"issue": "fixed"}) {
		t.Fatalf("%v %v\n%s", err, back.AdaptKinds, got.Text)
	}
}
