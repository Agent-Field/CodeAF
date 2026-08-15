// This file ports swe-pro/src/config/config.ts and src/config/provider.ts at commit 3b25a1a.
package config

// This file ports the loading/normalization core from
// swe-pro/src/config/config.ts:24-753, paths.ts:1-45, permission.ts:1-74, and
// parse.ts:1-88 at commit 3b25a1a. Effect services become explicit Go values.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Info is the deliberately open config object. The TypeScript schema has
// strongly typed known fields, but also contains several extensible record
// surfaces; retaining JSON values avoids lossy re-encoding during merges.
type Info map[string]any

// OrderedEntry is one insertion-ordered JSON object field.
type OrderedEntry struct {
	Key   string
	Value any
}

// OrderedObject preserves Object.entries order for precedence-sensitive
// permission and legacy tools objects.
type OrderedObject struct {
	entries []OrderedEntry
	index   map[string]int
}

func NewOrderedObject() *OrderedObject {
	return &OrderedObject{index: map[string]int{}}
}

func (o *OrderedObject) Set(key string, value any) {
	if o.index == nil {
		o.index = map[string]int{}
	}
	if index, ok := o.index[key]; ok {
		o.entries[index].Value = value
		return
	}
	o.index[key] = len(o.entries)
	o.entries = append(o.entries, OrderedEntry{Key: key, Value: value})
}

func (o *OrderedObject) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	index, ok := o.index[key]
	if !ok {
		return nil, false
	}
	return o.entries[index].Value, true
}

func (o *OrderedObject) Entries() []OrderedEntry {
	if o == nil {
		return nil
	}
	return append([]OrderedEntry(nil), o.entries...)
}

func (o *OrderedObject) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for index, entry := range o.entries {
		if index > 0 {
			buffer.WriteByte(',')
		}
		key, err := json.Marshal(entry.Key)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(entry.Value)
		if err != nil {
			return nil, err
		}
		buffer.Write(key)
		buffer.WriteByte(':')
		buffer.Write(value)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

type ConsoleState struct {
	ConsoleManagedProviders []string `json:"consoleManagedProviders"`
	ActiveOrgName           *string  `json:"activeOrgName"`
	SwitchableOrgCount      int      `json:"switchableOrgCount"`
}

func EmptyConsoleState() ConsoleState {
	return ConsoleState{ConsoleManagedProviders: []string{}, ActiveOrgName: nil, SwitchableOrgCount: 0}
}

type InvalidError struct {
	Path    string
	Message string
}

func (e *InvalidError) Error() string {
	if e.Message == "" {
		return "invalid config: " + e.Path
	}
	return e.Message
}

func stripJSONC(input string) string {
	out := []byte(input)
	inString, escaped := false, false
	for index := 0; index < len(out); index++ {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if out[index] == '\\' {
				escaped = true
			} else if out[index] == '"' {
				inString = false
			}
			continue
		}
		if out[index] == '"' {
			inString = true
			continue
		}
		if out[index] != '/' || index+1 >= len(out) {
			continue
		}
		switch out[index+1] {
		case '/':
			for out[index] != '\n' && out[index] != '\r' {
				out[index] = ' '
				index++
				if index >= len(out) {
					break
				}
			}
		case '*':
			out[index], out[index+1] = ' ', ' '
			index += 2
			for index < len(out) {
				if index+1 < len(out) && out[index] == '*' && out[index+1] == '/' {
					out[index], out[index+1] = ' ', ' '
					index++
					break
				}
				if out[index] != '\n' && out[index] != '\r' {
					out[index] = ' '
				}
				index++
			}
		}
	}
	// allowTrailingComma: true. Only commas whose next non-space byte closes an
	// array/object are removed; string contents were left untouched above.
	inString, escaped = false, false
	for index := 0; index < len(out); index++ {
		if inString {
			if escaped {
				escaped = false
			} else if out[index] == '\\' {
				escaped = true
			} else if out[index] == '"' {
				inString = false
			}
			continue
		}
		if out[index] == '"' {
			inString = true
			continue
		}
		if out[index] != ',' {
			continue
		}
		next := index + 1
		for next < len(out) && strings.ContainsRune(" \t\r\n", rune(out[next])) {
			next++
		}
		if next < len(out) && (out[next] == '}' || out[next] == ']') {
			out[index] = ' '
		}
	}
	return string(out)
}

