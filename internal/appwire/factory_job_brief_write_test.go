package appwire

import (
	"context"
	"encoding/json"
	"testing"
)

type briefWriteEngine struct {
	factoryEngine
	got []FactoryJobBriefWriteParams
}

func (b *briefWriteEngine) FactoryJobBriefWrite(ctx context.Context, p FactoryJobBriefWriteParams) (FactoryJobBriefWritten, error) {
	b.got = append(b.got, p)
	if deadline, ok := ctx.Deadline(); !ok || deadline.IsZero() {
		panic("a write without a wall")
	}
	return FactoryJobBriefWritten{Skip: "writing"}, nil
}

type onlyBriefWriter struct{ fakeEngine }

func (*onlyBriefWriter) FactoryJobBriefWrite(context.Context, FactoryJobBriefWriteParams) (FactoryJobBriefWritten, error) {
	return FactoryJobBriefWritten{}, nil
}

func TestJobBriefWriteIsRegisteredAnnouncedAndAbsentWithoutItsEngine(t *testing.T) {
	const name = "factory.job_brief_write"
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &onlyBriefWriter{}} {
		if _, err := methods[name].call(e, json.RawMessage(`{"factory":"f","job":1}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryJobBriefWriterFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &briefWriteEngine{}
	out, err := methods[name].call(e, json.RawMessage(`{"factory":"f","job":12}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.got) != 1 || e.got[0] != (FactoryJobBriefWriteParams{Factory: "f", Job: 12}) {
		t.Fatalf("handed %+v", e.got)
	}
	if got, _ := json.Marshal(out); string(got) != `{"skip":"writing","kept":false,"drops":null,"current":{"factory":"","job":0,"status":"","brief":{"rev":0,"issue":"","steps_done":0,"steps_total":0,"verdict":"","facts":null,"blocks":null,"changed_keys":null},"changed_since_seen":null}}` {
		t.Fatalf("answered %s", got)
	}
	if h := withFactoryJobBriefWriterFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryJobBriefWriter {
		t.Fatalf("announced %v", h.Features)
	}
	for _, bad := range []string{`{"factory":"","job":12}`, `{"factory":"f","job":0}`, `{"factory":"f","job":1,"trigger":"step"}`} {
		if _, err := methods[name].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
