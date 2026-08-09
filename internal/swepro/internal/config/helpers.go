// This file ports pure helpers from swe-pro/src/config/*.ts at commit 3b25a1a.
package config

// Pure helpers in this file port:
//   - swe-pro/src/config/entry-name.ts:1-16
//   - swe-pro/src/config/markdown.ts:1-68
//   - swe-pro/src/config/managed.ts:11-45
//   - swe-pro/src/config/plugin.ts:19-86
//   - swe-pro/src/config/variable.ts:10-88
// at commit 3b25a1a.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// ConfigEntryNameFromPath derives the slash-preserving config key.
func ConfigEntryNameFromPath(filePath string, searchRoots []string) string {
	normalized := strings.ReplaceAll(filePath, `\`, "/")
	candidate := ""
	for _, root := range searchRoots {
		if index := strings.Index(normalized, root); index >= 0 {
			candidate = normalized[index+len(root):]
			break
		}
	}
	if candidate == "" {
		candidate = filepath.Base(filePath)
	}
	ext := filepath.Ext(candidate)
	if ext == "" {
		return candidate
	}
	return candidate[:len(candidate)-len(ext)]
}

// Match is the JSON-visible part of JavaScript's RegExpMatchArray.
type Match []string

func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680',
		'\u2000', '\u2001', '\u2002', '\u2003', '\u2004', '\u2005',
		'\u2006', '\u2007', '\u2008', '\u2009', '\u200a', '\u2028',
		'\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	default:
		return false
	}
}

func isJSWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func tokenRune(r rune) bool {
	return !isJSWhitespace(r) && r != '`' && r != ',' && r != '.'
}

// Files ports FILE_REGEX, including its ASCII-only \w lookbehind and its
// refusal to consume a trailing dot.
func Files(template string) []Match {
	out := []Match{}
	for index := 0; index < len(template); {
		next := strings.IndexByte(template[index:], '@')
		if next < 0 {
			break
		}
		at := index + next
		index = at + 1
		if at > 0 {
			prev := template[at-1]
			if prev == '`' || isJSWordByte(prev) {
				continue
			}
		}
		cursor := at + 1
		if cursor < len(template) && template[cursor] == '.' {
			cursor++
		}
		for cursor < len(template) {
			r, width := utf8.DecodeRuneInString(template[cursor:])
			if !tokenRune(r) {
				break
			}
			cursor += width
		}
		for cursor < len(template) && template[cursor] == '.' {
			segment := cursor + 1
			end := segment
			for end < len(template) {
				r, width := utf8.DecodeRuneInString(template[end:])
				if !tokenRune(r) {
					break
				}
				end += width
			}
			if end == segment {
				break
			}
			cursor = end
		}
		capture := template[at+1 : cursor]
		out = append(out, Match{template[at:cursor], capture})
		index = cursor
	}
	return out
}

// Shell ports SHELL_REGEX.
func Shell(template string) []Match {
	out := []Match{}
	for offset := 0; offset < len(template); {
		start := strings.Index(template[offset:], "!`")
		if start < 0 {
			break
		}
		start += offset
		end := strings.IndexByte(template[start+2:], '`')
		if end < 0 {
			break
		}
		end += start + 2
		out = append(out, Match{template[start : end+1], template[start+2 : end]})
		offset = end + 1
	}
	return out
}

var frontmatterPattern = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---`)
var frontmatterKV = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_]*)\s*:\s*(.*)$`)

// FallbackSanitization makes colon-bearing scalar values acceptable to the
// permissive YAML retry.
func FallbackSanitization(content string) string {
	match := frontmatterPattern.FindStringSubmatchIndex(content)
	if match == nil {
		return content
	}
	frontmatter := content[match[2]:match[3]]
	lines := regexp.MustCompile(`\r?\n`).Split(frontmatter, -1)
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || trimmed == "" ||
			len(line) > 0 && unicode.IsSpace(rune(line[0])) {
			result = append(result, line)
			continue
		}
		kv := frontmatterKV.FindStringSubmatch(line)
		if kv == nil {
			result = append(result, line)
			continue
		}
		key, value := kv[1], strings.TrimSpace(kv[2])
		if value == "" || value == ">" || value == "|" ||
			strings.HasPrefix(value, `"`) || strings.HasPrefix(value, `'`) {
			result = append(result, line)
			continue
		}
		if strings.Contains(value, ":") {
			result = append(result, key+": |-", "  "+value)
			continue
		}
		result = append(result, line)
	}
	return content[:match[2]] + strings.Join(result, "\n") + content[match[3]:]
}