// ParseJSONC parses comments and trailing commas while preserving the source
// path in errors.
func ParseJSONC(text, source string) (any, error) {
	return parseJSONC(text, source, false)
}

func parseJSONC(text, source string, preserveRoot bool) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(stripJSONC(text)))
	decoder.UseNumber()
	value, err := decodeOrderedJSON(decoder)
	if err != nil {
		return nil, &InvalidError{
			Path:    source,
			Message: fmt.Sprintf("\n--- JSONC Input ---\n%s\n--- Errors ---\n%s\n--- End ---", text, err),
		}
	}
	return materializeConfigJSON(value, preserveRoot), nil
}

func materializeConfigJSON(value any, preserve bool) any {
	switch value := value.(type) {
	case orderedJSONObject:
		if preserve {
			out := NewOrderedObject()
			for _, field := range value {
				out.Set(field.key, materializeConfigJSON(field.value, true))
			}
			return out
		}
		out := make(map[string]any, len(value))
		for _, field := range value {
			keepOrder := field.key == "permission" || field.key == "tools"
			nested := materializeConfigJSON(field.value, keepOrder)
			if field.key == "permission" {
				if normalized, ok := NormalizePermission(nested); ok {
					nested = normalized
				}
			}
			out[field.key] = nested
		}
		return out
	case []any:
		out := make([]any, len(value))
		for index, nested := range value {
			out[index] = materializeConfigJSON(nested, preserve)
		}
		return out
	default:
		return value
	}
}

func cloneValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, nested := range value {
			out[key] = cloneValue(nested)
		}
		return out
	case *OrderedObject:
		out := NewOrderedObject()
		for _, entry := range value.Entries() {
			out.Set(entry.Key, cloneValue(entry.Value))
		}
		return out
	case []any:
		out := make([]any, len(value))
		for index, nested := range value {
			out[index] = cloneValue(nested)
		}
		return out
	default:
		return value
	}
}

func mergeValue(target, source any) any {
	if left, leftOK := asOrderedObject(target); leftOK {
		if right, rightOK := asOrderedObject(source); rightOK {
			out := cloneValue(left).(*OrderedObject)
			for _, entry := range right.Entries() {
				if current, ok := out.Get(entry.Key); ok {
					out.Set(entry.Key, mergeValue(current, entry.Value))
				} else {
					out.Set(entry.Key, cloneValue(entry.Value))
				}
			}
			return out
		}
	}
	left, leftOK := target.(map[string]any)
	right, rightOK := source.(map[string]any)
	if !leftOK || !rightOK {
		return cloneValue(source)
	}
	out := cloneValue(left).(map[string]any)
	for key, value := range right {
		if current, ok := out[key]; ok {
			out[key] = mergeValue(current, value)
		} else {
			out[key] = cloneValue(value)
		}
	}
	return out
}

func asOrderedObject(value any) (*OrderedObject, bool) {
	object, ok := value.(*OrderedObject)
	return object, ok && object != nil
}

// Merge reproduces mergeDeep and the one instructions-array concatenation
// exception used between config sources.
func Merge(target, source Info) Info {
	merged := mergeValue(map[string]any(target), map[string]any(source)).(map[string]any)
	left, leftOK := target["instructions"].([]any)
	right, rightOK := source["instructions"].([]any)
	if leftOK && rightOK {
		seen := map[any]struct{}{}
		joined := make([]any, 0, len(left)+len(right))
		for _, list := range [][]any{left, right} {
			for _, value := range list {
				if _, ok := seen[value]; ok {
					continue
				}
				seen[value] = struct{}{}
				joined = append(joined, value)
			}
		}
		merged["instructions"] = joined
	}
	return Info(merged)
}

