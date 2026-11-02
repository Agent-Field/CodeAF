package appwire

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

type briefEngine struct {
	factoryEngine
	got []FactoryJobBriefParams
}

func (b *briefEngine) FactoryJobBrief(_ context.Context, p FactoryJobBriefParams) (FactoryJobBrief, error) {
	b.got = append(b.got, p)
	return FactoryJobBrief{Factory: p.Factory, Job: p.Job, Status: factory.BriefStatusStub, Brief: factory.BriefVersion{Stub: true, Facts: []factory.BriefFact{}, Blocks: []factory.BriefBlock{}, ChangedKeys: []string{}}, ChangedSinceSeen: []string{}}, nil
}

type onlyBriefer struct{ fakeEngine }

func (*onlyBriefer) FactoryJobBrief(context.Context, FactoryJobBriefParams) (FactoryJobBrief, error) {
	return FactoryJobBrief{}, nil
}

func TestJobBriefIsRegisteredAnnouncedAndAbsentWithoutItsEngine(t *testing.T) {
	const name = "factory.job_brief"
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &onlyBriefer{}} {
		if _, err := methods[name].call(e, json.RawMessage(`{"factory":"f","job":1}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryJobBriefFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &briefEngine{}
	out, err := methods[name].call(e, json.RawMessage(`{"factory":"f","job":12,"since":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.got) != 1 || e.got[0] != (FactoryJobBriefParams{Factory: "f", Job: 12, Since: 3}) {
		t.Fatalf("handed %+v", e.got)
	}
	want := `{"factory":"f","job":12,"status":"stub","brief":{"rev":0,"stub":true,"issue":"","steps_done":0,"steps_total":0,"verdict":"","facts":[],"blocks":[],"changed_keys":[]},"changed_since_seen":[]}`
	if got, _ := json.Marshal(out); string(got) != want {
		t.Fatalf("answered %s", got)
	}
	if h := withFactoryJobBriefFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryJobBrief {
		t.Fatalf("announced %v", h.Features)
	}
	for _, bad := range []string{`{"factory":"","job":12}`, `{"factory":"f","job":0}`, `{"factory":"f","job":1,"since":-1}`, `{"factory":"f","job":1,"seen":2}`} {
		if _, err := methods[name].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
