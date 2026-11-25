package runs

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

var interpolation = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)
var reference = regexp.MustCompile(`^\$[A-Za-z_][A-Za-z_0-9]*(\.[A-Za-z_0-9-]+)*$`)

func reservedVariable(name string) bool {
	switch name {
	case "me", "params", "input", "record", "steps", "now":
		return true
	default:
		return false
	}
}

func resolve(value any, vars map[string]any) (any, error) {
	switch v := value.(type) {
	case string:
		if reference.MatchString(v) {
			return lookup(vars, strings.Split(v[1:], "."))
		}
		var err error
		result := interpolation.ReplaceAllStringFunc(v, func(match string) string {
			if err != nil {
				return ""
			}
			var result any
			expression := strings.TrimSpace(interpolation.FindStringSubmatch(match)[1])
			if reference.MatchString("$" + expression) {
				result, err = lookup(vars, strings.Split(expression, "."))
			} else {
				result, err = evaluate(expression, vars, nil)
			}
			if result == nil {
				return ""
			}
			if list, ok := result.([]any); ok {
				words := make([]string, len(list))
				for i, item := range list {
					words[i] = fmt.Sprint(item)
				}
				return strings.Join(words, ", ")
			}
			return fmt.Sprint(result)
		})
		return result, err
	case []any:
		list := make([]any, len(v))
		for i, value := range v {
			item, err := resolve(value, vars)
			if err != nil {
				return nil, err
			}
			list[i] = item
		}
		return list, nil
	case map[string]any:
		values := make(map[string]any, len(v))
		for key, value := range v {
			item, err := resolve(value, vars)
			if err != nil {
				return nil, err
			}
			values[key] = item
		}
		return values, nil
	default:
		return normalize(value), nil
	}
}

func lookup(value any, path []string) (any, error) {
	if len(path) == 0 {
		return value, nil
	}
	switch v := value.(type) {
	case map[string]any:
		if current, ok := v[path[0]]; ok {
			return lookup(current, path[1:])
		}
	case []any:
		projected := make([]any, 0, len(v))
		for _, item := range v {
			result, err := lookup(item, path)
			if err != nil {
				return nil, err
			}
			projected = append(projected, result)
		}
		return projected, nil
	}
	return nil, fmt.Errorf("missing value for %s", strings.Join(path, "."))
}

func evaluate(expr string, vars map[string]any, changed func(string) bool) (any, error) {
	opts := make([]cel.EnvOption, 0, len(vars)+1)
	for name := range vars {
		if reference.MatchString("$"+name) && !strings.Contains(name, ".") {
			opts = append(opts, cel.Variable(name, cel.DynType))
		}
	}
	if changed != nil {
		opts = append(opts, cel.Function("changed", cel.Overload("changed_field", []*cel.Type{cel.StringType}, cel.BoolType,
			cel.UnaryBinding(func(value ref.Val) ref.Val { return types.Bool(changed(string(value.(types.String)))) }))))
	}
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, err
	}
	expr = strings.TrimSpace(expr)
	if strings.HasPrefix(expr, "$") {
		expr = expr[1:]
	}
	ast, issues := env.Compile(expr)
	if err := issues.Err(); err != nil {
		return nil, err
	}
	program, err := env.Program(ast, cel.CostLimit(100_000))
	if err != nil {
		return nil, err
	}
	out, _, err := program.Eval(vars)
	if err != nil {
		return nil, err
	}
	return out.Value(), nil
}

func condition(expr string, vars map[string]any, changed func(string) bool) (bool, error) {
	if expr == "" {
		return true, nil
	}
	value, err := evaluate(expr, vars, changed)
	if err != nil {
		return false, err
	}
	ok, boolean := value.(bool)
	if !boolean {
		return false, fmt.Errorf("condition %q must return true or false", expr)
	}
	return ok, nil
}

// normalize keeps integral JSON numbers integral across checkpoints so a CEL
// expression sees the same type before and after reopening the journal.
func normalize(value any) any {
	switch v := value.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
		n, _ := v.Float64()
		return n
	case []any:
		for i, item := range v {
			v[i] = normalize(item)
		}
	case map[string]any:
		for key, item := range v {
			v[key] = normalize(item)
		}
	}
	return value
}

func (c Change) changed(field string) bool {
	return !reflect.DeepEqual(c.Before[field], c.After[field])
}
