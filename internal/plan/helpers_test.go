package plan

import (
	"context"
	"strings"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// stubClient answers planning calls without a network. Routing is by system
// prompt because that is what identifies the pass — the same goal and the same
// catalog reach every one of them.
type stubClient struct {
	reply func(system, user string) string

	mutex   sync.Mutex
	prompts []string // the final user message of every call, in arrival order
}

func (c *stubClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	var system, user string
	for _, message := range messages {
		text := textOf(message)
		if message.Role == "system" {
			system += text
			continue
		}
		user = text
	}
	c.mutex.Lock()
	c.prompts = append(c.prompts, user)
	c.mutex.Unlock()
	return response(c.reply(system, user)), nil
}

// asked reports whether any call carried the given text in its final user
// message — how a test checks which stages a pass was run over.
func (c *stubClient) asked(fragment string) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for _, prompt := range c.prompts {
		if strings.Contains(prompt, fragment) {
			return true
		}
	}
	return false
}

func textOf(message ai.Message) string {
	var body strings.Builder
	for _, part := range message.Content {
		body.WriteString(part.Text)
	}
	return body.String()
}

func response(text string) *ai.Response {
	return &ai.Response{Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
		FinishReason: "stop",
	}}}
}
