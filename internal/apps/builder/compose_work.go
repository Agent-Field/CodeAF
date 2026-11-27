package builder

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// composeWorkScreens fills the workspace the model leaves implicit when it
// authors a plain collection. It keeps the authored queries and controls.
func composeWorkScreens(files map[string]string, types schema.TypeSet) ([]schema.Problem, error) {
	app, err := schema.ParseApp([]byte(files[schema.AppFile]))
	if err != nil || app.Kit != 2 {
		return nil, nil
	}
	var notes []schema.Problem
	for i, id := range definition(files).ScreenIDs() {
		path := schema.ScreensDir + "/" + id + ".json"
		screen, err := schema.ParseScreen([]byte(files[path]))
		if err != nil || screen.Body == nil {
			continue
		}
		list := mainCollection(screen.Body)
		var subject string
		kind := "queue-detail"
		if list != nil && !check.WorkspaceFollows(screen.Body, list.ID) {
			subject = list.Bind.Records
		} else if list == nil {
			record := mainRecordPage(screen.Body)
			if record != nil && (i == 0 || screen.Body == record) {
				subject = record.Bind.Type
				if i != 0 {
					kind = "record"
				}
			}
		}
		if app.Capabilities.Records[subject] == "" {
			continue
		}
		slots := workSlots(app, subject, types)
		if list != nil && (list.Part == "Board" || slices.Contains(workWords(list.Props["views"]), "board")) {
			if state := app.Records[subject].Fields[slots.State]; state.Type == "enum" {
				kind = "board-detail"
			}
		}
		base := maps.Clone(files)
		delete(base, path)
		layout, err := schema.CompileRecipe(definition(base), types, schema.Recipe{
			Kind: "add-screen", ID: id, As: kind, Title: screen.Title, Record: subject, Slots: &slots,
		})
		if err != nil {
			continue // Check can point to the declaration that prevents lowering.
		}
		compiled, err := schema.ParseScreen(layout.Screen)
		if err != nil {
			return nil, err
		}
		if list == nil {
			// Keep the route's declared parameters so existing open actions
			// continue to work. The landing itself requires no parameter.
			screen.Body, screen.Views = compiled.Body, compiled.Views
			if kind == "record" {
				screen.Params = compiled.Params
			}
		} else {
			lowerCollection(&screen, list, compiled)
		}
		if len(layout.App) != 0 {
			files[schema.AppFile] = string(layout.App)
			app, _ = schema.ParseApp(layout.App)
		}
		if err := saveJSON(files, path, screen); err != nil {
			return nil, err
		}
		notes = append(notes, schema.Problem{File: path, Code: normalizationNote,
			Message: fmt.Sprintf("Added a record workspace to %s.", screen.Title)})
	}
	return notes, nil
}

func mainRecordPage(node *schema.Node) *schema.Node {
	if node == nil {
		return nil
	}
	if node.Part == "RecordPage" && node.Bind != nil {
		return node
	}
	for _, child := range node.Children {
		if found := mainRecordPage(child); found != nil {
			return found
		}
	}
	return nil
}

func workSlots(app schema.App, typ string, types schema.TypeSet) schema.ScreenSlots {
	record := app.Records[typ]
	fields := maps.Clone(record.Fields)
	if base, ok := types.ForRecord(typ, record); record.Uses && ok {
		fields = maps.Clone(base.Fields)
		maps.Copy(fields, record.Fields)
	}
	var slots schema.ScreenSlots
	for _, field := range []string{"state", "status", "stage"} {
		if fields[field].Type == "enum" && showableStoryField(fields[field]) {
			slots.State = field
			break
		}
	}
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		field := fields[name]
		if (field.Type == "file" || field.Type == "files") && showableStoryField(field) &&
			(record.Policy == nil || record.Policy.Fields[name] == "") {
			slots.Visual = &schema.ScreenVisual{Kind: "file", Title: "Evidence", Field: name}
			break
		}
	}
	return slots
}

func lowerCollection(screen *schema.Screen, old *schema.Node, compiled schema.Screen) {
	id := old.ID
	if id == "" {
		id = "work"
	}
	queue := compiled.Body.Children[0]
	queue.Label, queue.On, queue.Bind = old.Label, old.On, old.Bind
	queue.Props = maps.Clone(queue.Props)
	for _, name := range []string{"empty", "owner", "reason", "search"} {
		if value, found := old.Props[name]; found {
			queue.Props[name] = value
		}
	}
	if queue.Bind.View == "" && queue.Bind.Query == "" {
		queryID := id + "-rows"
		if screen.Views == nil {
			screen.Views = map[string]schema.QueryIR{}
		}
		screen.Views[queryID] = compiled.Views["work"]
		bind := *queue.Bind
		bind.View = queryID
		queue.Bind = &bind
	}
	var rename func(*schema.Node)
	rename = func(node *schema.Node) {
		if node == queue {
			node.ID = id
		} else {
			node.ID = id + "-" + node.ID
			if node.Bind != nil {
				if node.Bind.Selection == "work" {
					node.Bind.Selection = id
				}
				node.Bind.Record = strings.Replace(node.Bind.Record, "$selection.work.", "$selection."+id+".", 1)
				node.Bind.File = strings.Replace(node.Bind.File, "$selection.work.", "$selection."+id+".", 1)
			}
		}
		for _, child := range node.Children {
			rename(child)
		}
	}
	rename(compiled.Body)
	*old = *compiled.Body
}

func workWords(value any) []string {
	list, _ := value.([]any)
	out := make([]string, 0, len(list))
	for _, value := range list {
		word, _ := value.(string)
		out = append(out, word)
	}
	return out
}
