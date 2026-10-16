package session

import (
	"context"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const mediaInputMessageLimit = 64

type mediaInputMessages struct {
	IDs     []string
	Omitted bool
}
type mediaInputContextKey struct{}
type mediaInputCaptureKey struct{}

// A capture belongs to one synchronous completion invocation. Only the
// completion caller reads it, after the call returns; it never rides a tool or
// a streamed callback's context. Failed attempts cannot publish a snapshot.
type mediaInputCapture struct{ messages mediaInputMessages }

func withMediaInputCapture(ctx context.Context, capture *mediaInputCapture) context.Context {
	return context.WithValue(ctx, mediaInputCaptureKey{}, capture)
}
func publishMediaInputs(ctx context.Context, messages mediaInputMessages) {
	if capture, _ := ctx.Value(mediaInputCaptureKey{}).(*mediaInputCapture); capture != nil {
		capture.messages = messages
	}
}
func withMediaInputs(ctx context.Context, messages mediaInputMessages) context.Context {
	messages.IDs = append([]string(nil), messages.IDs...)
	return context.WithValue(ctx, mediaInputContextKey{}, messages)
}
func mediaInputsFrom(ctx context.Context) mediaInputMessages {
	messages, _ := ctx.Value(mediaInputContextKey{}).(mediaInputMessages)
	messages.IDs = append([]string(nil), messages.IDs...)
	return messages
}

// Only identities on the actual prepared request count. Equal words, latest
// user state and chronological proximity prove no input relationship. Work is
// once per provider request, never per token, with at most 64 retained IDs.
func (a *Agent) preparedMediaInputs(messages []ai.Message) mediaInputMessages {
	a.mu.Lock()
	p := a.presentation
	a.mu.Unlock()
	return p.inputMessages(messages)
}
func (p *presentationIndex) inputMessages(messages []ai.Message) mediaInputMessages {
	if p == nil {
		return mediaInputMessages{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var result mediaInputMessages
	seen := make(map[string]struct{})
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != "user" || len(message.Content) == 0 {
			continue
		}
		mark, ok := p.entries[&message.Content[0]]
		if !ok || mark.Audience != audiencePerson || mark.Composed == nil || mark.Composed.MessageID == "" {
			continue
		}
		id := mark.Composed.MessageID
		if _, ok := seen[id]; ok {
			continue
		}
		if len(result.IDs) == mediaInputMessageLimit {
			result.Omitted = true
			break
		}
		seen[id] = struct{}{}
		result.IDs = append(result.IDs, id)
	}
	for i, j := 0, len(result.IDs)-1; i < j; i, j = i+1, j-1 {
		result.IDs[i], result.IDs[j] = result.IDs[j], result.IDs[i]
	}
	return result
}
