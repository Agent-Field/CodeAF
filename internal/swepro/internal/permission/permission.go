// Package permission ports swe-pro/src/permission/index.ts:21-320 and
// evaluate.ts:1-19 at commit 3b25a1a. Rule and YAML-object order are preserved
// because the last matching rule wins.
package permission

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

const (
	ActionAllow = "allow"
	ActionDeny  = "deny"
	ActionAsk   = "ask"
)

type ruleOrder uint8

const (
	orderPermissionPatternAction ruleOrder = iota
	orderPermissionActionPattern
	orderActionPermissionPattern
)

// Rule is one permission/pattern/action entry.
type Rule struct {
	Permission string
	Pattern    string
	Action     string
	order      ruleOrder
}

func (r Rule) MarshalJSON() ([]byte, error) {
	switch r.order {
	case orderPermissionActionPattern:
		return jscompat.Stringify(struct {
			Permission string `json:"permission"`
			Action     string `json:"action"`
			Pattern    string `json:"pattern"`
		}{r.Permission, r.Action, r.Pattern})
	case orderActionPermissionPattern:
		return jscompat.Stringify(struct {
			Action     string `json:"action"`
			Permission string `json:"permission"`
			Pattern    string `json:"pattern"`
		}{r.Action, r.Permission, r.Pattern})
	default:
		return jscompat.Stringify(struct {
			Permission string `json:"permission"`
			Pattern    string `json:"pattern"`
			Action     string `json:"action"`
		}{r.Permission, r.Pattern, r.Action})
	}
}

func (r *Rule) UnmarshalJSON(data []byte) error {
	fields, err := orderedJSONFields(data)
	if err != nil {
		return err
	}
	order := make([]string, 0, len(fields))
	for _, field := range fields {
		order = append(order, field.key)
		switch field.key {
		case "permission":
			if err := json.Unmarshal(field.value, &r.Permission); err != nil {
				return err
			}
		case "pattern":
			if err := json.Unmarshal(field.value, &r.Pattern); err != nil {
				return err
			}
		case "action":
			if err := json.Unmarshal(field.value, &r.Action); err != nil {
				return err
			}
		}
	}
	switch strings.Join(order, ",") {
	case "permission,action,pattern":
		r.order = orderPermissionActionPattern
	case "action,permission,pattern":
		r.order = orderActionPermissionPattern
	default:
		r.order = orderPermissionPatternAction
	}
	return nil
}

// Ruleset is evaluated in slice order; the last matching rule wins.
type Ruleset []Rule

// PatternAction is one nested config entry.
type PatternAction struct {
	Pattern string
	Action  string
}

// ConfigEntry preserves one Object.entries(permission) entry.
type ConfigEntry struct {
	Permission string
	Action     *string
	Patterns   []PatternAction
}

// Config preserves JavaScript object enumeration order.
type Config struct {
	Entries []ConfigEntry
}

// ParseConfigJSON decodes a ConfigPermission.Info object without losing key
// order. JavaScript integer-index keys are emitted first in numeric order.
func ParseConfigJSON(data []byte) (Config, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var action string
		if err := json.Unmarshal(trimmed, &action); err != nil {
			return Config{}, err
		}
		return Config{Entries: []ConfigEntry{{
			Permission: "*",
			Action:     stringPointer(action),
		}}}, nil
	}
	fields, err := orderedJSONFields(trimmed)
	if err != nil {
		return Config{}, err
	}
	fields = jsObjectFieldOrder(fields)
	config := Config{Entries: make([]ConfigEntry, 0, len(fields))}
	for _, field := range fields {
		entry := ConfigEntry{Permission: field.key}
		var action string
		if err := json.Unmarshal(field.value, &action); err == nil {
			entry.Action = &action
			config.Entries = append(config.Entries, entry)
			continue
		}
		nested, err := orderedJSONFields(field.value)
		if err != nil {
			return Config{}, fmt.Errorf("permission %q: %w", field.key, err)
		}
		nested = jsObjectFieldOrder(nested)
		entry.Patterns = make([]PatternAction, 0, len(nested))
		for _, pattern := range nested {
			if err := json.Unmarshal(pattern.value, &action); err != nil {
				return Config{}, fmt.Errorf("permission %q pattern %q: %w", field.key, pattern.key, err)
			}
			entry.Patterns = append(entry.Patterns, PatternAction{Pattern: pattern.key, Action: action})
		}
		config.Entries = append(config.Entries, entry)
	}
	return config, nil
}

