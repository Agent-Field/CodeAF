package session

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

var workstreamSourceSchema = fmt.Sprintf(`{"type":"object","properties":{"kind":{"type":"string","enum":["factory_job","page","url"]},"id":{"type":"string"},"factory":{"type":"string"},"label":{"type":"string","maxLength":%d}},"required":["kind","id"],"additionalProperties":false}`, workstreamLineMax)

const workstreamReadSchema = `{"type":"object","properties":{"what":{"type":"string","enum":["workstream","pages","page","records","plan","goaheads"]},"page_id":{"type":"string"},"kind":{"type":"string","enum":["decision","question"]},"state":{"type":"string","enum":["proposed","kept","dropped","superseded"]}},"required":["what"],"additionalProperties":false}`

var workstreamRecordSchema = fmt.Sprintf(`{"type":"object","properties":{"text":{"type":"string","minLength":1,"maxLength":%d},"source":%s},"required":["text"],"additionalProperties":false}`, workstreamRecordMax, workstreamSourceSchema)

var workstreamJobsSchema = fmt.Sprintf(`{"type":"object","properties":{"jobs":{"type":"array","minItems":1,"maxItems":%d,"items":{"type":"object","properties":{"title":{"type":"string","minLength":1,"maxLength":%d},"repo":{"type":"string","maxLength":%d},"text":{"type":"string","maxLength":%d}},"required":["title"],"additionalProperties":false}},"wave":{"type":"integer"},"wave_title":{"type":"string","maxLength":%d},"milestone_id":{"type":"string"}},"required":["jobs"],"additionalProperties":false}`, workstreamJobsMax, workstreamLineMax, workstreamLineMax, workstreamDraftTextMax, workstreamLineMax)

var workstreamGoAheadSchema = fmt.Sprintf(`{"type":"object","properties":{"scope":{"type":"string","enum":["job","wave","milestone"]},"factory":{"type":"string"},"job":{"type":"integer"},"wave":{"type":"integer"},"milestone_id":{"type":"string"},"cap_usd":{"type":"number","exclusiveMinimum":0,"maximum":%d},"ends_on":{"type":"string"},"why":{"type":"string","minLength":1,"maxLength":%d}},"required":["scope","cap_usd","ends_on","why"],"additionalProperties":false}`, workstreamGoAheadCapMax, workstreamGoAheadWhyMax)

var workstreamPageSchema = fmt.Sprintf(`{"type":"object","properties":{"page_id":{"type":"string"},"title":{"type":"string","minLength":1,"maxLength":%d},"parent_id":{"type":"string"},"markdown":{"type":"string"},"updated_from":{"type":"string","minLength":1,"maxLength":%d},"source":%s},"required":["markdown","updated_from"],"additionalProperties":false}`, workstreamLineMax, workstreamLineMax, workstreamSourceSchema)

type workstreamArgumentSchema struct {
	Type       string                              `json:"type"`
	Properties map[string]workstreamArgumentSchema `json:"properties"`
	Required   []string                            `json:"required"`
	Enum       []string                            `json:"enum"`
	Items      *workstreamArgumentSchema           `json:"items"`
}

func workstreamDecode(args json.RawMessage, schema workstreamArgumentSchema, target any) error {
	if err := workstreamCheckArguments(args, schema, "arguments"); err != nil {
		return err
	}
	return decodeToolArguments(args, target)
}

func workstreamCheckArguments(args json.RawMessage, schema workstreamArgumentSchema, path string) error {
	if strings.TrimSpace(string(args)) == "null" {
		return fmt.Errorf("%s must not be null", path)
	}
	switch schema.Type {
	case "object":
		return workstreamCheckObject(args, schema, path)
	case "integer", "number":
		return workstreamCheckNumber(args, schema.Type, path)
	case "array":
		var items []json.RawMessage
		if err := json.Unmarshal(args, &items); err != nil {
			return fmt.Errorf("%s must be an array", path)
		}
		for i, item := range items {
			if err := workstreamCheckArguments(item, *schema.Items, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		var value string
		if err := json.Unmarshal(args, &value); err != nil {
			return fmt.Errorf("%s must be a string", path)
		}
		if len(schema.Enum) > 0 && !workstreamHasWord(schema.Enum, value) {
			return fmt.Errorf("%s must be one of %s", path, strings.Join(schema.Enum, ", "))
		}
	}
	return nil
}

func workstreamCheckNumber(args json.RawMessage, kind, path string) error {
	if kind == "integer" {
		var value int
		if err := json.Unmarshal(args, &value); err != nil {
			return fmt.Errorf("%s must be an integer", path)
		}
		return nil
	}
	var value float64
	if err := json.Unmarshal(args, &value); err != nil {
		return fmt.Errorf("%s must be a number", path)
	}
	return nil
}

func workstreamCheckObject(args json.RawMessage, schema workstreamArgumentSchema, path string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil || fields == nil {
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
