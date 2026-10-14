package draft

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// ownerID is the member name a query is parsed for; only the shape of the
// query matters here, never the value.
const ownerID = "me"

// issueSink collects findings of one document as "<file>: <pointer>: <text>".
type issueSink struct {
	file  string
	lines []string
}

func (s *issueSink) add(path, format string, args ...any) {
	s.lines = append(s.lines, fmt.Sprintf("%s: %s: %s", s.file, path, fmt.Sprintf(format, args...)))
}

// screenLint judges one screen against the draft's records alone. It never
// looks at another screen or run, so a broken sibling cannot block this one.
type screenLint struct {
	issueSink
	st    State
	kit   *spec.Kit
	lists map[string]string // list id → record type, as the screen declares it
}

// lintScreen returns the findings of one decoded screen.
func lintScreen(st State, id string, screen *spec.Screen) []string {
	kit, err := spec.LoadKit()
	if err != nil {
		return []string{"screens/" + id + ".json: the kit catalog cannot be read: " + err.Error()}
	}
	l := &screenLint{issueSink: issueSink{file: "screens/" + id + ".json"}, st: st, kit: kit, lists: map[string]string{}}
	screen.Walk(func(n *spec.Node) {
		if n.Bind != nil && n.Bind.Records != "" && n.ID != "" {
			l.lists[n.ID] = n.Bind.Records
		}
	})
	l.node(screen.Nav, "/nav", "")
	l.node(screen.Body, "/body", "")
	return l.lines
}

func (l *screenLint) node(n *spec.Node, path, inherited string) {
	if n == nil {
		return
	}
	typ := l.nodeType(n, inherited)
	if part := l.kit.Part(n.Part); part == nil {
		l.add(path+"/part", "unknown kit part %q — pick a part from: codeaf app catalog", n.Part)
	} else {
		l.props(part, n, typ, path)
		l.bindKind(part, n, path)
	}
	l.bind(n.Bind, path+"/bind")
	l.actions(n, typ, path)
	for i, child := range n.Children {
		l.node(child, fmt.Sprintf("%s/children/%d", path, i), typ)
	}
	for _, name := range sortedKeys(n.Slots) {
		l.node(n.Slots[name], path+"/slots/"+escapePointer(name), typ)
	}
}

// nodeType is the record type a node's fields belong to: the list it acts on,
// its own binding, the list a selection names, or the nearest ancestor's.
func (l *screenLint) nodeType(n *spec.Node, inherited string) string {
	if list, ok := n.Props["list"].(string); ok && l.lists[list] != "" {
		return l.lists[list]
	}
	switch b := n.Bind; {
	case b == nil:
		return inherited
	case b.Records != "":
		return b.Records
	case b.Type != "":
		return b.Type
	case b.Selection != "":
		return l.lists[b.Selection]
	}
	return inherited
}

func (l *screenLint) bindKind(part *spec.Part, n *spec.Node, path string) {
	if n.Bind == nil {
		return
	}
	if kind := n.Bind.Kind(); !contains(part.Bind, kind) {
		l.add(path+"/bind", "%s does not support a %s binding — allowed: %s", n.Part, kind, ids(part.Bind))
	}
}

func (l *screenLint) bind(b *spec.Binding, path string) {
	if b == nil {
		return
	}
	for _, t := range []struct{ key, typ string }{{"records", b.Records}, {"type", b.Type}} {
		if t.typ != "" && l.st.Records[t.typ] == nil {
			l.add(path+"/"+t.key, "%q is not a record of this app (records: %s) — define it first with: codeaf app record %s", t.typ, ids(sortedKeys(l.st.Records)), t.typ)
		}
	}
	if b.Record != "" && b.Type == "" {
		l.add(path+"/type", "a record binding needs a type — set type to the record type")
	}
	if b.Records != "" {
		l.query(path+"/query", b.Records, b.Query)
	}
	if b.Selection != "" && l.lists[b.Selection] == "" {
		l.add(path+"/selection", "unknown list %q — name the id of a record list in this screen", b.Selection)
	}
}