func (c *Config) UnmarshalJSON(data []byte) error {
	value, err := ParseConfigJSON(data)
	if err != nil {
		return err
	}
	*c = value
	return nil
}

type jsonField struct {
	key   string
	value json.RawMessage
	index int
}

func orderedJSONFields(data []byte) ([]jsonField, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil, errors.New("expected JSON object")
	}
	fields := []jsonField{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New("expected object key")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields = append(fields, jsonField{key: key, value: value, index: len(fields)})
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, errors.New("trailing JSON")
		}
		return nil, err
	}
	return fields, nil
}

func jsObjectFieldOrder(fields []jsonField) []jsonField {
	out := append([]jsonField(nil), fields...)
	sort.SliceStable(out, func(i, j int) bool {
		left, leftOK := arrayIndex(out[i].key)
		right, rightOK := arrayIndex(out[j].key)
		switch {
		case leftOK && rightOK:
			return left < right
		case leftOK:
			return true
		case rightOK:
			return false
		default:
			return out[i].index < out[j].index
		}
	})
	return out
}

func arrayIndex(value string) (uint64, bool) {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return 0, false
	}
	number, err := strconv.ParseUint(value, 10, 32)
	if err != nil || number == 1<<32-1 {
		return 0, false
	}
	if strconv.FormatUint(number, 10) != value {
		return 0, false
	}
	return number, true
}

func stringPointer(value string) *string {
	return &value
}

// FromConfig flattens an ordered config object.
func FromConfig(config Config) Ruleset {
	out := Ruleset{}
	for _, entry := range config.Entries {
		if entry.Action != nil {
			out = append(out, Rule{
				Permission: entry.Permission,
				Action:     *entry.Action,
				Pattern:    "*",
				order:      orderPermissionActionPattern,
			})
			continue
		}
		for _, pattern := range entry.Patterns {
			out = append(out, Rule{
				Permission: entry.Permission,
				Pattern:    expand(pattern.Pattern),
				Action:     pattern.Action,
				order:      orderPermissionPatternAction,
			})
		}
	}
	return out
}

// Evaluate returns the last matching rule across the flattened rulesets.
func Evaluate(permission string, pattern string, rulesets ...Ruleset) Rule {
	for i := len(rulesets) - 1; i >= 0; i-- {
		rules := rulesets[i]
		for j := len(rules) - 1; j >= 0; j-- {
			rule := rules[j]
			if WildcardMatch(permission, rule.Permission) && WildcardMatch(pattern, rule.Pattern) {
				return rule
			}
		}
	}
	return Rule{
		Action:     ActionAllow,
		Permission: permission,
		Pattern:    "*",
		order:      orderActionPermissionPattern,
	}
}

// Merge concatenates rulesets without deduplication.
func Merge(rulesets ...Ruleset) Ruleset {
	out := Ruleset{}
	for _, rules := range rulesets {
		out = append(out, rules...)
	}
	return out
}

// StringSet is an insertion-ordered Set<string>.
type StringSet struct {
	order  []string
	values map[string]struct{}
}

func newStringSet() StringSet {
	return StringSet{order: []string{}, values: map[string]struct{}{}}
}

func (s *StringSet) add(value string) {
	if _, ok := s.values[value]; ok {
		return
	}
	s.values[value] = struct{}{}
	s.order = append(s.order, value)
}

// Has reports set membership.
func (s StringSet) Has(value string) bool {
	_, ok := s.values[value]
	return ok
}

