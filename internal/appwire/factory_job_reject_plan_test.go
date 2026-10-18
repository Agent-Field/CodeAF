package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// rejectEngine is a factory engine that also sends a plan back and keeps what
// it was handed.
type rejectEngine struct {
	factoryEngine
	got []FactoryJobRejectPlanParams
}

func (r *rejectEngine) FactoryJobRejectPlan(_ context.Context, p FactoryJobRejectPlanParams) (FactoryJobActed, error) {
	r.got = append(r.got, p)
	return FactoryJobActed{Action: JobActRejectPlan, Job: p.Job}, nil
}

type onlyRejecter struct{ fakeEngine }

func (*onlyRejecter) FactoryJobRejectPlan(context.Context, FactoryJobRejectPlanParams) (FactoryJobActed, error) {
	return FactoryJobActed{}, nil
}

const planGuard = `"expect":{"version":4,"state":"needs_you"}`

func TestJobRejectPlanIsRegisteredAnnouncedAndAbsentWithoutItsEngine(t *testing.T) {
	const name = "factory.job_reject_plan"
	params := `{"factory":"f","job":2,"words":"use the other file",` + planGuard + `}`
	if _, ok := methods[name]; !ok || len(factoryJobRejectPlanMethods) != 1 {
		t.Fatalf("%s is not registered once", name)
	}
	// It is not one of the sixteen factory.job_* actions: a plan send-back is
	// its own optional feature, and no FactoryActor is one by accident.
	if _, ok := factoryActionMethods[name]; ok {
		t.Fatal("reject_plan joined the closed FactoryActor table")
	}
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &actorEngine{}, &stepEditEngine{}, &onlyRejecter{}} {
		if _, err := methods[name].call(e, json.RawMessage(params)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryJobRejectPlanFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &rejectEngine{}
	if _, err := methods[name].call(e, json.RawMessage(params)); err != nil {
		t.Fatal(err)
	}
	if len(e.got) != 1 || e.got[0].Words != "use the other file" || *e.got[0].Expect.Version != 4 {
		t.Fatalf("handed %+v", e.got)
	}
	if h := withFactoryJobRejectPlanFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryJobRejectPlan {
		t.Fatalf("announced %v", h.Features)
	}
	if h := withFactoryActorFeature(Hello{}, e); len(h.Features) != 0 {
		t.Fatalf("a plan rejecter is not an actor: %v", h.Features)
	}
}

func TestJobRejectPlanRefusesWhatCannotBeARequestBeforeTheEngine(t *testing.T) {
	long := strings.Repeat("é", api.MaxRejectPlanWords+1)
	exact := strings.Repeat("é", api.MaxRejectPlanWords)
	for name, params := range map[string]string{
		"no words":       `{"factory":"f","job":2,` + planGuard + `}`,
		"blank words":    `{"factory":"f","job":2,"words":" \n\t ",` + planGuard + `}`,
		"too many words": `{"factory":"f","job":2,"words":"` + long + `",` + planGuard + `}`,
		"no expect":      `{"factory":"f","job":2,"words":"x"}`,
		"no version":     `{"factory":"f","job":2,"words":"x","expect":{"state":"needs_you"}}`,
		"no state":       `{"factory":"f","job":2,"words":"x","expect":{"version":4}}`,
		"another state":  `{"factory":"f","job":2,"words":"x","expect":{"version":4,"state":"running"}}`,
		"no job":         `{"factory":"f","words":"x",` + planGuard + `}`,
		"a commit":       `{"factory":"f","job":2,"words":"x","commit":"abc1234",` + planGuard + `}`,
		"a yes":          `{"factory":"f","job":2,"words":"x","yes":true,` + planGuard + `}`,
		"a choice":       `{"factory":"f","job":2,"words":"x","choice":"cancel_job",` + planGuard + `}`,
		"a question id":  `{"factory":"f","job":2,"words":"x","question_id":"q2",` + planGuard + `}`,
		"a list of jobs": `{"factory":"f","jobs":[2,3],"words":"x",` + planGuard + `}`,
	} {
		e := &rejectEngine{}
		if _, err := methods["factory.job_reject_plan"].call(e, json.RawMessage(params)); wireCode(t, err) != CodeBadParams || len(e.got) != 0 {
			t.Errorf("%s: %v, engine handed %d", name, err, len(e.got))
		}
	}
	// 4000 characters are allowed whatever their bytes, and words are trimmed.
	e := &rejectEngine{}
	if _, err := methods["factory.job_reject_plan"].call(e, json.RawMessage(`{"factory":"f","job":2,"words":"  `+exact+`  ",`+planGuard+`}`)); err != nil || e.got[0].Words != exact {
		t.Fatalf("exact limit: %v", err)
	}
}
