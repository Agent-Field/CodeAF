package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A JOB'S SESSIONS RUN ON ONE MODEL: the job's own when a person chose one,
// else the factory's default, and "" when neither is known. The job's manager
// is first opened on that model with the app's fallback, never on a pair whose
// two models are the same, and on nothing at all when no model is known (the
// conversation then opens as any other does).
func TestAJobsSessionsRunOnTheJobsModel(t *testing.T) {
	def := func() string { return " factory/default " }
	for _, tc := range []struct {
		name string
		it   factory.Item
		def  func() string
		want string
	}{
		{"the job's own choice", factory.Item{Model: " acme/deep "}, def, "acme/deep"},
		{"the factory's default", factory.Item{}, def, "factory/default"},
		{"a factory with no default", factory.Item{}, func() string { return "" }, ""},
		{"no factory to ask", factory.Item{}, nil, ""},
		{"the job's own, with no factory to ask", factory.Item{Model: "acme/deep"}, nil, "acme/deep"},
	} {
		if got := jobModelOf(tc.it, tc.def); got != tc.want {
			t.Errorf("%s: model %q, want %q", tc.name, got, tc.want)
		}
	}
	if pair := jobManagerOpenModels("  ", "acme/fallback"); pair != nil {
		t.Fatalf("no model known opened the manager on %+v", pair)
	}
	if pair := jobManagerOpenModels(" acme/deep ", " acme/fallback "); pair == nil || pair.Main != "acme/deep" || pair.Fallback != "acme/fallback" {
		t.Fatalf("pair %+v, want the job's model with the app's fallback", pair)
	}
	if pair := jobManagerOpenModels("acme/deep", "acme/deep"); pair == nil || pair.Main != "acme/deep" || pair.Fallback != "" {
		t.Fatalf("pair %+v, want no fallback that is the same model", pair)
	}
}
