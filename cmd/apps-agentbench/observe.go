package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// observedModel delegates every request to the product's real provider door.
// It keeps partial spend on failed turns and catches tool-error feedback even
// when the model follows it with a friendly final answer.
type observedModel struct {
	svc.Model
	mu     sync.Mutex
	cost   float64
	errors map[string]bool
	final  bool
}

func (m *observedModel) Complete(ctx context.Context, call svc.ModelCall) (svc.ModelReply, error) {
	m.mu.Lock()
	for _, message := range call.Messages {
		m.noteToolError(message)
	}
	m.mu.Unlock()
	reply, err := m.Model.Complete(ctx, call)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cost += reply.Cost
	m.final = hasFinal(reply.Text)
	return reply, err
}

// noteToolError reads the agent's text-action feedback. Current turns carry
// named results as user messages; older saved turns may still use the tool role.
func (m *observedModel) noteToolError(message svc.ModelMessage) {
	text := message.Text
	switch {
	case message.Role == "tool":
	case message.Role == "user" && (strings.HasPrefix(text, "Result of ") || strings.HasPrefix(text, "Result:\n")):
		_, text, _ = strings.Cut(text, "\n")
	default:
		return
	}
	var feedback struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(text), &feedback) == nil && feedback.Error != "" {
		m.errors[feedback.Error] = true
	}
}

// hasFinal follows the agent's JSON-or-plain-speech protocol. Intermediate
// "say" text attached to a tool call does not count as a final reply.
func hasFinal(text string) bool {
	text = strings.TrimSpace(text)
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "```json"), "```"))
	body = strings.TrimSpace(strings.TrimPrefix(body, "```"))
	var action struct {
		Say   string `json:"say"`
		Tool  string `json:"tool"`
		Shape bool   `json:"shape_this"`
	}
	if json.Unmarshal([]byte(body), &action) != nil {
		return text != ""
	}
	return action.Tool == "" && (strings.TrimSpace(action.Say) != "" || action.Shape)
}

func (m *observedModel) measurements() (float64, []string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	errors := make([]string, 0, len(m.errors))
	for err := range m.errors {
		errors = append(errors, err)
	}
	return m.cost, errors, m.final
}
