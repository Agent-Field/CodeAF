package knowledge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// document preserves optional projection metadata when an earlier build is read.
// Model responses are untrusted documents; each stage validates references before publication.
type document map[string]any

func object(value any) document {
	switch value := value.(type) {
	case document:
		return value
	case map[string]any:
		return document(value)
	}
	return document{}
}
func items(value any) []document {
	var result []document
	switch value := value.(type) {
	case []document:
		return value
	case []any:
		for _, item := range value {
			if doc, ok := item.(map[string]any); ok {
				result = append(result, doc)
			} else if doc, ok := item.(document); ok {
				result = append(result, doc)
			}
		}
	}
	return result
}
func textValue(value any) string { s, _ := value.(string); return s }
func flag(value any) bool        { b, _ := value.(bool); return b }
func number(value any) float64 {
	switch n := value.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}
func stringsValue(value any) []string {
	switch value := value.(type) {
	case []string:
		return value
	case []any:
		result := make([]string, 0, len(value))
		for _, item := range value {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return []string{}
}
func unique(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, s := range values {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
func sortedUnique(values []string) []string {
	result := unique(values)
	sort.Strings(result)
	return result
}
func contains(values []string, target string) bool {
	for _, s := range values {
		if s == target {
			return true
		}
	}
	return false
}
func subset(values []string, allowed map[string]document) bool {
	for _, id := range values {
		if _, ok := allowed[id]; !ok {
			return false
		}
	}
	return true
}
func index(values []document) map[string]document {
	result := map[string]document{}
	for _, value := range values {
		result[textValue(value["id"])] = value
	}
	return result
}
func clone(value document) document {
	b, _ := json.Marshal(value)
	var result document
	_ = json.Unmarshal(b, &result)
	return result
}
func readDocument(path string) (document, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return document{}, nil
	}
	if err != nil {
		return nil, err
	}
	var value document
	err = json.Unmarshal(b, &value)
	return value, err
}
func choose(value document, keys ...string) document {
	result := document{}
	for _, key := range keys {
		if v, ok := value[key]; ok {
			result[key] = v
		}
	}
	return result
}
func merge(left, right document) document {
	result := document{}
	for key, value := range left {
		result[key] = value
	}
	for key, value := range right {
		result[key] = value
	}
	return result
}
func emptyCompilation() document {
	return document{"schema": 1, "strategy": "reconciled", "sources": document{}, "claims": []document{}, "notes": []document{}, "relations": []document{}, "rejected": []document{}, "scope_aliases": []document{}}
}

// identity matches the original canonical JSON so persisted source, page, and cache IDs stay stable.
func identity(value any) string {
	encoded, _ := json.Marshal(value)
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var plain any
	_ = decoder.Decode(&plain)
	var out strings.Builder
	canonicalJSON(&out, plain)
	return hash(out.String())[:16]
}
func canonicalJSON(out *strings.Builder, value any) {
	switch value := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for k := range value {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteString(", ")
			}
			canonicalJSON(out, key)
			out.WriteString(": ")
			canonicalJSON(out, value[key])
		}
		out.WriteByte('}')
	case []any:
		out.WriteByte('[')
		for i, item := range value {
			if i > 0 {
				out.WriteString(", ")
			}
			canonicalJSON(out, item)
		}
		out.WriteByte(']')
	case string:
		var b bytes.Buffer
		encoder := json.NewEncoder(&b)
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(value)
		s := strings.TrimSuffix(b.String(), "\n")
		s = strings.ReplaceAll(s, `\u2028`, "\u2028")
		s = strings.ReplaceAll(s, `\u2029`, "\u2029")
		out.WriteString(s)
	case json.Number:
		out.WriteString(string(value))
	case bool:
		fmt.Fprint(out, value)
	default:
		out.WriteString("null")
	}
}

func decodeDocument(raw []byte, value *document) error { return json.Unmarshal(raw, value) }
