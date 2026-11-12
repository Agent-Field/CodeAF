package appwire

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/skillpick"
)

type skillsEngine struct {
	fakeEngine
	calls int
}

func (e *skillsEngine) ListSkills(string) ([]skillpick.Row, error) { return []skillpick.Row{}, nil }
func (e *skillsEngine) SetSkill(p SkillSetParams) (SkillState, error) {
	e.calls++
	return SkillState{Name: p.Name, On: p.On}, nil
}

func TestSkillsMethodsValidateAndAdvertise(t *testing.T) {
	engine := &skillsEngine{}
	for _, name := range []string{"skills.list", "skills.set"} {
		_, err := methods[name].call(&fakeEngine{}, json.RawMessage(`{"agent":"a","name":"x","on":true}`))
		if wireCode(t, err) != CodeUnknownMethod {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, raw := range []string{`{}`, `{"agent":"a","name":"x"}`, `{"agent":"a","name":"x","on":null}`, `{"agent":"a","name":"x","on":1}`, `{"agent":"a","name":"x\ny","on":true}`, `{"agent":"a","name":"x","on":true,"toggle":true}`} {
		_, err := methods["skills.set"].call(engine, json.RawMessage(raw))
		if wireCode(t, err) != CodeBadParams {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	if engine.calls != 0 {
		t.Fatal("invalid calls reached engine")
	}
	for _, on := range []bool{true, false} {
		raw, _ := json.Marshal(SkillSetParams{Agent: "a", Name: "x", On: on})
		got, err := methods["skills.set"].call(engine, raw)
		if err != nil || got.(*SkillState).On != on {
			t.Fatalf("set: %v %v", got, err)
		}
	}
	w := startGraphWire(t, engine)
	hello := features(w.next())
	for _, want := range []string{"skills.list", "skills.set"} {
		found := false
		for _, feature := range hello {
			found = found || feature == want
		}
		if !found {
			t.Errorf("hello lacks %s", want)
		}
	}
	w.send(`{"id":1,"method":"skills.list","params":{"agent":"a"}}`)
	got := w.next()
	raw, _ := json.Marshal(got["result"])
	if string(raw) != "[]" {
		t.Fatalf("empty list: %s", raw)
	}
	schema, err := SchemaJSON()
	if err != nil || !json.Valid(schema) {
		t.Fatalf("schema: %v", err)
	}
	var doc struct {
		Methods map[string]json.RawMessage `json:"methods"`
		Events  map[string]json.RawMessage `json:"events"`
		Defs    map[string]struct {
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schema, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Methods["skills.list"] == nil || doc.Methods["skills.set"] == nil || doc.Events[EventSkillsChanged] == nil {
		t.Fatal("skills schema missing methods or event")
	}
	if doc.Defs["in.SkillSetParams"].Properties["on"].Type != "boolean" {
		t.Fatal("on must be a non-null boolean in the schema")
	}
	if len(doc.Defs["Row"].Properties) != 4 {
		t.Fatalf("shelf exposes internal fields: %+v", doc.Defs["Row"])
	}
	if EventShape(EventSkillsChanged) == nil {
		t.Fatal("skills event missing")
	}
}
