package appwire

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

type scoutEngine struct {
	factoryEngine
	got      []FactoryScoutParams
	deadline time.Duration
}

func (s *scoutEngine) FactoryScout(ctx context.Context, p FactoryScoutParams) (api.ScoutLook, error) {
	s.got = append(s.got, p)
	if d, ok := ctx.Deadline(); ok {
		s.deadline = time.Until(d)
	}
	return api.ScoutLook{Job: p.Job, Cached: !p.Refresh, Scout: api.Scout{Plan: []string{"a"}}}, nil
}

func TestScoutIsRegisteredAnnouncedAndGivenTimeToLook(t *testing.T) {
	const name = "factory.scout"
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}} {
		if _, err := methods[name].call(e, json.RawMessage(`{"factory":"f","job":3}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryScoutFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &scoutEngine{}
	if h := withFactoryScoutFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryScout {
		t.Fatalf("announced %v", h.Features)
	}
	out, err := methods[name].call(e, json.RawMessage(`{"factory":"factory_local","job":3,"refresh":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := json.Marshal(out); len(e.got) != 1 || !e.got[0].Refresh || string(got) == "" {
		t.Fatalf("handed %+v answered %s", e.got, got)
	}
	if e.deadline < time.Minute {
		t.Fatalf("a look was given %v, less than its own minute", e.deadline)
	}
	for _, bad := range []string{`{"factory":"","job":3}`, `{"factory":"f","job":0}`, `{"factory":"f","job":1,"x":1}`} {
		if _, err := methods[name].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