// ManagedConfigDir mirrors the platform default and test override.
func ManagedConfigDir(lookup Lookup) string {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if value, ok := lookup("CODEAF_TEST_MANAGED_CONFIG_DIR"); ok && value != "" {
		return value
	}
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/codeaf"
	case "windows":
		programData, ok := lookup("ProgramData")
		if !ok || programData == "" {
			programData = `C:\ProgramData`
		}
		return filepath.Join(programData, "codeaf")
	default:
		return "/etc/codeaf"
	}
}

var plistMeta = map[string]struct{}{
	"PayloadDisplayName": {}, "PayloadIdentifier": {}, "PayloadType": {},
	"PayloadUUID": {}, "PayloadVersion": {}, "_manualProfile": {},
}

type orderedJSONField struct {
	key   string
	value any
}

type orderedJSONObject []orderedJSONField

func decodeOrderedJSON(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := orderedJSONObject{}
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return nil, keyErr
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("JSON object key is not a string")
			}
			value, valueErr := decodeOrderedJSON(decoder)
			if valueErr != nil {
				return nil, valueErr
			}
			object = append(object, orderedJSONField{key: key, value: value})
		}
		_, err = decoder.Token()
		return object, err
	case '[':
		array := []any{}
		for decoder.More() {
			value, valueErr := decodeOrderedJSON(decoder)
			if valueErr != nil {
				return nil, valueErr
			}
			array = append(array, value)
		}
		_, err = decoder.Token()
		return array, err
	default:
		return nil, errors.New("unexpected JSON delimiter")
	}
}

func encodeOrderedJSON(buffer *bytes.Buffer, value any) error {
	switch value := value.(type) {
	case orderedJSONObject:
		buffer.WriteByte('{')
		for index, field := range value {
			if index > 0 {
				buffer.WriteByte(',')
			}
			key, _ := jscompat.Stringify(field.key)
			buffer.Write(key)
			buffer.WriteByte(':')
			if err := encodeOrderedJSON(buffer, field.value); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	case []any:
		buffer.WriteByte('[')
		for index, item := range value {
			if index > 0 {
				buffer.WriteByte(',')
			}
			if err := encodeOrderedJSON(buffer, item); err != nil {
				return err
			}
		}
		buffer.WriteByte(']')
	case json.Number:
		buffer.WriteString(value.String())
	default:
		data, err := jscompat.Stringify(value)
		if err != nil {
			return err
		}
		buffer.Write(data)
	}
	return nil
}

// ParseManagedPlist removes MDM metadata while preserving JavaScript object
// insertion order.
func ParseManagedPlist(input string) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	value, err := decodeOrderedJSON(decoder)
	if err != nil {
		return "", err
	}
	object, ok := value.(orderedJSONObject)
	if !ok {
		return "", errors.New("managed plist JSON must be an object")
	}
	filtered := make(orderedJSONObject, 0, len(object))
	for _, field := range object {
		if _, drop := plistMeta[field.key]; !drop {
			filtered = append(filtered, field)
		}
	}
	var buffer bytes.Buffer
	if err := encodeOrderedJSON(&buffer, filtered); err != nil {
		return "", err
	}
	return buffer.String(), nil
}

// PluginSpec is the Go projection of string | [string, Options].
type PluginSpec struct {
	Specifier string
	Options   map[string]any
	WithOpts  bool
}

func (s PluginSpec) MarshalJSON() ([]byte, error) {
	if !s.WithOpts {
		return jscompat.Stringify(s.Specifier)
	}
	return jscompat.Stringify([]any{s.Specifier, s.Options})
}

func (s *PluginSpec) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		s.WithOpts = false
		s.Options = nil
		return json.Unmarshal(data, &s.Specifier)
	}
	var pair []json.RawMessage
	if err := json.Unmarshal(data, &pair); err != nil || len(pair) != 2 {
		return errors.New("plugin spec must be a string or [string, options]")
	}
	if err := json.Unmarshal(pair[0], &s.Specifier); err != nil {
		return err
	}
	if err := json.Unmarshal(pair[1], &s.Options); err != nil {
		return err
	}
	s.WithOpts = true
	return nil
}

func PluginSpecifier(plugin PluginSpec) string { return plugin.Specifier }

func PluginOptions(plugin PluginSpec) map[string]any {
	if !plugin.WithOpts {
		return nil
	}
	return plugin.Options
}

type PluginScope string

const (
	PluginGlobal PluginScope = "global"
	PluginLocal  PluginScope = "local"
)

type PluginOrigin struct {
	Spec   PluginSpec  `json:"spec"`
	Source string      `json:"source"`
	Scope  PluginScope `json:"scope"`
}

