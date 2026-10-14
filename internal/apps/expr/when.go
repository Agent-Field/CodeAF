package expr

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/cel-go/cel"
)

var reference = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*(\.[a-zA-Z_][a-zA-Z0-9_-]*)*$`)

// builtinNames belong to CEL itself. A record field with one of these names
// stays reachable through $record.<field> without shadowing the built-in.
var builtinNames = []string{"now", "bool", "bytes", "double", "duration", "int", "list", "map", "null_type", "string", "timestamp", "type", "uint", "true", "false", "null", "in"}

// Resolve reads a $reference from nested values.
func Resolve(ref string, values map[string]any) (any, error) {
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

// Source: CEL identifiers cannot start with $. Only reference tokens outside quoted
// strings are rewritten; literal dollars and escaped quotes keep their bytes.
// Source rewrites $references to CEL variable reads.
func Source(source string, values map[string]any) (string, error) {
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
		if _, err := Resolve(ref, values); err != nil {
			// A known hyphenated field or list id wins. Otherwise a hyphen
			// starts subtraction, including expressions without spaces.
			for cut := strings.LastIndex(ref, "-"); cut > 0; cut = strings.LastIndex(ref, "-") {
				ref = ref[:cut]
				if _, prefixErr := Resolve(ref, values); prefixErr == nil {
					end = i + len(ref)
					break
				}
			}
			if _, prefixErr := Resolve(ref, values); prefixErr != nil {
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

// WhenEnv declares every key of values as a variable, except CEL built-in names.
func WhenEnv(values map[string]any) (*cel.Env, error) {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	options := []cel.EnvOption{}
	for _, name := range names {
		if !contains(builtinNames, name) {
			options = append(options, cel.Variable(name, cel.DynType))
		}
	}
	return NewEnv(options...)
}

// CompileWhen compiles a condition against the values it will run on and
// demands a boolean result.
func CompileWhen(source string, values map[string]any) (*cel.Env, *cel.Ast, error) {
	source, err := Source(source, values)
	if err != nil {
		return nil, nil, err
	}
	env, err := WhenEnv(values)
	if err != nil {
		return nil, nil, err
	}
	ast, err := Compile(env, source)
	if err != nil {
		return nil, nil, err
	}
	if ast.OutputType() != cel.BoolType && ast.OutputType() != cel.DynType {
		return nil, nil, fmt.Errorf("when must yield bool, got %s", ast.OutputType())
	}
	return env, ast, nil
}

// RecordEnv is the environment of a record's computed fields: its own fields
// and constants, plus the metadata every record has.
func RecordEnv(fields, constants []string) (*cel.Env, error) {
	options := []cel.EnvOption{cel.Variable("created_at", cel.TimestampType), cel.Variable("updated_at", cel.TimestampType), cel.Variable("id", cel.StringType)}
	for _, name := range fields {
		options = append(options, cel.Variable(name, cel.DynType))
	}
	for _, name := range constants {
		options = append(options, cel.Variable(name, cel.DynType))
	}
	return NewEnv(options...)
}

func contains(items []string, v string) bool {
	for _, item := range items {
		if item == v {
			return true
		}
	}
	return false
}
