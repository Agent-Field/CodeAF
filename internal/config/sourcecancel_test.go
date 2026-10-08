package config

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestCancelledConnectionsCannotSaveCredentials(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dir := t.TempDir()
	src := modelsource.Source{ID: "custom", Written: "local", KeyOptional: true}
	_, err := ConnectService(ctx, dir, PersistedSource{ID: "custom", Written: "local", Address: "http://localhost:1", KeyOptional: true}, src, nil)
	if !errors.Is(err, context.Canceled) || len(PersistedSources(dir)) != 0 {
		t.Fatalf("cancelled service persisted: %v", err)
	}
	_, err = ConnectCodex(ctx, dir, codexauth.Tokens{AccessToken: "cancelled-secret", RefreshToken: "cancelled-refresh"})
	if !errors.Is(err, context.Canceled) || codexauth.Connected(dir) || len(PersistedSources(dir)) != 0 {
		t.Fatalf("cancelled sign-in persisted: %v", err)
	}
}
