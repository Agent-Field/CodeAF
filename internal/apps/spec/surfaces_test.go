package spec

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSurfaceCatalog(t *testing.T) {
	kit, err := LoadKit()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		kind   PartKind
		events []string
		prop   string
		values []string
	}{
		{"Pages", KindPattern, []string{"pick"}, "value", nil},
		{"Canvas", KindSurface, []string{}, "frame", []string{"16:9", "board"}},
		{"Text", KindSurface, []string{}, "", nil},
		{"Grid", KindSurface, []string{}, "", nil},
		{"Timeline", KindSurface, []string{}, "view", []string{"gantt", "calendar", "shifts"}},
		{"Graph", KindSurface, []string{"open"}, "view", []string{"tree", "flow", "free"}},
		{"Map", KindSurface, []string{"open"}, "tiles", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			part := kit.Part(tt.name)
			if part == nil {
				t.Fatal("catalog entry missing")
			}
			if part.Kind != tt.kind || part.Wave != 2 || !reflect.DeepEqual(part.Bind, []string{"doc"}) || !reflect.DeepEqual(part.Events, tt.events) {
				t.Fatalf("part=%+v", part)
			}
			if tt.prop != "" {
				prop := part.Props[tt.prop]
				if prop == nil || !reflect.DeepEqual(prop.Values, tt.values) {
					t.Fatalf("prop=%+v", prop)
				}
			}
		})
	}
}

func TestSurfaceScreenBinding(t *testing.T) {
	tests := []struct {
		name, bind string
		valid      bool
	}{
		{"canvas", `{"doc":"$record.deck","surface":"canvas"}`, true},
		{"literal", `{"doc":"decks/q4","surface":"canvas"}`, true},
		{"text", `{"doc":"docs/plan","surface":"text"}`, true},
		{"grid", `{"doc":"sheets/budget","surface":"grid"}`, true},
		{"timeline", `{"doc":"plans/q4","surface":"timeline"}`, true},
		{"graph", `{"doc":"graphs/org","surface":"graph"}`, true},
		{"map", `{"doc":"places/routes","surface":"map"}`, true},
		{"empty doc", `{"doc":"","surface":"canvas"}`, false},
		{"missing surface", `{"doc":"$record.deck"}`, false},
		{"unknown surface", `{"doc":"$record.deck","surface":"editor"}`, false},
		{"mixed binding", `{"doc":"$record.deck","surface":"canvas","records":"deal"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, _ := GoldenCRM()
			files["screens/deck.json"] = []byte(`{"screen":"deck","title":"Deck","body":{"part":"Canvas","bind":` + tt.bind + `}}`)
			issues := Validate(files)
			if !tt.valid {
				if len(issues) == 0 {
					t.Fatal("invalid doc binding accepted")
				}
				return
			}
			if len(issues) != 0 {
				t.Fatal(issues)
			}
			bundle, err := Decode(files)
			if err != nil {
				t.Fatal(err)
			}
			bind := bundle.Screens["deck"].Body.Bind
			raw, err := json.Marshal(bind)
			if err != nil {
				t.Fatal(err)
			}
			var want, got any
			_ = json.Unmarshal([]byte(tt.bind), &want)
			_ = json.Unmarshal(raw, &got)
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("binding lost during decode: %s", raw)
			}
		})
	}
}
