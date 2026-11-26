package apppackage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"reflect"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"gopkg.in/yaml.v3"
)

type Conflict struct {
	ID       string          `json:"id"`
	File     string          `json:"file"`
	Path     string          `json:"path"`
	Base     json.RawMessage `json:"base,omitempty"`
	Local    json.RawMessage `json:"local,omitempty"`
	Upstream json.RawMessage `json:"upstream,omitempty"`
}

type missing struct{}

// Merge leaves unresolved values at their local value for preview. Publication
// must wait for a choice at each conflict and a Check of the resolved files.
func Merge(base, local, upstream map[string]string, choices map[string]string) (map[string]string, []Conflict, error) {
	names := map[string]bool{}
	for _, files := range []map[string]string{base, local, upstream} {
		for name := range files {
			names[name] = true
		}
	}
	m := merger{choices: choices}
	out := make(map[string]string, len(names))
	for _, name := range slices.Sorted(maps.Keys(names)) {
		m.file = name
		b, l, u := fileValue(base, name), fileValue(local, name), fileValue(upstream, name)
		if picked, ok := unchanged(b, l, u); ok && name != schema.AppFile {
			if raw, exists := picked.(string); exists {
				out[name] = raw
			}
			continue
		}
		before, beforeOK := decode(name, b)
		mine, mineOK := decode(name, l)
		theirs, theirsOK := decode(name, u)
		if beforeOK && mineOK && theirsOK {
			merged := m.value("", before, mine, theirs)
			switch {
			case reflect.DeepEqual(merged, mine):
				out[name] = l.(string)
			case reflect.DeepEqual(merged, theirs):
				out[name] = u.(string)
			default:
				raw, err := encode(name, merged)
				if err != nil {
					return nil, nil, err
				}
				out[name] = raw
			}
			continue
		}
		if raw, ok := m.conflict("", b, l, u).(string); ok {
			out[name] = raw
		}
	}
	if len(choices) != 0 {
		for _, id := range slices.Sorted(maps.Keys(choices)) {
			if !slices.ContainsFunc(m.conflicts, func(c Conflict) bool { return c.ID == id }) {
				return nil, nil, fmt.Errorf("no conflict has ID %q", id)
			}
			if choices[id] != "local" && choices[id] != "upstream" {
				return nil, nil, fmt.Errorf("choose local or upstream for conflict %q", id)
			}
		}
	}
	return out, m.conflicts, nil
}

func fileValue(files map[string]string, name string) any {
	value, ok := files[name]
	if !ok {
		return missing{}
	}
	return value
}

func decode(name string, value any) (any, bool) {
	raw, ok := value.(string)
	if !ok {
		return nil, false
	}
	var decoded any
	switch path.Ext(name) {
	case ".json":
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.UseNumber()
		if dec.Decode(&decoded) != nil {
			return nil, false
		}
		if name == schema.AppFile {
			if app, ok := decoded.(map[string]any); ok {
				app["version"] = json.Number("1")
			}
		}
	case ".yaml", ".yml":
		if yaml.Unmarshal([]byte(raw), &decoded) != nil {
			return nil, false
		}
		if _, err := json.Marshal(decoded); err != nil {
			return nil, false
		}
	default:
		return nil, false
	}
	return decoded, true
}

