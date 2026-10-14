package prompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// bareKind recognises one file's content sent without its path wrapper.
type bareKind struct {
	name  string
	match func(map[string]json.RawMessage) bool
	path  func(map[string]json.RawMessage) string
}

// bareKinds is checked in order; each kind names the keys only it has.
var bareKinds = []bareKind{
	{"screen", hasKeys("screen", "body"), func(o map[string]json.RawMessage) string { return idPath("screens/", o["screen"], ".json") }},
	{"run", hasKeys("run", "steps"), func(o map[string]json.RawMessage) string { return idPath("runs/", o["run"], ".yaml") }},
	{"app", hasAny("records", "capabilities"), func(map[string]json.RawMessage) string { return "app.json" }},
	{"plan", hasKeys("data", "screens"), func(map[string]json.RawMessage) string { return "plan.json" }},
}

func hasKeys(keys ...string) func(map[string]json.RawMessage) bool {
	return func(o map[string]json.RawMessage) bool {
		for _, k := range keys {
			if _, ok := o[k]; !ok {
				return false
			}
		}
		return true
	}
}

func hasAny(keys ...string) func(map[string]json.RawMessage) bool {
	return func(o map[string]json.RawMessage) bool {
		for _, k := range keys {
			if _, ok := o[k]; ok {
				return true
			}
		}
		return false
	}
}

func idPath(dir string, raw json.RawMessage, ext string) string {
	var id string
	if json.Unmarshal(raw, &id) != nil || id == "" {
		return ""
	}
	return dir + id + ext
}

func classify(o map[string]json.RawMessage) (kind bareKind, guess string, ok bool) {
	for _, k := range bareKinds {
		if k.match(o) {
			return k, k.path(o), true
		}
	}
	return bareKind{}, "", false
}

// sameKind reports whether two paths are the same kind of file.
func sameKind(a, b string) bool {
	if a == b {
		return true
	}
	da, db := path.Dir(a), path.Dir(b)
	return da != "." && da == db && path.Ext(a) == path.Ext(b)
}

// ParseFilesFor is ParseFiles for a call that asked for the files in expect
// (empty = any app file). A reply that sends file contents without their path
// wrapper is accepted when each object's kind names an expected path; with one
// expected path, any object of that file's kind is wrapped under it.
func ParseFilesFor(output string, expect []string) (map[string][]byte, error) {
	files, err := ParseFiles(output)
	var shape *FilesObjectError
	if !errors.As(err, &shape) {
		return files, err
	}
	shape.Expect = expect
	entries := map[string]json.RawMessage{}
	for _, raw := range jsonObjects(output) {
		object := objectEntries(raw)
		kind, guess, ok := classify(object)
		if !ok {
			continue
		}
		shape.Sent = kind.name
		target := bareTarget(guess, expect)
		if target == "" {
			continue
		}
		content, convErr := bareContent(target, raw)
		if convErr != nil {
			return nil, fmt.Errorf("prompt: %s: %w", target, convErr)
		}
		entries[target] = content
	}
	if len(entries) == 0 {
		return nil, shape
	}
	return filesFromObject(entries)
}

func bareTarget(guess string, expect []string) string {
	if len(expect) == 1 && sameKind(guess, expect[0]) {
		return expect[0]
	}
	for _, want := range expect {
		if want == guess {
			return guess
		}
	}
	if len(expect) == 0 && ValidPath(guess) {
		return guess
	}
	return ""
}

// bareContent keeps JSON files as objects and turns a run object into YAML.
func bareContent(target string, raw json.RawMessage) (json.RawMessage, error) {
	if !strings.HasSuffix(target, ".yaml") {
		return raw, nil
	}
	var value any
	if err := yaml.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	text, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(text))
}

// shapeHint is the exact repair for a reply that is not a path map.
func (e *FilesObjectError) shapeHint() string {
	example := "app.json"
	if len(e.Expect) > 0 {
		example = e.Expect[0]
	}
	hint := fmt.Sprintf(`; reply must be {%q: {…}}`, example)
	if len(e.Expect) > 1 {
		hint += " with one key per file: " + strings.Join(e.Expect, ", ")
	}
	if e.Sent != "" {
		hint += "; you sent the " + e.Sent + " object itself — wrap it under its path"
	}
	return hint
}
