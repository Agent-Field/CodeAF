package builder

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// DisplayChange is written from the actual screen change. The card never
// needs to display the model's transcript or identify a screen by its key.
type DisplayChange struct {
	Part   string `json:"part"`
	Before string `json:"before"`
	After  string `json:"after"`
}

func changedScreens(before, after map[string]string) []string {
	var changed []string
	for _, path := range slices.Sorted(maps.Keys(after)) {
		if strings.HasPrefix(path, "screens/") && before[path] != after[path] {
			changed = append(changed, path)
		}
	}
	return changed
}

func screenChange(before, after map[string]string, path string, target *Target) DisplayChange {
	old, _ := schema.ParseScreen([]byte(before[path]))
	next, _ := schema.ParseScreen([]byte(after[path]))
	app, _ := schema.ParseApp([]byte(after[schema.AppFile]))
	name := displayName(next.Title, "Screen")
	delta := DisplayChange{Part: name, Before: "Previous layout", After: "Updated layout"}
	if old.Body == nil {
		delta.Before, delta.After = "No screen", "New screen"
		return delta
	}
	if old.Title != next.Title {
		delta.Before = displayName(old.Title, "Previous screen name")
		delta.After = name
	}
	past := map[string]*schema.Node{}
	var index func(*schema.Node)
	index = func(node *schema.Node) {
		if node == nil {
			return
		}
		past[node.ID] = node
		for _, child := range node.Children {
			index(child)
		}
	}
	index(old.Nav)
	index(old.Body)
	var find func(*schema.Node) (DisplayChange, bool)
	find = func(node *schema.Node) (DisplayChange, bool) {
		if node == nil {
			return DisplayChange{}, false
		}
		if prior := past[node.ID]; prior != nil && (prior.Part != node.Part || !reflect.DeepEqual(prior.Props, node.Props)) {
			want := target == nil || target.Node == "" || target.Node == node.ID
			if want && (node.Part == "RecordList" || node.Part == "Table" || node.Part == "Board") {
				typ := ""
				if node.Bind != nil {
					typ = node.Bind.Records
				}
				title := displayName(node.Label, name)
				if !strings.HasSuffix(strings.ToLower(title), " list") {
					title += " list"
				}
				return DisplayChange{
					Part: title, Before: collectionWords(prior, node, app.Records[typ]),
					After: collectionWords(node, prior, app.Records[typ]),
				}, true
			}
		}
		for _, child := range node.Children {
			if delta, ok := find(child); ok {
				return delta, true
			}
		}
		return DisplayChange{}, false
	}
	if collection, ok := find(next.Body); ok && collection.Before != collection.After {
		return collection
	}
	return delta
}

func collectionWords(node, other *schema.Node, record schema.RecordType) string {
	view := strings.ToLower(node.Part)
	if views, ok := node.Props["views"].([]any); ok && len(views) != 0 {
		view, _ = views[0].(string)
	}
	switch view {
	case "board":
		group := asProperty(node, "board_by")
		if group == "" {
			group = asProperty(node, "groupBy")
		}
		if group != "" {
			return "Board by " + displayName(record.Fields[group].Label, titleWords(group))
		}
		return "Board"
	case "calendar":
		return "Calendar"
	case "table", "recordlist", "list":
		if columns, ok := node.Props["columns"].([]any); ok {
			prior, _ := other.Props["columns"].([]any)
			for _, col := range columns {
				field, ok := col.(string)
				if ok && (field == asProperty(other, "board_by") || field == asProperty(other, "groupBy") || !slices.Contains(prior, col)) {
					return "Table with a " + displayName(record.Fields[field].Label, titleWords(field)) + " column"
				}
			}
		}
		return "Table"
	default:
		return "List view"
	}
}

func asProperty(node *schema.Node, name string) string {
	value, _ := node.Props[name].(string)
	return value
}

func titleWords(value string) string {
	return strings.Join(strings.FieldsFunc(value, func(r rune) bool { return r == '-' || r == '_' || unicode.IsSpace(r) }), " ")
}

func displayName(value, fallback string) string {
	value = strings.TrimSpace(value)
	folded := strings.ToLower(value)
	if value == "" || strings.ContainsAny(value, "`{}[]") || strings.Contains(folded, "node-") ||
		strings.Contains(folded, "recordlist") || strings.Contains(folded, "board_by") ||
		strings.Contains(folded, "app check") || strings.Contains(folded, "app look") {
		value = fallback
	}
	value = titleWords(value)
	if runes := []rune(value); len(runes) > 0 {
		runes[0] = unicode.ToUpper(runes[0])
		return string(runes)
	}
	return "Screen"
}

// A field-only revision may have no changed screen file to name the edit.
func recordFieldChange(before, after map[string]string) (DisplayChange, bool) {
	old, _ := schema.ParseApp([]byte(before[schema.AppFile]))
	next, _ := schema.ParseApp([]byte(after[schema.AppFile]))
	var change DisplayChange
	for _, name := range slices.Sorted(maps.Keys(next.Records)) {
		record := next.Records[name]
		prior, exists := old.Records[name]
		if !exists {
			continue
		}
		for _, key := range slices.Sorted(maps.Keys(record.Fields)) {
			if _, exists := prior.Fields[key]; exists {
				continue
			}
			if change.Part != "" {
				return DisplayChange{}, false
			}
			label := displayName(record.Fields[key].Label, key)
			change = DisplayChange{
				Part:   displayName(record.Label, name) + " fields",
				Before: "Without " + label,
				After:  "Includes " + label,
			}
		}
	}
	return change, change.Part != ""
}

func (s *Jobs) beforeTurn(j *job) map[string]string {
	if len(j.Checkpoints) != 0 {
		return j.Saved[j.Checkpoints[len(j.Checkpoints)-1].ID].Files
	}
	if j.App != "" {
		if live, err := s.opts.Store.Get(j.App); err == nil && live.Meta.Version == j.Base {
			return live.Files()
		}
	}
	return nil
}

func (s *Jobs) displaySummary(j *job, t turn) string {
	before, after := s.beforeTurn(j), j.Working.Files
	names := changedScreens(before, after)
	if len(names) == 1 {
		change := screenChange(before, after, names[0], t.Target)
		if change.After != "Updated layout" && change.After != "New screen" && change.After != change.Part {
			words := []rune(change.After)
			words[0] = unicode.ToLower(words[0])
			article := ""
			if strings.HasPrefix(change.After, "Table") || strings.HasPrefix(change.After, "Board") {
				article = "a "
			}
			return "The " + change.Part + " is now " + article + string(words) + "."
		}
		return change.Part + " is ready to try."
	}
	if j.App != "" && len(names) == 0 {
		if field, ok := recordFieldChange(before, after); ok {
			return field.Part + " now include " + strings.TrimPrefix(field.After, "Includes ") + "."
		}
		old, _ := schema.ParseApp([]byte(before[schema.AppFile]))
		next, _ := schema.ParseApp([]byte(after[schema.AppFile]))
		for _, command := range next.Commands {
			if command.For == "" || slices.ContainsFunc(old.Commands, func(prior schema.Command) bool { return prior.ID == command.ID }) {
				continue
			}
			record := displayName(next.Records[command.For].Label, command.For)
			return "Added " + displayName(command.Label, "an action") + " to " + record + " records."
		}
	}
	return displayName(j.Name, "Your app") + " is ready to try."
}
