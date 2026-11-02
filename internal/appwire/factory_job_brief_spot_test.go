package appwire

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

type spotEngine struct {
	factoryEngine
	got []FactoryJobBriefSpotParams
}

func (s *spotEngine) FactoryJobBriefSpot(_ context.Context, p FactoryJobBriefSpotParams) (FactoryJobBriefSpot, error) {
	s.got = append(s.got, p)
	return FactoryJobBriefSpot{Factory: p.Factory, Job: p.Job, Rev: 2, Spot: p.Spot, File: "a.go", Why: "w", Lines: 1,
		Hunks: []factory.SpotHunk{{Start: 1, Added: 1, Text: "@@ -1 +1 @@\n+y\n"}}, Annotations: []factory.SpotNote{{AfterHunk: 0, Text: "adds y"}}}, nil
}

type onlySpotter struct{ fakeEngine }

func (*onlySpotter) FactoryJobBriefSpot(context.Context, FactoryJobBriefSpotParams) (FactoryJobBriefSpot, error) {
	return FactoryJobBriefSpot{}, nil
}

func TestJobBriefSpotIsRegisteredAnnouncedAndAbsentWithoutItsEngine(t *testing.T) {
	const name = "factory.job_brief_spot"
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &onlySpotter{}} {
		if _, err := methods[name].call(e, json.RawMessage(`{"factory":"f","job":1,"spot":"1"}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryJobBriefSpotFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &spotEngine{}
	out, err := methods[name].call(e, json.RawMessage(`{"factory":"f","job":12,"spot":"1"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"factory":"f","job":12,"rev":2,"spot":"1","file":"a.go","why":"w","lines":1,"hunks":[{"start":1,"added":1,"removed":0,"text":"@@ -1 +1 @@\n+y\n"}],"annotations":[{"after_hunk":0,"text":"adds y"}]}`
	if got, _ := json.Marshal(out); string(got) != want {
		t.Fatalf("answered %s", got)
	}
	if h := withFactoryJobBriefSpotFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryJobBriefSpot {
		t.Fatalf("announced %v", h.Features)
	}
	for _, bad := range []string{`{"factory":"f","job":12}`, `{"factory":"f","job":0,"spot":"1"}`, `{"factory":"f","job":1,"spot":"../x"}`, `{"factory":"f","job":1,"spot":"1/2"}`} {
		if _, err := methods[name].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if len(e.got) != 1 {
		t.Fatalf("handed %+v", e.got)
	}
}
