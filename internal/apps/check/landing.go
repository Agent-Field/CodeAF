package check

import (
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

const MinLandingRows = 20

// LandingCollections returns the row collections on the first screen a person
// opens. Metrics and detail bindings do not contribute records.
func LandingCollections(def schema.Definition) []schema.Bind {
	ids := def.ScreenIDs()
	if len(ids) == 0 {
		return nil
	}
	screen, _ := schema.ParseScreen(def.Files["screens/"+ids[0]+".json"])
	var collections []schema.Bind
	walkDepthNodes(screen.Body, func(node *schema.Node) {
		if node.Bind == nil || node.Bind.Records == "" {
			return
		}
		switch node.Part {
		case "List", "RecordList", "Table", "Board", "Timeline", "Calendar", "Grid":
			collections = append(collections, *node.Bind)
		}
	})
	return collections
}

// LandingRows uses the same declarations and store query as the app. The paths
// a row can appear through do not increase its contribution to the count.
func LandingRows(def schema.Definition, store *data.Store, collections []schema.Bind) map[string][]data.Row {
	result := map[string][]data.Row{}
	if len(collections) == 0 {
		return result
	}
	app, _ := schema.ParseApp(def.Files[schema.AppFile])
	screen, _ := schema.ParseScreen(def.Files["screens/"+def.ScreenIDs()[0]+".json"])
	scope := data.Scope{App: app.ID}
	seen := map[string]bool{}
	for _, bind := range collections {
		limit := 500
		var page *data.Page
		var err error
		if bind.View == "" {
			page, err = store.Query(scope, bind.Records, bind.Query, "", limit)
		} else {
			view := screen.Views[bind.View]
			if view.Type == "" || view.Relate != nil || len(view.Aggregate) != 0 {
				continue
			}
			if view.Window != nil && view.Window.Limit > 0 {
				limit = min(limit, view.Window.Limit)
			}
			ir, queryErr := data.DeclaredViewIR(view, nil)
			if queryErr != nil {
				continue
			}
			ir.Group = nil
			page, err = store.QueryRows(scope, &ir, "", limit)
		}
		if err != nil {
			continue
		}
		record := app.Records[bind.Records]
		for _, row := range page.Rows {
			title := ExampleTitle(record, row)
			// Projection can omit a title that the detail still exposes. The
			// same app scope is used when reading it, including field policy.
			if title == "" {
				full, _ := store.Get(scope, bind.Records, row.ID(), false)
				title = ExampleTitle(record, full)
			}
			key := bind.Records + "\x00" + strings.ToLower(title)
			if title == "" || PlaceholderTitle(title) || seen[row.ID()] || seen[key] {
				continue
			}
			seen[row.ID()], seen[key] = true, true
			result[bind.Records] = append(result[bind.Records], row)
		}
	}
	return result
}

// ExampleTitle follows the record's display fields when recognising a named
// example. A title declared by the author takes precedence over conventions.
func ExampleTitle(record schema.RecordType, row map[string]any) string {
	keys := []string{record.Title, "title", "name", "subject"}
	if record.Present != nil {
		keys = append([]string{record.Present.Title}, keys...)
	}
	for _, key := range slices.Compact(keys) {
		if value, ok := row[key].(string); key != "" && ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// WorkspaceFollows reports an on-screen detail panel for the supplied list.
func WorkspaceFollows(root *schema.Node, list string) bool {
	found := false
	walkDepthNodes(root, func(node *schema.Node) {
		if node.Part != "Panel" || node.Bind == nil || node.Bind.Selection != list {
			return
		}
		walkDepthNodes(node, func(child *schema.Node) {
			if child.Bind != nil && (child.Part == "RecordPage" || child.Part == "Thread") &&
				strings.HasPrefix(child.Bind.Record, "$selection."+list+".") {
				found = true
			}
		})
	})
	return found
}