func normalizeLoadedConfig(value any) Info {
	object, ok := value.(map[string]any)
	if !ok {
		return Info{}
	}
	out := Info(cloneValue(object).(map[string]any))
	delete(out, "theme")
	delete(out, "keybinds")
	delete(out, "tui")
	return out
}

// NormalizePermission expands the action shorthand to the "*" rule.
func NormalizePermission(value any) (*OrderedObject, bool) {
	if action, ok := value.(string); ok {
		if action != "ask" && action != "allow" && action != "deny" {
			return nil, false
		}
		object := NewOrderedObject()
		object.Set("*", action)
		return object, true
	}
	object, ok := value.(*OrderedObject)
	if !ok {
		return nil, false
	}
	return object, true
}

func normalizeTools(info Info) {
	tools, ok := asOrderedObject(info["tools"])
	if !ok {
		return
	}
	perms := NewOrderedObject()
	for _, entry := range tools.Entries() {
		tool, raw := entry.Key, entry.Value
		enabled, _ := raw.(bool)
		action := "deny"
		if enabled {
			action = "allow"
		}
		if tool == "write" || tool == "edit" || tool == "patch" {
			perms.Set("edit", action)
		} else {
			perms.Set(tool, action)
		}
	}
	if configured, ok := asOrderedObject(info["permission"]); ok {
		perms = mergeValue(perms, configured).(*OrderedObject)
	}
	info["permission"] = perms
}

// LoadText expands substitutions, parses JSONC, and applies schema-level
// normalizations used by the pipeline.
func LoadText(text string, input SubstituteInput) (Info, error) {
	input.Text = text
	expanded, err := Substitute(input)
	if err != nil {
		return nil, err
	}
	value, err := ParseJSONC(expanded, input.Source)
	if err != nil {
		return nil, err
	}
	info := normalizeLoadedConfig(value)
	if autoshare, ok := info["autoshare"].(bool); ok && autoshare {
		if _, exists := info["share"]; !exists {
			info["share"] = "auto"
		}
	}
	return info, nil
}

// FileInDirectory returns JSON before JSONC exactly like paths.ts.
func FileInDirectory(dir, name string) []string {
	return []string{filepath.Join(dir, name+".json"), filepath.Join(dir, name+".jsonc")}
}

