package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

var workstreamSourceSchema = fmt.Sprintf(`{"type":"object","properties":{"kind":{"type":"string","enum":["factory_job","page","url"]},"id":{"type":"string"},"factory":{"type":"string"},"label":{"type":"string","maxLength":%d}},"required":["kind","id"],"additionalProperties":false}`, workstreamLineMax)

const workstreamReadSchema = `{"type":"object","properties":{"what":{"type":"string","enum":["workstream","pages","page","records","plan","goaheads","sources","source"]},"page_id":{"type":"string"},"source_id":{"type":"string"},"kind":{"type":"string","enum":["decision","question","goahead"]},"state":{"type":"string","enum":["proposed","kept","dropped","superseded"]}},"required":["what"],"additionalProperties":false}`

var workstreamRecordSchema = fmt.Sprintf(`{"type":"object","properties":{"text":{"type":"string","minLength":1,"maxLength":%d},"source":%s},"required":["text"],"additionalProperties":false}`, workstreamRecordMax, workstreamSourceSchema)

var workstreamJobsSchema = fmt.Sprintf(`{"type":"object","properties":{"jobs":{"type":"array","minItems":1,"maxItems":%d,"items":{"type":"object","properties":{"title":{"type":"string","minLength":1,"maxLength":%d},"repo":{"type":"string","maxLength":%d},"text":{"type":"string","maxLength":%d}},"required":["title"],"additionalProperties":false}},"wave":{"type":"integer"},"wave_title":{"type":"string","maxLength":%d},"milestone_id":{"type":"string"},"source":%s},"required":["jobs"],"additionalProperties":false}`, workstreamJobsMax, workstreamLineMax, workstreamLineMax, workstreamDraftTextMax, workstreamLineMax, workstreamSourceSchema)

var workstreamGoAheadSchema = fmt.Sprintf(`{"type":"object","properties":{"scope":{"type":"string","enum":["job","wave","milestone"]},"factory":{"type":"string"},"job":{"type":"integer"},"wave":{"type":"integer"},"milestone_id":{"type":"string"},"cap_usd":{"type":"number","exclusiveMinimum":0,"maximum":%d},"ends_on":{"type":"string","description":"YYYY-MM-DD, after today in UTC and at most %d days ahead."},"why":{"type":"string","minLength":1,"maxLength":%d}},"required":["scope","cap_usd","ends_on","why"],"additionalProperties":false}`, workstreamGoAheadCapMax, workstreamGoAheadDaysMax, workstreamGoAheadWhyMax)

var workstreamPageSchema = fmt.Sprintf(`{"type":"object","properties":{"page_id":{"type":"string"},"title":{"type":"string","minLength":1,"maxLength":%d},"parent_id":{"type":"string"},"markdown":{"type":"string"},"updated_from":{"type":"string","minLength":1,"maxLength":%d},"source":%s},"required":["markdown","updated_from"],"additionalProperties":false}`, workstreamLineMax, workstreamLineMax, workstreamSourceSchema)

type workstreamArgumentSchema struct {
	Type       string                              `json:"type"`
	Properties map[string]workstreamArgumentSchema `json:"properties"`
	Required   []string                            `json:"required"`
	Enum       []string                            `json:"enum"`
	Items      *workstreamArgumentSchema           `json:"items"`
}

func workstreamDecode(args json.RawMessage, schema workstreamArgumentSchema, target any) (map[string]any, error) {
	if _, source := target.(*WorkstreamSourceProposal); source && !utf8.Valid(args) {
		return nil, fmt.Errorf("source arguments must be valid UTF-8")
	}
	if err := decodeToolArguments(args, target); err != nil {
		return nil, err
	}
	if err := workstreamUniqueKeys(args); err != nil {
		return nil, err
	}
	var value any
	if err := decodeToolArguments(args, &value); err != nil {
		return nil, err
	}
	if err := workstreamCheckArguments(value, schema, "arguments"); err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}

// Walk the original token stream so repeated fields survive typed decoding.
func workstreamUniqueKeys(args json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.UseNumber()
	if err := workstreamUniqueValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("arguments must contain one JSON value")
	}
	return nil
}

func workstreamUniqueValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		return workstreamUniqueObject(decoder)
	case '[':
		for decoder.More() {
			if err := workstreamUniqueValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	}
	return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
}

func workstreamUniqueObject(decoder *json.Decoder) error {
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := token.(string)
		if !ok {
			return fmt.Errorf("object keys must be strings")
		}
		if seen[name] {
			return fmt.Errorf("Duplicate field %q is not allowed.", name)
		}
		seen[name] = true
		if err := workstreamUniqueValue(decoder); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

func workstreamCheckArguments(value any, schema workstreamArgumentSchema, path string) error {
	if value == nil {
		return fmt.Errorf("%s must not be null", path)
	}
	switch schema.Type {
	case "object":
		return workstreamCheckObject(value, schema, path)
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		for i, item := range items {
			if err := workstreamCheckArguments(item, *schema.Items, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		if len(schema.Enum) > 0 && !workstreamHasWord(schema.Enum, text) {
			return fmt.Errorf("%s must be one of %s", path, strings.Join(schema.Enum, ", "))
		}
	}
	return nil
}

func workstreamCheckObject(value any, schema workstreamArgumentSchema, path string) error {
	fields, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be a JSON object", path)
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := schema.Properties[name]; !ok {
			return fmt.Errorf("Unknown field %q is not allowed.", name)
		}
	}
	for _, name := range names {
		if err := workstreamCheckArguments(fields[name], schema.Properties[name], name); err != nil {
			return err
		}
	}
	for _, name := range schema.Required {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

func workstreamHasWord(words []string, word string) bool {
	for _, candidate := range words {
		if candidate == word {
			return true
		}
	}
	return false
}
