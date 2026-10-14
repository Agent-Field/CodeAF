package builder

import (
	"context"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var errEmptyReply = errors.New("the model sent an empty reply")

// converse sends one prompt and parses the reply. A reply that does not parse
// gets one more chance with the parser's complaint, because models usually
// repair a stray fence or a missing field when told exactly what was wrong.
func converse[T any](ctx context.Context, s *Service, b *build, text string, parse func(string) (T, error)) (T, error) {
	messages := []svc.ModelMessage{{Role: "user", Text: text}}
	var zero T
	for attempt := 0; ; attempt++ {
		reply, err := s.ask(ctx, b, messages)
		if err != nil {
			return zero, err
		}
		out, parseErr := parse(reply)
		if parseErr == nil {
			return out, nil
		}
		debugLog(b.app, "model reply rejected", parseErr)
		if attempt == 1 {
			return zero, &replyError{err: parseErr}
		}
		var shape *prompt.ShapeError
		if errors.As(parseErr, &shape) {
			s.step(b, bridge.StepWrite, bridge.BuildRunning, logWrongShapeRetry)
		}
		messages = append(messages,
			svc.ModelMessage{Role: "assistant", Text: reply},
			svc.ModelMessage{Role: "user", Text: "That reply was rejected: " + parseErr.Error() +
				"\nReply again with the corrected JSON object only."})
	}
}

// ask makes one model call and charges it to the app's build budget.
func (s *Service) ask(ctx context.Context, b *build, messages []svc.ModelMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := b.withinBudget(); err != nil {
		return "", err
	}
	reply, err := s.all.Model.Complete(ctx, svc.ModelCall{Model: b.model, Messages: messages, App: b.app})
	b.charge(reply.Cost)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(reply.Text) == "" {
		return "", errEmptyReply
	}
	return reply.Text, nil
}
