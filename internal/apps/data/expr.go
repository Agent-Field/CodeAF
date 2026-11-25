package data

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// EXPRESSIONS ARE THE CEL SUBSET OF DESIGN A2: literals, fields, $ references,
// comparisons, in, && || !, ?:, and now() days_since() changed() count() me().
// They appear in computed fields, freeze rules and triggers, and every one is
// compiled against the record type it reads, so a field name that does not
// exist is a compile error at registration rather than a null at run time.
//
// EVERY NUMBER IS A DOUBLE. CEL refuses arithmetic between an int and a
// double, and the values a record holds come from JSON, where 48000 and 0.5
// are the same kind of thing. So an integer literal in an expression is read
// as a double ("amount * 2" is "amount * 2.0"), count() and days_since()
// answer doubles, and cross-type comparisons are on. The one cost is that a
// list cannot be indexed by a literal, which the subset does not offer.

// exprCostLimit bounds one evaluation. An expression comes from an app's
// definition, never from a person's query, but a comprehension over a large
// list could still run long, and a write waits on it.
const exprCostLimit = 1_000_000

// masked is what a masked value reads as wherever it is not revealed.
const masked = "••••"

// exprEnv builds the environment for one record type: its fields, the row's
// own names, its constants, and the functions of the subset. now is the
// store's clock, so a test's clock is the expressions' clock too.
func exprEnv(t *Type, now func() time.Time) (*cel.Env, error) {
	opts := []cel.EnvOption{
		cel.CrossTypeNumericComparisons(true),
		cel.Variable("_changed", cel.ListType(cel.StringType)),
		cel.Function("_ch",
			cel.Overload("_ch_list_string", []*cel.Type{cel.ListType(cel.StringType), cel.StringType}, cel.BoolType,
				cel.BinaryBinding(func(list, name ref.Val) ref.Val {
					l, ok := list.(traits.Lister)
					if !ok {
						return types.False
					}
					return l.Contains(name)
				}))),
		cel.Function("me",
			cel.Overload("me_void", []*cel.Type{}, cel.StringType,
				cel.FunctionBinding(func(...ref.Val) ref.Val { return types.String(Me) }))),
		cel.Function("now",
			cel.Overload("now_void", []*cel.Type{}, cel.TimestampType,
				cel.FunctionBinding(func(...ref.Val) ref.Val { return types.Timestamp{Time: now().UTC()} }))),
		cel.Function("days_since",
			cel.Overload("days_since_dyn", []*cel.Type{cel.DynType}, cel.DoubleType,
				cel.UnaryBinding(func(v ref.Val) ref.Val {
					at, ok := timeOf(v)
					if !ok {
						return types.NewErr("days_since needs a date")
					}
					return types.Double(math.Floor(now().Sub(at).Hours() / 24))
				}))),
		cel.Function("count",
			cel.Overload("count_dyn", []*cel.Type{cel.DynType}, cel.DoubleType,
				cel.UnaryBinding(func(v ref.Val) ref.Val {
					if s, ok := v.(traits.Sizer); ok {
						if n, ok := s.Size().(types.Int); ok {
							return types.Double(float64(n))
						}
					}
					if v == types.NullValue {
						return types.Double(0)
					}
					return types.NewErr("count needs a list")
				}))),
	}
	declared := map[string]bool{"_changed": true}
	declare := func(name string) {
		if declared[name] || celReserved[name] {
			return
		}
		declared[name] = true
		opts = append(opts, cel.Variable(name, cel.DynType))
	}
	for name := range reserved {
		declare(name)
	}
	for _, name := range t.Order {
		declare(name)
	}
	for _, name := range sortedKeys(t.Constants) {
		declare(name)
	}
	return cel.NewEnv(opts...)
}

// timeOf reads a date, a datetime or a timestamp.
func timeOf(v ref.Val) (time.Time, bool) {
	switch x := v.(type) {
	case types.Timestamp:
		return x.Time, true
	case types.String:
		s := string(x)
		if at, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return at, true
		}
		if at, err := time.Parse("2006-01-02", s); err == nil {
			return at, true
		}
	}
	return time.Time{}, false
}

// program is one compiled expression and whether it reads the clock, which
// makes a computed field go stale with time alone.
type program struct {
	src     string
	prg     cel.Program
	readsAt bool
}

// compile compiles one expression of type t.
func compile(env *cel.Env, src string) (*program, error) {
	text := rewrite(src)
	ast, iss := env.Compile(text)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("%q does not compile: %v", src, iss.Err())
	}
	prg, err := env.Program(ast, cel.CostLimit(exprCostLimit))
	if err != nil {
		return nil, fmt.Errorf("%q does not compile: %v", src, err)
	}
	return &program{src: src, prg: prg, readsAt: strings.Contains(text, "now(") || strings.Contains(text, "days_since(")}, nil
}

