package serve

import (
	"context"
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

// START ON ITS OWN AND ITS LABELS ARE KEPT UNLESS SENT, like the adapt word.
func TestFactoryRulesRouteKeepsStartOnItsOwnUnlessItIsSent(t *testing.T) {
	f := peopleV2Setup(t, "team")
	base := map[string]any{"rules": []string{}, "ask_first": []string{}, "never": []string{}}
	on := map[string]any{"rules": []string{}, "ask_first": []string{}, "never": []string{}, "start_on_its_own": true, "start_labels": []string{" codeaf ", "Codeaf"}}
	var got factory.HouseRules
	f.request(t, "PUT", "/v1/rules", "operator-secret", on, 200, &got)
	if !got.StartOnItsOwn || !reflect.DeepEqual(got.StartLabels, []string{"codeaf"}) {
		t.Fatalf("saved %#v", got)
	}
	got = factory.HouseRules{}
	f.request(t, "PUT", "/v1/rules", "operator-secret", base, 200, &got)
	if !got.StartOnItsOwn || len(got.StartLabels) != 1 {
		t.Fatalf("an omitted switch changed it: %#v", got)
	}
	got = factory.HouseRules{}
	off := map[string]any{"rules": []string{}, "ask_first": []string{}, "never": []string{}, "start_on_its_own": false, "start_labels": []string{}}
	f.request(t, "PUT", "/v1/rules", "operator-secret", off, 200, &got)
	if got.StartOnItsOwn || len(got.StartLabels) != 0 {
		t.Fatalf("turning it off kept %#v", got)
	}
}

// A WRITE THAT SAYS WHAT IT READ IS REFUSED WHEN THE RULES MOVED: two views of
// the rules cannot undo each other's change. The refusal is 409 with the phase
// marker and writes nothing; a write whose expect matches is saved.
func TestFactoryRulesRouteRefusesAWriteFromAStaleRead(t *testing.T) {
	f := peopleV2Setup(t, "team")
	var read factory.HouseRules
	f.request(t, "GET", "/v1/rules", "operator-secret", nil, 200, &read)
	var saved factory.HouseRules
	f.request(t, "PUT", "/v1/rules", "operator-secret", map[string]any{"rules": []string{"first"}, "ask_first": []string{}, "never": []string{}, "start_on_its_own": true, "expect": read}, 200, &saved)
	stale := map[string]any{"rules": []string{}, "ask_first": []string{}, "never": []string{}, "adapt": "ask", "expect": read}
	w := identityRequest(f.h, "PUT", "/v1/rules", rulesBody(t, stale), "operator-secret", nil)
	if w.Code != 409 || w.Header().Get(api.PhaseHeader) != api.PhasePreMutation {
		t.Fatalf("a stale write answered %d with phase %q: %s", w.Code, w.Header().Get(api.PhaseHeader), w.Body)
	}
	if now, _ := f.st.HouseRules(); !reflect.DeepEqual(now, saved) {
		t.Fatalf("a stale write changed the rules: %#v", now)
	}
	stale["expect"] = saved
	var got factory.HouseRules
	f.request(t, "PUT", "/v1/rules", "operator-secret", stale, 200, &got)
	if got.Adapt != factory.AdaptAsk || !got.StartOnItsOwn {
		t.Fatalf("a fresh write saved %#v", got)
	}
}

// THE ADAPT WORD "adapt" READS AS NONE when a write's read is compared: a
// client that shows the two as one choice is not refused as stale forever.
func TestFactoryRulesRouteReadsAdaptAndNoneAsTheSameRead(t *testing.T) {
	f := peopleV2Setup(t, "team")
	var saved factory.HouseRules
	f.request(t, "PUT", "/v1/rules", "operator-secret", map[string]any{"rules": []string{}, "ask_first": []string{}, "never": []string{}, "adapt": "adapt"}, 200, &saved)
	read := saved
	read.Adapt = ""
	var got factory.HouseRules
	f.request(t, "PUT", "/v1/rules", "operator-secret", map[string]any{"rules": []string{"x"}, "ask_first": []string{}, "never": []string{}, "expect": read}, 200, &got)
	if len(got.Rules) != 1 {
		t.Fatalf("saved %#v", got)
	}
}

// THE RULES SAY WHO LAST CHANGED THEM AND WHEN: rules that were never written
// carry neither field; after a save, the read and the save's own answer name
// the person who saved them and the time of that save.
func TestFactoryRulesRouteSaysWhoChangedThemLastAndWhen(t *testing.T) {
	f := peopleV2Setup(t, "team")
	var fresh map[string]any
	f.request(t, "GET", "/v1/rules", "operator-secret", nil, 200, &fresh)
	for _, k := range []string{"changed_by", "changed_at"} {
		if _, ok := fresh[k]; ok {
			t.Fatalf("rules never written carry %s: %v", k, fresh)
		}
	}
	body := map[string]any{"rules": []string{"keep the diff small"}, "ask_first": []string{}, "never": []string{}}
	var saved, read map[string]any
	f.request(t, "PUT", "/v1/rules", "operator-secret", body, 200, &saved)
	f.request(t, "GET", "/v1/rules", "operator-secret", nil, 200, &read)
	for name, got := range map[string]map[string]any{"save": saved, "read": read} {
		if got["changed_by"] != "owner" || got["changed_at"] != "2026-10-11T12:00:00Z" {
			t.Fatalf("the %s says changed_by %v changed_at %v", name, got["changed_by"], got["changed_at"])
		}
	}
}

// A SCRIPT'S SAVE IS THE OPERATOR'S, whoever the host happens to be signed in
// to GitHub as: a host login never becomes the author of a change to the rules.
func TestFactoryRulesNameTheOperatorWhenAScriptSavesOnAHostSignedInAsSomeoneElse(t *testing.T) {
	f := peopleV2Setup(t, "team")
	f.seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
		return factory.GitHubLink{Login: "buildbot"}, nil
	}
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	body := map[string]any{"rules": []string{"keep the diff small"}, "ask_first": []string{}, "never": []string{}}
	var saved, read map[string]any
	f.request(t, "PUT", "/v1/rules", "operator-secret", body, 200, &saved)
	f.request(t, "GET", "/v1/rules", "operator-secret", nil, 200, &read)
	for name, got := range map[string]map[string]any{"save": saved, "read": read} {
		if got["changed_by"] != "owner" {
			t.Fatalf("the %s says changed_by %v, want the operator", name, got["changed_by"])
		}
	}
}
