// File-state classification ports src/session/agent-json.ts:148-229.
package agentjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type FileStateKind string

const (
	FileMissing    FileStateKind = "missing"
	FileEmpty      FileStateKind = "empty"
	FileParseError FileStateKind = "parse-error"
	FileValidJSON  FileStateKind = "valid-json"
)

type FileState struct {
	Kind  FileStateKind   `json:"kind"`
	Error string          `json:"error,omitempty"`
	Raw   string          `json:"raw,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

func ClassifyFileState(raw []byte, exists bool) FileState {
	if !exists {
		return FileState{Kind: FileMissing}
	}
	text := string(raw)
	if jscompat.Trim(text) == "" {
		return FileState{Kind: FileEmpty}
	}
	if json.Valid(raw) {
		return FileState{Kind: FileValidJSON, Data: append(json.RawMessage(nil), raw...)}
	}
	return FileState{
		Kind:  FileParseError,
		Error: sliceUTF16(JSONParseError(raw), 280),
		Raw:   text,
	}
}

func readFileState(filePath string) FileState {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		if recovered, ok := readDroppedDotArtifact(filePath); ok {
			return ClassifyFileState(recovered, true)
		}
		return FileState{Kind: FileMissing}
	}
	return ClassifyFileState(raw, true)
}

// readDroppedDotArtifact recovers an artifact the agent wrote one character off.
//
// The private artifact path is a HIDDEN file with a random suffix —
// `.auditor-verdict.json.agentjson-4284993955` — and an agent transcribing that
// into a Write call can drop the leading dot. Observed on a real run: the
// auditor produced a complete, schema-valid verdict (7397 bytes, "verdict":
// "pass", zero blockers) at
//
//	.codeaf/auditor-verdict.json.agentjson-4284993955
//
// while the harness watched
//
//	.codeaf/.auditor-verdict.json.agentjson-4284993955
//
// and reported "auditor did not produce a parseable verdict after 3 attempts",
// twice, ending the run. The work was done and correct; only the filename was
// wrong by one character.
//
// The recovery is deliberately exact: same directory, same name minus the
// leading dot, and only when the expected path is absent. The random suffix
// makes the name unguessable, so a file sitting at it can only have come from
// this invocation's agent — there is no ambiguity about ownership, and content
// still goes through the same classification and schema validation as any other
// artifact. Nothing is accepted that would not have been accepted at the
// correct path.
func readDroppedDotArtifact(filePath string) ([]byte, bool) {
	dir, base := filepath.Split(filePath)
	if !strings.HasPrefix(base, ".") {
		return nil, false
	}
	// Only the harness's own random-suffixed private artifacts are eligible.
	if !strings.Contains(base, ".agentjson-") {
		return nil, false
	}
	raw, err := os.ReadFile(filepath.Join(dir, strings.TrimPrefix(base, ".")))
	if err != nil {
		return nil, false
	}
	return raw, true
}

// JSONParseError mirrors the stable JavaScriptCore/Bun JSON.parse error
// families used in repair prompts. It intentionally reports syntax classes,
// not Go's byte offsets.
func JSONParseError(raw []byte) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "JSON Parse error: Unexpected EOF"
	}
	if hasUnterminatedJSONString(trimmed) {
		return "JSON Parse error: Unterminated string"
	}
	var syntax *json.SyntaxError
	var value any
	err := json.Unmarshal(trimmed, &value)
	if !errors.As(err, &syntax) {
		return "JSON Parse error: Unable to parse JSON string"
	}
	message := syntax.Error()
	switch {
	case strings.Contains(message, "unexpected end of JSON input"):
		if trimmed[0] == '{' && !bytes.Contains(trimmed, []byte(":")) {
			return "JSON Parse error: Expected '}'"
		}
		return "JSON Parse error: Unexpected EOF"
	case strings.Contains(message, "invalid character '}' looking for beginning of object key string"):
		return "JSON Parse error: Property name must be a string literal"
	case strings.Contains(message, "invalid character") && identifierStart(trimmed):
		word := leadingIdentifier(trimmed)
		return "JSON Parse error: Unexpected identifier \"" + word + "\""
	case strings.Contains(message, "after top-level value"):
		return "JSON Parse error: Unable to parse JSON string"
	default:
		return "JSON Parse error: Unable to parse JSON string"
	}
}

func hasUnterminatedJSONString(raw []byte) bool {
	inString := false
	escaped := false
	for _, b := range raw {
		if !inString {
			if b == '"' {
				inString = true
			}
			continue
		}
		if escaped {
			escaped = false
			continue
		}
		switch b {
		case '\\':
			escaped = true
		case '"':
			inString = false
		case '\n', '\r':
			return true
		}
	}
	return inString
}

func identifierStart(raw []byte) bool {
	b := raw[0]
	return b == '_' || b == '$' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func leadingIdentifier(raw []byte) string {
	end := 0
	for end < len(raw) {
		b := raw[end]
		if !(b == '_' || b == '$' || b >= 'A' && b <= 'Z' ||
			b >= 'a' && b <= 'z' || end > 0 && b >= '0' && b <= '9') {
			break
		}
		end++
	}
	return string(raw[:end])
}

func ensureDir(filePath string) error {
	index := strings.LastIndexByte(filePath, '/')
	if index < 0 || index == len(filePath)-1 {
		return nil
	}
	dir := filePath[:index]
	if dir == "" || dir == filePath {
		return nil
	}
	return os.MkdirAll(dir, 0o777)
}

func safeUnlink(filePath string) {
	_ = os.Remove(filePath)
}

func sliceUTF16(value string, max int) string {
	units := utf16.Encode([]rune(value))
	if max < 0 {
		max = 0
	}
	if len(units) > max {
		units = units[:max]
	}
	return string(utf16.Decode(units))
}

func readJSONIfExists(filePath string) json.RawMessage {
	raw, err := os.ReadFile(filePath)
	if err != nil || !json.Valid(raw) {
		return nil
	}
	return raw
}
