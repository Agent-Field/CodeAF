package builder

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Shaper struct{}

var _ svc.Shaper = (*Shaper)(nil)

func NewShaper(svc.Config, *svc.Services) *Shaper { return &Shaper{} }
func (*Shaper) Shape(context.Context, svc.ShapeInput) (svc.ShapeOutput, error) {
	return svc.ShapeOutput{}, bridge.NotBuilt("shaper")
}
