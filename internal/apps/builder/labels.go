package builder

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// aiMark ends every label of an AI-made value or AI action (rule 9).
const aiMark = " · AI"

// aiVerbs are the action verbs that call a model.
var aiVerbs = map[string]bool{spec.VerbAISort: true, spec.VerbAgentAsk: true}

// humanize turns an id into a label: publish_on -> "Publish on".
func humanize(id string) string {
	words := strings.FieldsFunc(id, func(r rune) bool { return r == '_' || r == '-' })
	if len(words) == 0 {
		return id
	}
	text := strings.ToLower(strings.Join(words, " "))
	runes := []rune(text)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// humanLabel keeps a label a person wrote and replaces a missing one or one
// that is only the id spelled in code (equal to it, or snake/kebab case).
func humanLabel(label, id string) string {
	trimmed := strings.TrimSpace(strings.TrimSuffix(label, aiMark))
	switch {
	case trimmed == "" || trimmed == id:
		return humanize(id)
	case codeCase(trimmed):
		return humanize(trimmed)
	}
	return label
}

func codeCase(text string) bool {
	return !strings.ContainsRune(text, ' ') && strings.ContainsAny(text, "_-") || text == strings.ToLower(text) && strings.ContainsRune(text, '_')
}

func withAIMark(label string) string {
	if strings.HasSuffix(strings.TrimSpace(label), "AI") {
		return label
	}
	return strings.TrimSpace(label) + aiMark
}

// labelSetter writes label into obj[key] when it differs, reporting a change.
func setLabel(obj map[string]any, key, label string) bool {
	if old, _ := obj[key].(string); old == label {
		return false
	}
	obj[key] = label
	return true
}

// polishLabels writes human labels and AI marks into app.json and the screens
// before Check, so no id reaches the owner's screen. Every other byte of
// meaning stays as the model wrote it. It returns the paths it changed.
func polishLabels(files spec.Files) []string {
	var changed []string
	app, ok := decodeObject(files[spec.AppFile])
	if !ok {
		return nil
	}
	fieldLabels := map[string]string{}
	if polishApp(app, fieldLabels) && encodeInto(files, spec.AppFile, app) {
		changed = append(changed, spec.AppFile)
	}
	for _, path := range files.Paths() {
		if !strings.HasPrefix(path, "screens/") {
			continue
		}
		screen, ok := decodeObject(files[path])
		if ok && polishScreen(screen, fieldLabels) && encodeInto(files, path, screen) {
			changed = append(changed, path)
		}
	}
	return changed
}

func polishApp(app map[string]any, fieldLabels map[string]string) bool {
	changed := false
	records, _ := app["records"].(map[string]any)
	for id, raw := range records {
		record, _ := raw.(map[string]any)
		if record == nil {
			continue
		}
		label, _ := record["label"].(string)
		changed = setLabel(record, "label", humanLabel(label, id)) || changed
		fields, _ := record["fields"].(map[string]any)
		for name, rawField := range fields {
			field, _ := rawField.(map[string]any)
			if field == nil {
				continue
			}
			label, _ := field["label"].(string)
			label = humanLabel(label, name)
			if ai, _ := field["ai"].(string); ai != "" {
				label = withAIMark(label)
			}
			changed = setLabel(field, "label", label) || changed
			fieldLabels[name] = strings.TrimSuffix(label, aiMark)
		}
	}
	commands, _ := app["commands"].([]any)
	for _, raw := range commands {
		command, _ := raw.(map[string]any)
		if command == nil {
			continue
		}
		id, _ := command["id"].(string)
		label, _ := command["label"].(string)
		label = humanLabel(label, id)
		if actionsUseAI(command["do"]) {
			label = withAIMark(label)
		}
		changed = setLabel(command, "label", label) || changed
	}
	return changed
}

func polishScreen(screen map[string]any, fieldLabels map[string]string) bool {
	changed := false
	if id, _ := screen["screen"].(string); id != "" {
		title, _ := screen["title"].(string)
		changed = setLabel(screen, "title", humanLabel(title, id))
	}
	walkNodes(screen, func(node map[string]any) {
		changed = polishNode(node, fieldLabels) || changed
	})
	return changed
}

func polishNode(node map[string]any, fieldLabels map[string]string) bool {
	changed := false
	if label, ok := node["label"].(string); ok && codeCase(strings.TrimSpace(label)) {
		changed = setLabel(node, "label", humanize(label))
	}
	if label, ok := node["label"].(string); ok && label != "" {
		on, _ := node["on"].(map[string]any)
		for _, actions := range on {
			if actionsUseAI(actions) {
				changed = setLabel(node, "label", withAIMark(label)) || changed
			}
		}
	}
	props, _ := node["props"].(map[string]any)
	if views, ok := props["views"].([]any); ok {
		for i, raw := range views {
			if view, ok := raw.(string); ok {
				if titled := viewTitle(view, fieldLabels); titled != view {
					views[i], changed = titled, true
				}
			}
		}
	}
	return changed
}

// viewTitle names the field in "<Kind> by <field>" by its label.
func viewTitle(view string, fieldLabels map[string]string) string {
	head, field, found := strings.Cut(view, " by ")
	if !found || !codeCase(field) {
		return view
	}
	label := fieldLabels[field]
	if label == "" {
		label = humanize(field)
	}
	return head + " by " + strings.ToLower(label[:1]) + label[1:]
}

func actionsUseAI(raw any) bool {
	actions, _ := raw.([]any)
	for _, a := range actions {
		action, _ := a.(map[string]any)
		for verb := range action {
			if aiVerbs[verb] {
				return true
			}
		}
	}
	return false
}

// walkNodes visits nav, body and every child and slot node.
func walkNodes(screen map[string]any, visit func(map[string]any)) {
	var walk func(any)
	walk = func(raw any) {
		node, _ := raw.(map[string]any)
		if node == nil {
			return
		}
		visit(node)
		children, _ := node["children"].([]any)
		for _, child := range children {
			walk(child)
		}
		slots, _ := node["slots"].(map[string]any)
		for _, slot := range slots {
			walk(slot)
		}
	}
	walk(screen["nav"])
	walk(screen["body"])
}

func decodeObject(data []byte) (map[string]any, bool) {
	var obj map[string]any
	return obj, json.Unmarshal(data, &obj) == nil && obj != nil
}

func encodeInto(files spec.Files, path string, obj map[string]any) bool {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if enc.Encode(obj) != nil {
		return false
	}
	files[path] = buf.Bytes()
	return true
}
