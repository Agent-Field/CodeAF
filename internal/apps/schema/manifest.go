package schema

import (
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Manifest describes a versioned registry entry. Schema validates its values,
// and Example gives the Builder one value that passes it.
type Manifest struct {
	ID      string          `json:"id"`
	Version int             `json:"version"`
	Doc     string          `json:"doc"`
	Example json.RawMessage `json:"example"`
	Schema  json.RawMessage `json:"schema"`
	Rules   []ManifestRule  `json:"rules,omitempty"`
}

// ManifestRule selects values for checks that need the rest of the app. An
// asterisk in Path selects each member of a list or object.
type ManifestRule struct {
	Check   string `json:"check"`
	Path    string `json:"path"`
	Options string `json:"options,omitempty"`
}

type FieldManifest struct {
	Manifest
	Storage   string   `json:"storage"`
	Operators []string `json:"operators"`
	Renderer  string   `json:"renderer"`
	Editor    string   `json:"editor"`
	AIFill    bool     `json:"ai_fill"`
	Export    string   `json:"export"`
	Tags      []string `json:"tags,omitempty"`
}

type ContentManifest struct {
	Manifest
	Filename  string   `json:"filename,omitempty"`
	Directory string   `json:"directory"`
	Extension string   `json:"extension"`
	Encoding  string   `json:"encoding"`
	PartModel string   `json:"part_model"`
	Ops       []string `json:"ops"`
	Lock      string   `json:"lock"`
	Versions  string   `json:"versions"`
	Export    []string `json:"export"`
}

type ViewManifest struct {
	Manifest
	Collection string            `json:"collection"`
	Renderers  map[string]string `json:"renderers"`
}

type EffectManifest struct {
	Manifest
	Modifiers []string `json:"modifiers,omitempty"`
	Auxiliary bool     `json:"auxiliary,omitempty"`
}

type OperationManifest struct {
	Manifest
	Outbound string `json:"outbound"`
	Fake     string `json:"fake"`
}

type ToolManifest struct {
	Manifest
	Auth       string                       `json:"auth"`
	Operations map[string]OperationManifest `json:"operations"`
}

// Registries holds the first-party entries. Each map key is an entry ID.
type Registries struct {
	Fields   map[string]FieldManifest   `json:"field_types"`
	Content  map[string]ContentManifest `json:"content_types"`
	Views    map[string]ViewManifest    `json:"view_types"`
	Actions  map[string]EffectManifest  `json:"action_kinds"`
	Steps    map[string]EffectManifest  `json:"step_kinds"`
	Triggers map[string]Manifest        `json:"trigger_sources"`
	Policies map[string]Manifest        `json:"policy_kinds"`
	Tools    map[string]ToolManifest    `json:"tools"`
}

func (r Registries) schemas() map[string]Manifest {
	out := map[string]Manifest{}
	add := func(group, name string, m Manifest) { out[group+"/"+name] = m }
	for key, v := range r.Fields {
		add("field", key, v.Manifest)
	}
	for key, v := range r.Content {
		add("content", key, v.Manifest)
	}
	for key, v := range r.Views {
		add("view", key, v.Manifest)
	}
	for key, v := range r.Actions {
		add("action", key, v.Manifest)
	}
	for key, v := range r.Steps {
		add("step", key, v.Manifest)
	}
	for key, v := range r.Triggers {
		add("trigger", key, v)
	}
	for key, v := range r.Policies {
		add("policy", key, v)
	}
	for key, v := range r.Tools {
		add("tool", key, v.Manifest)
		for op, m := range v.Operations {
			add("tool", key+"/"+op, m.Manifest)
		}
	}
	return out
}

func checkManifest(key string, m Manifest, s *jsonschema.Schema) error {
	if m.ID == "" || m.Version < 1 || m.Doc == "" {
		return fmt.Errorf("%s needs an id, version and documentation", key)
	}
	var sample any
	if err := json.Unmarshal(m.Example, &sample); err != nil {
		return fmt.Errorf("%s example: %w", key, err)
	}
	if err := s.Validate(sample); err != nil {
		return fmt.Errorf("%s example: %w", key, err)
	}
	return nil
}