// Values returns Set iteration order.
func (s StringSet) Values() []string {
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// Disabled returns tools disabled by a final whole-permission deny.
func Disabled(tools []string, rules Ruleset) StringSet {
	out := newStringSet()
	for _, tool := range tools {
		permission := tool
		if tool == "edit" || tool == "write" || tool == "apply_patch" {
			permission = "edit"
		}
		var match *Rule
		for i := len(rules) - 1; i >= 0; i-- {
			if WildcardMatch(permission, rules[i].Permission) {
				copy := rules[i]
				match = &copy
				break
			}
		}
		if match != nil && match.Pattern == "*" && match.Action == ActionDeny {
			out.add(tool)
		}
	}
	return out
}

// WildcardMatch ports util/wildcard.ts match(): * spans any number of UTF-16
// code units, ? spans one, and a trailing " *" is optional as a unit.
func WildcardMatch(value string, pattern string) bool {
	value = strings.ReplaceAll(value, `\`, "/")
	pattern = strings.ReplaceAll(pattern, `\`, "/")
	if strings.HasSuffix(pattern, " *") {
		if wildcardUnits(value, pattern[:len(pattern)-2]) {
			return true
		}
	}
	return wildcardUnits(value, pattern)
}

func wildcardUnits(value string, pattern string) bool {
	input := utf16.Encode([]rune(value))
	glob := utf16.Encode([]rune(pattern))
	table := make([][]bool, len(glob)+1)
	for i := range table {
		table[i] = make([]bool, len(input)+1)
	}
	table[0][0] = true
	for i := 1; i <= len(glob); i++ {
		if glob[i-1] == '*' {
			table[i][0] = table[i-1][0]
		}
		for j := 1; j <= len(input); j++ {
			switch glob[i-1] {
			case '*':
				table[i][j] = table[i-1][j] || table[i][j-1]
			case '?':
				table[i][j] = table[i-1][j-1]
			default:
				table[i][j] = table[i-1][j-1] && glob[i-1] == input[j-1]
			}
		}
	}
	return table[len(glob)][len(input)]
}

func expand(pattern string) string {
	home, _ := os.UserHomeDir()
	switch {
	case strings.HasPrefix(pattern, "~/"):
		return home + pattern[1:]
	case pattern == "~":
		return home
	case strings.HasPrefix(pattern, "$HOME/"):
		return home + pattern[5:]
	case strings.HasPrefix(pattern, "$HOME"):
		return home + pattern[5:]
	default:
		return pattern
	}
}

// DeniedError is returned when Ask finds a deny rule.
type DeniedError struct {
	Ruleset Ruleset
}

func (e DeniedError) Error() string {
	data, err := jscompat.Stringify(e.Ruleset)
	if err != nil {
		panic(err)
	}
	return "The user has specified a rule which prevents you from using this specific tool call. Here are some of the relevant rules " + string(data)
}

// RejectedError mirrors the legacy reply error, although autonomous Ask never
// creates pending requests.
type RejectedError struct{}

func (RejectedError) Error() string {
	return "The user rejected permission to use this specific tool call."
}

// CorrectedError carries legacy rejection feedback.
type CorrectedError struct {
	Feedback string
}

func (e CorrectedError) Error() string {
	return "The user rejected permission to use this specific tool call with the following feedback: " + e.Feedback
}

// Service is the autonomous permission evaluator. Literal ask and allow both
// proceed; only deny returns an error.
type Service struct {
	Approved Ruleset
}

// AskInput is the tool-context request shape evaluated by the live registry.
type AskInput struct {
	Request
	Ruleset Ruleset `json:"ruleset"`
}

// Evaluate preserves the full request metadata while applying autonomous Ask.
func (s *Service) Evaluate(input AskInput) error {
	return s.Ask(input.Permission, input.Patterns, input.Ruleset)
}

// Ask evaluates every pattern against request rules followed by approvals.
func (s *Service) Ask(permission string, patterns []string, rules Ruleset) error {
	for _, pattern := range patterns {
		rule := Evaluate(permission, pattern, rules, s.Approved)
		if rule.Action != ActionDeny {
			continue
		}
		relevant := Ruleset{}
		for _, candidate := range rules {
			if WildcardMatch(permission, candidate.Permission) {
				relevant = append(relevant, candidate)
			}
		}
		return DeniedError{Ruleset: relevant}
	}
	return nil
}

// Pending returns the autonomous runtime's permanently-empty request list.
func (s *Service) Pending() []Request {
	return []Request{}
}

// Request is the value-level pending request shape.
type Request struct {
	ID         string         `json:"id"`
	SessionID  string         `json:"sessionID"`
	Permission string         `json:"permission"`
	Patterns   []string       `json:"patterns"`
	Metadata   map[string]any `json:"metadata"`
	Always     []string       `json:"always"`
}

// RulesetFromFrontmatter parses the permission mapping of an agent Markdown
// document while preserving YAML source order.
func RulesetFromFrontmatter(markdown string) (Ruleset, error) {
	config, err := ConfigFromFrontmatter(markdown)
	if err != nil {
		return nil, err
	}
	return FromConfig(config), nil
}

// ConfigFromFrontmatter parses the subset of YAML used by agent permission
// frontmatter: ordered scalar actions and one nested pattern/action mapping.
func ConfigFromFrontmatter(markdown string) (Config, error) {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return Config{}, errors.New("missing YAML frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return Config{}, errors.New("unterminated YAML frontmatter")
	}
	config := Config{Entries: []ConfigEntry{}}
	permissionIndex := -1
	for i := 1; i < end; i++ {
		indent := leadingSpaces(lines[i])
		key, value, ok := yamlKeyValue(strings.TrimSpace(lines[i]))
		if !ok {
			continue
		}
		if indent == 0 && key == "permission" {
			if value != "" {
				action := yamlScalar(value)
				config.Entries = append(config.Entries, ConfigEntry{
					Permission: "*",
					Action:     stringPointer(action),
				})
				return config, nil
			}
			permissionIndex = i
			break
		}
	}
	if permissionIndex == -1 {
		return config, nil
	}

	var current *ConfigEntry
	for i := permissionIndex + 1; i < end; i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		indent := leadingSpaces(line)
		if indent < 2 {
			break
		}
		key, value, ok := yamlKeyValue(strings.TrimSpace(line))
		if !ok {
			continue
		}
		switch indent {
		case 2:
			entry := ConfigEntry{Permission: yamlScalar(key)}
			if value != "" {
				action := yamlScalar(value)
				entry.Action = &action
			} else {
				entry.Patterns = []PatternAction{}
			}
			config.Entries = append(config.Entries, entry)
			current = &config.Entries[len(config.Entries)-1]
		default:
			if indent >= 4 && current != nil && current.Action == nil {
				current.Patterns = append(current.Patterns, PatternAction{
					Pattern: yamlScalar(key),
					Action:  yamlScalar(value),
				})
			}
		}
	}
	return config, nil
}

func leadingSpaces(value string) int {
	count := 0
	for count < len(value) && value[count] == ' ' {
		count++
	}
	return count
}

func yamlKeyValue(value string) (string, string, bool) {
	quoted := byte(0)
	escaped := false
	for i := 0; i < len(value); i++ {
		char := value[i]
		if quoted != 0 {
			if quoted == '"' && char == '\\' && !escaped {
				escaped = true
				continue
			}
			if char == quoted && !escaped {
				quoted = 0
			}
			escaped = false
			continue
		}
		if char == '\'' || char == '"' {
			quoted = char
			continue
		}
		if char == ':' {
			return strings.TrimSpace(value[:i]), strings.TrimSpace(value[i+1:]), true
		}
	}
	return "", "", false
}

func yamlScalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if decoded, err := strconv.Unquote(value); err == nil {
			return decoded
		}
	}
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	if index := strings.Index(value, " #"); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}
