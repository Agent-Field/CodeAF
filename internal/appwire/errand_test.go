package appwire

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type errandEngine struct {
	sendEngine
	asked []session.ErrandAsk
	err   error
}

func (e *errandEngine) Errand(ctx context.Context, agent string, ask session.ErrandAsk) (session.ErrandAnswer, error) {
	if _, ok := ctx.Deadline(); !ok {
		panic("errand without a deadline")
	}
	e.asked = append(e.asked, ask)
	if e.err != nil {
		return session.ErrandAnswer{}, e.err
	}
	return session.ErrandAnswer{Text: `{"ok":true}`, Model: "vendor/m", Provider: "openrouter", Role: ask.Role,
		Usage: session.ErrandUsage{In: 12, Out: 3}, Took: 1500 * time.Millisecond}, nil
}

func (e *errandEngine) ErrandModels(ErrandModelsParams) (ErrandModels, error) {
	return ErrandModels{Models: []ErrandModel{{ID: "vendor/m", Provider: "openrouter"}}}, nil
}

func TestTheErrandIsAdvertisedWithItsCaps(t *testing.T) {
	if h := withErrandFeature(Hello{}, &sendEngine{}); h.Errand != nil || len(h.Features) != 0 {
		t.Fatalf("an engine with no errand door advertised one: %+v", h)
	}
	h := withErrandFeature(Hello{}, &errandEngine{})
	if h.Errand == nil || h.Errand.V != session.ErrandVersion || !h.Errand.JSON || h.Errand.MaxInputBytes != session.ErrandMaxInputBytes ||
		len(h.Features) != 2 || h.Features[0] != FeatureErrand || h.Features[1] != FeatureErrandModels {
		t.Fatalf("hello: %+v caps=%+v", h.Features, h.Errand)
	}
	found := false
	for _, role := range h.Errand.Roles {
		found = found || role == "title"
	}
	if !found {
		t.Fatalf("caps name no naming role: %v", h.Errand.Roles)
	}
}

func TestAnErrandRequestReachesTheEngineAndAnswersItsShape(t *testing.T) {
	e := &errandEngine{}
	out, err := methods[FeatureErrand].call(e, json.RawMessage(`{"v":1,"agent":"a","purpose":"status_line","system":"s",
		"messages":[{"role":"user","text":"facts"}],"json":true,"max_tokens":99999,"deadline_ms":500,"future_field":{"x":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	ask := e.asked[0]
	if ask.MaxTokens != session.ErrandMaxTokens || ask.Deadline != session.ErrandMinDeadline || ask.Role != "title" || !ask.JSON {
		t.Fatalf("the ask was not clamped and defaulted: %+v", ask)
	}
	data, _ := json.Marshal(out)
	if got := string(data); got != `{"text":"{\"ok\":true}","model":"vendor/m","provider":"openrouter","role":"title","usage":{"in":12,"out":3},"ms":1500}` {
		t.Fatalf("answer on the wire: %s", got)
	}
}

func TestAnErrandRequestOutsideItsLimitsIsBadParams(t *testing.T) {
	e := &errandEngine{}
	big := strings.Repeat("x", session.ErrandMaxInputBytes+1)
	for _, raw := range []string{
		`{"purpose":"pp","messages":[{"role":"user","text":"x"}]}`,
		`{"v":2,"agent":"a","purpose":"pp","messages":[{"role":"user","text":"x"}]}`,
		`{"agent":"a","purpose":"P!","messages":[{"role":"user","text":"x"}]}`,
		`{"agent":"a","purpose":"pp","messages":[]}`,
		`{"agent":"a","purpose":"pp","messages":[{"role":"tool","text":"x"}]}`,
		`{"agent":"a","purpose":"pp","messages":[{"role":"user","text":"` + big + `"}]}`,
		`{"agent":"a","purpose":"pp","messages":"x"}`,
	} {
		if _, err := methods[FeatureErrand].call(e, json.RawMessage(raw)); codeOf(err) != CodeBadParams {
			t.Errorf("accepted %.80s: %v", raw, err)
		}
	}
	if len(e.asked) != 0 {
		t.Fatal("a refused ask reached the engine")
	}
}

func TestADisallowedErrandRouteSaysSo(t *testing.T) {
	e := &errandEngine{err: fmt.Errorf("%w: vendor/x is not served here", session.ErrErrandRoute)}
	_, err := methods[FeatureErrand].call(e, json.RawMessage(`{"agent":"a","purpose":"pp","model":"vendor/x","messages":[{"role":"user","text":"x"}]}`))
	if codeOf(err) != CodeModelNotAllowed || !strings.Contains(err.Error(), "vendor/x") {
		t.Fatalf("disallowed model: %v", err)
	}
	if _, err := methods[FeatureErrand].call(&sendEngine{}, json.RawMessage(`{}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatal(err)
	}
}

func TestErrandModelsCarriesTheCaps(t *testing.T) {
	out, err := methods[FeatureErrandModels].call(&errandEngine{}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	models := out.(*ErrandModels)
	if models.Caps.V != session.ErrandVersion || models.Roles == nil || len(models.Models) != 1 {
		t.Fatalf("%+v", models)
	}
}
