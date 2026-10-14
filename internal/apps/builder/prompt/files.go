package prompt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var errNoObject = errors.New("prompt: no JSON object in model output")

var appFilePath = regexp.MustCompile(
	`^(app\.json|plan\.json|agent\.md|screens/[A-Za-z0-9][A-Za-z0-9_-]*\.json|runs/[A-Za-z0-9][A-Za-z0-9_-]*\.yaml)$`)

// ValidPath reports whether path is a file the Builder may write.
func ValidPath(path string) bool { return appFilePath.MatchString(path) }

// FilesObjectError tells the repair call which keys were returned instead of
// app paths, so an inline app definition does not end the write step.
type FilesObjectError struct {
	Keys   []string
	Expect []string // paths the call asked for, when known
	Sent   string   // kind of file object sent without its path, if recognised
}

func (e *FilesObjectError) Error() string {
	return fmt.Sprintf("prompt: return a JSON object mapping app file paths to contents; got keys %q", e.Keys) + e.shapeHint()
}

// ParseFiles prefers an explicit files wrapper, then the first object whose
// keys are all app paths. Prose and unrelated objects are skipped; contents
// still have to parse for their extension.
func ParseFiles(output string) (map[string][]byte, error) {
	var objects []map[string]json.RawMessage
	keys := map[string]json.RawMessage{}
	for _, raw := range jsonObjects(output) {
		entries := objectEntries(raw)
		objects = append(objects, entries)
		for key := range entries {
			keys[key] = nil
		}
		if wrapped := objectEntries(entries["files"]); filesObject(wrapped) {
			return filesFromObject(wrapped)
		}
	}
	for _, entries := range objects {
		if filesObject(entries) {
			return filesFromObject(entries)
		}
	}
	return nil, &FilesObjectError{Keys: sortedKeys(keys)}
}

func objectEntries(raw json.RawMessage) map[string]json.RawMessage {
	var entries map[string]json.RawMessage
	_ = json.Unmarshal(raw, &entries)
	return entries
}

func filesObject(entries map[string]json.RawMessage) bool {
	if len(entries) == 0 {
		return false
	}
	for path := range entries {
		if !ValidPath(path) {
			return false
		}
	}
	return true
}

// jsonObjects returns each top-level JSON object found in s, scanning past
// anything that is not one (prose, a fence line, a stray brace).
func jsonObjects(s string) []json.RawMessage {
	var found []json.RawMessage
	for len(s) > 0 {
		start := strings.IndexByte(s, '{')
		if start < 0 {
			break
		}
		s = s[start:]
		dec := json.NewDecoder(strings.NewReader(s))
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			s = s[1:]
			continue
		}
		found = append(found, raw)
		s = s[dec.InputOffset():]
	}
	return found
}

func filesFromObject(entries map[string]json.RawMessage) (map[string][]byte, error) {
	files := make(map[string][]byte, len(entries))
	for _, path := range sortedKeys(entries) {
		content, err := fileContent(path, entries[path])
		if err != nil {
			return nil, fmt.Errorf("prompt: %s: %w", path, err)
		}
		files[path] = content
	}
	return files, nil
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func fileContent(path string, raw json.RawMessage) ([]byte, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return objectContent(path, raw)
	}
	return checkSyntax(path, []byte(text))
}

// objectContent handles a non-string value, which is only allowed for JSON files.
func objectContent(path string, raw json.RawMessage) ([]byte, error) {
	if !strings.HasSuffix(path, ".json") || raw[0] != '{' {
		return nil, fmt.Errorf("content must be a string")
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		return nil, err
	}
	pretty.WriteByte('\n')
	return pretty.Bytes(), nil
}

func checkSyntax(path string, content []byte) ([]byte, error) {
	switch {
	case strings.HasSuffix(path, ".json") && !json.Valid(content):
		return nil, fmt.Errorf("content is not valid JSON")
	case strings.HasSuffix(path, ".yaml"):
		var doc any
		if err := yaml.Unmarshal(content, &doc); err != nil {
			return nil, fmt.Errorf("content is not valid YAML: %w", err)
		}
	}
	return content, nil
}
