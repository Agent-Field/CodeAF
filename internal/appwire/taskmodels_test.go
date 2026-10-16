package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// A PATCH IS READ PART BY PART, each bounded, and nothing it does not know.
func TestATaskModelsPatchIsCheckedBeforeTheEngineSeesIt(t *testing.T) {
	patch, err := decodeTaskModelsPatch(json.RawMessage(`{"mode":"one","one_model":{"id":"z-ai/glm-5.3","provider":"OpenRouter"},
		"pins":{"checker":null,"worker":{"id":"deepseek/deepseek-v4-flash"}},
		"allowed":[{"model":{"id":"z-ai/glm-5.3","provider":"openrouter"},"allowed":false},{"model":{"id":"z-ai/glm-5.3","provider":"z-ai"},"allowed":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if *patch.Mode != TaskModelsOne || patch.OneModel.Provider != "openrouter" || len(patch.Allowed) != 2 {
		t.Fatalf("patch %+v", patch)
	}
	if pin, named := patch.Pins[RoleChecker]; !named || pin != nil {
		t.Errorf("checker sent as null must read as Auto, got %+v %v", pin, named)
	}
	if _, named := patch.Pins[RolePlanner]; named {
		t.Error("a role left out must be left alone")
	}
	for raw, want := range map[string]string{
		`{}`:                         "needs mode",
		`{"mode":"best"}`:            "mode is auto or one",
		`{"budget_usd":3}`:           "has no budget_usd",
		`{"pins":{"crew":null}}`:     "not a role",
		`{"one_model":null}`:         "one_model is",
		`{"one_model":{"id":"a b"}}`: "no spaces",
		`{"one_model":{"id":"a@b"}}`: "not after @",
		`{"pins":{"worker":{"id":"` + strings.Repeat("x", ModelRefIDMax+1) + `"}}}`:                                                "at most",
		`{"allowed":[{"model":{"id":"a/b"},"allowed":true}]}`:                                                                      "needs its provider",
		`{"allowed":[{"model":{"id":"a/b","provider":"p"},"allowed":true},{"model":{"id":"a/b","provider":"P"},"allowed":false}]}`: "twice",
		`{"one_model":{"id":"a/b","provider":"bad provider!"}}`:                                                                    "not a provider id",
	} {
		_, err := decodeTaskModelsPatch(json.RawMessage(raw))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", raw, err, want)
		}
		if e, ok := err.(*Error); ok && e.Code != CodeBadParams {
			t.Errorf("%s: code %s", raw, e.Code)
		}
	}
	rows := make([]string, TaskModelsAllowedMax+1)
	for i := range rows {
		rows[i] = `{"model":{"id":"m/` + strings.Repeat("x", i%7+1) + `","provider":"p` + string(rune('a'+i%26)) + `"},"allowed":true}`
	}
	if _, err := decodeTaskModelsPatch(json.RawMessage(`{"allowed":[` + strings.Join(rows, ",") + `]}`)); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Errorf("an oversize allowed list: %v", err)
	}
}

// A LIST REQUEST IS BOUNDED: a default page, a ceiling, a query of one line.
func TestATaskModelsListIsBounded(t *testing.T) {
	p, err := checkTaskModelsList(TaskModelsListParams{})
	if err != nil || p.Limit != TaskModelsPageDefault {
		t.Fatalf("default: %+v %v", p, err)
	}
	for _, bad := range []TaskModelsListParams{
		{Limit: TaskModelsPageMax + 1}, {Limit: -1}, {Q: strings.Repeat("q", TaskModelsQueryMax+1)},
		{Q: "a\nb"}, {Role: "crew"}, {Provider: "Open Router"}, {Cursor: strings.Repeat("c", 513)},
	} {
		if _, err := checkTaskModelsList(bad); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
}

// AN ENGINE WITHOUT TASK MODELS ANSWERS unknown_method, as for competence.
func TestAnEngineWithoutTaskModelsSaysSo(t *testing.T) {
	w := startWire(t, &fakeEngine{})
	w.next()
	w.send(`{"id":1,"method":"settings.models.get","params":{}}`)
	got := w.next()
	if e, _ := got["error"].(map[string]any); e == nil || e["code"] != CodeUnknownMethod {
		t.Fatalf("got %v", got)
	}
}
