package serve

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// /v1/rules: anyone signed in reads the factory's rules, the operator replaces
// all three lists at once, a body missing a list or holding a bad line is
// refused with the phase marker and changes nothing, and nobody unsigned gets
// either.
func TestFactoryRulesRouteReadsReplacesAndRefusesWithoutSaving(t *testing.T) {
	f := peopleV2Setup(t, "team")
	var got factory.HouseRules
	f.request(t, "GET", "/v1/rules", "operator-secret", nil, 200, &got)
	if !got.Empty() || got.Rules == nil {
		t.Fatalf("a new factory has rules %#v", got)
	}
	body := map[string]any{"rules": []string{" no new dependencies without asking "}, "ask_first": []string{"touch auth/"}, "never": []string{}}
	f.request(t, "PUT", "/v1/rules", "operator-secret", body, 200, &got)
	want := factory.HouseRules{Rules: []string{"no new dependencies without asking"}, AskFirst: []string{"touch auth/"}, Never: []string{}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("saved %#v, want %#v", got, want)
	}
	for _, bad := range []map[string]any{
		{"rules": []string{"x"}, "ask_first": []string{}},
		{"rules": []string{"two\nlines"}, "ask_first": []string{}, "never": []string{}},
	} {
		w := identityRequest(f.h, "PUT", "/v1/rules", rulesBody(t, bad), "operator-secret", nil)
		if w.Code != 422 || w.Header().Get(api.PhaseHeader) != api.PhasePreMutation {
			t.Fatalf("bad body answered %d with phase %q: %s", w.Code, w.Header().Get(api.PhaseHeader), w.Body)
		}
	}
	if saved, _ := f.st.HouseRules(); !reflect.DeepEqual(saved, want) {
		t.Fatalf("a refused write changed the rules: %#v", saved)
	}
	f.request(t, "GET", "/v1/rules", "", nil, 401, nil)
	f.request(t, "PUT", "/v1/rules", "", body, 401, nil)
}

func rulesBody(t *testing.T, body any) string {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// THE ADAPT WORD IS OPTIONAL ON A PUT: given, it is saved; omitted, the word
// the factory keeps stays; "" clears it; a word that is not one is refused.
func TestFactoryRulesRouteKeepsTheAdaptWordUnlessItIsSent(t *testing.T) {
	f := peopleV2Setup(t, "team")
	lists := func(more map[string]any) map[string]any {
		body := map[string]any{"rules": []string{}, "ask_first": []string{}, "never": []string{}}
		for k, v := range more {
			body[k] = v
		}
		return body
	}
	var got factory.HouseRules
	f.request(t, "PUT", "/v1/rules", "operator-secret", lists(map[string]any{"adapt": "ask"}), 200, &got)
	if got.Adapt != factory.AdaptAsk {
		t.Fatalf("saved adapt %q", got.Adapt)
	}
	f.request(t, "PUT", "/v1/rules", "operator-secret", lists(map[string]any{"rules": []string{"x"}}), 200, &got)
	if got.Adapt != factory.AdaptAsk || len(got.Rules) != 1 {
		t.Fatalf("an omitted adapt changed the word: %#v", got)
	}
	got = factory.HouseRules{}
	f.request(t, "PUT", "/v1/rules", "operator-secret", lists(map[string]any{"adapt": ""}), 200, &got)
	if got.Adapt != "" {
		t.Fatalf("an empty adapt kept %q", got.Adapt)
	}
	f.request(t, "PUT", "/v1/rules", "operator-secret", lists(map[string]any{"adapt": "sometimes"}), 422, nil)
}
