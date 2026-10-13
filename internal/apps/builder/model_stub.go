package builder

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Model is the stub model door. B5 replaces this file.
type Model struct{}

var _ svc.Model = (*Model)(nil)

func NewModel(svc.Config, *svc.Services) *Model { return &Model{} }

func (*Model) Complete(context.Context, svc.ModelCall) (svc.ModelReply, error) {
	return svc.ModelReply{}, bridge.NotBuilt("model")
}
