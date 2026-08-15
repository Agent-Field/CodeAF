// Package testmemo ports src/session/test-memo.ts lines 1-77.
package testmemo

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

const TEST_MEMO_CAP float64 = 50
const TEST_MEMO_ANNOTATION = "[codeaf: cached — tree unchanged since last identical run]"

const jsSpace = `[\t\n\x0b\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var (
	TEST_COMMAND_PATTERN = regexp.MustCompile(
		`\b(?:[Vv][Ii][Tt][Ee][Ss][Tt]|[Jj][Ee][Ss][Tt]|` +
			`[Bb][Uu][Nn] [Tt][Ee][Ss][Tt]|[Pp][Yy][Tt][Ee][Ss][Tt]|` +
			`[Cc][Aa][Rr][Gg][Oo] [Tt][Ee][Ss][Tt]|[Gg][Oo] [Tt][Ee][Ss][Tt])\b`,
	)
	exitStatusRe = regexp.MustCompile(
		`(?:[Ee][Xx][Ii][Tt](?:[Ee][Dd])?|[Cc][Oo][Dd][Ee]|[Ss][Tt][Aa][Tt][Uu][Ss])` +
			jsSpace + `*[:=]?` + jsSpace + `*(-?[0-9]+)\b`,
	)
	jsSpaceRunRe = regexp.MustCompile(jsSpace + `+`)
)

type BuildTestMemoKeyInput struct {
	Command         string `json:"command"`
	HeadSHA         string `json:"headSha"`
	StatusPorcelain string `json:"statusPorcelain"`
	TreeFingerprint string `json:"treeFingerprint,omitempty"`
	WorktreeID      string `json:"worktreeId,omitempty"`
}

type CachedTestResult struct {
	Code   jscompat.JSNumber `json:"code"`
	Stdout string            `json:"stdout"`
	Stderr string            `json:"stderr"`
}

// IsTestCommand reports whether a shell command contains one of the supported
// test-runner command phrases at JavaScript ASCII word boundaries.
func IsTestCommand(command string) bool {
	return TEST_COMMAND_PATTERN.MatchString(command)
}

// LatestTestCommandPassed scans tool parts in message order and returns the
// latest recognized test command's exit outcome, or nil for JavaScript
// undefined when no exit status can be recovered.
func LatestTestCommandPassed(messages []any) *bool {
	var result *bool
	for _, message := range messages {
		partsValue, ok := property(message, "parts")
		if !ok || partsValue == nil {
			continue
		}
		parts, ok := partsValue.([]any)
		if !ok {
			continue
		}
		for _, part := range parts {
			partType, _ := property(part, "type")
			if typeString, ok := partType.(string); !ok || typeString != "tool" {
				continue
			}
			input := firstNonNullish(
				nestedProperty(part, "state", "input"),
				propertyValue(part, "input"),
				propertyValue(part, "args"),
				propertyValue(part, "arguments"),
			)
			command, ok := input.(string)
			if !ok {
				commandValue, _ := property(input, "command")
				command, ok = commandValue.(string)
			}
			if !ok || !IsTestCommand(command) {
				continue
			}

			state := firstNonNullish(propertyValue(part, "state"), part)
			code := firstNonNullish(
				nestedProperty(state, "metadata", "exitCode"),
				nestedProperty(state, "metadata", "exit_code"),
				propertyValue(state, "exitCode"),
				propertyValue(state, "exit_code"),
			)
			if numeric, ok := jsNumber(code); ok {
				passed := numeric == 0
				result = &passed
				continue
			}
			output := firstNonNullish(
				propertyValue(state, "output"),
				nestedProperty(state, "metadata", "output"),
				propertyValue(state, "error"),
				"",
			)
			match := exitStatusRe.FindStringSubmatch(jsString(output))
			if match != nil {
				passed := signedIntegerIsZero(match[1])
				result = &passed
			}
		}
	}
	return result
}

func property(value any, key string) (any, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	out, exists := object[key]
	return out, exists
}

func propertyValue(value any, key string) any {
	out, _ := property(value, key)
	return out
}

func nestedProperty(value any, keys ...string) any {
	current := value
	for _, key := range keys {
		next, ok := property(current, key)
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

func firstNonNullish(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func jsNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int8:
		return float64(number), true
	case int16:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint8:
		return float64(number), true
	case uint16:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), true
	default:
		return 0, false
	}
}

func jsString(value any) string {
	switch item := value.(type) {
	case nil:
		return "null"
	case string:
		return item
	case bool:
		if item {
			return "true"
		}
		return "false"
	case []any:
		parts := make([]string, len(item))
		for index, element := range item {
			if element != nil {
				parts[index] = jsString(element)
			}
		}
		return strings.Join(parts, ",")
	case []string:
		return strings.Join(item, ",")
	case map[string]any:
		return "[object Object]"
	default:
		if number, ok := jsNumber(value); ok {
			return jscompat.FormatNumber(number)
		}
		return "[object Object]"
	}
}

func signedIntegerIsZero(value string) bool {
	value = strings.TrimPrefix(value, "-")
	return strings.Trim(value, "0") == ""
}

// NormalizeTestCommand applies JavaScript trim and collapses each JavaScript
// whitespace run to one ASCII space.
func NormalizeTestCommand(command string) string {
	return jsSpaceRunRe.ReplaceAllString(jscompat.Trim(command), " ")
}

// BuildTestMemoKey joins the normalized command, trimmed HEAD, and lowercase
// SHA-256 of the available tree state with NUL separators. StatusPorcelain is
// retained for frozen TS fixture parity; live callers provide a content-aware
// fingerprint and worktree identity so equal status shapes cannot collide.
func BuildTestMemoKey(args BuildTestMemoKeyInput) string {
	treeState := args.StatusPorcelain
	if args.TreeFingerprint != "" {
		treeState = args.TreeFingerprint
	}
	sum := sha256.Sum256([]byte(treeState))
	parts := []string{
		NormalizeTestCommand(args.Command),
		jscompat.Trim(args.HeadSHA),
		hex.EncodeToString(sum[:]),
	}
	if args.WorktreeID != "" {
		parts = append(parts, args.WorktreeID)
	}
	return strings.Join(parts, "\x00")
}

// TestCommandMemo is a per-run insertion-ordered cache. Set on an existing key
// moves it to the newest position, matching delete-then-set on a JavaScript Map.
type TestCommandMemo struct {
	cap     float64
	entries *jscompat.OrderedMap[string, *CachedTestResult]
}

func NewTestCommandMemo(capacity ...float64) *TestCommandMemo {
	cap := TEST_MEMO_CAP
	if len(capacity) != 0 {
		cap = capacity[0]
	}
	return &TestCommandMemo{
		cap: cap, entries: jscompat.NewOrderedMap[string, *CachedTestResult](),
	}
}

func (memo *TestCommandMemo) Get(key string) *CachedTestResult {
	value, _ := memo.entries.Get(key)
	return value
}

func (memo *TestCommandMemo) Set(key string, value *CachedTestResult) {
	if memo.entries.Has(key) {
		memo.entries.Delete(key)
	}
	memo.entries.Set(key, value)
	for float64(memo.entries.Len()) > memo.cap {
		keys := memo.entries.Keys()
		if len(keys) == 0 {
			break
		}
		memo.entries.Delete(keys[0])
	}
}

func (memo *TestCommandMemo) Size() int {
	return memo.entries.Len()
}
