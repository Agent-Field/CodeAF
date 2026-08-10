package plannertranslate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
)

// DAGSchema is the agent-json schema boundary corresponding to DAGSchema.
// Root and task objects tolerate extra keys; dependency objects are strict.
type DAGSchema struct{}

// SafeParse implements agentjson.Schema[DAGData].
func (DAGSchema) SafeParse(raw json.RawMessage) agentjson.Validation[DAGData] {
	data, issues := parseDAG(raw)
	return agentjson.Validation[DAGData]{Data: data, Issues: issues}
}

func parseDAG(raw json.RawMessage) (DAGData, []agentjson.Issue) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return DAGData{}, []agentjson.Issue{{Message: err.Error()}}
	}
	tasksRaw, ok := root["tasks"]
	if !ok {
		return DAGData{}, []agentjson.Issue{{Path: []string{"tasks"}, Message: "Required"}}
	}
	var taskObjects []map[string]json.RawMessage
	if err := json.Unmarshal(tasksRaw, &taskObjects); err != nil {
		return DAGData{}, []agentjson.Issue{{Path: []string{"tasks"}, Message: "Expected array"}}
	}

	out := DAGData{Tasks: make([]DAGTask, 0, len(taskObjects))}
	if value, exists := root["summary"]; exists {
		var summary string
		if json.Unmarshal(value, &summary) == nil {
			out.Summary = &summary
		} else {
			return DAGData{}, []agentjson.Issue{{Path: []string{"summary"}, Message: "Expected string"}}
		}
	}
	if value, exists := root["contexts"]; exists {
		if err := json.Unmarshal(value, &out.Contexts); err != nil {
			return DAGData{}, []agentjson.Issue{{Path: []string{"contexts"}, Message: "Expected array"}}
		}
	}
	if value, exists := root["residual"]; exists {
		if !validResidual(value) {
			return DAGData{}, []agentjson.Issue{{Path: []string{"residual"}, Message: "Invalid input"}}
		}
		out.Residual = append(json.RawMessage(nil), value...)
	}

	var issues []agentjson.Issue
	for index, object := range taskObjects {
		task, taskIssues := parseTask(object, index)
		issues = append(issues, taskIssues...)
		out.Tasks = append(out.Tasks, task)
	}
	return out, issues
}

func parseTask(object map[string]json.RawMessage, index int) (DAGTask, []agentjson.Issue) {
	task := DAGTask{Kind: "code", Tags: []string{}, Deps: []DAGDep{}}
	var issues []agentjson.Issue
	readString := func(key string, minimum, maximum int, target *string) {
		raw, ok := object[key]
		if !ok || json.Unmarshal(raw, target) != nil {
			issues = append(issues, agentjson.Issue{
				Path: []string{"tasks", strconv.Itoa(index), key}, Message: "Expected string",
			})
			return
		}
		length := len(utf16.Encode([]rune(*target)))
		if length < minimum || maximum > 0 && length > maximum {
			issues = append(issues, agentjson.Issue{
				Path: []string{"tasks", strconv.Itoa(index), key}, Message: "String constraint failed",
			})
		}
	}
	readString("taskKey", 1, 0, &task.TaskKey)
	readString("title", 1, 200, &task.Title)
	readString("description", 20, 0, &task.Description)
	if raw, ok := object["kind"]; ok {
		if json.Unmarshal(raw, &task.Kind) != nil ||
			(task.Kind != "code" && task.Kind != "test" && task.Kind != "research") {
			issues = append(issues, agentjson.Issue{
				Path: []string{"tasks", strconv.Itoa(index), "kind"}, Message: "Invalid enum value",
			})
		}
	}
	if raw, ok := object["tags"]; ok {
		if err := json.Unmarshal(raw, &task.Tags); err != nil {
			issues = append(issues, agentjson.Issue{
				Path: []string{"tasks", strconv.Itoa(index), "tags"}, Message: "Expected string array",
			})
		}
	}
	if raw, ok := object["priority"]; ok {
		var priority float64
		if err := json.Unmarshal(raw, &priority); err != nil {
			issues = append(issues, agentjson.Issue{
				Path: []string{"tasks", strconv.Itoa(index), "priority"}, Message: "Expected number",
			})
		} else {
			value := jscompat.JSNumber(priority)
			task.Priority = &value
		}
	}
	if raw, ok := object["deps"]; ok {
		var deps []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &deps); err != nil {
			issues = append(issues, agentjson.Issue{
				Path: []string{"tasks", strconv.Itoa(index), "deps"}, Message: "Expected array",
			})
		} else {
			task.Deps = make([]DAGDep, 0, len(deps))
			for depIndex, depObject := range deps {
				if len(depObject) != 2 {
					issues = append(issues, agentjson.Issue{
						Path:    []string{"tasks", strconv.Itoa(index), "deps", strconv.Itoa(depIndex)},
						Message: "Unrecognized key(s) in object",
					})
					continue
				}
				var dep DAGDep
				fromRaw, fromOK := depObject["from_task"]
				kindRaw, kindOK := depObject["kind"]
				if !fromOK || json.Unmarshal(fromRaw, &dep.FromTask) != nil || dep.FromTask == "" {
					issues = append(issues, agentjson.Issue{
						Path:    []string{"tasks", strconv.Itoa(index), "deps", strconv.Itoa(depIndex), "from_task"},
						Message: "Expected nonempty string",
					})
				}
				if !kindOK || json.Unmarshal(kindRaw, &dep.Kind) != nil ||
					(dep.Kind != "feeds_into" && dep.Kind != "blocks" && dep.Kind != "suggests") {
					issues = append(issues, agentjson.Issue{
						Path:    []string{"tasks", strconv.Itoa(index), "deps", strconv.Itoa(depIndex), "kind"},
						Message: "Invalid enum value",
					})
				}
				task.Deps = append(task.Deps, dep)
			}
		}
	}
	return task, issues
}

func validResidual(raw json.RawMessage) bool {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return true
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return false
	}
	content, ok := object["content"]
	return ok && json.Unmarshal(content, &text) == nil
}

func residualContent(data DAGData) string {
	for _, context := range data.Contexts {
		if context.Kind == "residual" {
			return jscompat.Trim(context.Content)
		}
	}
	if len(data.Residual) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(data.Residual, &text) == nil {
		return jscompat.Trim(text)
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data.Residual, &object) == nil {
		_ = json.Unmarshal(object["content"], &text)
	}
	return jscompat.Trim(text)
}

func validateDAG(data DAGData) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	validation := (DAGSchema{}).SafeParse(encoded)
	if validation.Success() {
		return nil
	}
	var message bytes.Buffer
	for index, issue := range validation.Issues {
		if index > 0 {
			message.WriteString("; ")
		}
		message.WriteString(fmt.Sprint(issue.Path))
		message.WriteString(" ")
		message.WriteString(issue.Message)
	}
	return fmt.Errorf("%s", message.String())
}
