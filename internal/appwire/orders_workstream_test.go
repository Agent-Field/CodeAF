package appwire

import (
	"encoding/json"
	"testing"
)

type fakeOrderMaker struct {
	Engine
	made []OrderCreate
}

func (f *fakeOrderMaker) CreateOrder(in OrderCreate) (Order, error) {
	f.made = append(f.made, in)
	return Order{ID: "o1", Title: in.Does, Workstream: in.Workstream, State: OrderProposed}, nil
}

func (f *fakeOrderMaker) PreviewOrder(when string) (OrderPreview, error) {
	return OrderPreview{When: when}, nil
}

// orders.create REFUSES AT THE DOOR what could never be a workstream's order,
// and trims what it passes on.
func TestOrdersCreateChecksItsWords(t *testing.T) {
	f := &fakeOrderMaker{}
	for _, params := range []string{
		`{"when":"every 1d","does":"","workstream":"w"}`,
		`{"when":"","does":"draft it","workstream":"w"}`,
		`{"when":"every 1d","does":"draft it","workstream":""}`,
		`{"when":"every 1d","does":"draft it","workstream":"a\nb"}`,
		`{"when":"every 1d","does":"draft it","workstream":"w","budget_per_run_usd":0}`,
	} {
		if _, err := methods["orders.create"].call(f, json.RawMessage(params)); wireCode(t, err) != CodeBadParams {
			t.Fatalf("%s: %v", params, err)
		}
	}
	out, err := methods["orders.create"].call(f, json.RawMessage(`{"when":" every 1d ","does":" draft it ","workstream":" w "}`))
	if err != nil || len(f.made) != 1 || f.made[0].Does != "draft it" || f.made[0].Workstream != "w" || f.made[0].When != "every 1d" {
		t.Fatalf("made %+v, %v, %v", f.made, out, err)
	}
	p, err := methods["orders.preview"].call(f, json.RawMessage(`{"when":"every 1d"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p)
	if string(raw) != `{"when":"every 1d","when_exact":"","next_runs":[]}` {
		t.Fatalf("preview = %s", raw)
	}
	if _, err := methods["orders.preview"].call(f, json.RawMessage(`{"when":" "}`)); wireCode(t, err) != CodeBadParams {
		t.Fatalf("an empty when was previewed: %v", err)
	}
}

func TestTheWorkstreamOrdersWordIsSaidOnlyByAnEngineThatMakesThem(t *testing.T) {
	if h := withWorkstreamOrdersFeature(Hello{}, &fakeOrderMaker{}); len(h.Features) != 1 || h.Features[0] != FeatureWorkstreamOrders {
		t.Fatalf("hello = %+v", h)
	}
	type plain struct{ Engine }
	if h := withWorkstreamOrdersFeature(Hello{}, plain{}); len(h.Features) != 0 {
		t.Fatalf("an engine that makes no orders said %v", h.Features)
	}
}
