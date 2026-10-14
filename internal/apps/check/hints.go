package check

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// Hints name the valid choices so a repair can pick one without the catalog.

func partNamesHint(kit *spec.Kit) string {
	names := make([]string, 0, len(kit.Parts))
	for _, part := range kit.Parts {
		names = append(names, part.Name)
	}
	return "choose one of the kit parts: " + strings.Join(names, ", ")
}

func bindKindsHint(part *spec.Part) string {
	if len(part.Bind) == 0 {
		return "remove bind: " + part.Name + " takes no binding; bind a part that does (RecordList, RecordPage, ...)"
	}
	return "use a binding kind " + part.Name + " supports: " + strings.Join(part.Bind, ", ")
}

func propNamesHint(part *spec.Part) string {
	if len(part.Props) == 0 {
		return "remove props: " + part.Name + " has no props"
	}
	return "use only " + part.Name + " props: " + strings.Join(keys(part.Props), ", ")
}

// toneValues mirrors the "tone" prop type in kit.json.
var toneValues = []string{"neutral", "accent", "positive", "caution", "critical"}

func propValueHint(kit *spec.Kit, part *spec.Part, key string, prop *spec.Prop) string {
	switch prop.Type {
	case "enum":
		return fmt.Sprintf("set %s.%s to one of: %s", part.Name, key, strings.Join(prop.Values, ", "))
	case "tone":
		return fmt.Sprintf("set %s.%s to one of: %s", part.Name, key, strings.Join(toneValues, ", "))
	}
	if desc := kit.PropTypes[prop.Type]; desc != "" {
		return fmt.Sprintf("%s.%s must be %s (%s)", part.Name, key, prop.Type, desc)
	}
	return fmt.Sprintf("%s.%s must be %s", part.Name, key, prop.Type)
}

// schemaHint is one JSON-schema failure turned into an exact instruction.
type schemaHint struct {
	match *regexp.Regexp
	hint  func(issue spec.Issue, m []string) string
}

// schemaHints maps the validator's message shapes to repairs. Messages already
// carry the allowed values, limits and key names; the hint says what to do
// with them at the failing path.
var schemaHints = []schemaHint{
	{regexp.MustCompile(`value must be one of (.+)$`), func(i spec.Issue, m []string) string {
		return "set " + at(i) + " to exactly one of " + m[1]
	}},
	{regexp.MustCompile(`additionalProperties (.+) not allowed`), func(i spec.Issue, m []string) string {
		return "remove " + m[1] + " from " + at(i) + "; use only the keys the file schema lists for this object" + allowedKeys(i)
	}},
	{regexp.MustCompile(`missing propert(?:y|ies):? (.+)$`), func(i spec.Issue, m []string) string {
		return "add " + m[1] + " to " + at(i)
	}},
	{regexp.MustCompile(`length must be <= (\d+), but got (\d+)`), func(i spec.Issue, m []string) string {
		return fmt.Sprintf("shorten %s to at most %s characters (now %s)", at(i), m[1], m[2])
	}},
	{regexp.MustCompile(`length must be >= (\d+)`), func(i spec.Issue, m []string) string {
		return fmt.Sprintf("write at least %s characters at %s", m[1], at(i))
	}},
	{regexp.MustCompile(`does not match pattern (.+)$`), func(i spec.Issue, m []string) string {
		return "rewrite " + at(i) + " to match " + m[1] + " (ids: lowercase letters, digits, _ or -, starting with a letter)"
	}},
	{regexp.MustCompile(`expected (\w+), but got (\w+)`), func(i spec.Issue, m []string) string {
		return fmt.Sprintf("write %s as a JSON %s, not a %s", at(i), m[1], m[2])
	}},
	{regexp.MustCompile(`must be (<=|>=|<|>) ([\d.]+)`), func(i spec.Issue, m []string) string {
		return fmt.Sprintf("set %s to a number %s %s", at(i), m[1], m[2])
	}},
	{regexp.MustCompile(`maximum (\d+) items required`), func(i spec.Issue, m []string) string {
		return fmt.Sprintf("keep at most %s items at %s", m[1], at(i))
	}},
	{regexp.MustCompile(`minimum (\d+) items required`), func(i spec.Issue, m []string) string {
		return fmt.Sprintf("give %s at least %s items", at(i), m[1])
	}},
}

func at(i spec.Issue) string {
	if i.Path == "" {
		return i.File
	}
	return i.File + ":" + i.Path
}

// schemaIssueHint returns the exact repair for one schema failure.
func schemaIssueHint(issue spec.Issue) string {
	for _, h := range schemaHints {
		if m := h.match.FindStringSubmatch(issue.Message); m != nil {
			return h.hint(issue, m)
		}
	}
	return "make " + at(issue) + " match the " + issue.File + " part of the File schemas"
}

// schemaURLs maps a definition file to its schema.
func schemaURL(file string) string {
	switch {
	case file == spec.AppFile:
		return spec.AppSchemaURL
	case file == spec.PlanFile:
		return spec.PlanSchemaURL
	case strings.HasPrefix(file, "screens/"):
		return spec.ScreenSchemaURL
	case strings.HasPrefix(file, "runs/"):
		return spec.RunSchemaURL
	}
	return ""
}

// allowedKeys lists the properties the schema declares for the object at the
// issue's path, following local $refs; "" when the node is not a plain object.
func allowedKeys(issue spec.Issue) string {
	raw, err := spec.SchemaJSON(schemaURL(issue.File))
	if err != nil {
		return ""
	}
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		return ""
	}
	node := resolveRef(root, root)
	for _, seg := range strings.Split(strings.TrimPrefix(issue.Path, "/"), "/") {
		if seg == "" || node == nil {
			continue
		}
		node = resolveRef(root, childSchema(node, strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")))
	}
	props, _ := node["properties"].(map[string]any)
	if len(props) == 0 {
		return ""
	}
	return " (allowed: " + strings.Join(keys(props), ", ") + ")"
}

func childSchema(node map[string]any, seg string) map[string]any {
	if props, ok := node["properties"].(map[string]any); ok {
		if child, ok := props[seg].(map[string]any); ok {
			return child
		}
	}
	for _, key := range []string{"items", "additionalProperties"} {
		if child, ok := node[key].(map[string]any); ok {
			return child
		}
	}
	if pattern, ok := node["patternProperties"].(map[string]any); ok {
		for _, child := range pattern {
			if m, ok := child.(map[string]any); ok {
				return m
			}
		}
	}
	return nil
}

func resolveRef(root, node map[string]any) map[string]any {
	for i := 0; node != nil && i < 8; i++ {
		ref, ok := node["$ref"].(string)
		if !ok || !strings.HasPrefix(ref, "#/") {
			return node
		}
		var next any = root
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			m, _ := next.(map[string]any)
			next = m[part]
		}
		node, _ = next.(map[string]any)
	}
	return node
}

func recordTypesHint(b *spec.Bundle) string {
	return "name one of the record types in app.json: " + strings.Join(keys(b.App.Records), ", ")
}

func commandsHint(b *spec.Bundle) string {
	ids := make([]string, 0, len(b.App.Commands))
	for _, c := range b.App.Commands {
		ids = append(ids, c.ID)
	}
	if len(ids) == 0 {
		return "app.json has no commands; add the command to app.json commands or remove it here"
	}
	return "name one of the app.json commands: " + strings.Join(ids, ", ")
}
