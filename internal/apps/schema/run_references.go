package schema

import (
	"maps"
	"regexp"
	"slices"
	"strings"
)

const stepValuePath = `steps\.([a-z][a-z0-9_-]*)((?:\.[A-Za-z0-9_-]+)*)`

var (
	runOutputBinding = regexp.MustCompile(`^(?:/steps/[0-9]+)+/(?:args/|loop/over$)`)
	stepValueRef     = regexp.MustCompile(`^\$` + stepValuePath + `$`)
	stepTemplateRef  = regexp.MustCompile(`\{\{\s*` + stepValuePath + `\s*\}\}`)
)

func (c *checker) checkRunReferences(file string, raw any, ids, textOutputs map[string]bool) {
	walkStrings(raw, "", func(path, value string) {
		missing := func(ref, id string) {
			c.add(file, path, CodeStepRefUnknown, "%q reads step %q, which does not exist. Use a declared step ID: %s.", ref, id, strings.Join(slices.Sorted(maps.Keys(ids)), ", "))
		}
		if runOutputBinding.MatchString(path) {
			check := func(ref []string) {
				id, suffix := ref[1], ref[2]
				if !ids[id] {
					missing(ref[0], id)
					return
				}
				if !textOutputs[id] {
					return
				}
				switch {
				case suffix != "" && suffix != ".output":
					c.add(file, path, CodeStepRefUnknown, "%q cannot resolve. The agent step %q returns text at $steps.%s.output. For a list or named fields, use an ai step with a JSON output schema and example.", ref[0], id, id)
				case strings.HasSuffix(path, "/loop/over"):
					c.add(file, path, CodeStepRefUnknown, "%q does not produce a list. Use an ai step with an array in its output schema and example.", ref[0])
				}
			}
			if ref := stepValueRef.FindStringSubmatch(value); ref != nil {
				check(ref)
				return
			}
			for _, ref := range stepTemplateRef.FindAllStringSubmatch(value, -1) {
				check(ref)
			}
		}
		// Keep checking step IDs in expressions as well as direct bindings.
		for _, ref := range stepsRef.FindAllStringSubmatch(value, -1) {
			if !ids[ref[1]] {
				missing(ref[0], ref[1])
			}
		}
	})
}
