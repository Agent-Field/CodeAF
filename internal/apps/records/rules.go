package records

import (
	"encoding/json"
	"reflect"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/expr"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/google/cel-go/cel"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

type computedField struct {
	name string
	ast  *cel.Ast
}

// Rules compiles computed fields once per schema, in dependency order.
type Rules struct {
	schema *spec.RecordType
	env    *cel.Env
	fields []computedField
	Timed  bool
}

func NewRules(schema *spec.RecordType) (*Rules, error) {
	r := &Rules{schema: schema}
	opts := []cel.EnvOption{
		cel.Variable("created_at", cel.TimestampType), cel.Variable("updated_at", cel.TimestampType), cel.Variable("id", cel.StringType),
	}
	for _, name := range sorted(schema.Fields) {
		opts = append(opts, cel.Variable(name, cel.DynType))
	}
	for _, name := range sorted(schema.Constants) {
		opts = append(opts, cel.Variable(name, cel.DynType))
	}
	env, err := expr.NewEnv(opts...)
	if err != nil {
		return nil, invalid("computed fields: " + err.Error())
	}
	r.env = env
	asts := map[string]*cel.Ast{}
	for _, name := range sorted(schema.Fields) {
		f := schema.Fields[name]
		if f == nil {
			return nil, invalid("invalid schema field " + name)
		}
		if f.Computed == "" {
			continue
		}
		ast, err := expr.Compile(env, f.Computed)
		if err != nil {
			return nil, invalid(name + ": " + err.Error())
		}
		asts[name] = ast
	}
	state := map[string]int{}
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 2 {
			return nil
		}
		if state[name] == 1 {
			return invalid("computed field cycle at " + name)
		}
		state[name] = 1
		identifiers, timed := dependencies(asts[name].Expr())
		r.Timed = r.Timed || timed
		for _, dep := range sorted(identifiers) {
			if asts[dep] != nil {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		state[name] = 2
		r.fields = append(r.fields, computedField{name, asts[name]})
		return nil
	}
	for _, name := range sorted(asts) {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return r, nil
}
func dependencies(e *exprpb.Expr) (map[string]bool, bool) {
	names := map[string]bool{}
	timed := false
	var walk func(*exprpb.Expr)
	walk = func(e *exprpb.Expr) {
		if e == nil {
			return
		}
		if ident := e.GetIdentExpr(); ident != nil {
			names[ident.Name] = true
			timed = timed || ident.Name == "now"
		}
		if call := e.GetCallExpr(); call != nil {
			if call.Function == "now" || call.Function == "days_since" {
				timed = true
			}
			walk(call.Target)
			for _, arg := range call.Args {
				walk(arg)
			}
		}
		if sel := e.GetSelectExpr(); sel != nil {
			walk(sel.Operand)
		}
		if list := e.GetListExpr(); list != nil {
			for _, item := range list.Elements {
				walk(item)
			}
		}
		if obj := e.GetStructExpr(); obj != nil {
			for _, item := range obj.Entries {
				walk(item.GetMapKey())
				walk(item.Value)
			}
		}
		if comp := e.GetComprehensionExpr(); comp != nil {
			walk(comp.IterRange)
			walk(comp.AccuInit)
			walk(comp.LoopCondition)
			walk(comp.LoopStep)
			walk(comp.Result)
		}
	}
	walk(e)
	return names, timed
}

// Prepare applies defaults and computed values to a detached record. It accepts
// seeded computed values but always replaces them with server-evaluated results.
func (r *Rules) Prepare(row bridge.Record, member string, at time.Time, defaults bool) (bridge.Record, error) {
	fields, err := copyFields(row.Fields)
	if err != nil {
		return row, err
	}
	row.Fields = fields
	for _, name := range sorted(r.schema.Fields) {
		f := r.schema.Fields[name]
		if defaults && f.Computed == "" && fields[name] == nil && len(f.Default) > 0 {
			var value any
			if json.Unmarshal(f.Default, &value) != nil {
				return row, invalid("invalid default for " + name)
			}
			if value == "$me" {
				value = member
			}
			fields[name] = value
		}
	}
	vars := map[string]any{"created_at": row.CreatedAt, "updated_at": row.UpdatedAt, "id": row.ID}
	for name, value := range r.schema.Constants {
		vars[name] = value
	}
	for name := range r.schema.Fields {
		vars[name] = fields[name]
	}
	vars["now"] = at
	dateFields := []string{}
	for name, field := range r.schema.Fields {
		if field.Type == spec.FieldDate || field.Type == spec.FieldDatetime {
			dateFields = append(dateFields, name)
		}
	}
	for _, f := range r.fields {
		program, err := expr.Program(r.env, f.ast, at, member)
		if err != nil {
			return row, invalid(f.name + ": " + expr.Diagnostic(r.schema.Fields[f.name].Computed, err).Error())
		}
		value, _, err := program.Eval(expr.Activation(vars, dateFields...))
		if err != nil {
			if missingDependency(f.ast, r.schema, fields) {
				fields[f.name], vars[f.name] = nil, nil
				continue
			}
			return row, invalid(f.name + ": " + expr.Diagnostic(r.schema.Fields[f.name].Computed, err).Error())
		}
		result, err := value.ConvertToNative(reflect.TypeFor[any]())
		if err != nil {
			return row, invalid(f.name + ": " + expr.Diagnostic(r.schema.Fields[f.name].Computed, err).Error())
		}
		if t, ok := result.(time.Time); ok {
			result = stamp(t)
			if r.schema.Fields[f.name].Type == spec.FieldDate {
				// A computed deadline (shoot_at + days(28)) is a timestamp; a date field keeps its day.
				result = t.UTC().Format(time.DateOnly)
			}
		}
		// JSON normalisation keeps CEL integers, lists and maps in the same types as bridge input.
		normal, err := copyFields(map[string]any{f.name: result})
		if err != nil {
			return row, err
		}
		fields[f.name], vars[f.name] = normal[f.name], normal[f.name]
	}
	if err := ValidateFields(r.schema, fields); err != nil {
		return row, err
	}
	for name, value := range fields {
		if r.schema.Fields[name].Type == spec.FieldDatetime && value != nil {
			t, _ := time.Parse(time.RFC3339Nano, value.(string))
			fields[name] = stamp(t)
		}
	}
	return row, nil
}
func writable(typ *spec.RecordType, fields map[string]any) error {
	for name := range fields {
		f := typ.Fields[name]
		if f == nil {
			return invalid("unknown field " + name)
		}
		if f.Computed != "" {
			return invalid(name + " is computed and read-only")
		}
	}
	return nil
}

func missingDependency(ast *cel.Ast, schema *spec.RecordType, fields map[string]any) bool {
	names, _ := dependencies(ast.Expr())
	for name := range names {
		if schema.Fields[name] != nil && fields[name] == nil {
			return true
		}
	}
	return false
}
