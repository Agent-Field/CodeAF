package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/question"
)

const questionSchema = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"type": "object",
	"properties": {
		"questions": {
			"description": "Questions to ask",
			"type": "array",
			"items": {
				"ref": "QuestionPrompt",
				"type": "object",
				"properties": {
					"question": {"description": "Complete question", "type": "string"},
					"header": {"description": "Very short label (max 30 chars)", "type": "string"},
					"options": {
						"description": "Available choices",
						"type": "array",
						"items": {
							"ref": "QuestionOption",
							"type": "object",
							"properties": {
								"label": {"description": "Display text (1-5 words, concise)", "type": "string"},
								"description": {"description": "Explanation of choice", "type": "string"}
							},
							"required": ["label", "description"]
						}
					},
					"multiple": {"description": "Allow selecting multiple choices", "type": "boolean"}
				},
				"required": ["question", "header", "options"]
			}
		}
	},
	"required": ["questions"]
}`

type questionInput struct {
	Questions []question.Prompt `json:"questions"`
}

func validateQuestion(raw json.RawMessage) error {
	_, err := decodeQuestionInput(raw)
	return err
}

func decodeQuestionInput(raw json.RawMessage) (questionInput, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return questionInput{}, fmt.Errorf("input must be a JSON object: %w", err)
	}
	if fields == nil {
		return questionInput{}, fmt.Errorf("input must be a JSON object")
	}
	questionsField, ok := fields["questions"]
	if !ok {
		return questionInput{}, fmt.Errorf("missing required field %q", "questions")
	}
	questionsRaw := bytes.TrimSpace(questionsField)
	if len(questionsRaw) == 0 || questionsRaw[0] != '[' {
		return questionInput{}, fmt.Errorf("field %q must be an array", "questions")
	}
	var prompts []json.RawMessage
	if err := json.Unmarshal(questionsRaw, &prompts); err != nil {
		return questionInput{}, fmt.Errorf("invalid questions: %w", err)
	}
	for index, prompt := range prompts {
		if !question.SchemaAccepts("prompt", "effect", prompt) {
			return questionInput{}, fmt.Errorf("invalid question at index %d", index)
		}
	}
	var input questionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return questionInput{}, fmt.Errorf("invalid input: %w", err)
	}
	return input, nil
}

func (r *Registry) executeQuestion(ctx context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
	input, err := decodeQuestionInput(call.Input)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	questions := make([]question.Info, len(input.Questions))
	for index, prompt := range input.Questions {
		questions[index] = question.Info{
			Question: prompt.Question,
			Header:   prompt.Header,
			Options:  append([]question.Option(nil), prompt.Options...),
			Multiple: prompt.Multiple,
		}
	}
	var origin *question.Tool
	if call.ID != "" {
		origin = &question.Tool{MessageID: call.MessageID, CallID: call.ID}
	}

	// The pinned TS tool delegates here without a timeout (src/tool/question.ts:22-28).
	// Question.ask publishes question.asked and then only awaits its Deferred
	// (src/question/index.ts:155-179). Headless codeaf merely streams bus events
	// (src/cli/harness-boot.ts:152-157), and its server has no control plane
	// (src/server/server.ts:1), so without an attached answerer this blocks until
	// the run context is cancelled. Embedded registries keep that behavior. A
	// headless runtime rejects through this service; after three consecutive
	// rejections the registry returns the bounded, model-visible result below.
	answers, err := r.question.Ask(ctx, question.AskInput{
		SessionID: call.SessionID,
		Questions: questions,
		Tool:      origin,
	})
	if err != nil {
		var rejected *question.RejectedError
		if errors.As(err, &rejected) && r.recordQuestionRejection(call.SessionID) >= 3 {
			return steploop.ToolResult{
				Title:    "Questions unavailable",
				Output:   "Questions are unavailable for this run. Your current answers are final; proceed with your best judgment.",
				Metadata: msgmodel.RawObject("{}"),
			}, nil
		}
		return steploop.ToolResult{}, err
	}
	r.resetQuestionRejections(call.SessionID)

	formatted := make([]string, len(input.Questions))
	for index, prompt := range input.Questions {
		answer := "Unanswered"
		if index < len(answers) && len(answers[index]) > 0 {
			answer = strings.Join(answers[index], ", ")
		}
		formatted[index] = `"` + prompt.Question + `"="` + answer + `"`
	}
	title := fmt.Sprintf("Asked %d question", len(input.Questions))
	if len(input.Questions) > 1 {
		title += "s"
	}
	metadata, marshalErr := jscompat.Stringify(struct {
		Answers []question.Answer `json:"answers"`
	}{Answers: answers})
	if marshalErr != nil {
		return steploop.ToolResult{}, marshalErr
	}
	return steploop.ToolResult{
		Title: title,
		Output: "User has answered your questions: " + strings.Join(formatted, ", ") +
			". You can now continue with the user's answers in mind.",
		Metadata: msgmodel.RawObject(metadata),
	}, nil
}
