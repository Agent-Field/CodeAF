package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type overviewEngine struct {
	sendEngine
	asked OverviewAsk
	err   error
}

func (o *overviewEngine) CrewsOverview(_ context.Context, ask OverviewAsk) (*Overview, error) {
	o.asked = ask
	return &Overview{Teams: []TeamSummary{}, Members: []OverviewMember{}}, o.err
}

// AN ENGINE WITHOUT THE READ REFUSES IT AS UNKNOWN AND DOES NOT NAME IT IN HELLO.
func TestCrewsOverviewIsUnknownToAnEngineWithoutIt(t *testing.T) {
	call := methods["crews.overview"]
	if _, err := call.call(&sendEngine{}, json.RawMessage(`{}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatalf("an engine without it answered %v", err)
	}
	if h := withCrewsOverviewFeature(Hello{}, &sendEngine{}); len(h.Features) != 0 {
		t.Fatalf("hello names %v", h.Features)
	}
	h := withCrewsOverviewFeature(Hello{Features: []string{"x"}}, &overviewEngine{})
	if len(h.Features) != 2 || h.Features[1] != FeatureCrewsOverview {
		t.Fatalf("hello names %v", h.Features)
	}
}

// THE PARAMS REACH THE ENGINE WHOLE, THE LIMIT'S ABSENCE INCLUDED.
func TestCrewsOverviewParamsRoundTripAndAreBounded(t *testing.T) {
	call := methods["crews.overview"]
	eng := &overviewEngine{}
	if _, err := call.call(eng, json.RawMessage(`{"parent":"p","kind":"members","limit":7,"cursor":"c"}`)); err != nil {
		t.Fatal(err)
	}
	if eng.asked.Parent != "p" || eng.asked.Kind != "members" || eng.asked.Limit == nil || *eng.asked.Limit != 7 || eng.asked.Cursor != "c" {
		t.Fatalf("the engine heard %+v", eng.asked)
	}
	if _, err := call.call(eng, json.RawMessage(`{}`)); err != nil || eng.asked.Limit != nil {
		t.Fatalf("no limit became %+v (%v)", eng.asked.Limit, err)
	}
	for _, bad := range []string{`{"limit":0}`, `{"limit":101}`, `{"kind":"x"}`, `{"kind":"members"}`, `{"parent":" p"}`} {
		if _, err := call.call(eng, json.RawMessage(bad)); codeOf(err) != CodeBadParams {
			t.Fatalf("%s answered %v", bad, err)
		}
	}
}

// A FAILED READ IS ONE CLEAN SENTENCE: a wire error as it is, a context's end
// in the method's words, anything else with no detail of the failure in it.
func TestCrewsOverviewErrorsAreCleanWire(t *testing.T) {
	for err, want := range map[error]string{
		context.DeadlineExceeded:                  "took longer",
		context.Canceled:                          "cancelled",
		Errorf(CodeTooLarge, "too big"):           "too big",
		&pathError{"/home/someone/teams.json: x"}: "could not be read",
	} {
		got := overviewError(err)
		if codeOf(got) == "" || !strings.Contains(got.Error(), want) || strings.Contains(got.Error(), "/home/") {
			t.Fatalf("%v became %v", err, got)
		}
	}
}

type pathError struct{ s string }

func (p *pathError) Error() string { return p.s }
