// Package prebuilt serves codeaf's Factory app. The definition is embedded
// and read-only: Open files shows those bytes, and archive, delete, copy and
// Shape are refused with the message "prebuilt".
package prebuilt

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service answers apps.prebuilt. The definition does not depend on config:
// Factory ships inside the binary.
type Service struct{}

var _ svc.Prebuilt = (*Service)(nil)

// New returns the prebuilt service. cfg and peers are unused because Factory
// is embedded, not stored under CODEAF_HOME.
func New(svc.Config, *svc.Services) *Service { return &Service{} }

// List returns Factory. Every caller receives the same app.
func (*Service) List(context.Context, svc.Caller) (bridge.PrebuiltResult, error) {
	b, err := bundle()
	if err != nil {
		return bridge.PrebuiltResult{}, bridge.Errorf(bridge.CodeInternal, err.Error())
	}
	app := b.App
	return bridge.PrebuiltResult{Apps: []bridge.PrebuiltApp{{
		ID:      app.ID,
		Name:    app.Name,
		Kind:    Kind,
		Version: app.Version,
		Space:   app.Space,
	}}}, nil
}
