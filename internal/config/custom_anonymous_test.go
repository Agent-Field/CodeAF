package config

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
)

func TestAnonymousCustomConnectionSurvivesReloadWithoutWeakeningVendors(t *testing.T) {
	server := sourcestub.New("local-model")
	defer server.Close()
	dir := t.TempDir()
	row := PrepareCustomSource(dir, server.URL(), "local")
	row.KeyOptional = true
	outcome, err := ConnectService(context.Background(), dir, row, vendoredSource(t, modelsource.CustomID), nil)
	if err != nil || outcome.Kind != modelsource.OutcomeConnected {
		t.Fatalf("connect = %+v, %v", outcome, err)
	}
	connected, ok := ResolveSources(dir, "", DefaultBaseURL).ByID(row.ID)
	if !ok || !connected.Source.KeyOptional || connected.Key != "" {
		t.Fatal("anonymous custom connection was not retained")
	}
	t.Setenv("ZHIPU_API_KEY", "")
	outcome, err = ConnectService(context.Background(), t.TempDir(), PersistedSource{ID: "z-ai", Written: "z-ai", KeyOptional: true}, vendoredSource(t, "z-ai"), nil)
	if err != nil || outcome.Kind != modelsource.OutcomeWrongShape {
		t.Fatalf("vendor accepted anonymous override: %+v, %v", outcome, err)
	}
}

func TestAnonymousCustomFlagDoesNotChangeOlderRows(t *testing.T) {
	server := sourcestub.New("local-model")
	defer server.Close()
	dir := t.TempDir()
	row := PrepareCustomSource(dir, server.URL(), "local")
	source := vendoredSource(t, modelsource.CustomID)
	outcome, err := ConnectService(context.Background(), dir, row, source, nil)
	if err != nil || outcome.Kind != modelsource.OutcomeWrongShape || len(PersistedSources(dir)) != 0 {
		t.Fatal("old row implicitly became key optional")
	}
	row.Key = "existing-key"
	if err := WriteSources(dir, []PersistedSource{row}); err != nil {
		t.Fatal(err)
	}
	connected, ok := ResolveSources(dir, "", DefaultBaseURL).ByID(row.ID)
	if !ok || connected.Source.KeyOptional || connected.Key != row.Key {
		t.Fatal("legacy authenticated row changed on reload")
	}
}

func TestAnonymousCustomConnectionDoesNotSaveAnAuthorizationRefusal(t *testing.T) {
	server := sourcestub.New("local-model")
	defer server.Close()
	server.Refuse(401, "key required")
	dir := t.TempDir()
	row := PrepareCustomSource(dir, server.URL(), "local")
	row.KeyOptional = true
	outcome, err := ConnectService(context.Background(), dir, row, vendoredSource(t, modelsource.CustomID), nil)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind == modelsource.OutcomeConnected || len(PersistedSources(dir)) != 0 {
		t.Fatal("failed anonymous check persisted a connection")
	}
}
