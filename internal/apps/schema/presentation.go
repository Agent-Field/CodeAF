package schema

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
)

// Presentation gives record rows a title, supporting facts and a trailing value.
type Presentation struct {
	Title    string   `json:"title,omitempty"`
	Meta     []string `json:"meta,omitempty"`
	Trailing string   `json:"trailing,omitempty"`
	State    string   `json:"state,omitempty"`
	Avatar   string   `json:"avatar,omitempty"`
	Money    string   `json:"money,omitempty"`
	Link     string   `json:"link,omitempty"`
}

// recordFieldOrder preserves the author's first text field. Shared fields
// that have no local declaration follow in a deterministic order.
func recordFieldOrder(raw json.RawMessage, fields map[string]Field) []string {
	var record struct{ Fields json.RawMessage }
	_ = json.Unmarshal(raw, &record)
	decoder := json.NewDecoder(bytes.NewReader(record.Fields))
	_, _ = decoder.Token()
	var order []string
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			break
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			break
		}
		if name, ok := key.(string); ok {
			order = append(order, name)
		}
	}
	for _, name := range sortedKeys(fields) {
		if !slices.Contains(order, name) {
			order = append(order, name)
		}
	}
	return order
}

func (c *checker) recordPresentation(record RecordType, fields map[string]Field, order []string) Presentation {
	visible := func(name string) bool {
		field, ok := fields[name]
		return ok && !field.Masked && field.Blind == nil && record.Policy.fieldVisibility(name) == ""
	}
	find := func(choose func(string, Field) bool) string {
		for _, name := range order {
			if visible(name) && choose(name, fields[name]) {
				return name
			}
		}
		return ""
	}
	kind := func(want string) func(string, Field) bool {
		return func(_ string, field Field) bool { return field.Type == want }
	}
	p := Presentation{Title: record.Title}
	if !visible(p.Title) {
		p.Title = find(kind("text"))
	}
	if record.Lifecycle != nil && visible(record.Lifecycle.Field) {
		p.State = record.Lifecycle.Field
	}
	if p.State == "" {
		p.State = find(func(name string, field Field) bool {
			return field.Type == "enum" && slices.Contains([]string{"state", "stage", "status"}, name)
		})
	}
	p.Money = find(kind("money"))
	p.Link = find(kind("url"))
	p.Avatar = find(func(_ string, field Field) bool {
		return field.Type == "person" || field.Type == "ref" &&
			(field.To == "person" || c.app.Records[field.To].Common == "person")
	})
	p.Trailing = find(func(name string, field Field) bool {
		return field.Type == "rating" || field.Type == "number" &&
			(strings.Contains(name, "score") || strings.Contains(name, "rank") || name == "priority" || name == "risk")
	})
	if p.Trailing == "" {
		p.Trailing = p.Money
	}
	if p.Trailing == "" {
		p.Trailing = p.State
	}
	for _, name := range order {
		if !visible(name) || name == p.Title || name == p.Avatar {
			continue
		}
		if slices.Contains([]string{"ref", "refs", "date", "datetime"}, fields[name].Type) {
			p.Meta = append(p.Meta, name)
		}
	}
	return p
}

func (c *checker) addPresentations() error {
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal(c.def.Files[AppFile], &manifest); err != nil {
		return err
	}
	var records map[string]json.RawMessage
	if err := json.Unmarshal(manifest["records"], &records); err != nil {
		return err
	}
	changed := false
	for _, name := range sortedKeys(c.app.Records) {
		record := c.app.Records[name]
		if record.Present != nil {
			continue
		}
		fields, _ := c.typeFields(name)
		p := c.recordPresentation(record, fields, recordFieldOrder(records[name], fields))
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(records[name], &raw); err != nil {
			return err
		}
		raw["present"], _ = json.Marshal(p)
		records[name], _ = json.Marshal(raw)
		record.Present = &p
		c.app.Records[name] = record
		changed = true
	}
	if !changed {
		return nil
	}
	manifest["records"], _ = json.Marshal(records)
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err == nil {
		c.def.Files[AppFile] = append(raw, '\n')
	}
	return err
}

func (p *RecordPolicy) fieldVisibility(name string) string {
	if p == nil {
		return ""
	}
	return p.Fields[name]
}
