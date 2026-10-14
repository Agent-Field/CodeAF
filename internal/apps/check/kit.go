package check

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

type screenInfo struct {
	lists      map[string]string
	params     map[string]string
	recordType string
}

func inspect(screen *spec.Screen) screenInfo {
	info := screenInfo{lists: map[string]string{}, params: map[string]string{}}
	for _, name := range screen.Params {
		info.params[name] = ""
	}
	screen.Walk(func(n *spec.Node) {
		if n.Bind == nil {
			return
		}
		if n.Bind.Records != "" && n.ID != "" {
			info.lists[n.ID] = n.Bind.Records
		}
		if n.Bind.Type != "" {
			if info.recordType == "" {
				info.recordType = n.Bind.Type
			}
			if strings.HasPrefix(n.Bind.Record, "$params.") {
				name := strings.TrimPrefix(n.Bind.Record, "$params.")
				if _, declared := info.params[name]; declared {
					info.params[name] = n.Bind.Type
				}
			}
		}
	})
	return info
}

func lint(b *spec.Bundle, kit *spec.Kit) []Issue {
	var issues []Issue
	for _, id := range b.ScreenIDs() {
		screen := b.Screens[id]
		info := inspect(screen)
		file := "screens/" + id + ".json"
		var walk func(*spec.Node, string, string)
		walk = func(n *spec.Node, path, typ string) {
			if n == nil {
				return
			}
			typ = nodeType(n, typ, info)
			part := kit.Part(n.Part)
			if part == nil {
				issues = append(issues, finding(file, path+"/part", "kit/parts", fmt.Sprintf("unknown kit part %q", n.Part), "choose a part from kit.json"))
			} else {
				issues = append(issues, lintProps(b, part, n, typ, file, path)...)
				if n.Bind != nil && !contains(part.Bind, n.Bind.Kind()) {
					issues = append(issues, finding(file, path+"/bind", "kit/bind", n.Part+" does not support "+n.Bind.Kind()+" binding", "use a binding kind declared for this part in kit.json"))
				}
			}
			issues = append(issues, lintBinding(b, n.Bind, info, file, path+"/bind")...)
			nodeChildren(n, path, func(child *spec.Node, childPath string) { walk(child, childPath, typ) })
		}
		walk(screen.Nav, "/nav", info.recordType)
		walk(screen.Body, "/body", info.recordType)
	}
	return issues
}

func nodeType(n *spec.Node, inherited string, info screenInfo) string {
	if n.Bind != nil {
		if n.Bind.Records != "" {
			return n.Bind.Records
		}
		if n.Bind.Type != "" {
			return n.Bind.Type
		}
		if n.Bind.Selection != "" {
			return info.lists[n.Bind.Selection]
		}
	}
	return inherited
}

func lintBinding(b *spec.Bundle, bind *spec.Binding, info screenInfo, file, path string) []Issue {
	if bind == nil {
		return nil
	}
	var issues []Issue
	if bind.Records != "" {
		issues = append(issues, queryFields(file, path+"/query", bind.Records, bind.Query, b.App.Records[bind.Records])...)
	}
	for key, target := range map[string]string{"records": bind.Records, "type": bind.Type} {
		if target != "" && b.App.Records[target] == nil {
			issues = append(issues, finding(file, path+"/"+key, "kit/bind", fmt.Sprintf("unknown record type %q", target), "name a record type declared in app.json"))
		}
	}
	if bind.Record != "" && bind.Type == "" {
		issues = append(issues, finding(file, path+"/type", "kit/bind", "record binding needs a type", "set type to the record type in app.json"))
	}
	for key, target := range map[string]string{"run": bind.Run, "runs": bind.Runs} {
		if target != "" && b.Runs[target] == nil {
			issues = append(issues, finding(file, path+"/"+key, "kit/bind", fmt.Sprintf("unknown run %q", target), "name a runs/<id>.yaml file"))
		}
	}
	if bind.Selection != "" && info.lists[bind.Selection] == "" {
		issues = append(issues, finding(file, path+"/selection", "kit/selection", "unknown list "+bind.Selection, "name a record list id in this screen"))
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Path < issues[j].Path })
	return issues
}

func lintProps(b *spec.Bundle, part *spec.Part, n *spec.Node, typ, file, path string) []Issue {
	var issues []Issue
	for _, key := range keys(part.Props) {
		if part.Props[key].Required {
			if _, ok := n.Props[key]; !ok {
				issues = append(issues, finding(file, ptr(path+"/props", key), "kit/parts", "required prop is missing", "supply "+key+" for "+part.Name))
			}
		}
	}
	for _, key := range keys(n.Props) {
		prop := part.Props[key]
		at := ptr(path+"/props", key)
		if prop == nil {
			issues = append(issues, finding(file, at, "kit/parts", fmt.Sprintf("%s has no prop %q", part.Name, key), "use only this part's props from kit.json"))
			continue
		}
		value := n.Props[key]
		if !prop.Accepts(value) {
			issues = append(issues, finding(file, at, "kit/parts", "prop must have type "+prop.Type, "use the type and token values in kit.json"))
			continue
		}
		switch prop.Type {
		case "field", "fields":
			fields := []string{}
			if prop.Type == "field" {
				fields = append(fields, value.(string))
			} else {
				fields, _ = spec.StringList(value)
			}
			for i, field := range fields {
				fieldPath := at
				if prop.Type == "fields" {
					fieldPath = fmt.Sprintf("%s/%d", at, i)
				}
				if b.App.Records[typ] == nil || b.App.Records[typ].Fields[field] == nil {
					issues = append(issues, finding(file, fieldPath, "kit/bind", fmt.Sprintf("unknown field %q on record type %q", field, typ), boundFieldHint(b.App.Records[typ], typ)))
				}
			}
		case "string":
			if key == "sort" {
				if record := b.App.Records[typ]; record != nil {
					if _, err := query.Expression(record, strings.TrimPrefix(value.(string), "-")); err != nil {
						issues = append(issues, finding(file, at, "kit/bind", fmt.Sprintf("%s: %v", file, err), fieldHint(typ, record)))
					}
				}
			}
		case "commands":
			for i, id := range mustStrings(value) {
				found := false
				for _, command := range b.App.Commands {
					if command.ID == id {
						found = true
					}
				}
				if !found {
					issues = append(issues, finding(file, fmt.Sprintf("%s/%d", at, i), "kit/bind", "unknown command "+id, "name a command from app.json"))
				}
			}
		}
	}
	return issues
}

func keys[V any](m map[string]V) []string {
	result := make([]string, 0, len(m))
	for key := range m {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
func escape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func actionValue(action spec.Action) map[string]any {
	value := map[string]any{}
	for key, data := range action {
		var v any
		_ = json.Unmarshal(data, &v)
		value[key] = v
	}
	return value
}

func boundFieldHint(record *spec.RecordType, typ string) string {
	if record == nil {
		return "bind to a record type declared in app.json"
	}
	return fieldHint(typ, record)
}

// mustStrings reads a list prop that validProp has already accepted.
func mustStrings(value any) []string {
	list, _ := spec.StringList(value)
	return list
}
