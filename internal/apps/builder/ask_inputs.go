package builder

import (
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// inputRef is a set value that is exactly one $input.<name>.
var inputRef = regexp.MustCompile(`^\$input\.([A-Za-z_][A-Za-z0-9_]*)$`)

// formOnlyTypes are field types an ask dialog cannot collect; the write
// prompt says they are edited in a Form.
var formOnlyTypes = map[spec.FieldType]bool{
	spec.FieldRef: true, spec.FieldRefs: true, spec.FieldPerson: true, spec.FieldPeople: true,
	spec.FieldFile: true, spec.FieldFiles: true,
}

// askEvents are the events whose actions may open an ask dialog; a change
// event carries its own value, so it is left to Check.
var askEvents = map[string]bool{"click": true, "submit": true}

// fillAskInputs makes every records.create/update outside a Form ask for each
// $input.<name> its set maps: the field's type is added to ask, and a
// Form-only field (ref, person, file) is left for the Form instead. It returns
// the screens it changed.
func fillAskInputs(files spec.Files) []string {
	app, ok := decodeObject(files[spec.AppFile])
	if !ok {
		return nil
	}
	records, _ := app["records"].(map[string]any)
	var changed []string
	for _, path := range files.Paths() {
		if !strings.HasPrefix(path, "screens/") {
			continue
		}
		screen, ok := decodeObject(files[path])
		if !ok {
			continue
		}
		edited := false
		walkNodes(screen, func(node map[string]any) {
			edited = askNode(node, records) || edited
		})
		if edited && encodeInto(files, path, screen) {
			changed = append(changed, path)
		}
	}
	return changed
}

func askNode(node map[string]any, records map[string]any) bool {
	if part, _ := node["part"].(string); part == "Form" {
		return false
	}
	on, _ := node["on"].(map[string]any)
	changed := false
	for event, raw := range on {
		if !askEvents[event] {
			continue
		}
		actions, _ := raw.([]any)
		for _, a := range actions {
			action, _ := a.(map[string]any)
			changed = askAction(action, nodeRecordType(node), records) || changed
		}
	}
	return changed
}

func askAction(action map[string]any, typ string, records map[string]any) bool {
	changed := false
	for _, verb := range []string{spec.VerbRecordsCreate, spec.VerbRecordsUpdate} {
		args, _ := action[verb].(map[string]any)
		set, _ := args["set"].(map[string]any)
		if set == nil {
			continue
		}
		if target, ok := args["type"].(string); ok {
			typ = target
		}
		ask, _ := action["ask"].(map[string]any)
		if ask == nil {
			ask = map[string]any{}
		}
		for field, value := range set {
			m := inputRef.FindStringSubmatch(stringOf(value))
			if m == nil || ask[m[1]] != nil {
				continue
			}
			fieldType := recordFieldType(records, typ, field)
			switch {
			case fieldType == "":
				continue
			case formOnlyTypes[fieldType]:
				delete(set, field)
			default:
				ask[m[1]] = string(fieldType)
			}
			changed = true
		}
		if len(ask) > 0 {
			action["ask"] = ask
		}
	}
	return changed
}

func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

// nodeRecordType is the record type a node is bound to, if it names one.
func nodeRecordType(node map[string]any) string {
	for _, key := range []string{"bind", "props"} {
		obj, _ := node[key].(map[string]any)
		for _, name := range []string{"type", "records"} {
			if typ, ok := obj[name].(string); ok && typ != "" {
				return typ
			}
		}
	}
	return ""
}

func recordFieldType(records map[string]any, typ, field string) spec.FieldType {
	record, _ := records[typ].(map[string]any)
	fields, _ := record["fields"].(map[string]any)
	def, _ := fields[field].(map[string]any)
	fieldType, _ := def["type"].(string)
	return spec.FieldType(fieldType)
}
