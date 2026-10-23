package appwire

import (
	"context"
	"encoding/json"
	"testing"
)

// chatsEngine is a factory engine that answers a job's conversations and keeps
// what it was asked.
type chatsEngine struct {
	factoryEngine
	got []FactoryJobChatsParams
}

func (c *chatsEngine) FactoryJobChats(_ context.Context, p FactoryJobChatsParams) (FactoryJobChats, error) {
	c.got = append(c.got, p)
	return FactoryJobChats{Factory: p.Factory, Job: p.Job, Agent: "talk1", Steps: []FactoryJobStepChat{{Step: "plan", Agent: "plan1"}}}, nil
}

type onlyChatter struct{ fakeEngine }

func (*onlyChatter) FactoryJobChats(context.Context, FactoryJobChatsParams) (FactoryJobChats, error) {
	return FactoryJobChats{}, nil
}

func TestJobChatsIsRegisteredAnnouncedAndAbsentWithoutItsEngine(t *testing.T) {
	const name = "factory.job_chats"
	params := `{"factory":"factory_local","job":12}`
	if _, ok := methods[name]; !ok {
		t.Fatalf("%s is not registered", name)
	}
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &onlyChatter{}} {
		if _, err := methods[name].call(e, json.RawMessage(params)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryJobChatsFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &chatsEngine{}
	out, err := methods[name].call(e, json.RawMessage(params))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.got) != 1 || e.got[0].Factory != "factory_local" || e.got[0].Job != 12 {
		t.Fatalf("handed %+v", e.got)
	}
	if got, _ := json.Marshal(out); string(got) != `{"factory":"factory_local","job":12,"agent":"talk1","steps":[{"step":"plan","agent":"plan1"}]}` {
		t.Fatalf("answered %s", got)
	}
	if h := withFactoryJobChatsFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryJobChats {
		t.Fatalf("announced %v", h.Features)
	}
	for _, bad := range []string{`{"factory":"","job":12}`, `{"factory":"f","job":0}`, `{"factory":"f","job":1,"path":"/x"}`} {
		if _, err := methods[name].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
