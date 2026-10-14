package spec

import (
	"fmt"
	"regexp"
	"strings"
)

// RunReferenceRule keeps repair reports explicit about the namespace a run reads.
const RunReferenceRule = "use $params.<name> for typed values and {{params.<name>}} inside text; $steps.<id>.output.<field> / {{steps.<id>.output.<field>}} for results; bare names are only declared loop variables"

var runTemplate = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
var runPath = regexp.MustCompile(`^[a-z_][a-z0-9_-]*(\.[a-z_][a-z0-9_-]*)*$`)

func runReferences(file string, value any) []Issue {
	root, _ := value.(map[string]any)
	params, _ := root["params"].(map[string]any)
	steps, _ := root["steps"].([]any)
	return referenceSteps(file, "/steps", steps, params, nil)
}

func referenceSteps(file, path string, steps []any, params, loops map[string]any) []Issue {
	var issues []Issue
	for i, raw := range steps {
		step, _ := raw.(map[string]any)
		at := fmt.Sprintf("%s/%d", path, i)
		for _, key := range []string{"prompt", "as", "args", "records.query", "records.create", "records.update", "records.delete", "gate", "wait"} {
			issues = append(issues, referenceValue(file, at+"/"+key, step[key], params, loops)...)
		}
		loop, _ := step["loop"].(map[string]any)
		issues = append(issues, referenceValue(file, at+"/loop/over", loop["over"], params, loops)...)
		innerLoops := make(map[string]any, len(loops)+1)
		for name := range loops {
			innerLoops[name] = true
		}
		if name, ok := loop["as"].(string); ok {
			innerLoops[name] = true
		}
		inner, _ := step["steps"].([]any)
		issues = append(issues, referenceSteps(file, at+"/steps", inner, params, innerLoops)...)
	}
	return issues
}

func referenceValue(file, path string, value any, params, loops map[string]any) []Issue {
	var issues []Issue
	switch v := value.(type) {
	case string:
		var paths []string
		if strings.HasPrefix(v, "$") && !strings.ContainsAny(v, " \t\n") {
			paths = append(paths, v[1:])
		}
		for _, match := range runTemplate.FindAllStringSubmatch(v, -1) {
			paths = append(paths, match[1])
		}
		for _, ref := range paths {
			if !validRunReference(ref, params, loops) {
				issues = append(issues, Issue{File: file, Path: path, Rule: "schema/run-reference", Message: fmt.Sprintf("unknown or invalid run reference %q; %s", ref, RunReferenceRule)})
			}
		}
	case map[string]any:
		for _, key := range sortedKeys(v) {
			issues = append(issues, referenceValue(file, path+"/"+strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1"), v[key], params, loops)...)
		}
	case []any:
		for i, item := range v {
			issues = append(issues, referenceValue(file, fmt.Sprintf("%s/%d", path, i), item, params, loops)...)
		}
	}
	return issues
}

func validRunReference(ref string, params, loops map[string]any) bool {
	if !runPath.MatchString(ref) {
		return false
	}
	parts := strings.Split(ref, ".")
	switch parts[0] {
	case "params":
		return len(parts) >= 2 && params[parts[1]] != nil
	case "steps":
		return len(parts) >= 3 && parts[2] == "output"
	case "me", "space", "now":
		return len(parts) == 1
	default:
		return loops[parts[0]] != nil
	}
}
