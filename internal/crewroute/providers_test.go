package crewroute

import "testing"

// A PROVIDER TURNED OFF TAKES ITS ROUTES AWAY AND NOTHING ELSE: a model it
// shares with a provider still on keeps that route, in its own order, and a
// model only it reached is no candidate.
func TestProvidersOffFiltersRoutesNotModels(t *testing.T) {
	shared := Candidate{Model: Model{ID: "z-ai/glm-5.3-flash"}, Routes: []Route{
		{Provider: "z-ai", Kind: Plan}, {Provider: "ollama", Kind: Local}, {Provider: "openrouter", Kind: Metered},
	}}
	only := Candidate{Model: Model{ID: "openai/gpt-6"}, Routes: []Route{{Provider: "codex", Kind: Plan}}}
	all := []Candidate{shared, only}

	if got := ProvidersOff(nil).Candidates(all); len(got) != 2 || len(got[0].Routes) != 3 {
		t.Fatalf("no provider off changed the candidates: %+v", got)
	}
	got := ProvidersOff{"codex": true, "ollama": true}.Candidates(all)
	if len(got) != 1 || got[0].Model.ID != shared.Model.ID {
		t.Fatalf("a model only an off provider reaches stayed a candidate: %+v", got)
	}
	if routes := got[0].Routes; len(routes) != 2 || routes[0].Provider != "z-ai" || routes[1].Provider != "openrouter" {
		t.Fatalf("the routes left are %+v, want z-ai then openrouter", routes)
	}
	if len(all[0].Routes) != 3 {
		t.Fatal("the filter changed the candidates it was handed")
	}
	if !(ProvidersOff{"codex": true}).On("OpenRouter") || (ProvidersOff{"codex": true}).On(" Codex ") {
		t.Fatal("On does not read a provider id case folded")
	}
}
