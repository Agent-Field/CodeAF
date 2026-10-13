package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// catalogText renders the kit as one line per part, the form the prompts
// carry: name, events, children, bindings, slots and typed props.
func catalogText(kit *spec.Kit) string {
	lines := make([]string, 0, len(kit.Parts))
	for _, part := range kit.Parts {
		lines = append(lines, partLine(part))
	}
	return strings.Join(lines, "\n")
}

func partLine(p *spec.Part) string {
	var b strings.Builder
	b.WriteString(p.Name)
	if p.Interactive {
		b.WriteString("*")
	}
	rule, names := p.ChildRule()
	if rule == "list" {
		rule = strings.Join(names, "|")
	}
	fmt.Fprintf(&b, " · children:%s", rule)
	appendList(&b, "bind", p.Bind)
	appendList(&b, "slots", p.Slots)
	appendList(&b, "events", p.Events)
	if props := propText(p.Props); props != "" {
		b.WriteString(" · props: " + props)
	}
	return b.String()
}

func appendList(b *strings.Builder, label string, items []string) {
	if len(items) > 0 {
		fmt.Fprintf(b, " · %s:%s", label, strings.Join(items, "|"))
	}
}

func propText(props map[string]*spec.Prop) string {
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		p := props[name]
		kind := p.Type
		if len(p.Values) > 0 {
			kind = strings.Join(p.Values, "|")
		}
		if p.Required {
			kind += "!"
		}
		parts = append(parts, name+":"+kind)
	}
	return strings.Join(parts, ", ")
}

// schemasText is the app, screen and run schemas, minified.
func schemasText() (string, error) {
	var b strings.Builder
	for _, url := range []string{spec.AppSchemaURL, spec.ScreenSchemaURL, spec.RunSchemaURL} {
		raw, err := spec.SchemaJSON(url)
		if err != nil {
			return "", err
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, raw); err != nil {
			return "", err
		}
		b.WriteString(compact.String() + "\n")
	}
	return b.String(), nil
}
