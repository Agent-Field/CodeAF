package main

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// scope is what a node knows about the record type it shows.
type scope struct {
	file  string
	lists map[string]string // list id → record type, for $selection.<id>
}

func (c *checker) checkScreen(id string, s *spec.Screen) {
	sc := scope{file: "screens/" + id + ".json", lists: map[string]string{}}
	s.Walk(func(n *spec.Node) {
		if n.ID != "" && n.Bind != nil && n.Bind.Records != "" {
			sc.lists[n.ID] = n.Bind.Records
		}
	})
	c.checkNav(sc, s)
	c.walkTyped(sc, s.Nav, "")
	c.walkTyped(sc, s.Body, "")
}

func (c *checker) checkNav(sc scope, s *spec.Screen) {
	if s.Nav == nil || s.Nav.Part != "SectionTabs" {
		c.fail(sc.file, "nav must be one SectionTabs")
		return
	}
	n := 0
	s.Walk(func(node *spec.Node) {
		if node.Part == "SectionTabs" {
			n++
		}
	})
	if n != 1 {
		c.fail(sc.file, "screen has %d SectionTabs, want exactly one", n)
	}
}

// walkTyped visits n under the record type its nearest bind established.
func (c *checker) walkTyped(sc scope, n *spec.Node, inherited string) {
	if n == nil {
		return
	}
	typ := c.nodeType(sc, n, inherited)
	c.checkNode(sc, n, typ)
	for _, child := range n.Children {
		c.walkTyped(sc, child, typ)
	}
	for _, name := range sortedKeys(n.Slots) {
		c.walkTyped(sc, n.Slots[name], typ)
	}
}

func (c *checker) nodeType(sc scope, n *spec.Node, inherited string) string {
	b := n.Bind
	switch {
	case b == nil:
		if t, _ := n.Props["type"].(string); t != "" && n.Part == "Form" {
			return t
		}
		return inherited
	case b.Records != "":
		return b.Records
	case b.Record != "" && b.Type != "":
		return b.Type
	case b.Record != "":
		return sc.lists[selectionList(b.Record)]
	}
	return inherited
}

// selectionList extracts "deals" from "$selection.deals.id".
func selectionList(ref string) string {
	parts := strings.Split(ref, ".")
	if len(parts) >= 3 && parts[0] == "$selection" {
		return parts[1]
	}
	return ""
}

func (c *checker) checkNode(sc scope, n *spec.Node, typ string) {
	part, ok := c.parts[n.Part]
	if !ok {
		c.fail(sc.file, "part %q is not in the kit", n.Part)
		return
	}
	c.checkLabel(sc, n, part)
	c.checkBind(sc, n)
	c.checkProps(sc, n, part, typ)
	c.noteCoverage(n, typ)
	for _, event := range sortedKeys(n.On) {
		for _, a := range n.On[event] {
			c.checkAction(sc, n, a, typ)
		}
	}
}

func (c *checker) checkBind(sc scope, n *spec.Node) {
	b := n.Bind
	if b == nil {
		return
	}
	for _, t := range []string{b.Records, b.Type} {
		if t != "" && !c.hasType(t) {
			c.fail(sc.file, "%s binds unknown record type %q", n.Part, t)
		}
	}
	if b.Run != "" {
		c.requireRun(sc.file, b.Run)
	}
	if b.Runs != "" {
		c.requireRun(sc.file, b.Runs)
	}
}

func (c *checker) requireRun(file, id string) {
	if _, ok := c.b.Runs[id]; !ok {
		c.fail(file, "run %q does not exist", id)
	}
}

func (c *checker) checkProps(sc scope, n *spec.Node, part *spec.Part, typ string) {
	for _, name := range sortedKeys(n.Props) {
		prop, ok := part.Props[name]
		if !ok {
			c.fail(sc.file, "%s has no prop %q", n.Part, name)
			continue
		}
		c.checkFieldProp(sc, n, name, prop, typ)
	}
	for name, prop := range part.Props {
		if _, set := n.Props[name]; prop.Required && !set {
			c.fail(sc.file, "%s needs prop %q", n.Part, name)
		}
	}
}

func (c *checker) checkFieldProp(sc scope, n *spec.Node, name string, prop *spec.Prop, typ string) {
	switch prop.Type {
	case "field", "fields":
		for _, f := range fieldNames(n.Props[name]) {
			c.requireField(sc.file, n.Part, typ, f)
		}
	case "commands":
		for _, id := range fieldNames(n.Props[name]) {
			if !c.hasCommand(id) {
				c.fail(sc.file, "%s action %q is not a command in app.json", n.Part, id)
			}
		}
	}
	if prop.Type == "enum" && len(prop.Values) > 0 {
		if v, ok := n.Props[name].(string); ok && !contains(prop.Values, v) {
			c.fail(sc.file, "%s prop %s=%q, want one of %s", n.Part, name, v, strings.Join(prop.Values, "|"))
		}
	}
}

func (c *checker) requireField(file, part, typ, field string) {
	if typ == "" {
		c.fail(file, "%s names field %q but is bound to no record type", part, field)
		return
	}
	if !c.hasField(typ, field) {
		c.fail(file, "%s: record %s has no field %q", part, typ, field)
	}
}

func (c *checker) hasCommand(id string) bool {
	for _, cmd := range c.b.App.Commands {
		if cmd.ID == id {
			return true
		}
	}
	return false
}

func fieldNames(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func (c *checker) checkAction(sc scope, n *spec.Node, a spec.Action, typ string) {
	switch a.Verb() {
	case spec.VerbOpen:
		var screen string
		_ = json.Unmarshal(a[spec.VerbOpen], &screen)
		if _, ok := c.b.Screens[screen]; !ok {
			c.fail(sc.file, "open %q: no such screen", screen)
		}
	case spec.VerbRunsStart:
		var run string
		_ = json.Unmarshal(a[spec.VerbRunsStart], &run)
		c.requireRun(sc.file, run)
	case spec.VerbRecordsCreate:
		c.checkCreate(sc, a)
	case spec.VerbRecordsUpdate:
		c.checkSet(sc, spec.VerbRecordsUpdate, typ, a)
	}
}

type setBody struct {
	Type string         `json:"type"`
	Set  map[string]any `json:"set"`
}

func (c *checker) checkCreate(sc scope, a spec.Action) {
	var body setBody
	_ = json.Unmarshal(a[spec.VerbRecordsCreate], &body)
	if !c.hasType(body.Type) {
		c.fail(sc.file, "records.create: unknown record type %q", body.Type)
		return
	}
	c.cover[body.Type].create = true
	for _, f := range sortedKeys(body.Set) {
		c.requireField(sc.file, "records.create", body.Type, f)
	}
}

func (c *checker) checkSet(sc scope, verb, typ string, a spec.Action) {
	var body setBody
	_ = json.Unmarshal(a[verb], &body)
	if typ == "" {
		return
	}
	if cov := c.cover[typ]; cov != nil {
		cov.update = true
	}
	for _, f := range sortedKeys(body.Set) {
		c.requireField(sc.file, verb, typ, f)
	}
}

// checkLabel enforces kit/label. The golden CRM leaves a RecordPage unlabelled,
// so a missing label on a data part is a note until Check settles the rule.
func (c *checker) checkLabel(sc scope, n *spec.Node, part *spec.Part) {
	if !part.Interactive || n.Label != "" || n.Part == "SectionTabs" {
		return
	}
	if part.Kind == spec.KindData {
		c.note("%s: %s has no label", sc.file, n.Part)
		return
	}
	c.fail(sc.file, "interactive part %s has no label", n.Part)
}
