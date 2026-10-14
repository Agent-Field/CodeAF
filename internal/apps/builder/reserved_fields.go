package builder

import (
	"regexp"
	"sort"
	"strings"

	"github.com/google/cel-go/cel"

	"github.com/Agent-Field/codeaf/internal/apps/expr"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// safePrefix starts the id a reserved field id is renamed to: type -> item_type.
const safePrefix = "item_"

// fieldKeyed are the object keys whose own keys are field ids.
var fieldKeyed = map[string]bool{"fields": true, "set": true, "ask": true}

// expressionKeyed are the keys whose text is a CEL expression over bare fields.
var expressionKeyed = map[string]bool{"computed": true, "when": true}

// reservedByGrammar reports whether a field id cannot be declared beside the
// expression grammar's own names (type, now, ...). It asks the grammar itself,
// so the set is never copied here.
func reservedByGrammar(id string) bool {
	env, err := expr.NewEnv(cel.Variable(id, cel.DynType))
	if err == nil {
		_, err = expr.Compile(env, "true")
	}
	return err != nil
}

// renameReservedFields gives every field whose id collides with the expression
// grammar a safe id, keeps its label, and rewrites the references in app.json
// and the screens: one such id breaks every computed field on its record, and
// no repair round can find that from the expression. It returns the changed paths.
func renameReservedFields(files spec.Files) []string {
	app, ok := decodeObject(files[spec.AppFile])
	if !ok {
		return nil
	}
	renames := reservedRenames(app)
	if len(renames) == 0 {
		return nil
	}
	rewriter := newFieldRewriter(renames)
	var changed []string
	for _, path := range files.Paths() {
		if path != spec.AppFile && !strings.HasPrefix(path, "screens/") {
			continue
		}
		doc, ok := app, true
		if path != spec.AppFile {
			doc, ok = decodeObject(files[path])
		}
		if !ok {
			continue
		}
		rewriter.walk(doc, "")
		if encodeInto(files, path, doc) {
			changed = append(changed, path)
		}
	}
	return changed
}

// reservedRenames maps each reserved field id to its safe id and writes the
// old id's label on the field first, so the owner's word survives the rename.
func reservedRenames(app map[string]any) map[string]string {
	renames := map[string]string{}
	records, _ := app["records"].(map[string]any)
	for _, raw := range records {
		record, _ := raw.(map[string]any)
		fields, _ := record["fields"].(map[string]any)
		for id, rawField := range fields {
			if !reservedByGrammar(id) {
				continue
			}
			renames[id] = safePrefix + id
			if field, _ := rawField.(map[string]any); field != nil {
				label, _ := field["label"].(string)
				field["label"] = humanLabel(label, id)
			}
		}
	}
	return renames
}

// fieldRewriter rewrites one set of field ids wherever a definition names a field.
type fieldRewriter struct {
	renames map[string]string
	bare    *regexp.Regexp // a whole word, for expressions
	token   *regexp.Regexp // a query token or a sort, inside a query
	dotted  *regexp.Regexp // $input.<id>, row.<id>, record.<id> or {{<id>}}, in any text
}

func newFieldRewriter(renames map[string]string) fieldRewriter {
	ids := make([]string, 0, len(renames))
	for id := range renames {
		ids = append(ids, regexp.QuoteMeta(id))
	}
	sort.Strings(ids)
	any := "(" + strings.Join(ids, "|") + ")"
	return fieldRewriter{
		renames: renames,
		bare:    regexp.MustCompile(`\b` + any + `\b`),
		token:   regexp.MustCompile(`(^|\s|sort:-?|has:|is:)` + any + `($|[\s:<>!=~])`),
		dotted:  regexp.MustCompile(`((?:input|row|record)\.|\{\{\s*)` + any + `\b`),
	}
}

func (r fieldRewriter) walk(value any, key string) any {
	switch value := value.(type) {
	case map[string]any:
		for name, child := range value {
			value[name] = r.walk(child, name)
		}
		if fieldKeyed[key] {
			r.renameKeys(value)
		}
	case []any:
		for i, child := range value {
			value[i] = r.walk(child, key)
		}
	case string:
		return r.text(value, key)
	}
	return value
}

func (r fieldRewriter) renameKeys(obj map[string]any) {
	for old, safe := range r.renames {
		if child, ok := obj[old]; ok {
			delete(obj, old)
			obj[safe] = child
		}
	}
}

func (r fieldRewriter) text(text, key string) string {
	if safe, ok := r.renames[text]; ok {
		return safe
	}
	if expressionKeyed[key] {
		return r.bare.ReplaceAllStringFunc(text, func(id string) string { return r.renames[id] })
	}
	if key == "query" {
		text = r.token.ReplaceAllString(text, "${1}"+safePrefix+"${2}${3}")
	}
	return r.dotted.ReplaceAllString(text, "${1}"+safePrefix+"${2}")
}
