package runs

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
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

type recordReader func(string) ([]any, error)

const recordReaderKey = "records()"

func (e *Engine) addRecordReader(app string, vars map[string]any) {
	if e.cfg.ReadRecords != nil {
		vars[recordReaderKey] = recordReader(func(recordType string) ([]any, error) {
			return e.cfg.ReadRecords(app, recordType)
		})
	}
}

func celVariable(name string) bool {
	switch name {
	case "bool", "bytes", "double", "duration", "int", "list", "map", "null_type", "string", "timestamp", "type", "uint":
		return false // These remain available through a qualified record field.
	}
	return reference.MatchString("$"+name) && !strings.Contains(name, ".")
}

func evaluate(expr string, vars map[string]any, changed func(string) bool) (any, error) {
	now, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(vars["now"]))
	opts := []cel.EnvOption{
		cel.CrossTypeNumericComparisons(true),
		cel.Function("me", cel.Overload("run_me", nil, cel.StringType,
			cel.FunctionBinding(func(...ref.Val) ref.Val { return types.String("me") }))),
		cel.Function("now", cel.Overload("run_now", nil, cel.TimestampType,
			cel.FunctionBinding(func(...ref.Val) ref.Val { return types.Timestamp{Time: now} }))),
		cel.Function("days_since", cel.Overload("run_days_since", []*cel.Type{cel.DynType}, cel.DoubleType,
			cel.UnaryBinding(func(value ref.Val) ref.Val {
				at, ok := expressionDate(value)
				if !ok {
					return types.NewErr("days_since needs a date")
				}
				return types.Double(math.Floor(now.Sub(at).Hours() / 24))
			}))),
		cel.Function("count", cel.Overload("run_count", []*cel.Type{cel.DynType}, cel.IntType,
			cel.UnaryBinding(func(value ref.Val) ref.Val {
				if s, ok := value.(traits.Sizer); ok {
					return s.Size()
				}
				if value == types.NullValue {
					return types.Int(0)
				}
				return types.NewErr("count needs a list")
			}))),
	}
	for name := range vars {
		if celVariable(name) {
			opts = append(opts, cel.Variable(name, cel.DynType))
		}
	}
	if query, ok := vars[recordReaderKey].(recordReader); ok {
		opts = append(opts, cel.Function("records", cel.Overload("run_records", []*cel.Type{cel.StringType},
			cel.ListType(cel.DynType), cel.UnaryBinding(recordLookup(query)))))
	}
	if changed != nil {
		opts = append(opts, cel.Function("changed", cel.Overload("changed_field", []*cel.Type{cel.StringType}, cel.BoolType,
			cel.UnaryBinding(func(value ref.Val) ref.Val {
				field, ok := value.(types.String)
				if !ok {
					return types.NewErr("changed needs a field name")
				}
				return types.Bool(changed(string(field)))
			}))))
	}
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, err
	}
	ast, issues := env.Compile(expressionReferences(strings.TrimSpace(expr)))
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

func recordLookup(query recordReader) func(ref.Val) ref.Val {
	cache := make(map[string]ref.Val)
	return func(v ref.Val) ref.Val {
		recordType, ok := v.(types.String)
		if !ok {
			return types.NewErr("records needs a record type")
		}
		key := string(recordType)
		if cached, ok := cache[key]; ok {
			return cached
		}
		if len(cache) >= 4 {
			return types.NewErr("an expression can read up to four record types")
		}
		rows, err := query(key)
		var value ref.Val
		if err != nil {
			value = types.NewErr("%v", err)
		} else {
			value = types.NewDynamicList(types.DefaultTypeAdapter, rows)
		}
		cache[key] = value
		return value
	}
}

func expressionDate(value ref.Val) (time.Time, bool) {
	switch v := value.(type) {
	case types.Timestamp:
		return v.Time, true
	case types.String:
		for _, layout := range []string{time.RFC3339Nano, time.DateOnly} {
			if at, err := time.Parse(layout, string(v)); err == nil {
				return at, true
			}
		}
	}
	return time.Time{}, false
}

// expressionReferences maps the app's dollar references to CEL variables
// and keeps dollar signs in quoted literals intact.
func expressionReferences(expr string) string {
	var b strings.Builder
	var quote byte
	for i := 0; i < len(expr); i++ {
		ch := expr[i]
		switch {
		case quote != 0:
			b.WriteByte(ch)
			if ch == '\\' && i+1 < len(expr) {
				i++
				b.WriteByte(expr[i])
			} else if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"':
			quote = ch
			b.WriteByte(ch)
		case ch != '$':
			b.WriteByte(ch)
		}
	}
	return b.String()
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