func encode(name string, value any) (string, error) {
	if path.Ext(name) != ".json" {
		raw, err := yaml.Marshal(value)
		return string(raw), err
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetIndent("", "  ")
	err := enc.Encode(value)
	return out.String(), err
}

type merger struct {
	file      string
	choices   map[string]string
	conflicts []Conflict
}

func unchanged(base, local, upstream any) (any, bool) {
	switch {
	case reflect.DeepEqual(local, upstream), reflect.DeepEqual(base, upstream):
		return local, true
	case reflect.DeepEqual(base, local):
		return upstream, true
	default:
		return nil, false
	}
}

func (m *merger) value(at string, base, local, upstream any) any {
	// A changed permission or budget needs a choice even when the local
	// version still has the original grant. The person can review other
	// upstream edits without agreeing to the new permission request.
	protect, descend := false, false
	if m.file == schema.AppFile {
		protect, descend = requestPath(at)
	}
	if protect && !reflect.DeepEqual(base, upstream) && !reflect.DeepEqual(local, upstream) {
		return m.conflict(at, base, local, upstream)
	}
	b, bOK := base.(map[string]any)
	l, lOK := local.(map[string]any)
	u, uOK := upstream.(map[string]any)
	if descend && bOK && lOK {
		if uOK {
			return m.object(at, b, l, u)
		}
		if hasProtectedRequest(at, b) {
			return m.conflict(at, base, local, upstream)
		}
	}
	if value, ok := unchanged(base, local, upstream); ok {
		return value
	}
	if bOK && lOK && uOK {
		return m.object(at, b, l, u)
	}
	if b, l, u := nodeList(base), nodeList(local), nodeList(upstream); b != nil && l != nil && u != nil && slices.Equal(b.ids, l.ids) && slices.Equal(b.ids, u.ids) {
		merged := make([]any, len(b.ids))
		for i, id := range b.ids {
			merged[i] = m.value(at+"/"+pointer(id), b.nodes[id], l.nodes[id], u.nodes[id])
		}
		return merged
	}
	return m.conflict(at, base, local, upstream)
}

// requestPath identifies requests that need a choice and their containing
// objects. Walk those objects even when all of their changes are upstream.
func requestPath(at string) (protect, descend bool) {
	switch at {
	case "/capabilities", "/budget", "/roles", "/policies", "/audiences", "/channels":
		return true, false
	case "", "/records":
		return false, true
	}
	parts := strings.Split(at, "/")
	if len(parts) < 3 || parts[1] != "records" {
		return false, false
	}
	if len(parts) == 3 {
		return false, true
	}
	if len(parts) == 4 && parts[3] == "policy" {
		return true, false
	}
	if parts[3] == "fields" {
		if len(parts) <= 5 {
			return false, true
		}
		if len(parts) == 6 {
			switch parts[5] {
			case "class", "masked", "blind":
				return true, false
			}
		}
	}
	return false, false
}

// Removing a field or record can remove requests nested under it. Other
// removals use the same merge rules as ordinary definition edits.
func hasProtectedRequest(at string, object map[string]any) bool {
	for name, value := range object {
		child := at + "/" + pointer(name)
		protect, descend := requestPath(child)
		if protect {
			return true
		}
		if nested, ok := value.(map[string]any); descend && ok && hasProtectedRequest(child, nested) {
			return true
		}
	}
	return false
}

func (m *merger) object(at string, base, local, upstream map[string]any) map[string]any {
	keys := map[string]bool{}
	for _, obj := range []map[string]any{base, local, upstream} {
		for key := range obj {
			keys[key] = true
		}
	}
	merged := make(map[string]any, len(keys))
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		value := m.value(at+"/"+pointer(key), member(base, key), member(local, key), member(upstream, key))
		if _, absent := value.(missing); !absent {
			merged[key] = value
		}
	}
	return merged
}

func member(values map[string]any, key string) any {
	value, ok := values[key]
	if !ok {
		return missing{}
	}
	return value
}

func pointer(key string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(key)
}

type identifiedList struct {
	ids   []string
	nodes map[string]map[string]any
}

func nodeList(value any) *identifiedList {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := &identifiedList{nodes: map[string]map[string]any{}}
	for _, item := range items {
		node, _ := item.(map[string]any)
		id, _ := node["id"].(string)
		if id == "" || out.nodes[id] != nil {
			return nil
		}
		out.nodes[id] = node
		out.ids = append(out.ids, id)
	}
	return out
}

func (m *merger) conflict(at string, base, local, upstream any) any {
	id := m.file + "#" + at
	m.conflicts = append(m.conflicts, Conflict{
		ID: id, File: m.file, Path: at, Base: conflictJSON(base), Local: conflictJSON(local), Upstream: conflictJSON(upstream),
	})
	if m.choices[id] == "upstream" {
		return upstream
	}
	return local
}

func conflictJSON(value any) json.RawMessage {
	if _, absent := value.(missing); absent {
		return nil
	}
	raw, _ := json.Marshal(value)
	return raw
}