// eval runs p over a record's values. changed names the fields this write
// changed, for changed(); it is empty outside a write.
func (p *program) eval(t *Type, values map[string]any, changed []string) (any, error) {
	vars := make(map[string]any, len(t.Order)+len(t.Constants)+8)
	for name, v := range t.Constants {
		vars[name] = v
	}
	for _, name := range t.Order {
		vars[name] = nil
	}
	for name, v := range values {
		vars[name] = v
	}
	if changed == nil {
		changed = []string{}
	}
	vars["_changed"] = changed
	out, _, err := p.prg.Eval(vars)
	if err != nil {
		return nil, err
	}
	return native(out), nil
}

// evalBool runs a condition. A condition that cannot be decided (a null in a
// comparison, a value of the wrong kind) is false: it did not hold.
func (p *program) evalBool(t *Type, values map[string]any, changed []string) bool {
	out, err := p.eval(t, values, changed)
	if err != nil {
		return false
	}
	b, _ := out.(bool)
	return b
}

// native turns a CEL value back into the Go value a record holds.
func native(v ref.Val) any {
	switch x := v.(type) {
	case types.Double:
		return float64(x)
	case types.Int:
		return float64(x)
	case types.Uint:
		return float64(x)
	case types.Timestamp:
		return x.Time.UTC().Format(stamp)
	case types.Null:
		return nil
	}
	return v.Value()
}

// identifiers is every name an expression reads, outside its string
// literals.
func identifiers(src string) map[string]bool {
	out := map[string]bool{}
	text := rewrite(src)
	isIdent := func(c byte) bool {
		return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '\'' || c == '"':
			j := i + 1
			for j < len(text) && text[j] != c {
				if text[j] == '\\' {
					j++
				}
				j++
			}
			i = j + 1
		case isIdent(c) && !(c >= '0' && c <= '9'):
			j := i
			for j < len(text) && isIdent(text[j]) {
				j++
			}
			out[text[i:j]] = true
			i = j
		case c >= '0' && c <= '9':
			for i < len(text) && (isIdent(text[i]) || text[i] == '.') {
				i++
			}
		default:
			i++
		}
	}
	return out
}

// rewrite turns the subset's spellings into CEL's: $me and $now become calls,
// $record. disappears (fields are read by name), changed('x') becomes a look
// in the fields this write changed, and an integer literal becomes a double.
// String literals are copied untouched.
func rewrite(src string) string {
	var b strings.Builder
	isIdent := func(c byte) bool {
		return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\'' || c == '"':
			j := i + 1
			for j < len(src) && src[j] != c {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j < len(src) {
				j++
			}
			b.WriteString(src[i:min(j, len(src))])
			i = j
		case c == '$':
			j := i + 1
			for j < len(src) && isIdent(src[j]) {
				j++
			}
			switch src[i+1 : j] {
			case "me":
				b.WriteString("me()")
			case "now":
				b.WriteString("now()")
			case "record":
				if j < len(src) && src[j] == '.' {
					j++
				}
			default:
				b.WriteString(src[i:j])
			}
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			whole := true
			if j < len(src) && (src[j] == '.' || src[j] == 'e' || src[j] == 'E' || src[j] == 'u' || src[j] == 'U' || src[j] == 'x' || src[j] == 'X') {
				whole = false
			}
			// A float's tail, an exponent or a hex literal is copied as it is.
			for j < len(src) && (isIdent(src[j]) || src[j] == '.' || (src[j] == '-' || src[j] == '+') && (src[j-1] == 'e' || src[j-1] == 'E')) {
				j++
			}
			b.WriteString(src[i:j])
			if whole {
				b.WriteString(".0")
			}
			i = j
		case isIdent(c):
			j := i
			for j < len(src) && isIdent(src[j]) {
				j++
			}
			word := src[i:j]
			k := j
			for k < len(src) && src[k] == ' ' {
				k++
			}
			if word == "changed" && k < len(src) && src[k] == '(' {
				b.WriteString("_ch(_changed, ")
				i = k + 1
				continue
			}
			b.WriteString(word)
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// matchKey names one compiled condition of one schema.
type matchKey struct {
	schema *schema
	typ    string
	expr   string
}

// Match evaluates a when condition (a trigger's, a screen's) over one record
// of a change: its values after the change (before, for a deletion), with
// changed() reading the fields the change touched. A condition that does not
// compile is an error; one that cannot be decided is false. Compiled
// conditions are kept per schema, so a trigger evaluated on every commit is
// compiled once.
func (s *Store) Match(typ, expr string, rc RecordChange) (bool, error) {
	sch := s.schema.Load()
	t := sch.types[typ]
	if t == nil {
		return false, errorf(CodeNotFound, "there is no record type named %q", typ)
	}
	key := matchKey{sch, typ, expr}
	var p *program
	if cached, ok := s.matches.Load(key); ok {
		p = cached.(*program)
	} else {
		env, err := exprEnv(t, s.now)
		if err != nil {
			return false, err
		}
		if p, err = compile(env, expr); err != nil {
			return false, errorf(CodeInvalid, "%v", err)
		}
		s.matches.Store(key, p)
	}
	row := rc.After
	if row == nil {
		row = rc.Before
	}
	values := map[string]any{}
	for k, v := range row {
		if k != "_meta" {
			values[k] = v
		}
	}
	return p.evalBool(t, values, rc.Changed), nil
}
