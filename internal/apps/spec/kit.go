package spec

import (
	"encoding/json"
	"slices"
	"sync"
)

// PartKind groups kit parts.
type PartKind string

const (
	KindPrimitive PartKind = "primitive"
	KindPattern   PartKind = "pattern"
	KindData      PartKind = "data"
)

// Kit is the wave-1 kit catalog (kit.json). The desk renders exactly these
// parts; Check rejects any other part name.
type Kit struct {
	Kit       int               `json:"kit"`
	Wave      int               `json:"wave"`
	PropTypes map[string]string `json:"propTypes"`
	Parts     []*Part           `json:"parts"`
	byName    map[string]*Part
}

// Part describes one kit part: allowed props, binding kinds, events,
// children and slots.
type Part struct {
	Name        string           `json:"name"`
	Kind        PartKind         `json:"kind"`
	Interactive bool             `json:"interactive"` // kit/label: needs a label
	NavOnly     bool             `json:"navOnly,omitempty"`
	Events      []string         `json:"events"`
	Children    json.RawMessage  `json:"children"` // "none" | "any" | ["Part", ...]
	Bind        []string         `json:"bind,omitempty"`
	Slots       []string         `json:"slots,omitempty"`
	Props       map[string]*Prop `json:"props"`
}

// Prop is one allowed prop.
type Prop struct {
	Type     string   `json:"type"`
	Values   []string `json:"values,omitempty"`
	Required bool     `json:"required,omitempty"`
}

// ChildRule reports whether the part takes children: "none", "any", or a
// list of allowed part names (returned in names).
func (p *Part) ChildRule() (rule string, names []string) {
	if json.Unmarshal(p.Children, &rule) == nil {
		return rule, nil
	}
	_ = json.Unmarshal(p.Children, &names)
	return "list", names
}

var (
	kitOnce sync.Once
	kit     *Kit
	kitErr  error
)

// LoadKit returns the embedded kit catalog.
func LoadKit() (*Kit, error) {
	kitOnce.Do(func() {
		data, err := embedded.ReadFile("kit.json")
		if err != nil {
			kitErr = err
			return
		}
		k := &Kit{}
		if err := json.Unmarshal(data, k); err != nil {
			kitErr = err
			return
		}
		k.byName = make(map[string]*Part, len(k.Parts))
		for _, part := range k.Parts {
			k.byName[part.Name] = part
		}
		kit = k
	})
	return kit, kitErr
}

// KitJSON returns kit.json as embedded, for serving to the desk.
func KitJSON() ([]byte, error) { return embedded.ReadFile("kit.json") }

// Part returns the named part, or nil when the kit has no such part.
func (k *Kit) Part(name string) *Part { return k.byName[name] }

// toneValues are the tokens a tone prop may hold.
var toneValues = []string{"neutral", "accent", "positive", "caution", "critical"}

// Accepts reports whether a decoded JSON value has the type this prop declares.
func (p *Prop) Accepts(value any) bool {
	text, isText := value.(string)
	switch p.Type {
	case "number":
		_, ok := value.(float64)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "strings", "fields", "commands":
		_, ok := StringList(value)
		return ok
	case "enum":
		return isText && slices.Contains(p.Values, text)
	case "tone":
		return isText && slices.Contains(toneValues, text)
	case "value":
		return true
	}
	return isText
}

// StringList reads a decoded JSON array of strings; ok is false for anything else.
func StringList(value any) (list []string, ok bool) {
	items, isList := value.([]any)
	if !isList {
		return nil, false
	}
	for _, item := range items {
		text, isText := item.(string)
		if !isText {
			return nil, false
		}
		list = append(list, text)
	}
	return list, true
}
