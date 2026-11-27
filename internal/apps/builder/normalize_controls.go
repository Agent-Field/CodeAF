package builder

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func normalizeCommandControls(files map[string]string, app schema.App) ([]schema.Problem, error) {
	if len(app.Commands) == 0 {
		return nil, nil
	}
	paths := definition(files).ScreenFiles()
	// The plan records the landing order explicitly. The remaining files
	// still have a stable order when the outline is incomplete.
	if plan, err := schema.ParsePlan([]byte(files[schema.PlanFile])); err == nil {
		for i := len(plan.Screens) - 1; i >= 0; i-- {
			path := schema.ScreensDir + "/" + plan.Screens[i].ID + ".json"
			if n := slices.Index(paths, path); n > 0 {
				paths = append([]string{path}, slices.Delete(paths, n, n+1)...)
			}
		}
	}
	var notes []schema.Problem
	for _, command := range check.UnplacedCommands(definition(files)) {
		typ := command.For
		if typ == "" && (len(command.Do) != 0 || len(command.Params) != 0) {
			continue
		}
		if typ != "" && app.Capabilities.Records[typ] == "" {
			continue
		}
		if typ == "" && command.Run == "" {
			continue
		}
		for _, path := range paths {
			screen, err := schema.ParseScreen([]byte(files[path]))
			if err != nil || screen.Body == nil || len(screen.Params) != 0 {
				continue
			}
			part := "control-" + command.ID
			var control *schema.Node
			if typ != "" {
				part = "row-actions-" + typ
				// A Kit 2 list renders the registered commands of its
				// type through its row menu. This list needs no new verb.
				control = &schema.Node{ID: part, Part: "RecordList",
					Label: displayName(app.Records[typ].Label, typ),
					Bind:  &schema.Bind{Records: typ}}
			} else {
				control = &schema.Node{ID: part, Part: "Button", Label: command.Label,
					On: map[string][]schema.Action{"click": {{"runs.start": command.Run}}}}
			}
			if strings.TrimSpace(command.Label) == "" || nodeHasID(screen.Body, part) {
				continue
			}
			screen.Body = &schema.Node{ID: part + "-layout", Part: "Stack", Children: []*schema.Node{screen.Body, control}}
			if err := saveJSON(files, path, screen); err != nil {
				return nil, err
			}
			notes = append(notes, schema.Problem{File: path, Path: part, Code: normalizationNote,
				Message: fmt.Sprintf("Placed %s on %s.", command.Label, screen.Title)})
			break
		}
	}
	return notes, nil
}

func nodeHasID(node *schema.Node, id string) bool {
	return node != nil && (node.ID == id || slices.ContainsFunc(node.Children, func(child *schema.Node) bool { return nodeHasID(child, id) }))
}
