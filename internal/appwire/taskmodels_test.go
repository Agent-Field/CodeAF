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

// pins.auxiliary travels like the other pins, and the hello says it is served.
func TestTaskModelsPatchCarriesTheAuxiliaryPin(t *testing.T) {
	patch, err := decodeTaskModelsPatch(json.RawMessage(`{"pins":{"auxiliary":{"id":"vendor/small","provider":"openrouter"},"worker":null}}`))
	if err != nil {
		t.Fatal(err)
	}
	if ref, ok := patch.Pins[RoleAuxiliary]; !ok || ref == nil || ref.ID != "vendor/small" || ref.Provider != "openrouter" {
		t.Fatalf("pins = %+v", patch.Pins)
	}
	cleared, err := decodeTaskModelsPatch(json.RawMessage(`{"pins":{"auxiliary":null}}`))
	if ref, ok := cleared.Pins[RoleAuxiliary]; err != nil || !ok || ref != nil {
		t.Fatalf("a null auxiliary pin = %+v, %v", cleared.Pins, err)
	}
}

// taskModelsEngine serves the Task models family and nothing else of note.
type taskModelsEngine struct{ sendEngine }

func (*taskModelsEngine) TaskModels() (TaskModels, error) { return TaskModels{}, nil }
func (*taskModelsEngine) TaskModelsList(TaskModelsListParams) (TaskModelsPage, error) {
	return TaskModelsPage{}, nil
}
func (*taskModelsEngine) SetTaskModels(TaskModelsPatch) (TaskModels, error) { return TaskModels{}, nil }

func TestTheHelloSaysTheAuxiliaryPinOnlyWhereTaskModelsAreServed(t *testing.T) {
	if h := withTaskModelsAuxiliaryFeature(Hello{}, &sendEngine{}); len(h.Features) != 0 {
		t.Fatalf("an engine without task models said %v", h.Features)
	}
	h := withTaskModelsAuxiliaryFeature(Hello{}, &taskModelsEngine{})
	if len(h.Features) != 1 || h.Features[0] != FeatureTaskModelsAuxiliary {
		t.Fatalf("features = %v", h.Features)
	}
}

// The page AI's four roles travel as pins like the others, null for "follow
// the family", and the hello says the engine serves them.
func TestTaskModelsPatchCarriesTheInsightPins(t *testing.T) {
	patch, err := decodeTaskModelsPatch(json.RawMessage(`{"pins":{"insight":{"id":"deepseek/deepseek-v4.1-flash","provider":"openrouter"},"insight_notes":{"id":"vendor/notes"},"insight_ask":null,"insight_themes":null}}`))
	if err != nil {
		t.Fatal(err)
	}
	if ref := patch.Pins[RoleInsight]; ref == nil || ref.ID != "deepseek/deepseek-v4.1-flash" || ref.Provider != "openrouter" {
		t.Fatalf("pins = %+v", patch.Pins)
	}
	if ref := patch.Pins[RoleInsightNotes]; ref == nil || ref.ID != "vendor/notes" {
		t.Fatalf("pins = %+v", patch.Pins)
	}
	for _, role := range []string{RoleInsightAsk, RoleInsightThemes} {
		if ref, ok := patch.Pins[role]; !ok || ref != nil {
			t.Fatalf("a null %s pin = %+v", role, patch.Pins)
		}
	}
	more, err := decodeTaskModelsPatch(json.RawMessage(`{"pins":{"insight_pipeline":{"id":"vendor/p"},"insight_views":null,"insight_factory_read":null,"insight_factory_scout":{"id":"vendor/s"}}}`))
	if err != nil || more.Pins["insight_pipeline"] == nil || more.Pins["insight_factory_scout"] == nil || len(more.Pins) != 4 {
		t.Fatalf("the page roles' pins = %+v, %v", more.Pins, err)
	}
	if _, err := decodeTaskModelsPatch(json.RawMessage(`{"pins":{"insight_other":null}}`)); err == nil {
		t.Fatal("an unknown insight role was accepted")
	}
}

func TestTheHelloSaysTheInsightRolesOnlyWhereTaskModelsAreServed(t *testing.T) {
	if h := withTaskModelsInsightFeature(Hello{}, &sendEngine{}); len(h.Features) != 0 {
		t.Fatalf("an engine without task models said %v", h.Features)
	}
	h := withTaskModelsInsightFeature(Hello{}, &taskModelsEngine{})
	if len(h.Features) != 1 || h.Features[0] != "settings.models.insight" {
		t.Fatalf("features = %v", h.Features)
	}
}

// An insight role's row says what an unpinned role runs on: the engine's
// default for insight, or that an override follows insight.
func TestTheInsightRowsTravelWithTheirDefaultAndFamily(t *testing.T) {
	roles := TaskModelRoles{
		Insight:      &TaskModelRole{Default: &ModelRef{ID: "deepseek/deepseek-v4.1-flash", Provider: "openrouter"}, EligibleCount: 3},
		InsightNotes: &TaskModelRole{Follows: RoleInsight, EligibleCount: 3},
	}
	raw, err := json.Marshal(roles)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"insight":{"pin":null,"warning":null,"eligible_count":3,"default":{"id":"deepseek/deepseek-v4.1-flash","provider":"openrouter"}}`,
		`"insight_notes":{"pin":null,"warning":null,"eligible_count":3,"follows":"insight"}`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("roles = %s, want %s", raw, want)
		}
	}
	if strings.Contains(string(raw), "insight_ask") || strings.Contains(string(raw), `"default":null`) {
		t.Errorf("an absent row or default was sent: %s", raw)
	}
}

// EVERY PAGE-AI ROW CARRIES ITS NAME IN THE PERSON'S WORDS, so a desk draws a
// role it has never heard of with no work of its own. An older desk ignores it.
func TestEveryInsightRoleHasALabel(t *testing.T) {
	want := map[string]string{
		"insight": "Page AI", "insight_notes": "Repo notes", "insight_ask": "Repo ask", "insight_themes": "Repo themes",
		"insight_pipeline": "Build with AI", "insight_views": "Topic views",
		"insight_factory_read": "Factory read", "insight_factory_scout": "Factory scout",
	}
	if len(InsightRoleKeys) != len(want) {
		t.Fatalf("InsightRoleKeys = %v", InsightRoleKeys)
	}
	for _, key := range InsightRoleKeys {
		if got := InsightLabel(key); got != want[key] {
			t.Errorf("InsightLabel(%q) = %q, want %q", key, got, want[key])
		}
	}
	raw, err := json.Marshal(TaskModelRole{Label: "Build with AI", Follows: RoleInsight})
	if err != nil || !strings.Contains(string(raw), `"label":"Build with AI"`) {
		t.Fatalf("a labelled row = %s, %v", raw, err)
	}
	if raw, _ := json.Marshal(TaskModelRole{}); strings.Contains(string(raw), "label") {
		t.Fatalf("an unlabelled row sends a label: %s", raw)
	}
}
