package runs

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

var parameterTemplate = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

// Missing references must fail where they are used, instead of disappearing
// into an empty query value or a record write with a blank id.
func resolveRunValue(value any, env map[string]any) (any, error) {
	switch v := value.(type) {
	case string:
		var refs []string
		if strings.HasPrefix(v, "$") && !strings.ContainsAny(v, " \t\n") {
			refs = append(refs, v)
		}
		for _, match := range parameterTemplate.FindAllStringSubmatch(v, -1) {
			refs = append(refs, match[1])
		}
		for _, ref := range refs {
			if lookup(ref, env) == nil {
				return nil, fmt.Errorf("missing run reference %q; %s", ref, spec.RunReferenceRule)
			}
		}
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			resolved, err := resolveRunValue(item, env)
			if err != nil {
				return nil, err
			}
			out[key] = resolved
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			resolved, err := resolveRunValue(item, env)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	}
	return resolveValue(value, env), nil
}

func resolveRunMap(value map[string]any, env map[string]any) (map[string]any, error) {
	resolved, err := resolveRunValue(value, env)
	if err != nil {
		return nil, err
	}
	return resolved.(map[string]any), nil
}
