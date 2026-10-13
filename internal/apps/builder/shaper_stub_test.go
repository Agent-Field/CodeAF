package builder

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestShaperStub(t *testing.T) {
	_, err := NewShaper(svc.Config{}, &svc.Services{}).Shape(context.Background(), svc.ShapeInput{})
	typed, ok := err.(*bridge.Error)
	if !ok || typed.Code != bridge.CodeNotBuilt || typed.Message != "shaper is not built yet" {
		t.Fatalf("error = %v", err)
	}
}
