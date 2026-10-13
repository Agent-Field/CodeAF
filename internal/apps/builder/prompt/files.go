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

// ParseFiles reads the model's reply as {path: content}. It tolerates leading
// and trailing prose and code fences, and accepts a .json file's content given
// as an object instead of an escaped string. Any path outside the app's files,
// or content that does not parse for its extension, rejects the whole reply.
func ParseFiles(output string) (map[string][]byte, error) {
	objects := jsonObjects(output)
	if len(objects) == 0 {
		return nil, errNoObject
	}
	var first error
	for _, raw := range objects {
		files, err := filesFromObject(raw)
		if err == nil {
			return files, nil
		}
		if first == nil {
			first = err
		}
	}
	return nil, first
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

func filesFromObject(raw json.RawMessage) (map[string][]byte, error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("prompt: files: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("prompt: files object is empty")
	}
	files := make(map[string][]byte, len(entries))
	for _, path := range sortedKeys(entries) {
		if !ValidPath(path) {
			return nil, fmt.Errorf("prompt: %q is not an app file", path)
		}
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
