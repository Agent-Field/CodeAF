package runs

import (
	"context"
	"errors"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

// CompleteAgent runs one headless app turn. A tool-less request keeps the run
// confined to its declared data and connector steps. A conversation adapter
// can use the same app prompt loader and supply its own scoped tools.
func CompleteAgent(ctx context.Context, client session.Completer, system, prompt string, attachments ...ai.ContentPart) (*ai.Response, error) {
	message := func(role, text string) ai.Message {
		return ai.Message{Role: role, Content: []ai.ContentPart{{Type: "text", Text: text}}}
	}
	user := message("user", prompt)
	user.Content = append(user.Content, attachments...)
	reply, err := client.CompleteWithMessages(provider.WithoutStream(ctx),
		[]ai.Message{message("system", system), user}, ai.WithMaxTokens(2048))
	if err == nil && (reply == nil || strings.TrimSpace(reply.Text()) == "") {
		err = errors.New("the app agent answered nothing")
	}
	return reply, err
}
