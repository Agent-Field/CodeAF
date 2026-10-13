package caps

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service is the stub checker: it allows every call. B3 replaces this file
// with the app.json × role × record-rule check.
type Service struct{}

var _ svc.Caps = (*Service)(nil)

func New(svc.Config, *svc.Services) *Service { return &Service{} }

func (*Service) Allow(context.Context, svc.Scope, string, json.RawMessage) error { return nil }
