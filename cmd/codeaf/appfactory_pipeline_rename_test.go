package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

func TestTheAppRenamesAStepEverywhereAndSaysSo(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	if !strings.Contains(r.hello, appwire.FeatureFactoryPipelineEditRename) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryPipelineEditRename, r.hello)
	}
	got, err := r.preview(editTestFile, appwire.FactoryPipelineEdit{Op: "rename_step", Name: "write", To: "build"})
	if err != nil || !got.Changed || got.Structure == nil {
		t.Fatalf("%+v %v", got, err)
	}
	if len(got.Structure.Renamed) != 1 || got.Structure.Renamed[0] != (appwire.FactoryPipelineRename{From: "write", To: "build"}) {
		t.Fatalf("%+v", got.Structure.Renamed)
	}
	back, err := pipeline.Parse(got.Text)
	if err != nil || len(got.Steps) != 0 || strings.Join(back.Pipeline["issue"], ",") != "plan,approve,build,proof" || back.Steps["build"].Prompt != "write it" {
		t.Fatalf("%v %+v\n%s", err, got.Steps, got.Text)
	}
}
