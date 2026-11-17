package appwire

import (
	"context"
	"encoding/json"
	"testing"
)

type organizeEngine struct {
	sendEngine
	asked OrganizeAsk
}

func (e *organizeEngine) CrewsOrganize(_ context.Context, p OrganizeAsk) (*OrganizeReply, error) {
	e.asked = p
	return &OrganizeReply{New: []OrganizeNew{}, Additions: []OrganizeAddition{}}, nil
}
func TestCrewsOrganizeDispatchAndFeature(t *testing.T) {
	call := methods[FeatureCrewsOrganize]
	if _, err := call.call(&sendEngine{}, json.RawMessage(`{"pass":"folders"}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatal(err)
	}
	if h := withCrewsOrganizeFeature(Hello{}, &sendEngine{}); len(h.Features) != 0 {
		t.Fatal(h)
	}
	e := &organizeEngine{}
	if h := withCrewsOrganizeFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureCrewsOrganize {
		t.Fatal(h)
	}
	for _, pass := range []string{"folders", "model"} {
		if _, err := call.call(e, json.RawMessage(`{"pass":"`+pass+`"}`)); err != nil || e.asked.Pass != pass {
			t.Fatalf("%s: %v", pass, err)
		}
	}
	for _, p := range []string{`{}`, `{"pass":"all"}`, `{"pass":1}`} {
		if _, err := call.call(e, json.RawMessage(p)); codeOf(err) != CodeBadParams {
			t.Fatalf("%s: %v", p, err)
		}
	}
}
