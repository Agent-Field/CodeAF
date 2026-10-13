package builder

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// DefaultModel builds the app unless the server is told another one. It is the
// fastest OpenRouter model that passed the live smoke runs (see LANE-RESULT).
const DefaultModel = "deepseek/deepseek-v4.1-flash"

// maxReplyTokens bounds one Builder reply; the largest screen is far below it.
const maxReplyTokens = 16000

// Model is the one model door for the Builder and the app agent. It rides the
// same provider stack, key ladder and billing receipts as the chat.
type Model struct {
	mu       sync.Mutex
	settings *config.Config
	clients  map[string]*provider.Client
}

var _ svc.Model = (*Model)(nil)

// NewModel returns the model door. Settings load on first use, so a server
// without a key still starts and answers only the calls that need one.
func NewModel(svc.Config, *svc.Services) *Model {
	return &Model{clients: map[string]*provider.Client{}}
}

// Complete sends one call and returns the text and what it cost in USD.
func (m *Model) Complete(ctx context.Context, call svc.ModelCall) (svc.ModelReply, error) {
	model := call.Model
	if model == "" {
		model = DefaultModel
	}
	client, wire, err := m.client(model)
	if err != nil {
		return svc.ModelReply{}, err
	}
	var billed float64
	ctx = provider.WithBilling(ctx, func(b provider.Billed) { billed += b.Cost })
	ctx = provider.WithoutStream(provider.WithReasoningEffort(ctx, provider.EffortOff))
	ctx = provider.WithCallTag(ctx, "apps.builder")
	response, err := client.CompleteWithMessages(ctx, messagesOf(call),
		ai.WithModel(wire), ai.WithMaxTokens(maxReplyTokens))
	if err != nil {
		return svc.ModelReply{}, fmt.Errorf("builder model %s: %w", model, err)
	}
	if billed == 0 && response.Usage != nil && response.Usage.Cost != nil {
		billed = *response.Usage.Cost
	}
	return svc.ModelReply{Text: response.Text(), Cost: billed}, nil
}

func (m *Model) client(model string) (*provider.Client, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settings == nil {
		loaded, err := config.Load()
		if err != nil {
			return nil, "", fmt.Errorf("builder model settings: %w", err)
		}
		m.settings = &loaded
	}
	configured := m.settings.ClientConfig(model)
	if c, ok := m.clients[model]; ok {
		return c, configured.Model, nil
	}
	c, err := provider.NewClient(configured)
	if err != nil {
		return nil, "", err
	}
	m.clients[model] = c
	return c, configured.Model, nil
}

func messagesOf(call svc.ModelCall) []ai.Message {
	messages := make([]ai.Message, 0, len(call.Messages)+1)
	if strings.TrimSpace(call.System) != "" {
		messages = append(messages, textMessage("system", call.System))
	}
	for _, m := range call.Messages {
		messages = append(messages, textMessage(m.Role, m.Text))
	}
	return messages
}

func textMessage(role, text string) ai.Message {
	return ai.Message{Role: role, Content: []ai.ContentPart{{Type: "text", Text: text}}}
}
