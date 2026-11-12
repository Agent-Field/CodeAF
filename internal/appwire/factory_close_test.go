package appwire

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
)

type closeEngine struct {
	actorEngine
	got []FactoryJobActionParams
}

func (e *closeEngine) FactoryJobClose(_ context.Context, p FactoryJobActionParams) (FactoryJobActed, error) {
	e.got = append(e.got, p)
	return FactoryJobActed{Action: JobActClose, Job: p.Job}, nil
}
func TestFactoryCloseHasAnOptionalGuardedDoor(t *testing.T) {
	e := &closeEngine{}
	if !slices.Contains(withFactoryActorFeature(Hello{}, e).Features, FeatureFactoryJobClose) {
		t.Fatal("Close feature absent")
	}
	if slices.Contains(withFactoryActorFeature(Hello{}, &actorEngine{}).Features, FeatureFactoryJobClose) {
		t.Fatal("old actor falsely advertised Close")
	}
	for _, bad := range []string{`{"factory":"f","job":9}`, `{"factory":"f","job":9,"expect":{"version":2},"discard":true}`} {
		if _, err := methods["factory.job_close"].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Fatalf("invalid close accepted: %v", err)
		}
	}
	if len(e.got) != 0 {
		t.Fatal("invalid close reached actor")
	}
	if _, err := methods["factory.job_close"].call(e, json.RawMessage(`{"factory":"f","job":9,"expect":{"version":2,"state":"running"}}`)); err != nil {
		t.Fatal(err)
	}
	if len(e.got) != 1 || e.got[0].Job != 9 || e.got[0].Expect.Version == nil || *e.got[0].Expect.Version != 2 {
		t.Fatal("close guard changed")
	}
}

func (e *closeEngine) FactoryJobReopen(_ context.Context, p FactoryJobActionParams) (FactoryJobActed, error) {
	return FactoryJobActed{Action: JobActReopen, Job: p.Job}, nil
}
