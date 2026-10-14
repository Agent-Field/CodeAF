package prompt

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// ShapeError is a reply that held no files object and no wrong shape the
// parser could place for certain. Its text is written for the model: it names
// the shape expected, the keys that came back and the exact paths wanted.
type ShapeError struct {
	Keys []string
	Want []string
}

func (e *ShapeError) Error() string {
	return fmt.Sprintf("reply with ONE JSON object whose keys are file paths and whose values are those files' contents, "+
		"for example {%q: {...}}; this reply must hold %s; got keys %q", e.example(), e.wanted(), e.Keys)
}

func (e *ShapeError) example() string {
	if len(e.Want) > 0 {
		return e.Want[0]
	}
	return "screens/<id>.json"
}

func (e *ShapeError) wanted() string {
	if len(e.Want) == 0 {
		return `app paths such as "app.json", "agent.md", "screens/<id>.json" or "runs/<id>.yaml"`
	}
	return fmt.Sprintf("exactly the paths %q", e.Want)
}

// normalize places the wrong shapes models commonly send when there is only
// one way to read them: a files list, folders spelled as nested objects, one
// wrapper key too many, or one bare screen. Anything else returns nil, so
// the caller reports a ShapeError rather than guessing.
func normalize(entries map[string]json.RawMessage, want []string) (map[string]json.RawMessage, string) {
	for _, shape := range shapes {
		if files := shape.place(entries, want); filesObject(files) {
			return files, shape.name
		}
	}
	return nil, ""
}

var shapes = []struct {
	name  string
	place func(map[string]json.RawMessage, []string) map[string]json.RawMessage
}{
	{"files list", filesList}, {"folder nesting", folders}, {"extra wrapper", wrapped}, {"bare screen", bareScreen},
}

// filesList reads {"files": [{"path": …, "content": …}, …]}.
func filesList(entries map[string]json.RawMessage, _ []string) map[string]json.RawMessage {
	var items []map[string]json.RawMessage
	if json.Unmarshal(entries["files"], &items) != nil || len(items) == 0 {
		return nil
	}
	files := map[string]json.RawMessage{}
	for _, item := range items {
		name, content := firstString(item, "path", "file", "name"), firstRaw(item, "content", "contents")
		if name == "" || content == nil {
			return nil
		}
		files[name] = content
	}
	return files
}

// folders reads {"screens": {"queue.json": …}, "app.json": …}: each folder
// key is joined back onto the file names inside it.
func folders(entries map[string]json.RawMessage, _ []string) map[string]json.RawMessage {
	files := map[string]json.RawMessage{}
	nested := false
	for key, raw := range entries {
		dir := strings.TrimSuffix(key, "/")
		inner := objectEntries(raw)
		if (dir == "screens" || dir == "runs") && len(inner) > 0 && !ValidPath(key) {
			nested = true
			for name, content := range inner {
				files[dir+"/"+path.Base(name)] = content
			}
			continue
		}
		files[key] = raw
	}
	if !nested {
		return nil
	}
	return files
}

// wrapped reads one extra wrapper key, such as {"app": {"app.json": …}}.
func wrapped(entries map[string]json.RawMessage, want []string) map[string]json.RawMessage {
	if len(entries) != 1 {
		return nil
	}
	for _, raw := range entries {
		inner := objectEntries(raw)
		if filesObject(inner) {
			return inner
		}
		return folders(inner, want)
	}
	return nil
}

// bareScreen places one screen's own object (screen, title, body…) at its
// file: the path its id names when that path was asked for, otherwise the
// only screen asked for. Two screens asked for and no matching id is
// ambiguous and returns nil.
func bareScreen(entries map[string]json.RawMessage, want []string) map[string]json.RawMessage {
	if entries["body"] == nil || (entries["screen"] == nil && entries["title"] == nil) {
		return nil
	}
	var screens []string
	for _, p := range want {
		if strings.HasPrefix(p, "screens/") {
			screens = append(screens, p)
		}
	}
	whole, _ := json.Marshal(entries)
	byID := "screens/" + firstString(entries, "screen", "id") + ".json"
	for _, p := range screens {
		if p == byID {
			return map[string]json.RawMessage{p: whole}
		}
	}
	if len(screens) == 1 {
		return map[string]json.RawMessage{screens[0]: whole}
	}
	return nil
}

// unwrapContent reads a file value spelled {"content": X} as X. A real file
// never has content as its only key, so this cannot hide one.
func unwrapContent(raw json.RawMessage) json.RawMessage {
	entries := objectEntries(raw)
	if len(entries) != 1 {
		return raw
	}
	if inner := firstRaw(entries, "content", "contents"); inner != nil {
		return inner
	}
	return raw
}

func firstRaw(entries map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, key := range keys {
		if raw, ok := entries[key]; ok {
			return raw
		}
	}
	return nil
}

func firstString(entries map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		var s string
		if json.Unmarshal(entries[key], &s) == nil && s != "" {
			return s
		}
	}
	return ""
}