func pluginIdentity(spec string) string {
	if strings.HasPrefix(spec, "file://") {
		return spec
	}
	raw := spec
	if strings.HasPrefix(raw, "npm:") {
		raw = strings.TrimPrefix(raw, "npm:")
	}
	if strings.HasPrefix(raw, "@") {
		if slash := strings.IndexByte(raw, '/'); slash >= 0 {
			if version := strings.IndexByte(raw[slash+1:], '@'); version >= 0 {
				return raw[:slash+1+version]
			}
		}
		return raw
	}
	if version := strings.IndexByte(raw, '@'); version >= 0 {
		return raw[:version]
	}
	return raw
}

// DeduplicatePluginOrigins keeps the last origin for each load identity.
func DeduplicatePluginOrigins(plugins []PluginOrigin) []PluginOrigin {
	seen := map[string]struct{}{}
	reversed := make([]PluginOrigin, 0, len(plugins))
	for index := len(plugins) - 1; index >= 0; index-- {
		plugin := plugins[index]
		identity := pluginIdentity(plugin.Spec.Specifier)
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		reversed = append(reversed, plugin)
	}
	out := make([]PluginOrigin, len(reversed))
	for index := range reversed {
		out[len(reversed)-1-index] = reversed[index]
	}
	return out
}

func isPathPluginSpec(spec string) bool {
	if strings.HasPrefix(spec, "file://") || strings.HasPrefix(spec, ".") || filepath.IsAbs(spec) {
		return true
	}
	return len(spec) >= 3 && ((spec[0] >= 'A' && spec[0] <= 'Z') || (spec[0] >= 'a' && spec[0] <= 'z')) &&
		spec[1] == ':' && (spec[2] == '\\' || spec[2] == '/')
}

// ResolvePluginSpec resolves path-like plugin declarations relative to the
// declaring config file. Directory entrypoint probing belongs to plugin load
// and deliberately degrades to this URL on failure.
func ResolvePluginSpec(plugin PluginSpec, configFilepath string) PluginSpec {
	spec := plugin.Specifier
	if !isPathPluginSpec(spec) {
		return plugin
	}
	if !strings.HasPrefix(spec, "file://") {
		if !filepath.IsAbs(spec) {
			spec = filepath.Join(filepath.Dir(configFilepath), spec)
		}
		absolute, err := filepath.Abs(spec)
		if err == nil {
			spec = absolute
		}
		spec = (&url.URL{Scheme: "file", Path: filepath.ToSlash(spec)}).String()
	}
	plugin.Specifier = spec
	return plugin
}

// SubstituteInput is the file/env substitution surface from variable.ts.
type SubstituteInput struct {
	Text    string
	Path    string
	Dir     string
	Source  string
	Missing string
	Lookup  Lookup
}

var envToken = regexp.MustCompile(`\{env:([^}]+)\}`)
var fileToken = regexp.MustCompile(`\{file:[^}]+\}`)

// Substitute applies {env:VAR} and {file:path} substitutions.
func Substitute(input SubstituteInput) (string, error) {
	lookup := input.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	text := envToken.ReplaceAllStringFunc(input.Text, func(token string) string {
		name := token[len("{env:") : len(token)-1]
		value, _ := lookup(name)
		return value
	})
	matches := fileToken.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text, nil
	}
	configDir := input.Dir
	if input.Path != "" {
		configDir = filepath.Dir(input.Path)
	}
	missing := input.Missing
	if missing == "" {
		missing = "error"
	}
	var out strings.Builder
	cursor := 0
	for _, match := range matches {
		token := text[match[0]:match[1]]
		out.WriteString(text[cursor:match[0]])
		lineStart := strings.LastIndex(text[:match[0]], "\n") + 1
		if strings.HasPrefix(strings.TrimLeftFunc(text[lineStart:match[0]], isJSWhitespace), "//") {
			out.WriteString(token)
			cursor = match[1]
			continue
		}
		file := strings.TrimSuffix(strings.TrimPrefix(token, "{file:"), "}")
		if strings.HasPrefix(file, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				file = filepath.Join(home, file[2:])
			}
		}
		if !filepath.IsAbs(file) {
			file = filepath.Join(configDir, file)
		}
		file = filepath.Clean(file)
		data, err := os.ReadFile(file)
		if err != nil {
			if missing == "empty" {
				data = nil
			} else if errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf(`bad file reference: %q %s does not exist`, token, file)
			} else {
				return "", fmt.Errorf(`bad file reference: %q`, token)
			}
		}
		quoted, _ := jscompat.Stringify(jscompat.Trim(string(data)))
		if len(quoted) >= 2 {
			out.Write(quoted[1 : len(quoted)-1])
		}
		cursor = match[1]
	}
	out.WriteString(text[cursor:])
	return out.String(), nil
}

// CompactJSON is shared by the JSONC loader and checkpoint writer.
func CompactJSON(input []byte) ([]byte, error) {
	var out bytes.Buffer
	if err := json.Compact(&out, input); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
