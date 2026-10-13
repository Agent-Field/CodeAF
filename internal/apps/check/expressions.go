package check

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

var reference = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*(\.[a-zA-Z_][a-zA-Z0-9_-]*)*$`)
var template = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

func resolve(ref string, values map[string]any) (any, error) {
	name := strings.TrimPrefix(ref, "$")
	if !reference.MatchString(name) {
		return nil, fmt.Errorf("invalid reference %q", ref)
	}
	var current any = values
	for _, key := range strings.Split(name, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: cannot read %q from %T", ref, key, current)
		}
		current, ok = object[key]
		if !ok {
			return nil, fmt.Errorf("%s: unknown target %q", ref, key)
		}
	}
	return current, nil
}

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

// CEL identifiers cannot start with $. Only reference tokens outside quoted
// strings are rewritten; literal dollars and escaped quotes keep their bytes.
func celSource(source string, values map[string]any) (string, error) {
	var out strings.Builder
	var quote byte
	for i := 0; i < len(source); {
		char := source[i]
		if quote != 0 {
			out.WriteByte(char)
			i++
			if char == '\\' && i < len(source) {
				out.WriteByte(source[i])
				i++
			} else if char == quote {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			out.WriteByte(char)
			i++
			continue
		}
		if char != '$' {
			out.WriteByte(char)
			i++
			continue
		}
		end := i + 1
		for end < len(source) && refByte(source[end]) {
			end++
		}
		ref := source[i:end]
		if _, err := resolve(ref, values); err != nil {
			// A known hyphenated field or list id wins. Otherwise a hyphen
			// starts subtraction, including expressions without spaces.
			for cut := strings.LastIndex(ref, "-"); cut > 0; cut = strings.LastIndex(ref, "-") {
				ref = ref[:cut]
				if _, prefixErr := resolve(ref, values); prefixErr == nil {
					end = i + len(ref)
					break
				}
			}
			if _, prefixErr := resolve(ref, values); prefixErr != nil {
				return "", err
			}
		}
		segments := strings.Split(ref[1:], ".")
		out.WriteString(segments[0])
		for _, key := range segments[1:] {
			fmt.Fprintf(&out, "[%q]", key)
		}
		i = end
	}
	return out.String(), nil
}

func refByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '.' || b == '-'
}

func compileWhen(source string, values map[string]any) (cel.Program, error) {
	source, err := celSource(source, values)
	if err != nil {
		return nil, err
	}
	options := []cel.EnvOption{cel.ParserExpressionSizeLimit(4096), cel.ParserRecursionLimit(64)}
	for _, name := range keys(values) {
		// These names belong to CEL itself. Fields with those names remain
		// accessible through $record.<field> without shadowing a built-in.
		if contains([]string{"bool", "bytes", "double", "duration", "int", "list", "map", "null_type", "string", "timestamp", "type", "uint", "true", "false", "null", "in"}, name) {
			continue
		}
		options = append(options, cel.Variable(name, cel.DynType))
	}
	env, err := cel.NewEnv(options...)
	if err != nil {
		return nil, err
	}
	ast, issues := env.Compile(source)
	if issues.Err() != nil {
		return nil, issues.Err()
	}
	if ast.OutputType() != cel.BoolType && ast.OutputType() != cel.DynType {
		return nil, fmt.Errorf("when must yield bool, got %s", ast.OutputType())
	}
	return env.Program(ast, cel.CostLimit(10000), cel.InterruptCheckFrequency(100))
}

func evalWhen(ctx context.Context, program cel.Program, values map[string]any) (bool, error) {
	output, _, err := program.ContextEval(ctx, values)
	if err != nil {
		return false, err
	}
	if output.Type() != types.BoolType {
		return false, fmt.Errorf("when must yield bool, got %s", output.Type())
	}
	return bool(output.(types.Bool)), nil
}
