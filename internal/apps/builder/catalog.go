package builder

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/appcmd"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

type catalogPart struct {
	Name string `json:"name"`
	json.RawMessage
}

func (p *catalogPart) UnmarshalJSON(raw []byte) error {
	var name struct{ Name string }
	if err := json.Unmarshal(raw, &name); err != nil {
		return err
	}
	p.Name = name.Name
	return p.RawMessage.UnmarshalJSON(raw)
}

// Parts reads the same live vocabulary as apps.kit. With an intent it sends
// just the matching view recipe and part, or a single named markup part.
func Parts(intent string) (json.RawMessage, error) {
	if !appcmd.ValidIntent(intent) {
		return nil, fmt.Errorf("choose one view intent or part name")
	}
	var kit struct {
		Kit        int
		Parts      []catalogPart
		Registries struct {
			Views  map[string]json.RawMessage `json:"view_types"`
			Fields map[string]json.RawMessage `json:"field_types"`
		}
	}
	if err := json.Unmarshal(schema.KitJSON(), &kit); err != nil {
		return nil, err
	}
	if intent == "" || intent == "add-view" {
		view := struct {
			Kit         int                      `json:"kit"`
			CommonTypes schema.CommonTypeLibrary `json:"common_types"`
			Views       []string                 `json:"views,omitempty"`
			Fields      []string                 `json:"field_types,omitempty"`
			Parts       []string                 `json:"parts"`
		}{Kit: kit.Kit, CommonTypes: schema.CommonTypes(), Views: slices.Sorted(maps.Keys(kit.Registries.Views)), Fields: slices.Sorted(maps.Keys(kit.Registries.Fields))}
		for _, part := range kit.Parts {
			view.Parts = append(view.Parts, part.Name)
		}
		return json.Marshal(view)
	}
	match := strings.TrimPrefix(intent, "add-view/")
	var recipe json.RawMessage
	if view := kit.Registries.Views[match]; view != nil {
		var descriptor struct {
			Recipe json.RawMessage
		}
		if err := json.Unmarshal(view, &descriptor); err != nil {
			return nil, err
		}
		recipe = descriptor.Recipe
		var hint struct{ Part string }
		if len(recipe) > 0 {
			if err := json.Unmarshal(recipe, &hint); err != nil {
				return nil, err
			}
			match = strings.ToLower(hint.Part)
		}
	}
	match = strings.TrimPrefix(match, "add-")
	for _, part := range kit.Parts {
		if strings.EqualFold(part.Name, match) {
			return json.Marshal(struct {
				Kit    int             `json:"kit"`
				Recipe json.RawMessage `json:"recipe,omitempty"`
				Part   json.RawMessage `json:"part"`
			}{kit.Kit, recipe, part.RawMessage})
		}
	}
	return nil, fmt.Errorf("no part found for %q. Use codeaf app parts to see the available names", intent)
}
