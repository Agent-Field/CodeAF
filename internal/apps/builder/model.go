package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// DefaultModel builds the app unless the server is told another one. It is the
// fastest OpenRouter model that passed the live smoke runs (see LANE-RESULT).
const DefaultModel = "deepseek/deepseek-v4.1-flash"

// maxReplyTokens bounds one Builder reply; the largest screen is far below it.
const maxReplyTokens = 16000

// Model is the one model door for the Builder and the app agent. It rides the
// same provider stack, key ladder and billing receipts as the chat, resolved
// the way appapi's AI routing resolves them.
type Model struct{}

var _ svc.Model = (*Model)(nil)

// NewModel returns the model door. Settings and the key are read on every call,
// so a key the desk hands over after the server started is used at once and a
// server without one still starts.
func NewModel(svc.Config, *svc.Services) *Model { return &Model{} }

// Complete sends one call and returns the text and what it cost in USD.
func (m *Model) Complete(ctx context.Context, call svc.ModelCall) (svc.ModelReply, error) {
	model := call.Model
	if model == "" {
		model = DefaultModel
	}
	role := roleOf(call)
	configured := clientConfig(model)
	client, err := provider.NewClient(configured)
	if err != nil {
		return svc.ModelReply{}, fmt.Errorf("%s model %s: %w", role, model, err)
	}
	var billed float64
	ctx = provider.WithBilling(ctx, func(b provider.Billed) { billed += b.Cost })
	ctx = provider.WithoutStream(provider.WithReasoningEffort(ctx, provider.EffortOff))
	ctx = provider.WithCallTag(ctx, "apps."+strings.ReplaceAll(role, " ", "-"))
	response, err := client.CompleteWithMessages(ctx, messagesOf(call),
		ai.WithModel(configured.Model), ai.WithMaxTokens(maxReplyTokens))
	if err != nil {
		return svc.ModelReply{}, fmt.Errorf("%s model %s: %w", role, model, err)
	}
	if billed == 0 && response.Usage != nil && response.Usage.Cost != nil {
		billed = *response.Usage.Cost
	}
	return svc.ModelReply{Text: response.Text(), Cost: billed}, nil
}

func roleOf(call svc.ModelCall) string {
	if call.Role == "" {
		return "builder"
	}
	return call.Role
}

// clientConfig is appapi's AI routing for one model: the app's profile, its
// model sources and key ladder, and the cached model catalog.
func clientConfig(model string) provider.Config {
	dir := config.ProfilePath(config.ProfileDir(), "")
	settings := config.Config{
		ProfileDir: dir,
		Sources:    config.ResolveSources(dir, config.APIKeyAt(dir), config.DefaultBaseURL),
		Models:     catalog.Recall(catalog.Options{Dir: dir}),
	}
	return settings.ClientConfig(model)
}

func messagesOf(call svc.ModelCall) []ai.Message {
	messages := make([]ai.Message, 0, len(call.Messages)+1)
	if system := strings.TrimSpace(call.System + schemaLine(call.Schema)); system != "" {
		messages = append(messages, textMessage("system", system))
	}
	for _, m := range call.Messages {
		messages = append(messages, textMessage(m.Role, m.Text))
	}
	return messages
}

// schemaLine states the reply schema in the system message: the provider call
// carries no response format, so a model only returns the required keys when
// the prompt names them.
func schemaLine(schema json.RawMessage) string {
	var parsed struct {
		Required []string `json:"required"`
	}
	if len(schema) == 0 || json.Unmarshal(schema, &parsed) != nil {
		return ""
	}
	line := "\nReply with one JSON object matching this JSON Schema: " + string(schema)
	if len(parsed.Required) > 0 {
		line += "\nIt must have every one of these keys: " + strings.Join(parsed.Required, ", ") + "."
	}
	return line
}

func textMessage(role, text string) ai.Message {
	return ai.Message{Role: role, Content: []ai.ContentPart{{Type: "text", Text: text}}}
}
