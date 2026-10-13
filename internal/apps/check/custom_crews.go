package check

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/custom"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func customDefinitions(files spec.Files) (spec.Files, []Issue) {
	definitions := make(spec.Files, len(files))
	var issues []Issue
	for _, name := range files.Paths() {
		if !custom.IsFile(name) {
			definitions[name] = files[name]
			continue
		}
		for _, problem := range custom.Validate(name, files[name]) {
			issues = append(issues, Issue{Issue: problem, FixHint: "use only react and @codeaf/bridge inside the custom sandbox"})
		}
	}
	b, err := spec.Decode(files)
	if err == nil {
		for _, id := range b.ScreenIDs() {
			issues = append(issues, customReferences(files, "screens/"+id+".json", b.Screens[id])...)
		}
	}
	return definitions, issues
}

func customReferences(files spec.Files, file string, screen *spec.Screen) []Issue {
	var issues []Issue
	var walk func(*spec.Node, string)
	walk = func(n *spec.Node, at string) {
		if n == nil {
			return
		}
		if n.Part == "Custom" {
			name, _ := n.Props["file"].(string)
			_, exists := files[name]
			if !custom.IsFile(name) || !exists {
				issues = append(issues, finding(file, at+"/props/file", custom.Rule,
					fmt.Sprintf("Custom file %q must exist at custom/<name>.tsx", name), "write the custom file through the Builder"))
			}
		}
		nodeChildren(n, at, walk)
	}
	walk(screen.Nav, "/nav")
	walk(screen.Body, "/body")
	return issues
}
