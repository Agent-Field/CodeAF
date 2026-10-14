package draft

import (
	"github.com/Agent-Field/codeaf/internal/apps/expr"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// sampleNow stands in for the clock: a condition is judged by its shape, and
// check supplies the same stand-in when it renders a screen.
const sampleNow = "2026-10-13T12:00:00Z"

// computedProblem compiles a computed field the way check does: against the
// record's own fields and constants and the metadata every record has. The
// result names the file and the JSON pointer of the expression.
func computedProblem(rec *spec.RecordType, record, field, source string) error {
	pointer := "/records/" + escapePointer(record) + "/fields/" + escapePointer(field) + "/computed"
	env, err := expr.RecordEnv(sortedKeys(rec.Fields), sortedKeys(rec.Constants))
	if err != nil {
		return refuse("%s: %s: %v — rename fields that conflict with CEL built-ins", spec.AppFile, pointer, err)
	}
	if _, err := expr.Compile(env, source); err != nil {
		return refuse("%s: %s: %s: %v — use declared record fields, constants and duration literals, for example now - days(7)", spec.AppFile, pointer, field, err)
	}
	return nil
}

// whenValues builds the names a screen condition may read, the same shape
// check renders with: me, space, now, params, selection, input, and the bound
// record's fields and constants. Only the names matter to a compile.
func (l *screenLint) whenValues(screen *spec.Screen) map[string]any {
	params := map[string]any{}
	for _, name := range screen.Params {
		params[name] = "sample"
	}
	selection := map[string]any{}
	for _, id := range sortedKeys(l.lists) {
		selection[id] = map[string]any{"ids": []string{}, "count": int64(0), "id": ""}
	}
	values := map[string]any{"me": "me", "space": "", "now": sampleNow, "params": params, "selection": selection, "input": map[string]any{}}
	if rec := l.st.Records[l.screenType(screen)]; rec != nil {
		record := map[string]any{"id": "", "type": "", "version": int64(0), "fields": map[string]any{}}
		values["record"], values["row"] = record, record
		for _, name := range append(sortedKeys(rec.Fields), sortedKeys(rec.Constants)...) {
			record[name] = nil
			if _, reserved := values[name]; !reserved {
				values[name] = nil
			}
		}
	}
	return values
}

// screenType is the record type check binds a screen's conditions to: the
// first typed binding in walk order, else the type of the first list.
func (l *screenLint) screenType(screen *spec.Screen) string {
	typ := ""
	screen.Walk(func(n *spec.Node) {
		if typ == "" && n.Bind != nil && n.Bind.Type != "" {
			typ = n.Bind.Type
		}
	})
	if typ == "" {
		for _, id := range sortedKeys(l.lists) {
			return l.lists[id]
		}
	}
	return typ
}

// when compiles one node condition through the shared grammar.
func (l *screenLint) when(n *spec.Node, path string) {
	if n.When == "" {
		return
	}
	if _, _, err := expr.CompileWhen(n.When, l.values); err != nil {
		l.add(path+"/when", "%v — write a boolean CEL expression using declared targets", expr.Diagnostic(n.When, err))
	}
}
