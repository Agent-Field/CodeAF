package main

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
)

// THE FACTORY'S GITHUB TOKEN RIDES keys.set LIKE A PROVIDER'S KEY, and it is no
// provider: the factory reads it first, before any gh login, while no model
// source, provider event or profile row hears of it. An empty one is refused,
// so it can never stand as a removal that shuts out the gh login.
func TestAppKeysSetHoldsTheFactoryGitHubToken(t *testing.T) {
	backend, rec, profile := appProviderTest(t)
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	const token = "github_pat_dummy_not_a_real_token_0001"
	if err := backend.SetProvider(appwire.KeyParams{Provider: appwire.KeyGitHub}); err == nil {
		t.Fatal("an empty GitHub token was held")
	}
	if _, ok := backend.keys.Key(appwire.KeyGitHub); ok {
		t.Fatal("a refused token was held as a removal")
	}
	if err := backend.SetProvider(appwire.KeyParams{Provider: appwire.KeyGitHub, Key: token}); err != nil {
		t.Fatal(err)
	}
	if got, via := factorygithub.TokenAt(context.Background(), profile); got != token || via != factory.ViaToken {
		t.Fatalf("the factory reads %q via %q", got, via)
	}
	rec.mu.Lock()
	for _, event := range rec.events {
		if event == appwire.EventProviderChanged {
			t.Errorf("a GitHub token was announced as a provider")
		}
	}
	rec.mu.Unlock()
	if rows := config.PersistedSources(profile); len(rows) != 0 {
		t.Fatalf("a GitHub token became a model source: %+v", rows)
	}
	assertNoKeyOnDisk(t, profile, token)
}
