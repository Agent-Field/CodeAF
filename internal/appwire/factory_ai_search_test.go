package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/aisearch"
)

// searchEngine is a factory engine that also searches, and keeps what it was
// handed.
type searchEngine struct {
	factoryEngine
	got FactoryAISearchParams
	run string
}

func (s *searchEngine) FactoryAISearchEstimate(_ context.Context, p FactoryAISearchParams) (FactoryAISearchEstimate, error) {
	s.got = p
	return FactoryAISearchEstimate{Factory: p.Factory, Question: p.Question, Estimate: aisearch.Estimate{Items: len(p.Jobs)}}, nil
}
func (s *searchEngine) FactoryAISearchStart(_ context.Context, p FactoryAISearchParams) (FactoryAISearchStarted, error) {
	s.got = p
	return FactoryAISearchStarted{Run: "s_1"}, nil
}
func (s *searchEngine) FactoryAISearchStatus(run string) (FactoryAISearchRun, error) {
	s.run = run
	return FactoryAISearchRun{Run: run, Status: aisearch.Status{Phase: aisearch.PhaseDone}}, nil
}
func (s *searchEngine) FactoryAISearchStop(run string) (FactoryAISearchRun, error) {
	s.run = run
	return FactoryAISearchRun{Run: run, Status: aisearch.Status{Phase: aisearch.PhaseStopped}}, nil
}

// THE FOUR METHODS ARE ABSENT WITHOUT THE INTERFACE, and announced with it.
func TestFactoryAISearchIsAbsentWithoutTheInterface(t *testing.T) {
	names := []string{"factory.ai_search", "factory.ai_search_estimate", "factory.ai_search_status", "factory.ai_search_stop"}
	for _, name := range names {
		for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}} {
			if _, err := methods[name].call(e, json.RawMessage(`{"factory":"f","question":"q","run":"r"}`)); wireCode(t, err) != CodeUnknownMethod {
				t.Errorf("%s on %T: %v", name, e, err)
			}
		}
	}
	if got := withFactoryAISearchFeature(Hello{}, &factoryEngine{}); len(got.Features) != 0 {
		t.Fatalf("announced %v", got.Features)
	}
	if got := withFactoryAISearchFeature(Hello{}, &searchEngine{}); len(got.Features) != 1 || got.Features[0] != FeatureFactoryAISearch {
		t.Fatalf("a search engine announced %v", got.Features)
	}
}

// A QUESTION IS CHECKED BEFORE ANYONE IS ASKED: required, cleaned of the
// bar's leading "?", bounded, and jobs are positive and deduplicated.
func TestFactoryAISearchParamsAreChecked(t *testing.T) {
	e := &searchEngine{}
	if _, err := methods["factory.ai_search"].call(e, json.RawMessage(`{"factory":" f ","question":"  ?  issues about   the ledger ","jobs":[3,1,3]}`)); err != nil {
		t.Fatal(err)
	}
	if e.got.Factory != "f" || e.got.Question != "issues about the ledger" || len(e.got.Jobs) != 2 || e.got.Jobs[0] != 3 {
		t.Fatalf("handed %+v", e.got)
	}
	for _, bad := range []string{`{}`, `{"factory":"f"}`, `{"factory":"f","question":"?"}`, `{"factory":"f","question":"q","jobs":[0]}`,
		`{"factory":"f","question":"q","model":"x"}`, `{"factory":"f","question":"` + strings.Repeat("a", aisearch.QuestionMost+2) + `"}`} {
		for _, name := range []string{"factory.ai_search", "factory.ai_search_estimate"} {
			if _, err := methods[name].call(&searchEngine{}, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
				t.Errorf("%s %s: %v", name, bad, err)
			}
		}
	}
	if _, err := methods["factory.ai_search_status"].call(e, json.RawMessage(`{"run":" s_1 "}`)); err != nil || e.run != "s_1" {
		t.Fatalf("status of %q: %v", e.run, err)
	}
	if _, err := methods["factory.ai_search_stop"].call(e, json.RawMessage(`{"run":""}`)); wireCode(t, err) != CodeBadParams {
		t.Fatalf("an empty run: %v", err)
	}
}