func withinOrSame(path, stop string) bool {
	if stop == "" {
		return true
	}
	rel, err := filepath.Rel(filepath.Clean(stop), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ProjectFiles finds config files from the project boundary inward.
func ProjectFiles(name, directory, worktree string) []string {
	found := []string{}
	current := filepath.Clean(directory)
	stop := filepath.Clean(worktree)
	for {
		for _, candidate := range FileInDirectory(current, name) {
			if _, err := os.Stat(candidate); err == nil {
				found = append(found, candidate)
			}
		}
		if current == stop || current == filepath.Dir(current) || !withinOrSame(filepath.Dir(current), stop) {
			break
		}
		current = filepath.Dir(current)
	}
	for left, right := 0, len(found)-1; left < right; left, right = left+1, right-1 {
		found[left], found[right] = found[right], found[left]
	}
	return found
}

// Loader is the explicit-DI replacement for Config.Service.
type Loader struct {
	GlobalDir string
	Env       Env
}

func (l Loader) globalDir() string {
	if l.GlobalDir != "" {
		return l.GlobalDir
	}
	if value, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok && value != "" {
		return filepath.Join(value, "codeaf")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "codeaf")
}

func readOptional(path string, lookup Lookup) (Info, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Info{}, nil
	}
	if err != nil {
		return nil, err
	}
	return LoadText(string(data), SubstituteInput{Path: path, Source: path, Lookup: lookup})
}

// Load merges global and project files in the same precedence order as the TS
// service. Managed/mobileconfig and plugin bootstrap remain behind their I/O
// owners and can be layered by callers.
func (l Loader) Load(directory, worktree string) (Info, error) {
	result := Info{}
	lookup := l.Env.Get
	for _, name := range []string{"config.json", "codeaf.json", "codeaf.jsonc"} {
		next, err := readOptional(filepath.Join(l.globalDir(), name), lookup)
		if err != nil {
			return nil, err
		}
		result = Merge(result, next)
	}
	if custom, ok := l.Env.Get("CODEAF_CONFIG"); ok && custom != "" {
		next, err := readOptional(custom, lookup)
		if err != nil {
			return nil, err
		}
		result = Merge(result, next)
	}
	if !l.Env.Enabled("CODEAF_DISABLE_PROJECT_CONFIG") {
		for _, path := range ProjectFiles("codeaf", directory, worktree) {
			next, err := readOptional(path, lookup)
			if err != nil {
				return nil, err
			}
			result = Merge(result, next)
		}
	}
	if content, ok := l.Env.Get("CODEAF_CONFIG_CONTENT"); ok && content != "" {
		next, err := LoadText(content, SubstituteInput{
			Dir: directory, Source: "CODEAF_CONFIG_CONTENT", Lookup: lookup,
		})
		if err != nil {
			return nil, err
		}
		result = Merge(result, next)
	}
	if raw, ok := l.Env.Get("CODEAF_PERMISSION"); ok && raw != "" {
		value, err := parseJSONC(raw, "CODEAF_PERMISSION", true)
		if err != nil {
			return nil, err
		}
		next, valid := NormalizePermission(value)
		if !valid {
			return nil, errors.New("CODEAF_PERMISSION must be a permission action or object")
		}
		current, _ := asOrderedObject(result["permission"])
		if current == nil {
			current = NewOrderedObject()
		}
		result["permission"] = mergeValue(current, next)
	}
	normalizeTools(result)
	if l.Env.Enabled("CODEAF_DISABLE_AUTOCOMPACT") {
		compaction, _ := result["compaction"].(map[string]any)
		if compaction == nil {
			compaction = map[string]any{}
		}
		compaction["auto"] = false
		result["compaction"] = compaction
	}
	if l.Env.Enabled("CODEAF_DISABLE_PRUNE") {
		compaction, _ := result["compaction"].(map[string]any)
		if compaction == nil {
			compaction = map[string]any{}
		}
		compaction["prune"] = false
		result["compaction"] = compaction
	}
	return result, nil
}

// Service caches per-directory config and supports explicit invalidation.
type Service struct {
	loader Loader
	mu     sync.RWMutex
	cache  map[string]Info
}

func NewService(loader Loader) *Service {
	return &Service{loader: loader, cache: map[string]Info{}}
}

func (s *Service) Get(directory, worktree string) (Info, error) {
	key := filepath.Clean(directory)
	s.mu.RLock()
	if value, ok := s.cache[key]; ok {
		s.mu.RUnlock()
		return Info(cloneValue(map[string]any(value)).(map[string]any)), nil
	}
	s.mu.RUnlock()
	value, err := s.loader.Load(directory, worktree)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[key] = value
	s.mu.Unlock()
	return Info(cloneValue(map[string]any(value)).(map[string]any)), nil
}

func (s *Service) Update(directory string, info Info) error {
	path := filepath.Join(directory, "config.json")
	existing, err := readOptional(path, s.loader.Env.Get)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(Merge(existing, info), "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	s.Invalidate(directory)
	return nil
}

func (s *Service) Invalidate(directory string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if directory == "" {
		s.cache = map[string]Info{}
		return
	}
	delete(s.cache, filepath.Clean(directory))
}

// EqualJSON is a test helper matching JSON.stringify byte comparison.
func EqualJSON(left, right any) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return bytes.Equal(a, b)
}
