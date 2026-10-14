package builder

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type modelFunc func(context.Context, svc.ModelCall) (svc.ModelReply, error)

func (f modelFunc) Complete(ctx context.Context, call svc.ModelCall) (svc.ModelReply, error) {
	return f(ctx, call)
}

func TestWriteUnitRepairsMissingFilesObjectOnce(t *testing.T) {
	tests := []struct {
		name, first, second string
		pass                bool
	}{
		{"garbage repaired", "not JSON {broken", `{"agent.md":"hello"}`, true},
		{"inline body repaired", `{"budget":{},"records":{}}`, `{"files":{"agent.md":"hello"}}`, true},
		{"garbage twice", "not JSON {broken", "still not JSON", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []svc.ModelCall
			all := &svc.Services{Model: modelFunc(func(_ context.Context, call svc.ModelCall) (svc.ModelReply, error) {
				calls = append(calls, call)
				text := tt.first
				if len(calls) > 1 {
					text = tt.second
				}
				return svc.ModelReply{Text: text, Cost: 0.01}, nil
			}), Hub: &memHub{}}
			s := New(svc.Config{}, all)
			b := s.newBuild("inventory", bridge.CreateAppParams{})
			files, err := s.writeUnit(context.Background(), b, prompt.Plan{}, goldenPlan(), writeUnit{only: []string{"agent.md"}})
			if tt.pass {
				if err != nil || string(files["agent.md"]) != "hello" {
					t.Fatalf("files=%v err=%v", files, err)
				}
			} else {
				var correction *prompt.ShapeError
				if !errors.As(err, &correction) {
					t.Fatalf("err=%v, want files object correction", err)
				}
			}
			if len(calls) != 2 {
				t.Fatalf("calls=%d, want exactly one retry", len(calls))
			}
			messages := calls[1].Messages
			if len(messages) != 3 || messages[1].Role != "assistant" || messages[1].Text != tt.first ||
				messages[2].Role != "user" || !strings.Contains(messages[2].Text, `this reply must hold exactly the paths ["agent.md"]; got keys`) {
				t.Fatalf("repair messages=%+v", messages)
			}
			if cost := b.cost().Spent; cost != 0.02 {
				t.Fatalf("cost=%v, want both calls charged", cost)
			}
		})
	}
}
