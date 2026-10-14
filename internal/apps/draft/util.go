package draft

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// slugPattern is the id shape app.schema.json demands of every id.
var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)

func checkSlug(what, id string) error {
	if !slugPattern.MatchString(id) {
		return refuse("%s id %q must be lowercase letters, digits, - or _, start with a letter, and be at most 48 characters", what, id)
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ids names the members of a set for an error hint, or "none yet".
func ids(keys []string) string {
	if len(keys) == 0 {
		return "none yet"
	}
	return strings.Join(keys, ", ")
}

// fieldList names a record's fields for a hint, cut to a readable length.
func fieldList(names []string) string {
	if len(names) > fieldHintLimit {
		return strings.Join(names[:fieldHintLimit], ", ") + ", …"
	}
	return ids(names)
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func contains(items []string, v string) bool {
	for _, item := range items {
		if item == v {
			return true
		}
	}
	return false
}

// nonEmpty trims and demands text.
func nonEmpty(what, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", refuse("%s must not be empty", what)
	}
	return text, nil
}

// titleOf makes an id readable: "close_on" becomes "Close on".
func titleOf(id string) string {
	words := strings.NewReplacer("_", " ", "-", " ").Replace(id)
	if words == "" {
		return id
	}
	return strings.ToUpper(words[:1]) + words[1:]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// asAny turns a typed value into the generic JSON value the schema checks.
func asAny(v any) any {
	raw, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

// issueLines renders schema issues as "<file>: <pointer>: <message>" lines,
// each ending in the same hint that says where to read the rule.
func issueLines(file string, issues []spec.Issue, hint string) []string {
	lines := make([]string, len(issues))
	for i, is := range issues {
		where := file + ": "
		if is.Path != "" {
			where += is.Path + ": "
		}
		lines[i] = where + is.Message + " — see: " + hint
	}
	return lines
}
