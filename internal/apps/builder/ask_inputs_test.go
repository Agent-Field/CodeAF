package builder

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestEveryInputASetMapsIsAskedOrLeftToTheForm(t *testing.T) {
	files := spec.Files{
		spec.AppFile: []byte(`{"records":{"ingredient":{"fields":{"name":{"type":"text"},"unit":{"type":"enum","values":["kg"]},"supplier":{"type":"ref","to":"supplier"}}}}}`),
		"screens/ingredients.json": []byte(`{"screen":"ingredients","body":{"part":"Stack","children":[
			{"part":"Button","label":"New ingredient","on":{"click":[{"ask":{"name":"text"},"records.create":{"type":"ingredient","set":{"name":"$input.name","unit":"$input.unit","supplier":"$input.supplier"}}}]}},
			{"part":"Form","props":{"type":"ingredient"},"on":{"submit":[{"records.update":{"id":"$selection.ingredients.id","set":{"supplier":"$input.supplier"}}}]}}]}}`),
	}
	if changed := fillAskInputs(files); len(changed) != 1 || changed[0] != "screens/ingredients.json" {
		t.Fatalf("changed = %v, want the screen", changed)
	}
	var screen struct {
		Body struct {
			Children []struct {
				On map[string][]map[string]map[string]any `json:"on"`
			} `json:"children"`
		} `json:"body"`
	}
	if err := json.Unmarshal(files["screens/ingredients.json"], &screen); err != nil {
		t.Fatal(err)
	}
	button := screen.Body.Children[0].On["click"][0]
	if button["ask"]["unit"] != "enum" || button["ask"]["supplier"] != nil {
		t.Errorf("ask = %v, want unit asked as enum and supplier left to the Form", button["ask"])
	}
	set := button["records.create"]["set"].(map[string]any)
	if _, ok := set["supplier"]; ok || set["unit"] != "$input.unit" {
		t.Errorf("set = %v, want supplier dropped and unit kept", set)
	}
	form := screen.Body.Children[1].On["submit"][0]["records.update"]["set"].(map[string]any)
	if form["supplier"] != "$input.supplier" {
		t.Errorf("a Form's own inputs were touched: %v", form)
	}
	if again := fillAskInputs(files); len(again) != 0 {
		t.Errorf("a second pass changed %v", again)
	}
}
