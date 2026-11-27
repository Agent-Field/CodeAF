package data

import (
	"encoding/json"
	"fmt"
	appschema "github.com/Agent-Field/codeaf/internal/apps/schema"
	"reflect"
	"strings"
)

// DeclaredViewIR translates a saved screen view into the query the store executes.
func DeclaredViewIR(view appschema.QueryIR, arguments map[string]json.RawMessage) (QueryIR, error) {
	ir := QueryIR{Type: view.Type, Group: view.Group, Select: view.Select}
	args := map[string]any{}
	for key, raw := range arguments {
		var value any
		if json.Unmarshal(raw, &value) != nil || !ViewArgument(view.Args[key], value) {
			return QueryIR{}, fmt.Errorf("Invalid argument %q.", key)
		}
		args[key] = value
	}
	for _, filter := range view.Filter {
		value := filter.Value
		if ref, ok := value.(string); ok {
			if key, ref := strings.CutPrefix(ref, "$args."); ref {
				value, ok = args[key]
				if !ok {
					return QueryIR{}, fmt.Errorf("Missing argument %q.", key)
				}
			}
		}
		op := filter.Op
		neg := op == "!=" || op == "ne"
		switch op {
		case "=", "==", "!=", "eq", "ne":
			op = ":"
		case "lt":
			op = "<"
		case "lte":
			op = "<="
		case "gt":
			op = ">"
		case "gte":
			op = ">="
		}
		if op == "has" {
			ir.Where = append(ir.Where, Cond{Field: filter.Field, Op: op})
		} else {
			ir.Where = append(ir.Where, Cond{Field: filter.Field, Op: op, Value: fmt.Sprint(value), Neg: neg, Literal: true})
		}
	}
	for _, agg := range view.Aggregate {
		ir.Aggregate = append(ir.Aggregate, Agg{Fn: agg.Op, Field: agg.Field, As: agg.As})
	}
	if view.Window != nil && len(view.Window.OrderBy) == 1 {
		ir.Sort = view.Window.OrderBy[0]
	}
	return ir, nil
}

func ViewArgumentType(kind string) reflect.Type {
	switch kind {
	case "text", "longtext", "enum", "date", "datetime", "ref", "email", "url", "phone", "person":
		return reflect.TypeFor[string]()
	case "number", "money":
		return reflect.TypeFor[float64]()
	case "bool":
		return reflect.TypeFor[bool]()
	}
	return nil
}

func ViewArgument(kind string, value any) bool {
	return value != nil && reflect.TypeOf(value) == ViewArgumentType(kind)
}
