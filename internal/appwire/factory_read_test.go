package appwire

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

type readEngine struct {
	factoryEngine
	got      []FactoryReadParams
	deadline time.Duration
}

func (e *readEngine) FactoryRead(ctx context.Context, p FactoryReadParams) (api.ReadResult, error) {
	e.got = append(e.got, p)
	if d, ok := ctx.Deadline(); ok {
		e.deadline = time.Until(d)
	}
	return api.ReadResult{Read: p.Jobs, Skipped: []int{}, Failed: []int{}}, nil
}

// A person's Read is one method over chosen job ids, announced only by an
// engine that serves it, given time for twenty reads one after another.
func TestReadIsRegisteredAnnouncedAndGivenTimeForItsReads(t *testing.T) {
	const name = "factory.read"
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}} {
		if _, err := methods[name].call(e, json.RawMessage(`{"factory":"f","jobs":[3]}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryReadFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &readEngine{}
	if h := withFactoryReadFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryRead {
		t.Fatalf("announced %v", h.Features)
	}
	out, err := methods[name].call(e, json.RawMessage(`{"factory":"factory_local","jobs":[3,4]}`))
	if got, _ := json.Marshal(out); err != nil || string(got) != `{"read":[3,4],"skipped":[],"failed":[]}` {
		t.Fatalf("answered %s, %v", got, err)
	}
	if e.deadline < 2*time.Minute {
		t.Fatalf("a read was given %v", e.deadline)
	}
	for _, bad := range []string{`{"factory":"","jobs":[3]}`, `{"factory":"f","jobs":[]}`, `{"factory":"f","jobs":[0]}`, `{"factory":"f","jobs":[1],"x":1}`} {
		if _, err := methods[name].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