func (l *screenLint) query(path, typ, text string) {
	if err := query.Problem(l.st.Records[typ], text, ownerID); err != nil {
		l.add(path, "%v (fields of %s: %s) — %s", err, typ, fieldList(sortedKeys(l.st.Records[typ].Fields)), spec.RunQueryRule)
	}
}

func (l *screenLint) props(part *spec.Part, n *spec.Node, typ, path string) {
	for _, key := range sortedKeys(part.Props) {
		if _, ok := n.Props[key]; part.Props[key].Required && !ok {
			l.add(path+"/props/"+key, "required prop is missing — supply %s for %s", key, part.Name)
		}
	}
	for _, key := range sortedKeys(n.Props) {
		at := path + "/props/" + escapePointer(key)
		prop := part.Props[key]
		switch {
		case prop == nil:
			l.add(at, "%s has no prop %q — allowed: %s", part.Name, key, ids(sortedKeys(part.Props)))
		case !prop.Accepts(n.Props[key]):
			l.add(at, "prop %q must have type %s", key, prop.Type)
		default:
			l.propValue(prop, key, n.Props[key], typ, at)
		}
	}
}

// propValue checks the props whose meaning needs the draft: field names of
// the bound record and the sort expression.
func (l *screenLint) propValue(prop *spec.Prop, key string, value any, typ, at string) {
	switch prop.Type {
	case "field":
		l.fieldName(typ, value.(string), at)
	case "fields":
		names, _ := spec.StringList(value)
		for i, name := range names {
			l.fieldName(typ, name, fmt.Sprintf("%s/%d", at, i))
		}
	case "string":
		if key == "sort" {
			if rec := l.st.Records[typ]; rec != nil {
				if _, err := query.Expression(rec, strings.TrimPrefix(value.(string), "-")); err != nil {
					l.add(at, "%v (fields of %s: %s)", err, typ, fieldList(sortedKeys(rec.Fields)))
				}
			}
		}
	}
}

func (l *screenLint) fieldName(typ, name, at string) {
	rec := l.st.Records[typ]
	switch {
	case typ == "":
		l.add(at, "%q cannot be checked: no record type is bound here (bind the part, or an ancestor, to a record)", name)
	case rec == nil:
		// The binding that names this record type already reported it.
	case rec.Fields[name] == nil:
		l.add(at, "%q is not a field of %s (fields: %s)", name, typ, fieldList(sortedKeys(rec.Fields)))
	}
}

// actions checks the record writes a node's events make: the type and every
// field in set must exist.
func (l *screenLint) actions(n *spec.Node, typ, path string) {
	for _, event := range sortedKeys(n.On) {
		for i, action := range n.On[event] {
			lintActionWrite(&l.issueSink, l.st, action, typ, fmt.Sprintf("%s/on/%s/%d", path, escapePointer(event), i))
		}
	}
}

// lintActionWrite checks one action when it writes records; other verbs wait
// for check and done.
func lintActionWrite(sink *issueSink, st State, action spec.Action, typ, path string) {
	verb := action.Verb()
	if verb != spec.VerbRecordsCreate && verb != spec.VerbRecordsUpdate {
		return
	}
	var args struct {
		Type string         `json:"type"`
		Set  map[string]any `json:"set"`
	}
	_ = json.Unmarshal(action[verb], &args)
	checkRecordWrite(sink, st, orElse(args.Type, typ), args.Set, path+"/"+verb)
}

// checkRecordWrite is shared by screen actions and run steps: the record type
// exists and each key of set is one of its fields.
func checkRecordWrite(sink *issueSink, st State, typ string, set map[string]any, path string) {
	if typ == "" {
		return // no record is bound here; check and done judge it with the full app
	}
	rec := st.Records[typ]
	if rec == nil {
		sink.add(path, "unknown record type %q (records: %s)", typ, ids(sortedKeys(st.Records)))
		return
	}
	for _, field := range sortedKeys(set) {
		if rec.Fields[field] == nil {
			sink.add(path+"/set/"+escapePointer(field), "%q is not a field of %s (fields: %s)", field, typ, fieldList(sortedKeys(rec.Fields)))
		}
	}
}

func escapePointer(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
