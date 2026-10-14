// Package expr defines the expression grammar shared by checking and execution.
package expr

import (
	"fmt"
	"math"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/functions"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// NewEnv keeps the grammar independent of services so worker drafts can use it.
func NewEnv(options ...cel.EnvOption) (*cel.Env, error) {
	base := []cel.EnvOption{
		cel.ParserExpressionSizeLimit(maxSourceBytes), cel.ParserRecursionLimit(64),
		cel.Variable("now", cel.TimestampType),
		cel.Function("now", cel.Overload("records_now", nil, cel.TimestampType)),
		cel.Function("me", cel.Overload("records_me", nil, cel.StringType)),
		cel.Function("count", cel.Overload("records_count", []*cel.Type{cel.DynType}, cel.IntType, cel.UnaryBinding(func(value ref.Val) ref.Val {
			if s, ok := value.(traits.Sizer); ok {
				return s.Size()
			}
			return types.NewErr("count needs a list, map or text")
		}))),
		cel.Function("days", cel.Overload("apps_days_int", []*cel.Type{cel.IntType}, cel.DurationType, cel.UnaryBinding(dayDuration)), cel.Overload("apps_days_double", []*cel.Type{cel.DoubleType}, cel.DurationType, cel.UnaryBinding(dayDuration))),
		cel.Function("days_since", cel.Overload("records_days_string", []*cel.Type{cel.StringType}, cel.DoubleType), cel.Overload("records_days_timestamp", []*cel.Type{cel.TimestampType}, cel.DoubleType)),
	}
	return cel.NewEnv(append(base, options...)...)
}

func dayDuration(value ref.Val) ref.Val {
	var n float64
	switch value := value.(type) {
	case types.Int:
		n = float64(value)
	case types.Double:
		n = float64(value)
	default:
		return types.NewErr("days needs a number")
	}
	nanos := n * float64(24*time.Hour)
	if math.IsNaN(nanos) || math.IsInf(nanos, 0) || nanos >= math.Exp2(63) || nanos < -math.Exp2(63) {
		return types.NewErr("days duration out of range")
	}
	return types.Duration{Duration: time.Duration(nanos)}
}

// Program binds the supplied clock instead of consulting wall time during evaluation.
func Program(env *cel.Env, ast *cel.Ast, at time.Time, member string) (cel.Program, error) {
	since := func(value ref.Val) ref.Val {
		var t time.Time
		if v, ok := value.Value().(time.Time); ok {
			t = v
		} else {
			var err error
			t, err = date(fmt.Sprint(value.Value()))
			if err != nil {
				return types.NewErr("invalid date")
			}
		}
		return types.Double(at.Sub(t).Hours() / 24)
	}
	return env.Program(ast, cel.CostLimit(10000), cel.InterruptCheckFrequency(100), cel.Functions(
		&functions.Overload{Operator: "records_now", Function: func(...ref.Val) ref.Val { return types.Timestamp{Time: at} }},
		&functions.Overload{Operator: "records_me", Function: func(...ref.Val) ref.Val { return types.String(member) }},
		&functions.Overload{Operator: "records_days_string", Unary: since},
		&functions.Overload{Operator: "records_days_timestamp", Unary: since},
	))
}

// Activation gives declared dates timestamp semantics without changing stored JSON.
func Activation(values map[string]any, dateFields ...string) map[string]any {
	dates := map[string]bool{"now": true, "created_at": true, "updated_at": true}
	for _, name := range dateFields {
		dates[name] = true
	}
	return activation(values, dates)
}
func activation(values map[string]any, dates map[string]bool) map[string]any {
	result := make(map[string]any, len(values))
	for name, value := range values {
		switch v := value.(type) {
		case string:
			if dates[name] {
				if t, err := date(v); err == nil {
					value = t
				}
			}
		case map[string]any:
			value = activation(v, dates)
		}
		result[name] = value
	}
	return result
}
func date(value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Parse("2006-01-02", value)
	}
	return t, nil
}
