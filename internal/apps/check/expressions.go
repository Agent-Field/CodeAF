package check

import (
	"context"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/apps/expr"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"regexp"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

// resolve reads a $reference from the render values.
func resolve(ref string, values map[string]any) (any, error) { return expr.Resolve(ref, values) }

var template = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

func resolveValue(value any, values map[string]any) (any, error) {
	switch value := value.(type) {
	case string:
		if strings.HasPrefix(value, "$") {
			return resolve(value, values)
		}
		var err error
		result := template.ReplaceAllStringFunc(value, func(match string) string {
			name := strings.TrimSpace(template.FindStringSubmatch(match)[1])
			item, itemErr := resolve(name, values)
			if itemErr != nil {
				err = itemErr
				return ""
			}
			if item == nil {
				return ""
			}
			switch item.(type) {
			case map[string]any, []any, []string:
				err = fmt.Errorf("template %q must resolve to a scalar", name)
				return ""
			}
			return fmt.Sprint(item)
		})
		if err != nil {
			return nil, err
		}
		if strings.Contains(result, "{{") || strings.Contains(result, "}}") {
			return nil, fmt.Errorf("invalid template %q", value)
		}
		return result, nil
	case []any:
		result := make([]any, 0, len(value))
		for _, item := range value {
			resolved, err := resolveValue(item, values)
			if err != nil {
				return nil, err
			}
			result = append(result, resolved)
		}
		return result, nil
	case map[string]any:
		result := map[string]any{}
		for _, key := range keys(value) {
			resolved, err := resolveValue(value[key], values)
			if err != nil {
				return nil, err
			}
			result[key] = resolved
		}
		return result, nil
	default:
		return value, nil
	}
}

func compileWhen(source string, values map[string]any) (cel.Program, error) {
	env, ast, err := expr.CompileWhen(source, values)
	if err != nil {
		return nil, err
	}
	at, _ := time.Parse(time.RFC3339, fmt.Sprint(values["now"]))
	if supplied, ok := values["now"].(time.Time); ok {
		at = supplied
	}
	return expr.Program(env, ast, at, fmt.Sprint(values["me"]))
}

func evalWhen(ctx context.Context, program cel.Program, values map[string]any, dateFields ...string) (bool, error) {
	output, _, err := program.ContextEval(ctx, expr.Activation(values, dateFields...))
	if err != nil {
		return false, err
	}
	if output.Type() != types.BoolType {
		return false, fmt.Errorf("when must yield bool, got %s", output.Type())
	}
	return bool(output.(types.Bool)), nil
}

func computedExpressions(b *spec.Bundle) []Issue {
	var result []Issue
	for _, typ := range keys(b.App.Records) {
		schema := b.App.Records[typ]
		env, err := expr.RecordEnv(keys(schema.Fields), keys(schema.Constants))
		if err != nil {
			result = append(result, finding(spec.AppFile, ptr("/records", typ), "expression/env", err.Error(), "rename fields that conflict with CEL built-ins"))
			continue
		}
		for _, name := range keys(schema.Fields) {
			source := schema.Fields[name].Computed
			if source == "" {
				continue
			}
			if _, err := expr.Compile(env, source); err != nil {
				result = append(result, finding(spec.AppFile, ptr(ptr("/records", typ)+"/fields", name)+"/computed", "expression/computed", name+": "+err.Error(), "use declared record fields and duration literals, for example now - days(7)"))
			}
		}
	}
	return result
}
