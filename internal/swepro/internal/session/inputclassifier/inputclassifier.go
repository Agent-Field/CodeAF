// Package inputclassifier ports src/session/input-classifier.ts:1-92 from
// swe-pro commit 3b25a1a.
package inputclassifier

import (
	"context"
	"encoding/json"
	"path/filepath"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
)

const OutputRelativePath = ".codeaf/plan/classification.json"

type Classification struct {
	Class  string `json:"class"`
	Reason string `json:"reason"`
}

var Fallback = Classification{
	Class: "focused",
	Reason: "Input Classifier produced no parseable JSON after retries. " +
		"Defaulting to `focused` — runs single-pass architecture-gate, " +
		"which is the middle-cost option.",
}

type Input struct {
	Workspace       string `json:"workspace"`
	ParentSessionID string `json:"parentSessionID"`
	UserPrompt      string `json:"userPrompt"`
}

func BuildPrompt(input Input, outputPath string) string {
	return "# Input classification\n\n" +
		"Classify the user's prompt below as trivial / focused / vague per the\n" +
		"rules in your system prompt. Write a single JSON object to the output\n" +
		"file.\n\n" +
		"Workspace: " + input.Workspace + "\n" +
		"Output file: " + outputPath + "\n\n" +
		"## User prompt\n\n" +
		input.UserPrompt
}

func Dispatch(
	ctx context.Context, input Input, deps agentjson.Dependencies,
) (agentjson.Result[Classification], error) {
	outputPath := filepath.Join(input.Workspace, OutputRelativePath)
	maxRetries := 1
	timeoutMS := int64(3 * 60_000)
	label := "input-classifier"
	fallback := Fallback
	return agentjson.DispatchJSON(ctx, agentjson.Input[Classification]{
		Agent: "input-classifier", ParentSessionID: input.ParentSessionID,
		Workspace: input.Workspace, TaskPrompt: BuildPrompt(input, outputPath),
		OutputPath: outputPath, Schema: Schema, Fallback: &fallback,
		MaxRetries: &maxRetries, TimeoutMS: &timeoutMS, Label: &label,
	}, deps)
}

var Schema agentjson.Schema[Classification] = agentjson.SchemaFunc[Classification](
	func(raw json.RawMessage) agentjson.Validation[Classification] {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil || object == nil {
			return invalid("Expected object")
		}
		issues := strictKeys(object, "class", "reason")
		class, ok := stringField(object, "class")
		if !ok {
			issues = append(issues, agentjson.Issue{
				Path: []string{"class"}, Message: "Invalid input",
			})
		} else if class != "trivial" && class != "focused" && class != "vague" {
			issues = append(issues, agentjson.Issue{
				Path: []string{"class"}, Message: "Invalid enum value",
			})
		}
		reason, ok := stringField(object, "reason")
		if !ok {
			issues = append(issues, agentjson.Issue{
				Path: []string{"reason"}, Message: "Invalid input",
			})
		} else {
			length := len(utf16.Encode([]rune(reason)))
			if length < 1 || length > 2000 {
				issues = append(issues, agentjson.Issue{
					Path: []string{"reason"}, Message: "String must contain between 1 and 2000 character(s)",
				})
			}
		}
		if len(issues) > 0 {
			return agentjson.Validation[Classification]{Issues: issues}
		}
		return agentjson.Validation[Classification]{
			Data: Classification{Class: class, Reason: reason},
		}
	},
)

func invalid(message string) agentjson.Validation[Classification] {
	return agentjson.Validation[Classification]{
		Issues: []agentjson.Issue{{Message: message}},
	}
}

func strictKeys(object map[string]json.RawMessage, allowed ...string) []agentjson.Issue {
	set := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		set[key] = true
	}
	var issues []agentjson.Issue
	for key := range object {
		if !set[key] {
			issues = append(issues, agentjson.Issue{
				Message: `Unrecognized key: "` + key + `"`,
			})
		}
	}
	return issues
}

func stringField(object map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := object[key]
	if !ok {
		return "", false
	}
	var value string
	return value, json.Unmarshal(raw, &value) == nil
}
